package wg

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestVehiclePageRequestsFieldsExplicitly(t *testing.T) {
	var query url.Values
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		w.Write(fixture(t, "encyclopedia-vehicles-page.json"))
	})

	if _, err := client.VehiclePage(context.Background(), 1, ""); err != nil {
		t.Fatalf("VehiclePage: %v", err)
	}

	// Without an explicit field list the API returns default_profile for every
	// vehicle, which is orders of magnitude more data than any question here
	// needs and would make storing responses verbatim impractical.
	fields := query.Get("fields")
	if fields == "" {
		t.Fatal("no fields parameter was sent")
	}
	for _, required := range []string{"tank_id", "tier", "type", "nation", "next_tanks", "prices_xp"} {
		if !strings.Contains(fields, required) {
			t.Errorf("fields %q is missing %q", fields, required)
		}
	}
	if strings.Contains(fields, "default_profile") {
		t.Error("fields asks for default_profile, which is the bulk this list exists to avoid")
	}
	if query.Get("limit") != "100" {
		t.Errorf("limit = %q, want the API maximum of 100", query.Get("limit"))
	}
	if query.Get("page_no") != "1" {
		t.Errorf("page_no = %q, want 1", query.Get("page_no"))
	}
}

func TestVehiclePageParsesMetaAndVehicles(t *testing.T) {
	client := newTestClient(t, serveFixture(t, "encyclopedia-vehicles-page.json"))

	page, err := client.VehiclePage(context.Background(), 1, "")
	if err != nil {
		t.Fatalf("VehiclePage: %v", err)
	}
	if page.PageTotal != 3 {
		t.Errorf("PageTotal = %d, want 3", page.PageTotal)
	}
	if page.Total != 9 {
		t.Errorf("Total = %d, want 9", page.Total)
	}
	if len(page.Vehicles) != 4 {
		t.Fatalf("parsed %d vehicles, want 4", len(page.Vehicles))
	}

	// ParseVehicles sorts by id, so this ordering is part of the contract: a
	// re-sync must write identical rows.
	if page.Vehicles[0].TankID >= page.Vehicles[1].TankID {
		t.Error("vehicles are not sorted by tank_id")
	}

	byName := map[string]Vehicle{}
	for _, v := range page.Vehicles {
		byName[v.Name] = v
	}

	executor := byName["Executor"]
	if executor.Tier != 11 {
		t.Errorf("Executor tier = %d, want 11", executor.Tier)
	}
	if executor.Type != "mediumTank" || executor.Nation != "uk" {
		t.Errorf("Executor = %+v", executor)
	}

	obj277 := byName["Object 277"]
	if obj277.ShortName != "Obj. 277" {
		t.Errorf("Object 277 short name = %q, want %q", obj277.ShortName, "Obj. 277")
	}
}

// TestEdgesAreDerivedFromBothDirections covers the reason both maps are read:
// next_tanks and prices_xp describe the same link seen from either end, and
// either can be absent for a given vehicle.
func TestEdgesAreDerivedFromBothDirections(t *testing.T) {
	client := newTestClient(t, serveFixture(t, "encyclopedia-vehicles-page.json"))

	page, err := client.VehiclePage(context.Background(), 1, "")
	if err != nil {
		t.Fatalf("VehiclePage: %v", err)
	}

	var concept, executor Vehicle
	for _, v := range page.Vehicles {
		switch v.Name {
		case "Concept No. 5":
			concept = v
		case "Executor":
			executor = v
		}
	}

	// Concept No. 5 has both: it unlocks the Executor and is itself unlocked.
	conceptEdges := concept.Edges()
	if len(conceptEdges) != 2 {
		t.Fatalf("Concept No. 5 produced %d edges, want 2: %+v", len(conceptEdges), conceptEdges)
	}
	var forward, backward bool
	for _, e := range conceptEdges {
		switch {
		case e.From == 19281 && e.To == 26705 && e.XPCost == 325000 && e.Source == EdgeSourceNextTanks:
			forward = true
		case e.From == 18209 && e.To == 19281 && e.XPCost == 220000 && e.Source == EdgeSourcePricesXP:
			backward = true
		}
	}
	if !forward {
		t.Error("missing the next_tanks edge to the Executor")
	}
	if !backward {
		t.Error("missing the prices_xp edge from the parent")
	}

	// The Executor is the top of its line: next_tanks is null, and only the
	// prices_xp side survives.
	executorEdges := executor.Edges()
	if len(executorEdges) != 1 {
		t.Fatalf("Executor produced %d edges, want 1: %+v", len(executorEdges), executorEdges)
	}
	if executorEdges[0].Source != EdgeSourcePricesXP || executorEdges[0].From != 19281 {
		t.Errorf("Executor edge = %+v", executorEdges[0])
	}
}

// TestPremiumVehiclesHaveNoEdges: a premium is bought, not researched, so null
// maps must produce nothing rather than a zero-cost edge.
func TestPremiumVehiclesHaveNoEdges(t *testing.T) {
	client := newTestClient(t, serveFixture(t, "encyclopedia-vehicles-premium.json"))

	page, err := client.VehiclePage(context.Background(), 1, "")
	if err != nil {
		t.Fatalf("VehiclePage: %v", err)
	}
	if len(page.Vehicles) != 1 {
		t.Fatalf("parsed %d vehicles, want 1", len(page.Vehicles))
	}

	v := page.Vehicles[0]
	if !v.IsPremium {
		t.Error("is_premium was not parsed")
	}
	if v.PriceGold != 8200 || v.PriceCredit != 0 {
		t.Errorf("prices = %d gold / %d credits", v.PriceGold, v.PriceCredit)
	}
	if edges := v.Edges(); len(edges) != 0 {
		t.Errorf("a premium vehicle produced edges: %+v", edges)
	}
}

func TestEdgesAreDeterministic(t *testing.T) {
	v := Vehicle{
		TankID:    100,
		NextTanks: map[string]int{"300": 3, "200": 2, "400": 4},
		PricesXP:  map[string]int{"50": 1, "60": 2},
	}

	// Map iteration order varies, so this would flake if Edges did not sort.
	first := v.Edges()
	for range 20 {
		got := v.Edges()
		for i := range got {
			if got[i] != first[i] {
				t.Fatalf("Edges is not deterministic:\n%+v\n%+v", first, got)
			}
		}
	}
}

func TestEdgesSkipUnparseableIDs(t *testing.T) {
	v := Vehicle{
		TankID:    100,
		NextTanks: map[string]int{"not-an-id": 5, "200": 2},
	}

	edges := v.Edges()
	if len(edges) != 1 || edges[0].To != 200 {
		t.Errorf("Edges = %+v, want only the parseable entry", edges)
	}
}

func TestParseVehiclesFallsBackToTheMapKey(t *testing.T) {
	// tank_id is absent from the body when it was not in the requested field
	// list, so the key is the more reliable source.
	vehicles, err := ParseVehicles([]byte(`{"19281":{"name":"Concept No. 5","tier":10}}`))
	if err != nil {
		t.Fatalf("ParseVehicles: %v", err)
	}
	if len(vehicles) != 1 {
		t.Fatalf("parsed %d vehicles, want 1", len(vehicles))
	}
	if vehicles[0].TankID != 19281 {
		t.Errorf("tank_id = %d, want it recovered from the key", vehicles[0].TankID)
	}
}

func TestParseVehiclesSkipsUnidentifiableEntries(t *testing.T) {
	vehicles, err := ParseVehicles([]byte(`{"not-an-id":{"name":"Mystery"},"19281":{"name":"Concept No. 5"}}`))
	if err != nil {
		t.Fatalf("ParseVehicles: %v", err)
	}
	if len(vehicles) != 1 || vehicles[0].TankID != 19281 {
		t.Errorf("ParseVehicles = %+v, want only the identifiable entry", vehicles)
	}
}

func TestParseVehiclesHandlesEmptyData(t *testing.T) {
	vehicles, err := ParseVehicles(nil)
	if err != nil {
		t.Fatalf("ParseVehicles(nil): %v", err)
	}
	if len(vehicles) != 0 {
		t.Errorf("ParseVehicles(nil) = %+v, want none", vehicles)
	}
}

func TestVehiclePageHandlesNotModified(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	})

	page, err := client.VehiclePage(context.Background(), 1, `W/"v1"`)
	if err != nil {
		t.Fatalf("VehiclePage on a 304 = %v, want nil", err)
	}
	if !page.Result.NotModified {
		t.Error("NotModified was not set")
	}
	if len(page.Vehicles) != 0 {
		t.Error("a 304 produced vehicles")
	}
}

func TestVehiclePagePropagatesAPIErrors(t *testing.T) {
	client := newTestClient(t, serveFixture(t, "error-invalid-application-id.json"))

	page, err := client.VehiclePage(context.Background(), 1, "")
	if err == nil {
		t.Fatal("VehiclePage = nil, want an error")
	}
	if !IsAppCredentialError(err) {
		t.Errorf("IsAppCredentialError = false for %v", err)
	}
	// The Result survives so the caller can record the failed attempt.
	if page.Result.WGError != msgInvalidApplicationID {
		t.Errorf("Result.WGError = %q, want it preserved", page.Result.WGError)
	}
}
