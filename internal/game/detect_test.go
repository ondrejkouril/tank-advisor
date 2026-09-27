package game

import (
	"os"
	"path/filepath"
	"testing"
)

// writeClient lays out the three files detection and ModsDir read, in the
// shapes measured on the owner's machine (docs/plan.md step D1).
func writeClient(t *testing.T, dir, id, version, modsRel string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"game_info.xml": `<?xml version="1.0" encoding="utf-8"?>
<protocol name="game_info" version="2.16" wgc_publisher_id="wargaming">
    <game>
        <id>` + id + `</id>
        <content_localizations><content_localization realm="eu">en</content_localization></content_localizations>
    </game>
</protocol>`,
		"version.xml": "<version.xml>\r\n\t<version>\t" + version + "\t</version>\r\n</version.xml>",
		"paths.xml": `<root>
  <Paths>
    <Path cacheSubdirs="true">./res_mods/x</Path>
    <Path mask="*.wotmod" mode="recursive" root="res">./` + modsRel + `</Path>
    <Packages><Package type="sd,hd">./res/packages/shaders.pkg</Package></Packages>
  </Paths>
</root>`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFindUsesGameCenterAndSkipsTheCommonTest(t *testing.T) {
	root := t.TempDir()
	eu := filepath.Join(root, "World_of_Tanks_EU")
	ct := filepath.Join(root, "World_of_Tanks_CT")
	writeClient(t, eu, "WOT.EU.PRODUCTION", "v.2.4.0.1 #952", "mods/2.4.0.1")
	writeClient(t, ct, "WOT.CT.PRODUCTION", "v.2.4.1.0 Common Test #954", "mods/2.4.1.0 Common Test")

	prefs := filepath.Join(root, "preferences.xml")
	os.WriteFile(prefs, []byte(`<?xml version="1.0" encoding="utf-8"?>
<protocol name="preferences" version="3.34">
    <application>
        <games_manager>
            <games>
                <game><working_dir>`+ct+`</working_dir></game>
                <game><working_dir>`+eu+`</working_dir></game>
            </games>
            <selectedGames><WOT>`+eu+`</WOT></selectedGames>
        </games_manager>
    </application>
</protocol>`), 0o644)

	got := Locator{GameCenterPrefs: prefs}.Find("eu")
	if len(got) != 1 {
		t.Fatalf("Find = %+v, want only the EU client", got)
	}
	if got[0].Dir != eu || got[0].Realm != "eu" || got[0].Source != "game-center" {
		t.Errorf("Find = %+v", got[0])
	}
	if got := (Locator{GameCenterPrefs: prefs}).Find("com"); len(got) != 0 {
		t.Errorf("Find(com) = %+v, want nothing", got)
	}
}

func TestFindFallsBackToTheDefaultFolder(t *testing.T) {
	root := t.TempDir()
	eu := filepath.Join(root, "World_of_Tanks_EU")
	writeClient(t, eu, "WOT.EU.PRODUCTION", "v.2.4.0.1 #952", "mods/2.4.0.1")

	got := Locator{
		GameCenterPrefs: filepath.Join(root, "missing.xml"),
		Defaults:        []string{filepath.Join(root, "World_of_Tanks*")},
	}.Find("eu")
	if len(got) != 1 || got[0].Source != "default" {
		t.Fatalf("Find = %+v", got)
	}
}

func TestFindReadsSteamLibraries(t *testing.T) {
	root := t.TempDir()
	steam := filepath.Join(root, "Steam")
	lib := filepath.Join(root, "Games")
	client := filepath.Join(lib, "steamapps", "common", "World of Tanks", "eu")
	writeClient(t, client, "WOT.EU.PRODUCTION", "v.2.4.0.1 #952", "mods/2.4.0.1")
	os.MkdirAll(filepath.Join(steam, "steamapps"), 0o755)
	os.WriteFile(filepath.Join(steam, "steamapps", "libraryfolders.vdf"),
		[]byte("\"libraryfolders\"\n{\n\t\"1\"\n\t{\n\t\t\"path\"\t\t\""+filepath.ToSlash(lib)+"\"\n\t}\n}\n"), 0o644)

	got := Locator{SteamRoots: []string{steam}}.Find("eu")
	if len(got) != 1 || got[0].Source != "steam" {
		t.Fatalf("Find = %+v", got)
	}
}

func TestModsDirReadsPathsXML(t *testing.T) {
	ct := filepath.Join(t.TempDir(), "CT")
	writeClient(t, ct, "WOT.CT.PRODUCTION", "v.2.4.1.0 Common Test #954", "mods/2.4.1.0 Common Test")
	got, err := ModsDir(ct)
	if err != nil {
		t.Fatalf("ModsDir: %v", err)
	}
	// version.xml alone would say mods/2.4.1.0, which the client never reads.
	if want := filepath.Join(ct, "mods", "2.4.1.0 Common Test"); got != want {
		t.Errorf("ModsDir = %q, want %q", got, want)
	}
}

func TestRealmOfID(t *testing.T) {
	for id, want := range map[string]string{
		"WOT.EU.PRODUCTION":   "eu",
		"WOT.NA.PRODUCTION":   "com",
		"WOT.ASIA.PRODUCTION": "asia",
		"WOT.CT.PRODUCTION":   "",
		"WOWS.EU.PRODUCTION":  "",
		"":                    "",
	} {
		if got := RealmOfID(id); got != want {
			t.Errorf("RealmOfID(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestFindTakesTheFolderOfARunningClient(t *testing.T) {
	root := t.TempDir()
	odd := filepath.Join(root, "Somewhere", "Tanks")
	writeClient(t, odd, "WOT.EU.PRODUCTION", "v.2.4.0.1 #952", "mods/2.4.0.1")

	got := Locator{
		GameCenterPrefs: filepath.Join(root, "missing.xml"),
		Running:         func() []string { return []string{filepath.Join(odd, "win64", "WorldOfTanks.exe")} },
	}.Find("eu")
	if len(got) != 1 || got[0].Source != "running" || got[0].Dir != odd {
		t.Fatalf("Find = %+v", got)
	}
}

func TestClientDir(t *testing.T) {
	for exe, want := range map[string]string{
		filepath.Join("C:", "G", "win64", "WorldOfTanks.exe"): filepath.Join("C:", "G"),
		filepath.Join("C:", "G", "WIN32", "WorldOfTanks.exe"): filepath.Join("C:", "G"),
		filepath.Join("C:", "G", "WorldOfTanks.exe"):          filepath.Join("C:", "G"),
	} {
		if got := ClientDir(exe); got != want {
			t.Errorf("ClientDir(%s) = %s, want %s", exe, got, want)
		}
	}
}
