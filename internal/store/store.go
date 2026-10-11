package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"cysta-dyska/internal/scan"
)

// Options control what a snapshot keeps.
type Options struct {
	MinAlloc int64 // entries smaller than this on disk are not stored
	MaxDepth int   // entries deeper than this below the root are not stored
	Keep     int   // snapshots kept per scanned folder; older ones are pruned
}

// DefaultOptions keep about 10k rows (~1 MB) for a full system drive.
var DefaultOptions = Options{MinAlloc: 4 << 20, MaxDepth: 12, Keep: 30}

// Store is the scan history database.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// Snapshot describes one saved scan.
type Snapshot struct {
	ID       int64
	Root     string
	TakenAt  time.Time
	Alloc    int64
	Size     int64
	Files    int64
	Dirs     int64
	Duration time.Duration
	MinAlloc int64
	Method   string // MethodWalk or MethodMFT
}

// Scan methods. A full (MFT) scan also counts folders the normal scan cannot
// read, so snapshots are only compared with snapshots of the same method.
const (
	MethodWalk = "walk"
	MethodMFT  = "mft"
)

// DiffRow is one entry compared between two snapshots. An entry that was
// not stored in a snapshot (absent, or smaller than its MinAlloc) has
// Known=false and counts as 0.
type DiffRow struct {
	Key         string
	Path        string
	IsDir       bool
	Before      int64
	After       int64
	BeforeKnown bool
	AfterKnown  bool
	HasChildren bool
}

// Delta is the change in on-disk bytes.
func (r DiffRow) Delta() int64 { return r.After - r.Before }

// migrations[i] brings the schema from version i to i+1. Never edit a
// shipped migration; add a new one.
var migrations = []string{
	// 1: scan history (M4)
	`
CREATE TABLE snapshots (
	id          INTEGER PRIMARY KEY,
	root        TEXT    NOT NULL,
	root_key    TEXT    NOT NULL,
	taken_at    INTEGER NOT NULL, -- unix milliseconds, UTC
	alloc       INTEGER NOT NULL,
	size        INTEGER NOT NULL,
	files       INTEGER NOT NULL,
	dirs        INTEGER NOT NULL,
	duration_ms INTEGER NOT NULL,
	min_alloc   INTEGER NOT NULL,
	max_depth   INTEGER NOT NULL
);
CREATE INDEX snapshots_by_root ON snapshots (root_key, taken_at);

CREATE TABLE entries (
	snapshot_id INTEGER NOT NULL REFERENCES snapshots (id) ON DELETE CASCADE,
	key         TEXT    NOT NULL,
	parent      TEXT    NOT NULL,
	path        TEXT    NOT NULL,
	is_dir      INTEGER NOT NULL,
	alloc       INTEGER NOT NULL,
	size        INTEGER NOT NULL,
	files       INTEGER NOT NULL,
	PRIMARY KEY (snapshot_id, key)
) WITHOUT ROWID;
CREATE INDEX entries_by_parent ON entries (snapshot_id, parent);
`,
	// 2: settings and the log of cleanup actions (M5)
	`
CREATE TABLE settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
) WITHOUT ROWID;

CREATE TABLE actions (
	id           INTEGER PRIMARY KEY,
	at           INTEGER NOT NULL, -- unix milliseconds, UTC
	kind         TEXT    NOT NULL,
	rule_id      TEXT    NOT NULL,
	owner        TEXT    NOT NULL,
	target       TEXT    NOT NULL,
	command      TEXT    NOT NULL,
	bytes_before INTEGER NOT NULL,
	bytes_after  INTEGER NOT NULL,
	outcome      TEXT    NOT NULL, -- running, done, failed, declined
	detail       TEXT    NOT NULL
);
`,
	// v3: how each snapshot was made. Everything before was a normal scan.
	`ALTER TABLE snapshots ADD COLUMN method TEXT NOT NULL DEFAULT 'walk';`,
}

// DefaultPath is %LOCALAPPDATA%\CystaDyska\history.db on Windows. Local, not
// roaming: history is per machine and should not be synced.
func DefaultPath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "CystaDyska", "history.db"), nil
}

// Open opens or creates the database at path and brings its schema up to
// date.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	// The path is passed as a plain file name: Windows paths cannot contain
	// '?', so it cannot be confused with the parameters.
	dsn := path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db, now: time.Now}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("history database %s: %w", path, err)
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	var v int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return err
	}
	if v > len(migrations) {
		return fmt.Errorf("created by a newer version of Čysta Dyska (schema %d)", v)
	}
	// Each step commits on its own, so an interrupted upgrade resumes.
	for ; v < len(migrations); v++ {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[v]); err != nil {
			tx.Rollback()
			return fmt.Errorf("upgrading to schema %d: %w", v+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, v+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func rootKey(root string) string { return strings.ToLower(filepath.Clean(root)) }

// Save stores a snapshot of tree, made by method, in one transaction and
// prunes old snapshots of the same root and method.
func (s *Store) Save(tree *scan.Node, took time.Duration, method string, opts Options) (int64, error) {
	if method != MethodWalk && method != MethodMFT {
		return 0, fmt.Errorf("unknown scan method %q", method)
	}
	rows := Select(tree, opts.MinAlloc, opts.MaxDepth)
	root := tree.Path()

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`INSERT INTO snapshots
		(root, root_key, taken_at, alloc, size, files, dirs, duration_ms, min_alloc, max_depth, method)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		root, rootKey(root), s.now().UTC().UnixMilli(), tree.Alloc, tree.Size, tree.Files, tree.Dirs,
		took.Milliseconds(), opts.MinAlloc, opts.MaxDepth, method)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	ins, err := tx.Prepare(`INSERT INTO entries
		(snapshot_id, key, parent, path, is_dir, alloc, size, files) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer ins.Close()
	for _, r := range rows {
		if _, err := ins.Exec(id, r.Key, parentKey(r.Key), r.Path, r.IsDir, r.Alloc, r.Size, r.Files); err != nil {
			return 0, err
		}
	}

	if opts.Keep > 0 {
		if _, err := tx.Exec(`DELETE FROM snapshots WHERE root_key = ? AND method = ? AND id NOT IN (
			SELECT id FROM snapshots WHERE root_key = ? AND method = ? ORDER BY taken_at DESC, id DESC LIMIT ?)`,
			rootKey(root), method, rootKey(root), method, opts.Keep); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

// Snapshots lists the snapshots of root made by method, newest first.
func (s *Store) Snapshots(root, method string) ([]Snapshot, error) {
	rows, err := s.db.Query(`SELECT id, root, taken_at, alloc, size, files, dirs, duration_ms, min_alloc, method
		FROM snapshots WHERE root_key = ? AND method = ? ORDER BY taken_at DESC, id DESC`, rootKey(root), method)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Snapshot
	for rows.Next() {
		var sn Snapshot
		var taken, ms int64
		if err := rows.Scan(&sn.ID, &sn.Root, &taken, &sn.Alloc, &sn.Size, &sn.Files, &sn.Dirs, &ms, &sn.MinAlloc, &sn.Method); err != nil {
			return nil, err
		}
		sn.TakenAt = time.UnixMilli(taken)
		sn.Duration = time.Duration(ms) * time.Millisecond
		out = append(out, sn)
	}
	return out, rows.Err()
}

// sameRoot fails unless both snapshots exist, are of the same folder and
// were made by the same scan method.
func (s *Store) sameRoot(a, b int64) error {
	var found, roots, methods int
	err := s.db.QueryRow(`SELECT COUNT(*), COUNT(DISTINCT root_key), COUNT(DISTINCT method)
		FROM snapshots WHERE id IN (?, ?)`, a, b).Scan(&found, &roots, &methods)
	if err != nil {
		return err
	}
	want := 2
	if a == b {
		want = 1
	}
	if found != want {
		return errors.New("snapshot not found")
	}
	if roots != 1 {
		return errors.New("snapshots are of different folders")
	}
	if methods != 1 {
		return errors.New("a full scan cannot be compared with a normal scan")
	}
	return nil
}

type entry struct {
	path  string
	isDir bool
	alloc int64
}

// Compare lists the entries directly inside the folder parent ("" for the
// root) in both snapshots, largest change first.
func (s *Store) Compare(before, after int64, parent string) ([]DiffRow, error) {
	if err := s.sameRoot(before, after); err != nil {
		return nil, err
	}
	parent = strings.ToLower(parent)
	load := func(id int64) (map[string]entry, map[string]bool, error) {
		rows, err := s.db.Query(`SELECT e.key, e.path, e.is_dir, e.alloc,
			EXISTS (SELECT 1 FROM entries c WHERE c.snapshot_id = e.snapshot_id AND c.parent = e.key)
			FROM entries e WHERE e.snapshot_id = ? AND e.parent = ? AND e.key <> ''`, id, parent)
		if err != nil {
			return nil, nil, err
		}
		defer rows.Close()
		m, kids := map[string]entry{}, map[string]bool{}
		for rows.Next() {
			var k string
			var e entry
			var has bool
			if err := rows.Scan(&k, &e.path, &e.isDir, &e.alloc, &has); err != nil {
				return nil, nil, err
			}
			m[k] = e
			kids[k] = has
		}
		return m, kids, rows.Err()
	}
	b, bKids, err := load(before)
	if err != nil {
		return nil, err
	}
	a, aKids, err := load(after)
	if err != nil {
		return nil, err
	}

	out := make([]DiffRow, 0, len(a)+len(b))
	for k, e := range a {
		r := DiffRow{Key: k, Path: e.path, IsDir: e.isDir, After: e.alloc, AfterKnown: true, HasChildren: aKids[k] || bKids[k]}
		if old, ok := b[k]; ok {
			r.Before, r.BeforeKnown = old.alloc, true
		}
		out = append(out, r)
	}
	for k, e := range b {
		if _, ok := a[k]; !ok {
			out = append(out, DiffRow{Key: k, Path: e.path, IsDir: e.isDir, Before: e.alloc, BeforeKnown: true, HasChildren: bKids[k]})
		}
	}
	sortByChange(out)
	return out, nil
}

func sortByChange(rows []DiffRow) {
	abs := func(x int64) int64 {
		if x < 0 {
			return -x
		}
		return x
	}
	sort.Slice(rows, func(i, j int) bool {
		di, dj := abs(rows[i].Delta()), abs(rows[j].Delta())
		if di != dj {
			return di > dj
		}
		return rows[i].Key < rows[j].Key
	})
}

// Hotspots returns where space changed by at least minDelta. An entry is a
// hotspot only if no single child accounts for half or more of its change,
// so the result names the place where growth happened, not its parents.
func (s *Store) Hotspots(before, after int64, minDelta int64, limit int) ([]DiffRow, error) {
	if err := s.sameRoot(before, after); err != nil {
		return nil, err
	}
	load := func(id int64) (map[string]entry, error) {
		rows, err := s.db.Query(`SELECT key, path, is_dir, alloc FROM entries WHERE snapshot_id = ?`, id)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		m := map[string]entry{}
		for rows.Next() {
			var k string
			var e entry
			if err := rows.Scan(&k, &e.path, &e.isDir, &e.alloc); err != nil {
				return nil, err
			}
			m[k] = e
		}
		return m, rows.Err()
	}
	b, err := load(before)
	if err != nil {
		return nil, err
	}
	a, err := load(after)
	if err != nil {
		return nil, err
	}

	all := map[string]DiffRow{}
	for k, e := range a {
		r := DiffRow{Key: k, Path: e.path, IsDir: e.isDir, After: e.alloc, AfterKnown: true}
		if old, ok := b[k]; ok {
			r.Before, r.BeforeKnown = old.alloc, true
		}
		all[k] = r
	}
	for k, e := range b {
		if _, ok := all[k]; !ok {
			all[k] = DiffRow{Key: k, Path: e.path, IsDir: e.isDir, Before: e.alloc, BeforeKnown: true}
		}
	}
	// The biggest same-direction change among each entry's children.
	maxChild := map[string]int64{}
	for k, r := range all {
		if k == "" {
			continue
		}
		p := parentKey(k)
		d := r.Delta()
		if pd := all[p].Delta(); (pd > 0) != (d > 0) {
			continue
		}
		if d < 0 {
			d = -d
		}
		if d > maxChild[p] {
			maxChild[p] = d
		}
	}

	var out []DiffRow
	for k, r := range all {
		d := r.Delta()
		if d < 0 {
			d = -d
		}
		if d < minDelta || maxChild[k]*2 >= d {
			continue
		}
		r.HasChildren = maxChild[k] > 0
		out = append(out, r)
	}
	sortByChange(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
