//go:build windows

package mft

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

// fsctlGetNTFSVolumeData is FSCTL_GET_NTFS_VOLUME_DATA (winioctl.h):
// CTL_CODE(FILE_DEVICE_FILE_SYSTEM, 25, METHOD_BUFFERED, FILE_ANY_ACCESS).
// It only reads volume information.
const fsctlGetNTFSVolumeData = 0x00090064

// volumeData holds the NTFS_VOLUME_DATA_BUFFER fields we use, followed by
// NTFS_EXTENDED_VOLUME_DATA (major/minor version).
type volumeData struct {
	bytesPerCluster    int64
	bytesPerRecord     int64
	mftValidDataLength int64
	mftStartLCN        int64
	major, minor       uint16
}

// parseVolumeData decodes the FSCTL output; offsets follow the documented
// NTFS_VOLUME_DATA_BUFFER layout (checked by TestParseVolumeData).
func parseVolumeData(b []byte) (volumeData, error) {
	if len(b) < 104 {
		return volumeData{}, errors.New("short NTFS volume data")
	}
	v := volumeData{
		bytesPerCluster:    int64(binary.LittleEndian.Uint32(b[44:])),
		bytesPerRecord:     int64(binary.LittleEndian.Uint32(b[48:])),
		mftValidDataLength: int64(binary.LittleEndian.Uint64(b[56:])),
		mftStartLCN:        int64(binary.LittleEndian.Uint64(b[64:])),
		major:              binary.LittleEndian.Uint16(b[100:]),
		minor:              binary.LittleEndian.Uint16(b[102:]),
	}
	if v.major != 3 {
		return v, fmt.Errorf("NTFS version %d.%d is not supported", v.major, v.minor)
	}
	if v.bytesPerCluster <= 0 || v.bytesPerRecord < 1024 || v.bytesPerRecord > 65536 {
		return v, errors.New("unexpected NTFS geometry")
	}
	return v, nil
}

// ErrFragmentedMFT means the $MFT's own run list does not fit in its first
// record. Rare; the normal scan is used instead.
var ErrFragmentedMFT = errors.New("the master file table is too fragmented for the fast scan")

var driveRe = regexp.MustCompile(`^[A-Za-z]:$`)

const chunkSize = 4 << 20

// openVolume opens drive for reading only. flags may add
// FILE_FLAG_NO_BUFFERING: reads then bypass the file cache (much faster for
// one pass over gigabytes) but need sector-aligned offsets, sizes and memory.
func openVolume(drive string, flags uint32) (*os.File, windows.Handle, error) {
	path, err := windows.UTF16PtrFromString(`\\.\` + drive)
	if err != nil {
		return nil, 0, err
	}
	h, err := windows.CreateFile(path, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, flags, 0)
	if err != nil {
		return nil, 0, fmt.Errorf("open %s: %w", drive, err)
	}
	return os.NewFile(uintptr(h), drive), h, nil
}

// ReadVolume reads the whole MFT of drive (e.g. "C:") into a Tree. It needs
// administrator rights. The volume is opened with GENERIC_READ only. Several
// workers, each with its own handle, read and parse chunks in parallel, so
// the disk gets several requests at once.
func ReadVolume(ctx context.Context, drive string) (*Tree, error) {
	if !driveRe.MatchString(drive) {
		return nil, fmt.Errorf("not a drive: %q", drive)
	}
	openStart := time.Now()
	vol, h, err := openVolume(drive, 0)
	if err != nil {
		return nil, err
	}
	defer vol.Close()

	out := make([]byte, 128)
	var n uint32
	if err := windows.DeviceIoControl(h, fsctlGetNTFSVolumeData, nil, 0,
		&out[0], uint32(len(out)), &n, nil); err != nil {
		return nil, fmt.Errorf("%s is not an NTFS volume: %w", drive, err)
	}
	vd, err := parseVolumeData(out[:n])
	if err != nil {
		return nil, err
	}

	// Record 0 is $MFT itself; its $DATA runs say where the table is.
	first := make([]byte, roundUp(vd.bytesPerRecord, vd.bytesPerCluster))
	if _, err := vol.ReadAt(first, vd.mftStartLCN*vd.bytesPerCluster); err != nil {
		return nil, fmt.Errorf("read $MFT: %w", err)
	}
	self, err := Parse(first[:vd.bytesPerRecord], 0, true)
	if err != nil {
		return nil, fmt.Errorf("$MFT record: %w", err)
	}
	if self.AttrList {
		return nil, ErrFragmentedMFT
	}
	total := vd.mftValidDataLength / vd.bytesPerRecord * vd.bytesPerRecord
	chunks, err := planChunks(self.Runs, vd.bytesPerCluster, vd.bytesPerRecord, total, chunkSize)
	if err != nil {
		return nil, err
	}

	t := NewTree()
	t.grow(uint64(total / vd.bytesPerRecord)) // workers then only write their own slots
	t.Open = time.Since(openStart)
	t.Workers = min(runtime.NumCPU(), 8)

	type result struct {
		ext          []Record // extension records, merged after the workers
		records, bad int
		bytes        int64
		read, parse  time.Duration
		err          error
	}
	results := make([]result, t.Workers)
	work := make(chan chunk)
	var wg sync.WaitGroup
	for w := range results {
		wg.Add(1)
		go func(res *result) {
			defer wg.Done()
			f, _, err := openVolume(drive, windows.FILE_FLAG_NO_BUFFERING)
			if err != nil {
				res.err = err
				for range work { // keep the feeder from blocking
				}
				return
			}
			defer f.Close()
			// Chunk extents are cluster-aligned, so with an aligned buffer
			// every read meets the unbuffered I/O rules.
			buf := alignedBuffer(chunkSize + vd.bytesPerCluster)
			for c := range work {
				if res.err != nil || ctx.Err() != nil {
					continue
				}
				readStart := time.Now()
				var pos int64
				for _, p := range c.pieces {
					if _, err := f.ReadAt(buf[pos:pos+p.length], p.offset); err != nil {
						res.err = fmt.Errorf("read MFT at %d: %w", p.offset, err)
						break
					}
					pos += p.length
				}
				res.read += time.Since(readStart)
				res.bytes += pos
				if res.err != nil {
					continue
				}
				parseStart := time.Now()
				for off := int64(0); off < c.size; off += vd.bytesPerRecord {
					num := uint64((c.mftOffset + off) / vd.bytesPerRecord)
					res.records++
					r, err := Parse(buf[off:off+vd.bytesPerRecord], num, false)
					switch {
					case err == nil && r.Base != 0:
						res.ext = append(res.ext, r)
					case err == nil:
						t.Add(r) // a base record: only its own slot
					case !errors.Is(err, ErrNotInUse):
						res.bad++
					}
				}
				res.parse += time.Since(parseStart)
			}
		}(&results[w])
	}
	for _, c := range chunks {
		work <- c
	}
	close(work)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	for _, r := range results {
		if r.err != nil {
			return nil, r.err
		}
		for _, e := range r.ext {
			t.Add(e)
		}
		t.Records += r.records
		t.Bad += r.bad
		t.Bytes += r.bytes
		t.Read += r.read
		t.Parse += r.parse
	}
	return t, nil
}
