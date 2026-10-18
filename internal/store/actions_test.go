package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// Proves: a database created by the M4 version (schema 1) is upgraded in
// place: its snapshots survive and the new tables work.
func TestMigrateFromVersion1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(migrations[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO snapshots (root, root_key, taken_at, alloc, size, files, dirs, duration_ms, min_alloc, max_depth)
		VALUES ('C:\x', 'c:\x', 1, 2, 3, 4, 5, 6, 7, 8); PRAGMA user_version = 1`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	list, err := s.Snapshots(`C:\x`, MethodWalk)
	if err != nil || len(list) != 1 {
		t.Fatalf("snapshots after upgrade = %v, %v", list, err)
	}
	if list[0].Method != MethodWalk {
		t.Errorf("old snapshot method = %q, want %q (they were all normal scans)", list[0].Method, MethodWalk)
	}
	if err := s.SetSetting("k", "v"); err != nil {
		t.Fatalf("settings table missing after upgrade: %v", err)
	}
	var v int
	s.db.QueryRow(`PRAGMA user_version`).Scan(&v)
	if v != len(migrations) {
		t.Errorf("user_version = %d, want %d", v, len(migrations))
	}
}

// Proves: actions left "running" by a previous run of the app (it crashed or
// was killed mid-action) are marked "interrupted" with a note, finished
// actions are left alone, and the count of marked actions is returned.
func TestMarkInterrupted(t *testing.T) {
	s := openTest(t)
	done, _ := s.StartAction(ActionRecord{Kind: "recycle", Target: `C:\a`})
	s.FinishAction(done, "done", "", 0)
	cut, _ := s.StartAction(ActionRecord{Kind: "delete", Target: `C:\b`, BytesBefore: 50})

	n, err := s.MarkInterrupted()
	if err != nil || n != 1 {
		t.Fatalf("MarkInterrupted = %d, %v; want 1", n, err)
	}
	log, _ := s.Actions(10)
	for _, r := range log {
		switch r.ID {
		case cut:
			if r.Outcome != "interrupted" || r.Detail == "" || r.BytesBefore != 50 {
				t.Errorf("cut-short action = %+v", r)
			}
		case done:
			if r.Outcome != "done" {
				t.Errorf("finished action changed: %+v", r)
			}
		}
	}
	if n, _ := s.MarkInterrupted(); n != 0 {
		t.Errorf("second call marked %d, want 0", n)
	}
}

// Proves: settings round-trip and missing keys are reported as absent.
func TestSettings(t *testing.T) {
	s := openTest(t)
	if _, ok, err := s.Setting("allow_delete"); ok || err != nil {
		t.Fatalf("missing setting: ok=%v err=%v", ok, err)
	}
	if err := s.SetSetting("allow_delete", "true"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting("allow_delete", "false"); err != nil {
		t.Fatal(err)
	}
	if v, ok, err := s.Setting("allow_delete"); !ok || err != nil || v != "false" {
		t.Errorf("setting = %q, %v, %v", v, ok, err)
	}
}

// Proves: ClearHistory removes every snapshot with its entries and the whole
// action log, but keeps the settings, and the store keeps working afterwards.
func TestClearHistory(t *testing.T) {
	s := openTest(t)
	tree := scanFixture(t, map[string]int{"a/big.bin": 200_000})
	if _, err := s.Save(tree, time.Second, MethodWalk, testOpts()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartAction(ActionRecord{Kind: "recycle", Target: `C:\a`}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting("allow_permanent_delete", "true"); err != nil {
		t.Fatal(err)
	}

	if err := s.ClearHistory(); err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{"snapshots", "entries", "actions"} {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s has %d rows after clearing (err %v)", table, n, err)
		}
	}
	if v, ok, _ := s.Setting("allow_permanent_delete"); !ok || v != "true" {
		t.Errorf("setting after clearing = %q, %v; settings must be kept", v, ok)
	}
	if _, err := s.Save(tree, time.Second, MethodWalk, testOpts()); err != nil {
		t.Errorf("save after clearing: %v", err)
	}
}

// Proves: an action is logged as running before it starts and updated when
// it ends, so the log shows actions interrupted by a crash; the log lists
// newest first.
func TestActionLog(t *testing.T) {
	s := openTest(t)
	id, err := s.StartAction(ActionRecord{
		Kind: "recycle", RuleID: "npm-cache", Owner: "npm", Target: `C:\npm`, BytesBefore: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	crashed, err := s.StartAction(ActionRecord{Kind: "command", Command: "go clean -cache"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishAction(id, "done", "", 0); err != nil {
		t.Fatal(err)
	}

	log, err := s.Actions(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != 2 || log[0].ID != crashed || log[1].ID != id {
		t.Fatalf("log = %+v", log)
	}
	if log[0].Outcome != "running" {
		t.Errorf("unfinished action outcome = %q, want running", log[0].Outcome)
	}
	if log[1].Outcome != "done" || log[1].BytesBefore != 1000 || log[1].BytesAfter != 0 || log[1].At.IsZero() {
		t.Errorf("finished action = %+v", log[1])
	}
	if log[1].At.After(time.Now().Add(time.Hour * 24 * 365 * 10)) {
		t.Error("timestamp is nonsense")
	}
}
