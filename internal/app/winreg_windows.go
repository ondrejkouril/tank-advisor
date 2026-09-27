package app

import (
	"errors"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Where the app records itself in the user's registry. Tests point these at a
// throwaway key.
var (
	environmentKey = `Environment`
	appKey         = `Software\Tank Advisor`
	runKey         = `Software\Microsoft\Windows\CurrentVersion\Run`
	classesKey     = `Software\Classes`
)

// autostartValue is the name Wails gives the app's Run entry: its name,
// slugged.
const autostartValue = "tank-advisor"

// notifierName is the name Wails' notification service registers under.
const notifierName = "Tank Advisor"

// AddToUserPath puts dir on the user's PATH, so wotctx runs from any
// terminal and the Claude Code plugin finds it (section 4). A new process
// sees it; running ones keep their PATH.
func AddToUserPath(dir string) error {
	parts, err := userPath()
	if err != nil {
		return err
	}
	for _, p := range parts {
		if strings.EqualFold(strings.TrimRight(p, `\`), strings.TrimRight(dir, `\`)) {
			return nil
		}
	}
	return setUserPath(append(parts, dir))
}

// RemoveFromUserPath takes dir off the user's PATH.
func RemoveFromUserPath(dir string) error {
	parts, err := userPath()
	if err != nil {
		return err
	}
	var keep []string
	for _, p := range parts {
		if !strings.EqualFold(strings.TrimRight(p, `\`), strings.TrimRight(dir, `\`)) {
			keep = append(keep, p)
		}
	}
	if len(keep) == len(parts) {
		return nil
	}
	return setUserPath(keep)
}

func userPath() ([]string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, environmentKey, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer k.Close()
	v, _, err := k.GetStringValue("Path")
	if errors.Is(err, registry.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var parts []string
	for _, p := range strings.Split(v, ";") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts, nil
}

func setUserPath(parts []string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, environmentKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	// Expandable, as Windows writes it: entries may use %USERPROFILE% and
	// the like.
	if err := k.SetExpandStringValue("Path", strings.Join(parts, ";")); err != nil {
		return err
	}
	broadcastEnvironmentChange()
	return nil
}

// broadcastEnvironmentChange tells Explorer the environment changed, so a
// terminal started from it sees the new PATH.
func broadcastEnvironmentChange() {
	env, _ := windows.UTF16PtrFromString("Environment")
	const hwndBroadcast, wmSettingChange, smtoAbortIfHung = 0xffff, 0x001A, 0x0002
	var result uintptr
	windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW").Call(
		hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(env)), smtoAbortIfHung, 2000, uintptr(unsafe.Pointer(&result)))
}

// RecordInstallDir writes where the app is, for the Claude Desktop launcher
// (cmd/wotctx-launcher reads HKCU\Software\Tank Advisor\InstallDir).
func RecordInstallDir(dir string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, appKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue("InstallDir", dir)
}

// RemoveRegistration takes out every registry entry the app wrote, apart from
// the uninstaller's own, which the installer removes: the PATH entry, the
// autostart entry, the notification registration and HKCU\Software\Tank
// Advisor (the install folder and notification categories).
func RemoveRegistration(dir string) []error {
	var errs []error
	if err := RemoveFromUserPath(dir); err != nil {
		errs = append(errs, err)
	}
	if k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE); err == nil {
		if err := k.DeleteValue(autostartValue); err != nil && !errors.Is(err, registry.ErrNotExist) {
			errs = append(errs, err)
		}
		k.Close()
	}
	// The notification service's activator: its CLSID, found through the
	// AppUserModelId entry.
	aumid := classesKey + `\AppUserModelId\` + notifierName
	if k, err := registry.OpenKey(registry.CURRENT_USER, aumid, registry.QUERY_VALUE); err == nil {
		if guid, _, err := k.GetStringValue("CustomActivator"); err == nil && strings.HasPrefix(guid, "{") {
			k.Close()
			if err := deleteTree(classesKey + `\CLSID\` + guid); err != nil {
				errs = append(errs, err)
			}
		} else {
			k.Close()
		}
	}
	for _, key := range []string{aumid, appKey} {
		if err := deleteTree(key); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// deleteTree deletes a key under HKCU with everything below it.
func deleteTree(path string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	subs, err := k.ReadSubKeyNames(-1)
	k.Close()
	if err != nil {
		return err
	}
	for _, s := range subs {
		if err := deleteTree(path + `\` + s); err != nil {
			return err
		}
	}
	err = registry.DeleteKey(registry.CURRENT_USER, path)
	if errors.Is(err, registry.ErrNotExist) || errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return nil
	}
	return err
}
