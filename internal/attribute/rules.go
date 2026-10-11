// Package attribute explains who owns the space found by a scan. It matches
// the scan tree against an embedded rule pack (rules/*.yaml) and against
// installed programs read from the registry.
package attribute

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"cysta-dyska/internal/sources/locations"
)

//go:embed rules/*.yaml
var embedded embed.FS

// Locales every text in the rule pack must provide.
var Locales = []string{"en", "be"}

// Category groups findings in the UI.
type Category string

const (
	CatDevCache     Category = "dev-cache"
	CatDevTools     Category = "dev-tools"
	CatBuildOutput  Category = "build-output"
	CatBrowserCache Category = "browser-cache"
	CatAppCache     Category = "app-cache"
	CatAppData      Category = "app-data"
	CatInstalledApp Category = "installed-app"
	CatVMDisk       Category = "vm-disk"
	CatSystem       Category = "system"
	CatTemp         Category = "temp"
	CatUserFiles    Category = "user-files"
)

var categories = []Category{
	CatDevCache, CatDevTools, CatBuildOutput, CatBrowserCache, CatAppCache, CatAppData,
	CatInstalledApp, CatVMDisk, CatSystem, CatTemp, CatUserFiles,
}

// Risk says how careful the user must be with a finding.
type Risk string

const (
	RiskSafe   Risk = "safe"   // a cache its owner rebuilds by itself
	RiskReview Risk = "review" // may hold things you still need: look first
	RiskSystem Risk = "system" // managed by Windows: use Windows tools only
	RiskNever  Risk = "never"  // do not touch: removing it breaks things
)

var risks = []Risk{RiskSafe, RiskReview, RiskSystem, RiskNever}

// Text is a message in every supported locale.
type Text map[string]string

// Suggestion is advice shown with a finding. In v1 nothing is executed:
// commands are shown for the user to copy and run themselves.
type Suggestion struct {
	Text     Text   `yaml:"text" json:"text"`
	Command  string `yaml:"command,omitempty" json:"command,omitempty"`
	Settings string `yaml:"settings,omitempty" json:"settings,omitempty"` // ms-settings: URI
	// Run makes Command runnable from the app. Validation requires Command
	// to be exactly Program and Args, so what is shown is what runs.
	Run *Run `yaml:"run,omitempty" json:"run,omitempty"`
}

// Run is a command the app can run for the user after confirmation.
type Run struct {
	Program string   `yaml:"program" json:"program"` // found on PATH; never a path
	Args    []string `yaml:"args,omitempty" json:"args,omitempty"`
	Admin   bool     `yaml:"admin,omitempty" json:"admin,omitempty"` // needs a UAC prompt
	// In is where to run it: "" for the user's home, "project" for the
	// folder containing the finding (e.g. cargo clean next to Cargo.toml).
	In string `yaml:"in,omitempty" json:"in,omitempty"`
}

// FileAction says what the app may do with a finding's files.
type FileAction string

const (
	FileActionNone     FileAction = ""         // advice only
	FileActionFolder   FileAction = "folder"   // the whole folder or file
	FileActionContents FileAction = "contents" // what is inside, keeping the folder
)

// NameMatch matches a folder by name anywhere in the tree, optionally only
// when a sibling entry matching one of Siblings (glob) exists next to it.
type NameMatch struct {
	Name     string   `yaml:"name"`
	Siblings []string `yaml:"siblings,omitempty"`
}

// Rule describes one kind of space user.
type Rule struct {
	ID       string       `yaml:"id"`
	Owner    string       `yaml:"owner"`
	Category Category     `yaml:"category"`
	Risk     Risk         `yaml:"risk"`
	Paths    []string     `yaml:"paths,omitempty"` // {VAR}\path, "*" matches one segment
	Names    []NameMatch  `yaml:"names,omitempty"`
	Template bool         `yaml:"template,omitempty"` // filled in by code, e.g. installed apps
	Explain  Text         `yaml:"explain"`
	Suggest  []Suggestion `yaml:"suggest,omitempty"`
	// FileAction allows moving to the Recycle Bin (or deleting, if the
	// user enabled it). Off unless a rule opts in.
	FileAction FileAction `yaml:"fileAction,omitempty"`
}

// Pack is the full set of rules.
type Pack struct {
	Rules []*Rule
	byID  map[string]*Rule
}

// Rule returns the rule with the given id, or nil.
func (p *Pack) Rule(id string) *Rule { return p.byID[id] }

// Template ids the matcher relies on.
const (
	TemplateInstalledApp = "installed-app"
	TemplateAppData      = "app-data"
)

type file struct {
	Version int     `yaml:"version"`
	Rules   []*Rule `yaml:"rules"`
}

// LoadEmbedded loads and validates the rule pack built into the binary.
func LoadEmbedded() (*Pack, error) {
	return Load(embedded, "rules")
}

// Load reads every *.yaml file in dir of fsys as one pack and validates it.
func Load(fsys fs.FS, dir string) (*Pack, error) {
	names, err := fs.Glob(fsys, path.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no rule files in %s", dir)
	}
	pack := &Pack{byID: map[string]*Rule{}}
	for _, name := range names {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true) // a typo in a field name is an error, not a silently ignored rule
		var f file
		if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if f.Version != 1 {
			return nil, fmt.Errorf("%s: unsupported version %d", name, f.Version)
		}
		pack.Rules = append(pack.Rules, f.Rules...)
	}
	if err := pack.validate(); err != nil {
		return nil, err
	}
	return pack, nil
}

var varRef = regexp.MustCompile(`\{([A-Z0-9_]+)\}`)

// validateRun checks a runnable suggestion.
func validateRun(r *Rule, i int, s Suggestion, bad func(*Rule, string, ...any)) {
	run := s.Run
	if run.Program == "" || strings.ContainsAny(run.Program, `\/:`) {
		bad(r, "suggest[%d].run.program %q must be a program name, not a path", i, run.Program)
	}
	shown := strings.Join(append([]string{run.Program}, run.Args...), " ")
	if s.Command != shown {
		bad(r, "suggest[%d].command %q must be exactly the run command %q", i, s.Command, shown)
	}
	for _, a := range run.Args {
		if strings.ContainsAny(a, "<>") {
			bad(r, "suggest[%d].run has a placeholder argument %q", i, a)
		}
	}
	switch run.In {
	case "":
	case "project":
		if len(r.Names) == 0 {
			bad(r, "suggest[%d].run.in: project needs a name rule", i)
		}
	default:
		bad(r, "suggest[%d].run.in %q is unknown", i, run.In)
	}
}

func (p *Pack) validate() error {
	known := map[string]bool{}
	for _, k := range locations.Known {
		known[k] = true
	}
	var errs []error
	bad := func(r *Rule, format string, args ...any) {
		errs = append(errs, fmt.Errorf("rule %q: %s", r.ID, fmt.Sprintf(format, args...)))
	}
	checkText := func(r *Rule, what string, t Text) {
		for _, l := range Locales {
			if strings.TrimSpace(t[l]) == "" {
				bad(r, "%s is missing locale %q", what, l)
			}
		}
	}

	for _, r := range p.Rules {
		switch {
		case r.ID == "":
			bad(r, "missing id")
		case p.byID[r.ID] != nil:
			bad(r, "duplicate id")
		default:
			p.byID[r.ID] = r
		}
		if !slices.Contains(categories, r.Category) {
			bad(r, "unknown category %q", r.Category)
		}
		if !slices.Contains(risks, r.Risk) {
			bad(r, "unknown risk %q", r.Risk)
		}
		if r.Owner == "" && !r.Template {
			bad(r, "missing owner")
		}
		if r.Template == (len(r.Paths)+len(r.Names) > 0) {
			bad(r, "needs either paths/names or template: true, not both")
		}
		for _, pt := range r.Paths {
			if !strings.HasPrefix(pt, "{") {
				bad(r, "path %q must start with a {VAR}", pt)
			}
			for _, m := range varRef.FindAllStringSubmatch(pt, -1) {
				if !known[m[1]] {
					bad(r, "path %q uses unknown variable %s", pt, m[1])
				}
			}
		}
		for _, n := range r.Names {
			if n.Name == "" || strings.ContainsAny(n.Name, `\/*?`) {
				bad(r, "name %q must be a plain folder name", n.Name)
			}
		}
		checkText(r, "explain", r.Explain)
		for i, s := range r.Suggest {
			checkText(r, fmt.Sprintf("suggest[%d].text", i), s.Text)
			if s.Settings != "" && !strings.HasPrefix(s.Settings, "ms-settings:") {
				bad(r, "suggest[%d].settings must be an ms-settings: URI", i)
			}
			if s.Run != nil {
				validateRun(r, i, s, bad)
			}
		}
		switch r.FileAction {
		case FileActionNone, FileActionFolder, FileActionContents:
		default:
			bad(r, "unknown fileAction %q", r.FileAction)
		}
		if r.FileAction != FileActionNone &&
			(r.Risk == RiskSystem || r.Risk == RiskNever || r.Category == CatInstalledApp) {
			bad(r, "fileAction is not allowed on %s/%s findings", r.Category, r.Risk)
		}
	}
	for _, id := range []string{TemplateInstalledApp, TemplateAppData} {
		if r := p.byID[id]; r == nil || !r.Template {
			errs = append(errs, fmt.Errorf("template rule %q is missing", id))
		}
	}
	return errors.Join(errs...)
}
