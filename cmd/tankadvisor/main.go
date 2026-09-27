// Command tankadvisor is the Tank Advisor app: a tray icon and a small window
// over the same core as wotctx (docs/spec-desktop.md section 5). The window is
// internal/app on screen; this file only wires it to Wails.
//
// Wails and its dependencies stay in this command, so wotctx's own
// dependency list does not change (docs/spec-desktop.md section 3).
package main

import (
	"context"
	"embed"
	"flag"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"github.com/ondrejkouril/tank-advisor/internal/app"
	"github.com/ondrejkouril/tank-advisor/internal/game"
)

// version is set by the linker; see the Makefile.
var version = "dev"

//go:embed frontend
var frontend embed.FS

//go:embed icon.png
var icon []byte

// periodicEvery is how often the clock-driven duties look whether anything
// is due: the daily sync, the login's renewal, the update check.
const periodicEvery = time.Hour

// gamePollEvery is how often the app looks for a running game.
const gamePollEvery = 30 * time.Second

// every runs fn after first, and then at each interval, for as long as the
// app runs.
func every(interval, first time.Duration, fn func(context.Context)) {
	time.Sleep(first)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		fn(context.Background())
		<-ticker.C
	}
}

// watchGame calls exited each time World of Tanks exits. It looks for the
// game every half minute, and while it runs, waits on its process.
func watchGame(exited func(context.Context)) {
	ctx := context.Background()
	for {
		running := game.RunningClients()
		if len(running) == 0 {
			time.Sleep(gamePollEvery)
			continue
		}
		for _, p := range running {
			game.WaitForExit(ctx, p.PID)
		}
		exited(ctx)
	}
}

// notify shows a notice as a Windows notification, with its action as a
// button.
func notify(n *notifications.NotificationService, notice app.Notice) {
	opts := notifications.NotificationOptions{ID: notice.ID, Title: notice.Title, Body: notice.Body}
	if notice.Action == "" {
		n.SendNotification(opts)
		return
	}
	category := "tankadvisor-" + notice.Action
	n.RegisterNotificationCategory(notifications.NotificationCategory{
		ID:      category,
		Actions: []notifications.NotificationAction{{ID: notice.Action, Title: notice.ActionLabel}},
	})
	opts.CategoryID = category
	n.SendNotificationWithActions(opts)
}

// modUpkeepEvery is how often the app checks that the mod is in the game's
// current mods folder. Game Center updates the game while it is closed, so a
// few minutes is soon enough to beat the next start.
const modUpkeepEvery = 3 * time.Minute

// statusChanged tells the window to reload its rows after something happened
// outside it, such as a sync from the tray.
const statusChanged = "status-changed"

func main() {
	hidden := flag.Bool("hidden", false, "start in the tray without opening the window (used when starting with Windows)")
	installMod := flag.String("install-mod", "", "copy this mod package into the game folder and exit (run with administrator permission by the app itself)")
	gameDir := flag.String("game-dir", "", "the game folder for --install-mod")
	installPayloads := flag.Bool("install-payloads", false, "write out what this executable carries, add its folder to PATH, and exit (run by the installer)")
	uninstall := flag.Bool("uninstall", false, "take out the mod, the registry entries and the browser cache, and exit (run by the uninstaller)")
	deleteData := flag.Bool("delete-data", false, "with --uninstall: delete the account's data too")
	quit := flag.Bool("quit", false, "ask a running Tank Advisor to quit (run by the installer before it replaces files)")
	flag.Parse()

	// The elevated copy is a separate, windowless run of this executable. It
	// must return before the single-instance check, which would hand it to
	// the running app instead.
	if *installMod != "" {
		os.Exit(app.ElevatedModInstall(context.Background(), version, *installMod, *gameDir))
	}
	// So are the installer's and uninstaller's runs.
	if *installPayloads {
		os.Exit(runInstallPayloads())
	}
	if *uninstall {
		os.Exit(runUninstall(*deleteData))
	}

	// A normal start writes out any payload an update changed.
	if release, _ := payloadsCarried(); release {
		setUpPayloads()
	}

	assets, err := fs.Sub(frontend, "frontend")
	if err != nil {
		log.Fatal(err)
	}

	h := &host{}
	notifier := notifications.New()
	svc := app.New(app.Options{Version: version, PayloadDir: payloadDir(), Host: h})

	var win *application.WebviewWindow
	show := func() {
		if win != nil {
			win.Show()
			win.Focus()
		}
	}

	var wails *application.App
	wails = application.New(application.Options{
		Name:        "Tank Advisor",
		Description: "A companion app for World of Tanks that lets Claude read your own account data",
		Icon:        icon,
		Services:    []application.Service{application.NewService(svc), application.NewService(notifier)},
		Assets:      application.AssetOptions{Handler: application.BundledAssetFileServer(assets)},
		Windows:     application.WindowsOptions{WebviewUserDataPath: webviewDataDir()},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "io.github.ondrejkouril.tankadvisor",
			// A second start, from the Start menu say, opens the window of
			// the one already in the tray.
			OnSecondInstanceLaunch: func(d application.SecondInstanceData) {
				if slices.Contains(d.Args, "--quit") || slices.Contains(d.Args, "-quit") {
					wails.Quit()
					return
				}
				show()
			},
		},
	})
	h.app = wails
	if *quit {
		// Reaching here means no other copy was running: nothing to quit.
		os.Exit(0)
	}
	if su, err := newSelfUpdate(wails.Updater, version); err != nil {
		log.Printf("self-update is off: %v", err)
	} else if su != nil {
		svc.SetUpdates(su)
	}

	win = wails.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      "main",
		Title:     "Tank Advisor",
		Width:     780,
		Height:    760,
		MinWidth:  560,
		MinHeight: 480,
		URL:       "/",
		// Starting with Windows opens only the tray, unless setup is not
		// done: then the window opens on it.
		Hidden: *hidden && !svc.NeedsSetup(),
		// The window has its own look; the page draws the background.
		BackgroundColour: application.NewRGB(0xf6, 0xf7, 0xf9),
	})
	// Closing the window leaves the app in the tray, where its background
	// duties run (docs/spec-desktop.md section 5.6). Quit is in the tray menu.
	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		win.Hide()
		e.Cancel()
	})

	tray := wails.SystemTray.New()
	tray.SetIcon(icon)
	tray.SetTooltip("Tank Advisor")
	menu := wails.NewMenu()
	menu.Add("Open Tank Advisor").OnClick(func(*application.Context) { show() })
	menu.Add("Sync now").OnClick(func(*application.Context) {
		go func() {
			wails.Event.Emit(statusChanged)
			r := svc.Do(context.Background(), "sync", "")
			wails.Event.Emit(statusChanged, r)
		}()
	})
	menu.AddSeparator()
	menu.Add("Quit").OnClick(func(*application.Context) { wails.Quit() })
	tray.SetMenu(menu)
	tray.OnClick(show)

	// The background duties (docs/spec-desktop.md section 5.6). Idle, they
	// cost a process-list check every half minute and a few file reads every
	// few minutes; while the game runs, a wait on its process handle.
	duties := svc.NewDuties(app.DutyHooks{
		Notify:  func(n app.Notice) { notify(notifier, n) },
		Changed: func(r app.Result) { wails.Event.Emit(statusChanged, r) },
	})
	notifier.OnNotificationResponse(func(r notifications.NotificationResult) {
		show()
		if r.Error == nil && r.Response.ActionIdentifier != "" && r.Response.ActionIdentifier != notifications.DefaultActionIdentifier {
			go func() {
				wails.Event.Emit(statusChanged)
				wails.Event.Emit(statusChanged, svc.Do(context.Background(), r.Response.ActionIdentifier, ""))
			}()
		}
	})
	go every(modUpkeepEvery, 0, func(ctx context.Context) { duties.Mod(ctx) })
	go every(periodicEvery, time.Minute, func(ctx context.Context) { duties.Periodic(ctx) })
	go watchGame(func(ctx context.Context) { duties.AfterGame(ctx) })

	if err := wails.Run(); err != nil {
		log.Fatal(err)
	}
}

// payloadDir is the folder beside the executable, where the installer puts
// the mod package and the Claude Desktop bundle.
func payloadDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe)
}

// webviewDataDir is WebView2's own cache. Wails would put it in the roaming
// profile under the executable's name; a browser cache belongs in the local
// one, beside nothing of wotctx's, so the uninstaller can remove it whole.
func webviewDataDir() string {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		return ""
	}
	return filepath.Join(local, "Tank Advisor", "WebView2")
}

// host gives internal/app the desktop: the browser, Explorer and the
// start-with-Windows entry.
type host struct {
	app *application.App
}

func (h *host) OpenURL(url string) error { return h.app.Browser.OpenURL(url) }

func (h *host) ShowInFolder(path string) error { return showInFolder(path) }

func (h *host) Autostart() (bool, error) { return h.app.Autostart.IsEnabled() }

// ChooseFolder shows Windows' folder picker.
func (h *host) ChooseFolder(title, start string) (string, error) {
	return h.app.Dialog.OpenFile().
		CanChooseDirectories(true).
		CanChooseFiles(false).
		SetTitle(title).
		SetDirectory(start).
		PromptForSingleSelection()
}

func (h *host) InstallModElevated(pkg, gameDir string) error { return installModElevated(pkg, gameDir) }

// SetAutostart registers the app under HKCU\...\Run, started with --hidden so
// that logging on puts it in the tray rather than on screen.
func (h *host) SetAutostart(on bool) error {
	if on {
		return h.app.Autostart.EnableWithOptions(application.AutostartOptions{Arguments: []string{"--hidden"}})
	}
	return h.app.Autostart.Disable()
}
