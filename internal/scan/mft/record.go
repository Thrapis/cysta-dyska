// Package mft reads the NTFS master file table to build a scan tree much
// faster than walking directories. Reading the MFT needs administrator
// rights, so it runs in a separate elevated helper (see internal/fastscan).
//
// The volume is only ever opened for reading. Layouts follow the NTFS 3.x
// structures documented on learn.microsoft.com (FILE_RECORD_SEGMENT_HEADER,
// ATTRIBUTE_RECORD_HEADER, MULTI_SECTOR_HEADER).
package mft

import (
	"encoding/binary"
	"errors"
	"fmt"
	"unicode/utf16"
)

// Attribute type codes.
const (
	attrAttributeList = 0x20
	attrFileName      = 0x30
	attrData          = 0x80
	attrReparsePoint  = 0xC0
	attrEnd           = 0xFFFFFFFF
)

// Record header flags.
const (
	recordInUse     = 0x0001
	recordDirectory = 0x0002
)

// Attribute flags.
const (
	attrFlagCompressed = 0x00FF
	attrFlagSparse     = 0x8000
)

// ioReparseTagWOF marks a file compressed by the Windows Overlay Filter
// (CompactOS, driver store): its data is in the wofStream named stream.
const (
	ioReparseTagWOF = 0x80000017
	wofStream       = "WofCompressedData"
)

// $FILE_NAME namespaces. A DOS name is an extra 8.3 alias of another name.
const namespaceDOS = 2

// sequenceStride is the size of each block protected by the update
// sequence array, independent of the disk's sector size.
const sequenceStride = 512

// ErrNotInUse marks a free record slot; it is not corruption.
var ErrNotInUse = errors.New("record not in use")

// Name is one $FILE_NAME of a file: a directory entry pointing at it.
type Name struct {
	Parent    uint64 // record number of the containing directory
	ParentSeq uint16 // must match the parent's sequence number
	Name      string
	Namespace uint8
}

// Record is what the scan needs from one file record segment.
type Record struct {
	Num   uint64
	Seq   uint16
	Base  uint64 // base record number for extension records, else 0
	Dir   bool
	Names []Name
	// HasData is set when this segment holds the start of the unnamed
	// $DATA stream (LowestVcn 0), which carries the sizes.
	HasData     bool
	Size, Alloc int64
	ReparseTag  uint32
	// WofAlloc is the disk space of the WofCompressedData stream.
	WofAlloc int64
	// AttrList is set when the file's attributes span several segments.
	AttrList bool
	// Runs is the unnamed $DATA run list. Only filled when asked for (the
	// $MFT itself, to find where the rest of the table is).
	Runs []Run
}

// Run is a stretch of clusters: Length clusters starting at LCN. Sparse
// runs (LCN < 0) take no disk space.
type Run struct {
	VCN, LCN, Length int64
}

// le is shorthand for little-endian reads that never panic: out-of-range
// reads are caught by the bounds checks before them.
var le = binary.LittleEndian

// applyFixups checks and undoes the update sequence protection in place. A
// mismatch means the record was torn by an interrupted write.
func applyFixups(buf []byte) error {
	if len(buf) < 8 {
		return errors.New("record too short")
	}
	ofs := int(le.Uint16(buf[4:]))
	count := int(le.Uint16(buf[6:]))
	if count < 1 || ofs+2*count > len(buf) || (count-1)*sequenceStride > len(buf) {
		return errors.New("bad update sequence array")
	}
	usn := le.Uint16(buf[ofs:])
	for i := 1; i < count; i++ {
		end := i*sequenceStride - 2
		if le.Uint16(buf[end:]) != usn {
			return errors.New("torn record")
		}
		copy(buf[end:end+2], buf[ofs+2*i:ofs+2*i+2])
	}
	return nil
}

// Parse decodes the file record segment num from buf, which is modified
// (fixups are applied in place). wantRuns also decodes the $DATA run list.
func Parse(buf []byte, num uint64, wantRuns bool) (Record, error) {
	r := Record{Num: num}
	if len(buf) < 0x30 || string(buf[:4]) != "FILE" {
		if len(buf) >= 4 && le.Uint32(buf) == 0 {
			return r, ErrNotInUse // never-used slot
		}
		return r, errors.New("no FILE signature")
	}
	if err := applyFixups(buf); err != nil {
		return r, err
	}
	flags := le.Uint16(buf[0x16:])
	if flags&recordInUse == 0 {
		return r, ErrNotInUse
	}
	r.Seq = le.Uint16(buf[0x10:])
	r.Dir = flags&recordDirectory != 0
	r.Base = le.Uint64(buf[0x20:]) & (1<<48 - 1)

	used := min(int(le.Uint32(buf[0x18:])), len(buf))
	off := int(le.Uint16(buf[0x14:]))
	for {
		if off+8 > used {
			return r, errors.New("attributes run past the record")
		}
		typ := le.Uint32(buf[off:])
		if typ == attrEnd {
			return r, nil
		}
		length := int(le.Uint32(buf[off+4:]))
		if length < 0x18 || off+length > used {
			return r, fmt.Errorf("bad attribute length %d", length)
		}
		if err := r.parseAttr(buf[off:off+length], typ, wantRuns); err != nil {
			return r, err
		}
		off += length
	}
}

func (r *Record) parseAttr(a []byte, typ uint32, wantRuns bool) error {
	nonresident := a[8] != 0
	unnamed := a[9] == 0
	flags := le.Uint16(a[0x0C:])

	var value []byte
	if !nonresident {
		vlen, vofs := int(le.Uint32(a[0x10:])), int(le.Uint16(a[0x14:]))
		if vofs+vlen > len(a) {
			return errors.New("resident value runs past its attribute")
		}
		value = a[vofs : vofs+vlen]
	} else if len(a) < 0x40 {
		return errors.New("nonresident attribute too short")
	}

	switch typ {
	case attrAttributeList:
		r.AttrList = true
	case attrFileName:
		if nonresident || len(value) < 0x42 {
			return errors.New("bad $FILE_NAME")
		}
		n := int(value[0x40])
		if 0x42+2*n > len(value) {
			return errors.New("$FILE_NAME name runs past its value")
		}
		u := make([]uint16, n)
		for i := range u {
			u[i] = le.Uint16(value[0x42+2*i:])
		}
		ref := le.Uint64(value)
		r.Names = append(r.Names, Name{
			Parent: ref & (1<<48 - 1), ParentSeq: uint16(ref >> 48),
			Name: string(utf16.Decode(u)), Namespace: value[0x41],
		})
	case attrData:
		if !unnamed {
			// Alternate data streams are not counted, as in the walker,
			// except where a WOF-compressed file keeps its data.
			if attrName(a) == wofStream {
				if !nonresident {
					r.WofAlloc = (int64(len(value)) + 7) &^ 7
				} else if le.Uint64(a[0x10:]) == 0 {
					r.WofAlloc = nonresidentAlloc(a, flags)
				}
			}
			return nil
		}
		if !nonresident {
			r.HasData = true
			r.Size = int64(len(value))
			r.Alloc = (r.Size + 7) &^ 7 // what NTFS reports for resident data
			return nil
		}
		if lowest := int64(le.Uint64(a[0x10:])); lowest == 0 {
			r.HasData = true
			r.Alloc = nonresidentAlloc(a, flags)
			r.Size = int64(le.Uint64(a[0x30:]))
		}
		if wantRuns {
			runs, err := decodeRuns(a)
			if err != nil {
				return err
			}
			r.Runs = append(r.Runs, runs...)
		}
	case attrReparsePoint:
		if len(value) >= 4 {
			r.ReparseTag = le.Uint32(value)
		}
	}
	return nil
}

// nonresidentAlloc is the disk space of a nonresident attribute that starts
// at LowestVcn 0. Compressed and sparse streams also store the clusters
// really in use (TotalAllocated); that is their size on disk.
func nonresidentAlloc(a []byte, flags uint16) int64 {
	if flags&(attrFlagCompressed|attrFlagSparse) != 0 && len(a) >= 0x48 {
		return int64(le.Uint64(a[0x40:]))
	}
	return int64(le.Uint64(a[0x28:]))
}

// attrName returns an attribute's name, or "" if it is unnamed or damaged.
func attrName(a []byte) string {
	n, ofs := int(a[9]), int(le.Uint16(a[0x0A:]))
	if n == 0 || ofs+2*n > len(a) {
		return ""
	}
	u := make([]uint16, n)
	for i := range u {
		u[i] = le.Uint16(a[ofs+2*i:])
	}
	return string(utf16.Decode(u))
}

// decodeRuns decodes the mapping pairs of a nonresident attribute, as
// described for ATTRIBUTE_RECORD_HEADER.
func decodeRuns(a []byte) ([]Run, error) {
	vcn := int64(le.Uint64(a[0x10:]))
	p := int(le.Uint16(a[0x20:]))
	var lcn int64
	var runs []Run
	for p < len(a) {
		head := a[p]
		if head == 0 {
			return runs, nil
		}
		v, l := int(head&0x0F), int(head>>4)
		p++
		if v == 0 || v > 8 || l > 8 || p+v+l > len(a) {
			return nil, errors.New("bad mapping pairs")
		}
		length := signed(a[p : p+v])
		p += v
		run := Run{VCN: vcn, Length: length, LCN: -1}
		if l > 0 {
			lcn += signed(a[p : p+l])
			run.LCN = lcn
		}
		p += l
		if length <= 0 {
			return nil, errors.New("bad run length")
		}
		runs = append(runs, run)
		vcn += length
	}
	return nil, errors.New("mapping pairs not terminated")
}

// signed reads a little-endian, sign-extended integer of 1 to 8 bytes.
func signed(b []byte) int64 {
	var v int64
	for i := len(b) - 1; i >= 0; i-- {
		v = v<<8 | int64(b[i])
	}
	shift := uint(64 - 8*len(b))
	return v << shift >> shift
}
