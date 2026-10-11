package attribute

import (
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"cysta-dyska/internal/scan"
	"cysta-dyska/internal/sources/locations"
	"cysta-dyska/internal/sources/registry"
)

// Finding is one folder or file in the scan that a rule explains.
type Finding struct {
	Rule  *Rule
	Owner string // rule owner, or the app name for template rules
	// App is the installed program for installed-app findings, so the UI
	// can offer its uninstaller.
	App *registry.App
	Node  *scan.Node
	// Alloc is the on-disk size that belongs to this finding alone: the
	// node's size minus any nested findings, which are listed separately.
	Alloc int64
}

// Unreadable reports whether the finding's folder could not be measured.
func (f Finding) Unreadable() bool { return f.Node.Err != "" && f.Node.Alloc == 0 }

// Match explains the scan tree. Precedence when rules overlap on the same
// folder: path rules, then installed programs, then app data folders, then
// name rules. Nested findings are reported separately and subtracted from
// the finding that contains them, so no byte is counted twice.
func Match(tree *scan.Node, pack *Pack, vars locations.Vars, apps []registry.App) []Finding {
	m := &matcher{tree: tree, claims: map[*scan.Node]*Finding{}}

	for _, r := range pack.Rules {
		for _, pt := range r.Paths {
			if p, ok := expand(pt, vars); ok {
				for _, n := range m.glob(p) {
					m.claim(n, r, r.Owner)
				}
			}
		}
	}
	m.matchApps(pack, vars, apps)
	m.matchNames(pack, vars) // last: it must see every folder claimed above
	return m.finish()
}

// appAreas are folders where programs keep their own files. A folder that
// looks like project output there (node_modules, bin, ...) belongs to a
// program, and deleting it would break that program.
var appAreas = []string{"LOCALAPPDATA", "APPDATA", "PROGRAMDATA", "PROGRAMFILES", "PROGRAMFILESX86", "SYSTEMROOT"}

type matcher struct {
	tree   *scan.Node
	claims map[*scan.Node]*Finding
	order  []*scan.Node
}

func (m *matcher) claim(n *scan.Node, r *Rule, owner string) {
	if _, taken := m.claims[n]; taken {
		return
	}
	m.claims[n] = &Finding{Rule: r, Owner: owner, Node: n}
	m.order = append(m.order, n)
}

// claimApp claims an installed program's folder.
func (m *matcher) claimApp(n *scan.Node, r *Rule, app registry.App) {
	if _, taken := m.claims[n]; taken {
		return
	}
	m.claims[n] = &Finding{Rule: r, Owner: app.Name, Node: n, App: &app}
	m.order = append(m.order, n)
}

// expand replaces {VAR} references. It fails if a variable is unknown on
// this machine, so the rule is skipped instead of matching a wrong path.
func expand(pattern string, vars locations.Vars) (string, bool) {
	ok := true
	out := varRef.ReplaceAllStringFunc(pattern, func(ref string) string {
		v := vars[ref[1:len(ref)-1]]
		if v == "" {
			ok = false
		}
		return v
	})
	return filepath.Clean(out), ok
}

// glob returns the tree nodes matching an absolute pattern whose segments
// may contain wildcards. Matching is case-insensitive.
func (m *matcher) glob(pattern string) []*scan.Node {
	rel, err := filepath.Rel(m.tree.Path(), pattern)
	if err != nil || rel == ".." || strings.HasPrefix(rel, `..\`) {
		return nil // outside the scanned folder
	}
	nodes := []*scan.Node{m.tree}
	if rel == "." {
		return nodes
	}
	for _, seg := range strings.Split(rel, `\`) {
		seg = strings.ToLower(seg)
		var next []*scan.Node
		for _, n := range nodes {
			for _, c := range n.Children {
				if ok, _ := filepath.Match(seg, strings.ToLower(c.Name)); ok {
					next = append(next, c)
				}
			}
		}
		if nodes = next; len(nodes) == 0 {
			return nil
		}
	}
	return nodes
}

// matchNames finds folders by name (node_modules, target, ...) in the
// user's own folders. It does not look inside app areas or inside any
// folder already claimed (a program, a cache, or an outer node_modules),
// so it only ever reports project folders.
func (m *matcher) matchNames(pack *Pack, vars locations.Vars) {
	type nameRule struct {
		rule *Rule
		nm   NameMatch
	}
	byName := map[string][]nameRule{}
	for _, r := range pack.Rules {
		for _, nm := range r.Names {
			k := strings.ToLower(nm.Name)
			byName[k] = append(byName[k], nameRule{r, nm})
		}
	}
	if len(byName) == 0 {
		return
	}
	skip := map[*scan.Node]bool{}
	for _, name := range appAreas {
		if dir := vars[name]; dir != "" {
			if n := m.tree.Find(dir); n != nil {
				skip[n] = true
			}
		}
	}
	// Tools also keep their files in dot-folders of the home folder, e.g.
	// editor extensions in ~\.vscode\extensions.
	if home := vars["USERPROFILE"]; home != "" {
		if n := m.tree.Find(home); n != nil {
			for _, c := range n.Children {
				if strings.HasPrefix(c.Name, ".") {
					skip[c] = true
				}
			}
		}
	}

	var walk func(n *scan.Node)
	walk = func(n *scan.Node) {
		for _, c := range n.Children {
			if !c.IsDir || c.Link || skip[c] || m.claims[c] != nil {
				continue
			}
			matched := false
			for _, nr := range byName[strings.ToLower(c.Name)] {
				if hasSibling(n, nr.nm.Siblings) {
					m.claim(c, nr.rule, nr.rule.Owner)
					matched = true
					break
				}
			}
			if !matched {
				walk(c)
			}
		}
	}
	walk(m.tree)
}

func hasSibling(parent *scan.Node, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, c := range parent.Children {
		name := strings.ToLower(c.Name)
		for _, p := range patterns {
			if ok, _ := filepath.Match(strings.ToLower(p), name); ok {
				return true
			}
		}
	}
	return false
}

// matchApps attributes program folders and, by name, the data folders that
// programs keep in AppData and ProgramData.
func (m *matcher) matchApps(pack *Pack, vars locations.Vars, apps []registry.App) {
	installed := pack.Rule(TemplateInstalledApp)
	for _, a := range apps {
		if a.InstallDir == "" {
			continue
		}
		if n := m.tree.Find(a.InstallDir); n != nil && n.IsDir {
			m.claimApp(n, installed, a)
		}
	}

	// Index app names and publishers by a normalized key, e.g.
	// "JetBrains s.r.o." -> "jetbrains". Microsoft is skipped: its folders
	// hold parts of Windows, which the system rules explain better.
	labels := map[string]string{}
	for _, a := range apps {
		if k := normalize(a.Name); len(k) >= 4 {
			labels[k] = a.Name
		}
		if k := normalize(trimCompanySuffix(a.Publisher)); len(k) >= 4 && k != "microsoft" {
			if _, exists := labels[k]; !exists {
				labels[k] = trimCompanySuffix(a.Publisher)
			}
		}
	}
	data := pack.Rule(TemplateAppData)
	for _, base := range []string{"LOCALAPPDATA", "APPDATA", "PROGRAMDATA"} {
		dir := vars[base]
		if dir == "" {
			continue
		}
		n := m.tree.Find(dir)
		if n == nil {
			continue
		}
		for _, c := range n.Children {
			if !c.IsDir || c.Link {
				continue
			}
			if label, ok := labels[normalize(c.Name)]; ok {
				m.claim(c, data, label)
			}
		}
	}
}

// normalize keeps only lower-case letters and digits.
func normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

var companySuffixes = []string{
	" corporation", " corp.", " corp", " incorporated", " inc.", " inc", " llc", " ltd.", " ltd",
	" limited", " gmbh", " s.r.o.", " s.a.", " ag", " co.", " b.v.", ",",
}

func trimCompanySuffix(s string) string {
	s = strings.TrimSpace(s)
	for changed := true; changed; {
		changed = false
		low := strings.ToLower(s)
		for _, suf := range companySuffixes {
			if strings.HasSuffix(low, suf) {
				s = strings.TrimSpace(s[:len(s)-len(suf)])
				changed = true
				break
			}
		}
	}
	return s
}

// finish computes exclusive sizes with one walk over the tree and drops
// findings that use no space and could be read.
func (m *matcher) finish() []Finding {
	if len(m.claims) == 0 {
		return nil
	}
	var walk func(n *scan.Node, owner *Finding)
	walk = func(n *scan.Node, owner *Finding) {
		if f := m.claims[n]; f != nil {
			f.Alloc = n.Alloc
			if owner != nil {
				owner.Alloc -= n.Alloc
			}
			owner = f
		}
		for _, c := range n.Children {
			walk(c, owner)
		}
	}
	walk(m.tree, nil)

	out := make([]Finding, 0, len(m.order))
	for _, n := range m.order {
		f := m.claims[n]
		if f.Alloc > 0 || f.Unreadable() {
			out = append(out, *f)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Alloc > out[j].Alloc })
	return out
}
