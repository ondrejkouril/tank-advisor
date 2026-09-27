package app

// About carries the notices Wargaming's API Policy requires of an application
// (docs/spec-desktop.md section 12.2). The installer's licence page and the
// README carry the same text.
type About struct {
	Notices []string `json:"notices"`
	Links   []Link   `json:"links"`
	// Support is Wargaming's Customer Support Center, shown as its own,
	// clearly marked button (Policy section 1.10).
	Support Link `json:"support"`
}

// Link is a labelled URL.
type Link struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// gameSites are the official World of Tanks websites, by realm (Policy
// section 1.9).
var gameSites = map[string]string{
	"eu":   "https://worldoftanks.eu/",
	"com":  "https://worldoftanks.com/",
	"asia": "https://worldoftanks.asia/",
}

// supportURL is Wargaming's Customer Support Center. The per-realm addresses
// all lead to it.
const supportURL = "https://wargaming.net/support/"

func aboutFor(realm string) About {
	site, ok := gameSites[realm]
	if !ok {
		site = gameSites["eu"]
	}
	return About{
		Notices: []string{
			"Tank Advisor, a companion app for World of Tanks. © 2026 ondrejkouril. Free software under the MIT licence.",
			"Tank Advisor is not affiliated with or endorsed by Wargaming.",
			"Account data comes from Wargaming.net through its public API. © Wargaming.net. All rights reserved.",
			"WN8 expected values come from XVM. Marks of Excellence thresholds come from tomato.gg.",
			"Everything Tank Advisor fetches stays on this computer. When you ask Claude about your account, Claude receives the figures it needs to answer.",
		},
		Links: []Link{
			{Label: "Official World of Tanks website", URL: site},
			{Label: "Source code and issues", URL: "https://github.com/" + repository},
		},
		Support: Link{Label: "Wargaming Support", URL: supportURL},
	}
}
