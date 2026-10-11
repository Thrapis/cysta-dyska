package mft

import "testing"

// Proves: the MFT byte stream is cut into record-aligned chunks that map to
// the right disk positions, including a chunk that spans two runs (the MFT
// is fragmented) and a last chunk that ends at the valid data length; reads
// stay cluster-aligned.
func TestPlanChunks(t *testing.T) {
	const cluster, record = 4096, 1024
	runs := []Run{
		{VCN: 0, LCN: 100, Length: 3}, // MFT bytes [0, 12288) at disk 409600
		{VCN: 3, LCN: 900, Length: 4}, // MFT bytes [12288, 28672) at disk 3686400
	}
	total := int64(26 * record) // 26624: ends inside the second run's last cluster
	chunks, err := planChunks(runs, cluster, record, total, 8192)
	if err != nil {
		t.Fatal(err)
	}
	want := []chunk{
		{mftOffset: 0, size: 8192, pieces: []extent{{409600, 8192}}},
		{mftOffset: 8192, size: 8192, pieces: []extent{{409600 + 8192, 4096}, {3686400, 4096}}},
		{mftOffset: 16384, size: 8192, pieces: []extent{{3686400 + 4096, 8192}}},
		{mftOffset: 24576, size: 2048, pieces: []extent{{3686400 + 12288, 4096}}}, // read a whole cluster
	}
	if len(chunks) != len(want) {
		t.Fatalf("chunks = %+v", chunks)
	}
	for i := range want {
		c, w := chunks[i], want[i]
		if c.mftOffset != w.mftOffset || c.size != w.size || len(c.pieces) != len(w.pieces) {
			t.Fatalf("chunk %d = %+v, want %+v", i, c, w)
		}
		for j := range w.pieces {
			if c.pieces[j] != w.pieces[j] {
				t.Errorf("chunk %d piece %d = %+v, want %+v", i, j, c.pieces[j], w.pieces[j])
			}
			if c.pieces[j].offset%cluster != 0 || c.pieces[j].length%cluster != 0 {
				t.Errorf("chunk %d piece %d is not cluster-aligned", i, j)
			}
		}
	}
}

// Proves: an MFT with a sparse run, or whose runs end before its valid data
// length, is refused instead of being read from wrong disk positions.
func TestPlanChunksRefusesBadRuns(t *testing.T) {
	if _, err := planChunks([]Run{{0, -1, 4}}, 4096, 1024, 4096, 8192); err == nil {
		t.Error("sparse run accepted")
	}
	if _, err := planChunks([]Run{{0, 10, 1}}, 4096, 1024, 8192, 8192); err == nil {
		t.Error("runs shorter than the table accepted")
	}
}

// Proves: buffers for unbuffered volume reads start on a 4 KiB boundary
// (FILE_FLAG_NO_BUFFERING needs sector-aligned memory) and have the asked size.
func TestAlignedBuffer(t *testing.T) {
	for _, n := range []int64{4096, 1 << 20, 4<<20 + 4096} {
		b := alignedBuffer(n)
		if int64(len(b)) != n || addr(b)%4096 != 0 {
			t.Errorf("alignedBuffer(%d): len %d, address %% 4096 = %d", n, len(b), addr(b)%4096)
		}
	}
}
