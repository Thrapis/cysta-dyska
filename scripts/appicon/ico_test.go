package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"testing"
)

// Proves: encodeICO writes a valid ICONDIR (reserved 0, type 1, count) and one entry per frame
// whose size byte (0 for 256) and offset/length point at a decodable PNG of that size.
func TestEncodeICO(t *testing.T) {
	sizes := []int{16, 32, 256}
	var frames []image.Image
	for _, s := range sizes {
		frames = append(frames, image.NewNRGBA(image.Rect(0, 0, s, s)))
	}
	var buf bytes.Buffer
	if err := encodeICO(&buf, frames); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	le := binary.LittleEndian
	if le.Uint16(b[0:]) != 0 || le.Uint16(b[2:]) != 1 || int(le.Uint16(b[4:])) != len(sizes) {
		t.Fatalf("bad header % x", b[:6])
	}
	for i, s := range sizes {
		e := b[6+16*i:]
		want := byte(s)
		if s == 256 {
			want = 0
		}
		if e[0] != want || e[1] != want {
			t.Errorf("entry %d: size bytes %d,%d want %d", i, e[0], e[1], want)
		}
		if le.Uint16(e[4:]) != 1 || le.Uint16(e[6:]) != 32 {
			t.Errorf("entry %d: planes/bpp %d/%d", i, le.Uint16(e[4:]), le.Uint16(e[6:]))
		}
		n, off := le.Uint32(e[8:]), le.Uint32(e[12:])
		img, err := png.Decode(bytes.NewReader(b[off : off+n]))
		if err != nil {
			t.Fatalf("entry %d: %v", i, err)
		}
		if img.Bounds().Dx() != s || img.Bounds().Dy() != s {
			t.Errorf("entry %d: decoded %v, want %dx%d", i, img.Bounds(), s, s)
		}
	}
}

// Proves: the icon is opaque in the middle (the ball) and transparent in the corners
// (outside the rounded square), at both a small and a large size.
func TestRenderShape(t *testing.T) {
	for _, s := range []int{16, 256} {
		img := render(s)
		if img.Bounds().Dx() != s {
			t.Fatalf("size %d: got %v", s, img.Bounds())
		}
		if a := img.NRGBAAt(s/2, s/2).A; a != 255 {
			t.Errorf("size %d: centre alpha %d, want 255", s, a)
		}
		if a := img.NRGBAAt(0, 0).A; a != 0 {
			t.Errorf("size %d: corner alpha %d, want 0", s, a)
		}
	}
}
