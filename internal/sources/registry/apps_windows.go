package registry

import (
	"errors"
	"sort"

	"golang.org/x/sys/windows/registry"
)

const uninstallPath = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`

// Installed lists installed programs from the 64-bit and 32-bit machine
// views and the current user's view. Updates and hidden system components
// are left out, as Settings > Apps does.
func Installed() ([]App, error) {
	views := []struct {
		root   registry.Key
		access uint32
	}{
		{registry.LOCAL_MACHINE, registry.WOW64_64KEY},
		{registry.LOCAL_MACHINE, registry.WOW64_32KEY},
		{registry.CURRENT_USER, 0},
	}
	seen := map[string]bool{}
	var apps []App
	var errs []error
	for _, v := range views {
		list, err := readView(v.root, v.access)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, a := range list {
			key := a.Name + "|" + a.Version + "|" + a.InstallDir
			if !seen[key] {
				seen[key] = true
				apps = append(apps, a)
			}
		}
	}
	if len(apps) == 0 && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].Name < apps[j].Name })
	return apps, nil
}

func readView(root registry.Key, view uint32) ([]App, error) {
	k, err := registry.OpenKey(root, uninstallPath, registry.READ|view)
	if err != nil {
		return nil, err
	}
	defer k.Close()
	names, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return nil, err
	}

	var apps []App
	for _, name := range names {
		sk, err := registry.OpenKey(k, name, registry.READ|view)
		if err != nil {
			continue // unreadable entry: skip it
		}
		a, ok := readApp(sk)
		sk.Close()
		if ok {
			apps = append(apps, a)
		}
	}
	return apps, nil
}

func readApp(k registry.Key) (App, bool) {
	str := func(name string) string {
		s, _, _ := k.GetStringValue(name)
		return s
	}
	num := func(name string) uint64 {
		n, _, _ := k.GetIntegerValue(name)
		return n
	}

	a := App{
		Name:            str("DisplayName"),
		Publisher:       str("Publisher"),
		Version:         str("DisplayVersion"),
		UninstallString: str("UninstallString"),
		InstallDir:      installDir(str("InstallLocation"), str("DisplayIcon")),
		EstimatedSize:   int64(num("EstimatedSize")) * 1024, // stored in KiB
	}
	// DisplayIcon is often REG_EXPAND_SZ with %variables%.
	icon := str("DisplayIcon")
	if exp, err := registry.ExpandString(icon); err == nil {
		icon = exp
	}
	a.IconPath, a.IconIndex = parseIcon(icon)
	hidden := num("SystemComponent") == 1 || str("ParentKeyName") != "" || str("ReleaseType") != ""
	return a, a.Name != "" && !hidden
}
