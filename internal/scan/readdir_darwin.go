//go:build darwin

package scan

import (
	"io/fs"
	"os"
	"syscall"
)

// dirReader reads directories with os.ReadDir and lstat. On-disk size is
// st_blocks*512 (APFS clones and sparse files are therefore counted by what
// they allocate), and hardlinks are keyed by device+inode.
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
				if st, ok := info.Sys().(*syscall.Stat_t); ok {
					e.alloc = st.Blocks * 512
					e.fileID = hardlinkID(uint64(st.Dev), st.Ino, uint64(st.Nlink))
				}
			}
		}
		out = append(out, e)
	}
	return out, err
}
