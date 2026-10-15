package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cysta-dyska/internal/actions"
	"cysta-dyska/internal/attribute"
	"cysta-dyska/internal/scan"
	"cysta-dyska/internal/store"
)

// Action kinds.
const (
	KindCommand   = "command"   // the owner's command, e.g. go clean -cache
	KindAdmin     = "admin"     // a Windows command run after a UAC prompt
	KindRecycle   = "recycle"   // move to the Recycle Bin
	KindDelete    = "delete"    // delete permanently (only if the user enabled it)
	KindUninstall = "uninstall" // start the program's own uninstaller
)

const (
	settingPermanentDelete = "allow_permanent_delete"
	planLifetime           = 15 * time.Minute
	commandTimeout         = 30 * time.Minute
)

// executors do the actual work; tests replace them.
type executors struct {
	recycle   func([]string) error
	remove    func([]string) error
	run       func(ctx context.Context, program string, args []string, dir string) (actions.Result, error)
	elevated  func(program string, args []string) (int, error)
	uninstall func(uninstallString string) error
	resolve   func(name string, admin bool, systemRoot string) (string, error)
}

func defaultExecutors() executors {
	return executors{
		recycle: actions.Recycle, remove: actions.Remove, run: actions.RunCommand,
		elevated: actions.RunElevated, uninstall: actions.LaunchUninstaller, resolve: actions.ResolveProgram,
	}
}

// ActionOptionDTO is an action the UI can offer for a finding.
type ActionOptionDTO struct {
	Kind     string `json:"kind"`
	Suggest  int    `json:"suggest"`  // index into the rule's suggestions, for commands
	Command  string `json:"command"`  // shown to the user
	NeedsAck bool   `json:"needsAck"` // the user must confirm they looked inside
	Blocked  string `json:"blocked"`  // why it cannot run here, or ""
}

// ActionRequest names a finding and the action to plan for it.
type ActionRequest struct {
	RuleID  string `json:"ruleId"`
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Suggest int    `json:"suggest"`
}

// PlanDTO is what the confirmation dialog shows.
type PlanDTO struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Display     string   `json:"display"` // command line or uninstaller
	Targets     []string `json:"targets"` // first few paths, for file actions
	TargetCount int      `json:"targetCount"`
	Bytes       int64    `json:"bytes"`
	Files       int64    `json:"files"`
	NeedsAck    bool     `json:"needsAck"`
	Blocked     string   `json:"blocked"`
	Warnings    []string `json:"warnings"`
}

// ActionResultDTO is how an action ended.
type ActionResultDTO struct {
	Outcome string `json:"outcome"` // done, failed, declined, started
	Freed   int64  `json:"freed"`
	Output  string `json:"output"`
	Detail  string `json:"detail"`
}

// ActionLogDTO is one entry of the action log.
type ActionLogDTO struct {
	ID          int64  `json:"id"`
	At          int64  `json:"at"`
	Kind        string `json:"kind"`
	Owner       string `json:"owner"`
	Target      string `json:"target"`
	Command     string `json:"command"`
	BytesBefore int64  `json:"bytesBefore"`
	BytesAfter  int64  `json:"bytesAfter"`
	Outcome     string `json:"outcome"`
	Detail      string `json:"detail"`
}

// SettingsDTO holds the user's settings.
type SettingsDTO struct {
	AllowPermanentDelete bool `json:"allowPermanentDelete"`
	Available            bool `json:"available"` // false without the history database
}

type plan struct {
	PlanDTO
	finding   attribute.Finding
	program   string
	args      []string
	dir       string
	targets   []string
	uninstall string
	created   time.Time
}

// actionOptions lists the actions a finding allows.
func actionOptions(f attribute.Finding, guard *actions.Guard, allowDelete bool) []ActionOptionDTO {
	review := f.Rule.Risk == attribute.RiskReview
	out := []ActionOptionDTO{} // [] not null in JSON: the UI reads .length
	for i, s := range f.Rule.Suggest {
		if s.Run == nil {
			continue
		}
		kind := KindCommand
		if s.Run.Admin {
			kind = KindAdmin
		}
		out = append(out, ActionOptionDTO{Kind: kind, Suggest: i, Command: s.Command, NeedsAck: review})
	}
	if f.Rule.FileAction != attribute.FileActionNone {
		blocked := ""
		if err := guard.Check(f.Node.Path()); err != nil {
			blocked = err.Error()
		} else if f.Unreadable() {
			blocked = "this folder could not be read"
		}
		out = append(out, ActionOptionDTO{Kind: KindRecycle, Suggest: -1, NeedsAck: review, Blocked: blocked})
		if allowDelete {
			out = append(out, ActionOptionDTO{Kind: KindDelete, Suggest: -1, NeedsAck: review, Blocked: blocked})
		}
	}
	if f.App != nil && f.App.UninstallString != "" {
		out = append(out, ActionOptionDTO{Kind: KindUninstall, Suggest: -1, Command: f.App.UninstallString})
	}
	return out
}

func newPlanID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// PlanAction prepares an action and measures what it would affect. Nothing
// is changed until RunAction.
func (a *App) PlanAction(req ActionRequest) (PlanDTO, error) {
	if err := a.windowsOnly(); err != nil {
		return PlanDTO{}, err
	}
	a.mu.Lock()
	var f *attribute.Finding
	for i := range a.findings {
		if a.findings[i].Rule.ID == req.RuleID && strings.EqualFold(a.findings[i].Node.Path(), req.Path) {
			f = &a.findings[i]
			break
		}
	}
	vars := a.vars
	a.mu.Unlock()
	if f == nil {
		return PlanDTO{}, errors.New("this finding is not in the current scan; scan again")
	}

	var opt *ActionOptionDTO
	for _, o := range actionOptions(*f, actions.NewGuard(vars), a.allowPermanentDelete()) {
		if o.Kind == req.Kind && (o.Suggest == req.Suggest || o.Suggest < 0) {
			o := o
			opt = &o
			break
		}
	}
	if opt == nil {
		return PlanDTO{}, fmt.Errorf("%q is not available for this finding", req.Kind)
	}
	if opt.Blocked != "" {
		return PlanDTO{}, errors.New(opt.Blocked)
	}

	p := &plan{finding: *f, created: time.Now()}
	p.PlanDTO = PlanDTO{
		ID: newPlanID(), Kind: req.Kind, NeedsAck: opt.NeedsAck, Bytes: f.Node.Alloc, Files: f.Node.Files,
		Targets: []string{}, Warnings: []string{},
	}
	path := f.Node.Path()

	switch req.Kind {
	case KindCommand, KindAdmin:
		s := f.Rule.Suggest[opt.Suggest]
		prog, err := a.exec.resolve(s.Run.Program, s.Run.Admin, vars["SYSTEMROOT"])
		if err != nil {
			return PlanDTO{}, err
		}
		p.program, p.args, p.Display = prog, s.Run.Args, s.Command
		p.dir = vars["USERPROFILE"]
		if s.Run.In == "project" {
			p.dir = filepath.Dir(path)
		}
	case KindRecycle, KindDelete:
		if err := a.measureTargets(p, path, vars); err != nil {
			return PlanDTO{}, err
		}
	case KindUninstall:
		p.uninstall, p.Display = f.App.UninstallString, f.App.UninstallString
	}

	a.mu.Lock()
	if a.plans == nil {
		a.plans = map[string]*plan{}
	}
	for id, old := range a.plans {
		if time.Since(old.created) > planLifetime {
			delete(a.plans, id)
		}
	}
	a.plans[p.ID] = p
	a.mu.Unlock()
	return p.PlanDTO, nil
}

// measureTargets lists and measures a file action's targets now.
func (a *App) measureTargets(p *plan, path string, vars map[string]string) error {
	if err := actions.NewGuard(vars).Check(path); err != nil {
		return err
	}
	targets, err := actions.Targets(path, p.finding.Rule.FileAction)
	if err != nil {
		return err
	}
	m, err := actions.Measure(context.Background(), targets)
	if err != nil {
		return err
	}
	p.targets, p.TargetCount = targets, len(targets)
	p.Targets = targets[:min(len(targets), 20)]
	p.Bytes, p.Files = m.Bytes, m.Files
	p.Blocked = m.Blocked()
	p.Warnings = []string{}
	if m.Unreadable > 0 {
		p.Warnings = append(p.Warnings, fmt.Sprintf("%d folder(s) could not be read and may be left behind", m.Unreadable))
	}
	if p.Kind == KindRecycle && m.LongPath != "" && p.Blocked == "" {
		p.Blocked = "some paths are too long for the Recycle Bin (" + m.LongPath + ")"
	}
	return nil
}

// RunAction carries out a planned action after checking everything again.
func (a *App) RunAction(planID string, acknowledged bool) (ActionResultDTO, error) {
	if err := a.windowsOnly(); err != nil {
		return ActionResultDTO{}, err
	}
	if !a.actionMu.TryLock() {
		return ActionResultDTO{}, errors.New("another action is still running")
	}
	defer a.actionMu.Unlock()

	a.mu.Lock()
	p := a.plans[planID]
	scanning := a.cancel != nil
	vars := a.vars
	a.mu.Unlock()
	switch {
	case p == nil || time.Since(p.created) > planLifetime:
		return ActionResultDTO{}, errors.New("this plan has expired; plan the action again")
	case p.NeedsAck && !acknowledged:
		return ActionResultDTO{}, errors.New("please confirm you have checked what is inside first")
	case scanning:
		return ActionResultDTO{}, errors.New("wait for the scan to finish")
	}
	a.mu.Lock()
	delete(a.plans, planID) // a plan runs at most once
	a.mu.Unlock()

	path := p.finding.Node.Path()
	if p.Kind == KindRecycle || p.Kind == KindDelete {
		if p.Kind == KindDelete && !a.allowPermanentDelete() {
			return ActionResultDTO{}, errors.New("permanent delete has been turned off")
		}
		if err := a.measureTargets(p, path, vars); err != nil {
			return ActionResultDTO{}, err
		}
		if p.Blocked != "" {
			return ActionResultDTO{}, errors.New(p.Blocked)
		}
	}

	logID := a.logStart(p)
	res := a.execute(p)
	after := p.Bytes
	if res.Outcome != "started" && res.Outcome != "declined" {
		after = measureRemaining(p, path)
	}
	res.Freed = max(p.Bytes-after, 0)
	a.logFinish(logID, res, after)
	return res, nil
}

func (a *App) execute(p *plan) ActionResultDTO {
	switch p.Kind {
	case KindCommand:
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		r, err := a.exec.run(ctx, p.program, p.args, p.dir)
		switch {
		case err != nil:
			return ActionResultDTO{Outcome: "failed", Output: r.Output, Detail: err.Error()}
		case r.ExitCode != 0:
			return ActionResultDTO{Outcome: "failed", Output: r.Output, Detail: fmt.Sprintf("exit code %d", r.ExitCode)}
		}
		return ActionResultDTO{Outcome: "done", Output: r.Output}
	case KindAdmin:
		code, err := a.exec.elevated(p.program, p.args)
		switch {
		case errors.Is(err, actions.ErrDeclined):
			return ActionResultDTO{Outcome: "declined", Detail: err.Error()}
		case err != nil:
			return ActionResultDTO{Outcome: "failed", Detail: err.Error()}
		case code != 0:
			return ActionResultDTO{Outcome: "failed", Detail: fmt.Sprintf("exit code %d", code)}
		}
		return ActionResultDTO{Outcome: "done"}
	case KindRecycle, KindDelete:
		do := a.exec.recycle
		if p.Kind == KindDelete {
			do = a.exec.remove
		}
		if err := do(p.targets); err != nil {
			return ActionResultDTO{Outcome: "failed", Detail: err.Error()}
		}
		return ActionResultDTO{Outcome: "done"}
	case KindUninstall:
		if err := a.exec.uninstall(p.uninstall); err != nil {
			return ActionResultDTO{Outcome: "failed", Detail: err.Error()}
		}
		return ActionResultDTO{Outcome: "started", Detail: "Finish in the uninstaller's window, then scan again."}
	}
	return ActionResultDTO{Outcome: "failed", Detail: "unknown action"}
}

// measureRemaining measures what is left of a finding after an action.
func measureRemaining(p *plan, path string) int64 {
	paths := []string{path}
	if p.Kind == KindRecycle || p.Kind == KindDelete {
		paths = p.targets
	}
	var total int64
	for _, t := range paths {
		info, err := os.Lstat(t)
		if err != nil {
			continue // gone
		}
		if !info.IsDir() {
			total += info.Size()
			continue
		}
		if tree, err := scan.Scan(context.Background(), t, scan.Options{}); err == nil {
			total += tree.Alloc
		}
	}
	return total
}

func (a *App) logStart(p *plan) int64 {
	if a.history == nil {
		return 0
	}
	id, _ := a.history.StartAction(store.ActionRecord{
		Kind: p.Kind, RuleID: p.finding.Rule.ID, Owner: p.finding.Owner, Target: p.finding.Node.Path(),
		Command: p.Display, BytesBefore: p.Bytes,
	})
	return id
}

func (a *App) logFinish(id int64, res ActionResultDTO, after int64) {
	if a.history == nil || id == 0 {
		return
	}
	detail := res.Detail
	if out := strings.TrimSpace(res.Output); out != "" {
		const keep = 4000
		if len(out) > keep {
			out = out[len(out)-keep:]
		}
		detail = strings.TrimSpace(detail + "\n" + out)
	}
	a.history.FinishAction(id, res.Outcome, detail, after)
}

// ActionLog returns the most recent actions, newest first.
func (a *App) ActionLog() ([]ActionLogDTO, error) {
	out := []ActionLogDTO{}
	if a.history == nil {
		return out, nil
	}
	recs, err := a.history.Actions(100)
	if err != nil {
		return nil, err
	}
	for _, r := range recs {
		out = append(out, ActionLogDTO{
			ID: r.ID, At: r.At.UnixMilli(), Kind: r.Kind, Owner: r.Owner, Target: r.Target, Command: r.Command,
			BytesBefore: r.BytesBefore, BytesAfter: r.BytesAfter, Outcome: r.Outcome, Detail: r.Detail,
		})
	}
	return out, nil
}

func (a *App) allowPermanentDelete() bool {
	if a.history == nil {
		return false
	}
	v, ok, err := a.history.Setting(settingPermanentDelete)
	return err == nil && ok && v == "true"
}

// Settings returns the user's settings.
func (a *App) Settings() SettingsDTO {
	return SettingsDTO{AllowPermanentDelete: a.allowPermanentDelete(), Available: a.history != nil}
}

// SetAllowPermanentDelete turns permanent delete on or off.
func (a *App) SetAllowPermanentDelete(on bool) error {
	if a.history == nil {
		return errors.New("settings need the history database, which is not available")
	}
	return a.history.SetSetting(settingPermanentDelete, fmt.Sprint(on))
}
