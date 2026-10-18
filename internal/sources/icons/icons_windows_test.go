package icons

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"
)

// Proves: the Win32 struct mirrors have the sizes Windows expects on 64-bit
// (ICONINFO 32, BITMAP 32, BITMAPINFOHEADER 40); a wrong layout would make
// the API calls write outside the Go structs.
func TestStructLayouts(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("layouts checked for 64-bit only")
	}
	if got := unsafe.Sizeof(iconInfo{}); got != 32 {
		t.Errorf("iconInfo size = %d, want 32", got)
	}
	if got := unsafe.Sizeof(bitmap{}); got != 32 {
		t.Errorf("bitmap size = %d, want 32", got)
	}
	if got := unsafe.Sizeof(bitmapInfoHeader{}); got != 40 {
		t.Errorf("bitmapInfoHeader size = %d, want 40", got)
	}
}

// Proves: a real system icon comes back as a decodable, non-empty PNG data
// URI, and the same request is answered from the cache.
func TestDataURIFromSystemFile(t *testing.T) {
	path := filepath.Join(os.Getenv("SystemRoot"), "System32", "shell32.dll")
	uri, err := DataURI(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := strings.CutPrefix(uri, "data:image/png;base64,")
	if !ok {
		t.Fatalf("not a PNG data URI: %.40s", uri)
	}
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	visible := false
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y && !visible; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a != 0 {
				visible = true
				break
			}
		}
	}
	if !visible {
		t.Error("icon is fully transparent")
	}
	if again, err := DataURI(path, 0); err != nil || again != uri {
		t.Errorf("second call differs or failed: %v", err)
	}
}

// Proves: missing files and files that are not icon sources fail with an
// error instead of a blank icon or a crash.
func TestDataURIFailures(t *testing.T) {
	dir := t.TempDir()
	txt := filepath.Join(dir, "a.txt")
	os.WriteFile(txt, []byte("x"), 0o600)
	for _, p := range []string{filepath.Join(dir, "missing.exe"), txt, "relative.exe", ""} {
		if uri, err := DataURI(p, 0); err == nil || uri != "" {
			t.Errorf("DataURI(%q) = %.30q, %v; want an error", p, uri, err)
		}
	}
}
