package app

import (
	"testing"

	"golang.org/x/sys/windows/registry"
)

// withTestKeys points the registry functions at a throwaway key, removed
// afterwards, so the test never touches the real PATH or the app's entries.
func withTestKeys(t *testing.T) string {
	t.Helper()
	const root = `Software\TankAdvisorTest`
	saved := []string{environmentKey, appKey, runKey, classesKey}
	environmentKey, appKey, runKey, classesKey = root+`\Environment`, root+`\App`, root+`\Run`, root+`\Classes`
	t.Cleanup(func() {
		deleteTree(root)
		environmentKey, appKey, runKey, classesKey = saved[0], saved[1], saved[2], saved[3]
	})
	return root
}

func TestPathIsAddedOnceAndRemoved(t *testing.T) {
	withTestKeys(t)
	k, _, _ := registry.CreateKey(registry.CURRENT_USER, environmentKey, registry.SET_VALUE)
	k.SetExpandStringValue("Path", `%USERPROFILE%\bin;C:\Tools`)
	k.Close()

	dir := `C:\Users\x\AppData\Local\Programs\Tank Advisor`
	AddToUserPath(dir)
	AddToUserPath(dir + `\`)
	parts, _ := userPath()
	if len(parts) != 3 || parts[2] != dir {
		t.Fatalf("PATH = %v", parts)
	}
	RemoveFromUserPath(dir)
	if parts, _ = userPath(); len(parts) != 2 || parts[0] != `%USERPROFILE%\bin` {
		t.Errorf("PATH = %v", parts)
	}
}

func TestRemoveRegistrationTakesEverythingOut(t *testing.T) {
	withTestKeys(t)
	RecordInstallDir(`C:\TA`)
	AddToUserPath(`C:\TA`)
	run, _, _ := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	run.SetStringValue(autostartValue, `"C:\TA\TankAdvisor.exe" --hidden`)
	run.SetStringValue("someone-else", "keep me")
	run.Close()
	aumid, _, _ := registry.CreateKey(registry.CURRENT_USER, classesKey+`\AppUserModelId\`+notifierName, registry.SET_VALUE)
	aumid.SetStringValue("CustomActivator", "{1234}")
	aumid.Close()
	clsid, _, _ := registry.CreateKey(registry.CURRENT_USER, classesKey+`\CLSID\{1234}\LocalServer32`, registry.SET_VALUE)
	clsid.Close()

	if errs := RemoveRegistration(`C:\TA`); len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	for _, gone := range []string{appKey, classesKey + `\AppUserModelId\` + notifierName, classesKey + `\CLSID\{1234}`} {
		if k, err := registry.OpenKey(registry.CURRENT_USER, gone, registry.QUERY_VALUE); err == nil {
			k.Close()
			t.Errorf("%s is still there", gone)
		}
	}
	run, _ = registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	defer run.Close()
	if _, _, err := run.GetStringValue(autostartValue); err == nil {
		t.Error("the autostart entry is still there")
	}
	if v, _, _ := run.GetStringValue("someone-else"); v != "keep me" {
		t.Error("another program's autostart entry went too")
	}
	if parts, _ := userPath(); len(parts) != 0 {
		t.Errorf("PATH = %v", parts)
	}
}
