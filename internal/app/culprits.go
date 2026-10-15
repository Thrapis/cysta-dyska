package app

import (
	"errors"
	"strings"
	"sync"

	"cysta-dyska/internal/actions"
	"cysta-dyska/internal/attribute"
	"cysta-dyska/internal/scan"
	"cysta-dyska/internal/sources/locations"
)

// maxCulprits caps how many findings are sent to the UI. Totals still
// cover all of them.
const maxCulprits = 2000

// CulpritDTO is one explained folder or file.
type CulpritDTO struct {
	RuleID     string                 `json:"ruleId"`
	Owner      string                 `json:"owner"`
	Category   string                 `json:"category"`
	Risk       string                 `json:"risk"`
	Path       string                 `json:"path"`
	Alloc      int64                  `json:"alloc"` // excluding nested findings
	Unreadable bool                   `json:"unreadable"`
	Explain    map[string]string      `json:"explain"` // locale -> text
	Suggest    []attribute.Suggestion `json:"suggest"`
	Actions    []ActionOptionDTO      `json:"actions"` // what the app can do here
}

// CulpritsDTO is the result of Culprits.
type CulpritsDTO struct {
	Items     []CulpritDTO `json:"items"`
	Omitted   int          `json:"omitted"`   // findings beyond maxCulprits
	Explained int64        `json:"explained"` // on-disk bytes covered by all findings
	ScanAlloc int64        `json:"scanAlloc"` // on-disk bytes of the whole scan
	Warning   string       `json:"warning,omitempty"`
}

var (
	packOnce sync.Once
	pack     *attribute.Pack
	packErr  error
)

func loadPack() (*attribute.Pack, error) {
	packOnce.Do(func() { pack, packErr = attribute.LoadEmbedded() })
	return pack, packErr
}

// attribute explains a finished scan. It never fails the scan: problems
// are reported as a warning alongside whatever could be explained.
// It also returns the findings behind the items (same order) and the
// machine's folders, which actions need.
func (a *App) attribute(tree *scan.Node) (CulpritsDTO, []attribute.Finding, locations.Vars) {
	res := CulpritsDTO{ScanAlloc: tree.Alloc, Items: []CulpritDTO{}}
	vars := locations.Resolve(a.getenv)
	p, err := loadPack()
	if err != nil {
		res.Warning = "rule pack: " + err.Error()
		return res, nil, vars
	}
	apps, err := a.installedApps()
	if err != nil {
		res.Warning = "installed programs: " + err.Error()
	}

	found := attribute.Match(tree, p, vars, apps)
	for i, f := range found {
		res.Explained += f.Alloc
		if i >= maxCulprits {
			res.Omitted++
			continue
		}
		res.Items = append(res.Items, toCulprit(f))
	}
	if len(found) > maxCulprits {
		found = found[:maxCulprits]
	}
	return res, found, vars
}

func toCulprit(f attribute.Finding) CulpritDTO {
	fill := func(t attribute.Text) map[string]string {
		out := make(map[string]string, len(t))
		for l, s := range t {
			out[l] = strings.ReplaceAll(s, "{owner}", f.Owner)
		}
		return out
	}
	sugg := make([]attribute.Suggestion, len(f.Rule.Suggest))
	for i, s := range f.Rule.Suggest {
		sugg[i] = attribute.Suggestion{Text: fill(s.Text), Command: s.Command, Settings: s.Settings, Run: s.Run}
	}
	return CulpritDTO{
		RuleID: f.Rule.ID, Owner: f.Owner,
		Category: string(f.Rule.Category), Risk: string(f.Rule.Risk),
		Path: f.Node.Path(), Alloc: f.Alloc, Unreadable: f.Unreadable(),
		Explain: fill(f.Rule.Explain), Suggest: sugg,
	}
}

// Culprits returns what explains the space in the last completed scan,
// with the actions currently available for each finding.
func (a *App) Culprits() (CulpritsDTO, error) {
	a.mu.Lock()
	if a.culprits == nil {
		a.mu.Unlock()
		return CulpritsDTO{}, errors.New("no scan results yet")
	}
	res := *a.culprits
	findings, vars := a.findings, a.vars
	a.mu.Unlock()

	allowDelete := a.allowPermanentDelete()
	guard := actions.NewGuard(vars)
	res.Items = append(make([]CulpritDTO, 0, len(res.Items)), res.Items...)
	for i := range res.Items {
		if i < len(findings) {
			if a.windowsOnly() != nil {
				res.Items[i].Actions = []ActionOptionDTO{} // explain only off Windows
				continue
			}
			res.Items[i].Actions = actionOptions(findings[i], guard, allowDelete)
		}
	}
	return res, nil
}
