package app

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fakeEmitter records events instead of sending them to the Wails runtime.
type fakeEmitter struct {
	mu     sync.Mutex
	events []string
	data   []any
	done   chan struct{}
}

func newFakeEmitter() *fakeEmitter { return &fakeEmitter{done: make(chan struct{}, 1)} }

func (f *fakeEmitter) Emit(name string, data any) {
	f.mu.Lock()
	f.events = append(f.events, name)
	f.data = append(f.data, data)
	f.mu.Unlock()
	if name == EventDone || name == EventError || name == EventCancelled || name == EventDeclined {
		f.done <- struct{}{}
	}
}

func (f *fakeEmitter) last() (string, any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.events[len(f.events)-1], f.data[len(f.data)-1]
}

func (f *fakeEmitter) wait(t *testing.T) {
	t.Helper()
	select {
	case <-f.done:
	case <-time.After(10 * time.Second):
		t.Fatal("scan did not finish")
	}
}

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, n := range map[string]int{"a/1.bin": 30_000, "a/2.bin": 20_000, "b/3.bin": 10_000} {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// Proves: a scan runs in the background, finishes with a done event carrying
// the root summary, and Children then serves the tree level by level.
func TestScanThenBrowseChildren(t *testing.T) {
	em := newFakeEmitter()
	a := New(em, nil)
	root := fixture(t)

	if err := a.StartScan(root); err != nil {
		t.Fatal(err)
	}
	em.wait(t)

	name, data := em.last()
	if name != EventDone {
		t.Fatalf("last event = %s (%v), want %s", name, data, EventDone)
	}
	sum := data.(NodeDTO)
	if sum.Size != 60_000 || sum.Files != 3 || sum.Alloc < sum.Size {
		t.Errorf("root summary = %+v", sum)
	}

	kids, err := a.Children(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(kids) != 2 || kids[0].Name != "a" || kids[0].Size != 50_000 || !kids[0].HasChildren {
		t.Errorf("children(root) = %+v", kids)
	}

	grand, err := a.Children(filepath.Join(root, "a"))
	if err != nil {
		t.Fatal(err)
	}
	if len(grand) != 2 || grand[0].Name != "1.bin" || grand[0].HasChildren {
		t.Errorf("children(a) = %+v", grand)
	}
}

// Proves: Children before any scan is a clear error, not a nil-pointer panic.
func TestChildrenBeforeScan(t *testing.T) {
	a := New(newFakeEmitter(), nil)
	if _, err := a.Children(`C:\`); err == nil {
		t.Fatal("want error before any scan")
	}
}

// Proves: a second scan cannot start while one is running.
func TestStartScanRejectsConcurrentScan(t *testing.T) {
	em := newFakeEmitter()
	a := New(em, nil)
	root := fixture(t)

	a.mu.Lock()
	a.cancel = func() {} // simulate a scan in flight
	a.mu.Unlock()

	if err := a.StartScan(root); err == nil {
		t.Fatal("want error when a scan is already running")
	}
}

// Proves: a scan of a missing folder reports an error event.
func TestScanMissingFolderEmitsError(t *testing.T) {
	em := newFakeEmitter()
	a := New(em, nil)
	if err := a.StartScan(filepath.Join(t.TempDir(), "missing")); err != nil {
		t.Fatal(err)
	}
	em.wait(t)
	if name, _ := em.last(); name != EventError {
		t.Fatalf("last event = %s, want %s", name, EventError)
	}
}

// Proves: Children caps very large directories and reports what was hidden,
// so a folder with 100k files cannot flood the UI.
func TestChildrenCapsLargeDirectories(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < maxChildren+5; i++ {
		p := filepath.Join(root, "f"+itoa(i))
		if err := os.WriteFile(p, []byte{1}, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	em := newFakeEmitter()
	a := New(em, nil)
	if err := a.StartScan(root); err != nil {
		t.Fatal(err)
	}
	em.wait(t)

	kids, err := a.Children(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(kids) != maxChildren+1 {
		t.Fatalf("len = %d, want %d (cap + 1 summary row)", len(kids), maxChildren+1)
	}
	rest := kids[len(kids)-1]
	if !rest.IsRest || rest.Files != 5 || rest.Size != 5 {
		t.Errorf("summary row = %+v, want 5 files / 5 bytes", rest)
	}
}

func itoa(i int) string {
	return string([]byte{byte('0' + i/1000%10), byte('0' + i/100%10), byte('0' + i/10%10), byte('0' + i%10)})
}
