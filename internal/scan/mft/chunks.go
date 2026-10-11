package mft

import (
	"errors"
	"reflect"
)

// extent is a stretch of the volume to read.
type extent struct {
	offset, length int64
}

// chunk is a record-aligned part of the MFT: size bytes starting at
// mftOffset, read from one or more extents (more than one when it spans a
// fragment boundary). The extents may read a little past size to stay
// cluster-aligned; those bytes are ignored.
type chunk struct {
	mftOffset, size int64
	pieces          []extent
}

// planChunks cuts the first total bytes of the MFT, stored in runs, into
// chunks of at most chunkSize bytes so several workers can read and parse
// them in parallel. chunkSize must be a multiple of the cluster and record
// sizes.
func planChunks(runs []Run, cluster, record, total, chunkSize int64) ([]chunk, error) {
	var chunks []chunk
	for start := int64(0); start < total; start += chunkSize {
		c := chunk{mftOffset: start, size: min(chunkSize, total-start)}
		// Bytes to read: whole clusters covering the chunk.
		pos, end := start, start+roundUp(c.size, cluster)
		for pos < end {
			e, n, ok := locate(runs, cluster, pos, end-pos)
			if !ok {
				return nil, errors.New("the MFT's run list does not cover its data")
			}
			if e.offset < 0 {
				return nil, errors.New("the MFT has a sparse run")
			}
			c.pieces = append(c.pieces, e)
			pos += n
		}
		chunks = append(chunks, c)
	}
	return chunks, nil
}

// locate maps MFT byte pos to a disk extent of up to want bytes, stopping
// at the end of the run that contains pos. A sparse run gives offset -1.
func locate(runs []Run, cluster, pos, want int64) (extent, int64, bool) {
	for _, r := range runs {
		lo, hi := r.VCN*cluster, (r.VCN+r.Length)*cluster
		if pos < lo || pos >= hi {
			continue
		}
		n := min(want, hi-pos)
		if r.LCN < 0 {
			return extent{-1, n}, n, true
		}
		return extent{r.LCN*cluster + (pos - lo), n}, n, true
	}
	return extent{}, 0, false
}

func roundUp(n, to int64) int64 { return (n + to - 1) / to * to }

// pageSize is the alignment used for unbuffered reads; it is a multiple of
// every sector size NTFS supports.
const pageSize = 4096

// alignedBuffer returns n bytes starting on a pageSize boundary, as
// FILE_FLAG_NO_BUFFERING requires. Go's heap does not move objects, so the
// alignment holds for the buffer's lifetime.
func alignedBuffer(n int64) []byte {
	b := make([]byte, n+pageSize)
	off := int64((pageSize - addr(b)%pageSize) % pageSize)
	return b[off : off+n : off+n]
}

// addr is the address of a slice's first byte.
func addr(b []byte) uintptr { return reflect.ValueOf(b).Pointer() }
