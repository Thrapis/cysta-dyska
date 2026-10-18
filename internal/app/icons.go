package app

import "strings"

// AppIcons returns a PNG data URI for each owner name that is an installed
// program with a readable icon. Owners without one are left out, so the UI
// shows its placeholder. The map is never nil.
func (a *App) AppIcons(owners []string) map[string]string {
	out := map[string]string{}
	apps, _ := a.installedApps() // on error the list is empty: no icons, no failure
	if len(apps) == 0 {
		return out
	}
	byName := make(map[string]int, len(apps))
	for i, p := range apps {
		if p.IconPath == "" {
			continue
		}
		if _, dup := byName[strings.ToLower(p.Name)]; !dup {
			byName[strings.ToLower(p.Name)] = i
		}
	}
	for _, owner := range owners {
		i, ok := byName[strings.ToLower(owner)]
		if !ok {
			continue
		}
		if uri, err := a.appIcon(apps[i].IconPath, apps[i].IconIndex); err == nil {
			out[owner] = uri
		}
	}
	return out
}
