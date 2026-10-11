//go:build !windows

package scan

import (
	"io/fs"
	"os"
)

// dirReader is a portable fallback so the package builds and its portable
// tests run off Windows. It reports logical size as on-disk size and does
// not detect hardlinks.
type dirReader struct{}

func newDirReader() *dirReader { return &dirReader{} }

func (r *dirReader) readDir(path string) ([]entry, error) {
	des, err := os.ReadDir(path)
	out := make([]entry, 0, len(des))
	for _, d := range des {
		e := entry{name: d.Name(), isDir: d.IsDir()}
		if d.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			e.reparseTag = 0x20000000 // treat like a name surrogate: not followed
		} else if !e.isDir {
			if info, ierr := d.Info(); ierr == nil {
				e.size, e.alloc = info.Size(), info.Size()
			}
		}
		out = append(out, e)
	}
	return out, err
}
