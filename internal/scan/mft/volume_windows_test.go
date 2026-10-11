//go:build windows

package mft

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cysta-dyska/internal/scan"
)

// Proves: NTFS_VOLUME_DATA_BUFFER fields are read from their documented
// offsets (8-byte fields, then four DWORDs from offset 40), and the extended
// version that follows at offset 96; unsupported versions are refused.
func TestParseVolumeData(t *testing.T) {
	b := make([]byte, 104)
	binary.LittleEndian.PutUint32(b[40:], 512)    // BytesPerSector
	binary.LittleEndian.PutUint32(b[44:], 4096)   // BytesPerCluster
	binary.LittleEndian.PutUint32(b[48:], 1024)   // BytesPerFileRecordSegment
	binary.LittleEndian.PutUint64(b[56:], 1<<30)  // MftValidDataLength
	binary.LittleEndian.PutUint64(b[64:], 786432) // MftStartLcn
	binary.LittleEndian.PutUint32(b[96:], 8)      // ByteCount
	binary.LittleEndian.PutUint16(b[100:], 3)     // MajorVersion
	binary.LittleEndian.PutUint16(b[102:], 1)     // MinorVersion
	v, err := parseVolumeData(b)
	if err != nil {
		t.Fatal(err)
	}
	if v.bytesPerCluster != 4096 || v.bytesPerRecord != 1024 || v.mftValidDataLength != 1<<30 || v.mftStartLCN != 786432 {
		t.Errorf("parsed %+v", v)
	}
	binary.LittleEndian.PutUint16(b[100:], 4)
	if _, err := parseVolumeData(b); err == nil {
		t.Error("NTFS 4.x accepted")
	}
}

// Proves: only drive letters are accepted, so the helper cannot be pointed
// at a file or a device path.
func TestReadVolumeRejectsNonDrives(t *testing.T) {
	for _, d := range []string{`C:\`, `\.\PhysicalDrive0`, `C:\Windows`, "", "CC:"} {
		if _, err := ReadVolume(context.Background(), d); err == nil || !strings.Contains(err.Error(), "not a drive") {
			t.Errorf("%q: %v", d, err)
		}
	}
}

// Proves (opt-in, needs an elevated shell): the fast scan of a real folder
// matches the directory walker exactly: same entries, sizes on disk and
// hardlink handling.
//
//	CYSTA_MFT_TEST=1 go test -run MFTMatchesWalker ./internal/scan/mft/
func TestMFTMatchesWalker(t *testing.T) {
	if os.Getenv("CYSTA_MFT_TEST") == "" {
		t.Skip("set CYSTA_MFT_TEST=1 in an elevated shell")
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "a", "b"), 0o755)
	os.WriteFile(filepath.Join(dir, "a", "small.txt"), []byte("hi"), 0o644)
	os.WriteFile(filepath.Join(dir, "a", "b", "big.bin"), make([]byte, 3<<20), 0o644)
	os.Link(filepath.Join(dir, "a", "b", "big.bin"), filepath.Join(dir, "linked.bin"))

	drive := filepath.VolumeName(dir)
	tr, err := ReadVolume(context.Background(), drive)
	if err != nil {
		t.Fatal(err)
	}
	flat, err := tr.Flat(drive + `\`)
	if err != nil {
		t.Fatal(err)
	}
	full, err := scan.Build(flat)
	if err != nil {
		t.Fatal(err)
	}
	fast := full.Find(dir)
	slow, err := scan.Scan(context.Background(), dir, scan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if fast == nil {
		t.Fatalf("%s not found in the MFT tree", dir)
	}
	var compare func(a, b *scan.Node)
	compare = func(a, b *scan.Node) {
		if a.Alloc != b.Alloc || a.Size != b.Size || a.Files != b.Files || len(a.Children) != len(b.Children) {
			t.Errorf("%s: fast %d/%d/%d files/%d kids, walker %d/%d/%d files/%d kids", b.Path(),
				a.Alloc, a.Size, a.Files, len(a.Children), b.Alloc, b.Size, b.Files, len(b.Children))
			return
		}
		for i := range a.Children {
			compare(a.Children[i], b.Children[i])
		}
	}
	compare(fast, slow)
}
