package scan

import (
	"errors"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// fileIDBothDirInfo mirrors FILE_ID_BOTH_DIR_INFO (winbase.h).
// Layout is checked by TestDirInfoLayout.
type fileIDBothDirInfo struct {
	NextEntryOffset uint32
	FileIndex       uint32
	CreationTime    int64
	LastAccessTime  int64
	LastWriteTime   int64
	ChangeTime      int64
	EndOfFile       int64
	AllocationSize  int64
	FileAttributes  uint32
	FileNameLength  uint32 // bytes, not characters
	EaSize          uint32 // holds the reparse tag for reparse points
	ShortNameLength uint8
	ShortName       [12]uint16
	FileId          uint64
	FileName        [1]uint16
}

// dirBufferSize is the batch size for one enumeration call. 64 KiB is the
// largest size that also works on network shares.
const dirBufferSize = 64 << 10

// dirReader reads directories with GetFileInformationByHandleEx, which
// returns many entries per call along with their allocated size and file
// ID. Each worker owns one reader and its buffer.
type dirReader struct {
	buf []uint64 // uint64 so the buffer is 8-byte aligned as the API requires
}

func newDirReader() *dirReader {
	return &dirReader{buf: make([]uint64, dirBufferSize/8)}
}

func (r *dirReader) readDir(path string) ([]entry, error) {
	p, err := windows.UTF16PtrFromString(extendedPath(path))
	if err != nil {
		return nil, err
	}
	// FILE_LIST_DIRECTORY needs no read access to file contents, and the
	// share flags never block other programs using these files.
	h, err := windows.CreateFile(p,
		windows.FILE_LIST_DIRECTORY,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(h)

	buf := unsafe.Slice((*byte)(unsafe.Pointer(&r.buf[0])), dirBufferSize)
	var out []entry
	class := uint32(windows.FileIdBothDirectoryRestartInfo)
	for {
		err := windows.GetFileInformationByHandleEx(h, class, &buf[0], uint32(len(buf)))
		if errors.Is(err, windows.ERROR_NO_MORE_FILES) || errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		class = windows.FileIdBothDirectoryInfo

		for off := uint32(0); ; {
			info := (*fileIDBothDirInfo)(unsafe.Pointer(&buf[off]))
			name := windows.UTF16ToString(unsafe.Slice(&info.FileName[0], info.FileNameLength/2))
			if name != "." && name != ".." {
				e := entry{
					name:   name,
					isDir:  info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0,
					size:   info.EndOfFile,
					alloc:  info.AllocationSize,
					fileID: info.FileId,
				}
				if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
					e.reparseTag = info.EaSize
				}
				out = append(out, e)
			}
			if info.NextEntryOffset == 0 {
				break
			}
			off += info.NextEntryOffset
		}
	}
}

// extendedPath adds the \\?\ prefix so paths longer than MAX_PATH work.
// path must be absolute and clean.
func extendedPath(path string) string {
	switch {
	case strings.HasPrefix(path, `\\?\`):
		return path
	case strings.HasPrefix(path, `\\`):
		return `\\?\UNC\` + path[2:]
	default:
		return `\\?\` + path
	}
}
