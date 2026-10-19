// Command appicon renders the app icon: a flat disco ball ("Čysta dyska" = "Pure Disco")
// with one slice pulled out like a disk-usage pie chart. Standard library only.
//
//	go run ./scripts/appicon                 # writes build/appicon.png and build/windows/icon.ico
//	go run ./scripts/appicon -preview <dir>  # also writes every ico size as a PNG into <dir>
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
)

var icoSizes = []int{16, 24, 32, 48, 64, 128, 256}

func main() {
	preview := flag.String("preview", "", "also write each ico size as PNG into this folder")
	flag.Parse()

	if err := writePNG("build/appicon.png", render(1024)); err != nil {
		log.Fatal(err)
	}
	var frames []image.Image
	for _, s := range icoSizes {
		img := render(s)
		frames = append(frames, img)
		if *preview != "" {
			if err := writePNG(filepath.Join(*preview, fmt.Sprintf("icon-%d.png", s)), img); err != nil {
				log.Fatal(err)
			}
		}
	}
	f, err := os.Create("build/windows/icon.ico")
	if err != nil {
		log.Fatal(err)
	}
	if err := encodeICO(f, frames); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// encodeICO writes frames as an ICO file with PNG-compressed entries (supported since Vista).
func encodeICO(w io.Writer, frames []image.Image) error {
	var blobs [][]byte
	for _, img := range frames {
		var b bytes.Buffer
		if err := png.Encode(&b, img); err != nil {
			return err
		}
		blobs = append(blobs, b.Bytes())
	}
	le := binary.LittleEndian
	head := make([]byte, 6+16*len(frames))
	le.PutUint16(head[2:], 1)
	le.PutUint16(head[4:], uint16(len(frames)))
	off := len(head)
	for i, img := range frames {
		e := head[6+16*i:]
		dim := func(n int) byte {
			if n >= 256 {
				return 0
			}
			return byte(n)
		}
		e[0], e[1] = dim(img.Bounds().Dx()), dim(img.Bounds().Dy())
		le.PutUint16(e[4:], 1)
		le.PutUint16(e[6:], 32)
		le.PutUint32(e[8:], uint32(len(blobs[i])))
		le.PutUint32(e[12:], uint32(off))
		off += len(blobs[i])
	}
	if _, err := w.Write(head); err != nil {
		return err
	}
	for _, b := range blobs {
		if _, err := w.Write(b); err != nil {
			return err
		}
	}
	return nil
}

type rgb struct{ r, g, b float64 }

func (a rgb) add(b rgb) rgb       { return rgb{a.r + b.r, a.g + b.g, a.b + b.b} }
func (a rgb) mul(k float64) rgb   { return rgb{a.r * k, a.g * k, a.b * k} }
func mix(a, b rgb, t float64) rgb { return a.mul(1 - t).add(b.mul(t)) }
func hex(v uint32) rgb {
	return rgb{float64(v>>16&255) / 255, float64(v>>8&255) / 255, float64(v&255) / 255}
}
func clamp(x, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, x)) }

// Flat palette: every area is one solid colour, no gradients or shading.
var (
	bg        = hex(0x33105c)
	cord      = hex(0xb6acd9)
	tileLight = hex(0xeae5fa)
	tileMid   = hex(0xbcb2de)
	tileDark  = hex(0x8c80bd)
	sliceMain = hex(0xff3fa4)
	sliceLite = hex(0xff86c4)
	accents   = []rgb{hex(0x38e8ff), hex(0xffd36b)} // no pink: that is the slice
	white     = rgb{1, 1, 1}
)

// style holds the size-dependent simplifications that keep small icons legible.
type style struct {
	margin, corner float64
	rows, cols     int     // horizontal bands and meridian bands of the tile grid
	line           float64 // grid line half-width, in ball radii; 0 = no lines
	sparkles, cord bool
	accents        bool // a few coloured tiles
	ballY, ballR   float64
	sliceOut       float64 // how far the slice is pulled out
}

func styleFor(size int) style {
	switch {
	case size <= 24:
		return style{margin: 0.03, corner: 0.18, rows: 3, cols: 3, line: 0, ballY: 0.5, ballR: 0.38, sliceOut: 0.08}
	case size <= 48:
		return style{margin: 0.02, corner: 0.2, rows: 4, cols: 4, line: 0.07, ballY: 0.52, ballR: 0.36, sliceOut: 0.07}
	case size <= 128:
		return style{margin: 0.05, corner: 0.22, rows: 6, cols: 6, line: 0.04, sparkles: true, cord: true, accents: true, ballY: 0.54, ballR: 0.31, sliceOut: 0.06}
	default:
		return style{margin: 0.06, corner: 0.22, rows: 8, cols: 8, line: 0.03, sparkles: true, cord: true, accents: true, ballY: 0.54, ballR: 0.31, sliceOut: 0.06}
	}
}

// Fixed geometry in unit coordinates (y down): ball centre x and slice angles.
const (
	ballX              = 0.5
	sliceFrom, sliceTo = 15 * math.Pi / 180, 70 * math.Pi / 180 // counterclockwise from +x
)

// render draws the icon at size×size with 4×4 (8×8 for tiny sizes) supersampling.
func render(size int) *image.NRGBA {
	st := styleFor(size)
	ss := 4
	if size <= 48 {
		ss = 8
	}
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	n := float64(size * ss)
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var acc rgb
			var accA float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					u := (float64(px*ss+sx) + 0.5) / n
					v := (float64(py*ss+sy) + 0.5) / n
					c, a := sample(u, v, st)
					acc = acc.add(c.mul(a))
					accA += a
				}
			}
			k := float64(ss * ss)
			out := color.NRGBA{}
			if accA > 0 {
				c := acc.mul(1 / accA)
				out = color.NRGBA{to8(c.r), to8(c.g), to8(c.b), to8(accA / k)}
			}
			img.SetNRGBA(px, py, out)
		}
	}
	return img
}

func to8(x float64) uint8 { return uint8(clamp(x, 0, 1)*255 + 0.5) }

// sample returns the colour and alpha of one point (u, v) in the unit square.
func sample(u, v float64, st style) (rgb, float64) {
	if !inRoundedSquare(u, v, st.margin, st.corner) {
		return rgb{}, 0
	}
	if st.cord && math.Abs(u-ballX) < 0.008 && v < st.ballY-st.ballR+0.01 {
		return cord, 1
	}
	// Pulled-out slice: the same ball, offset along the slice's bisector.
	mid := (sliceFrom + sliceTo) / 2
	ox, oy := st.sliceOut*math.Cos(mid), -st.sliceOut*math.Sin(mid)
	if c, ok := ball(u-ox, v-oy, st, true); ok {
		return c, 1
	}
	if c, ok := ball(u, v, st, false); ok {
		return c, 1
	}
	if st.sparkles {
		for _, s := range []struct{ x, y, r float64 }{{0.2, 0.22, 0.075}, {0.82, 0.83, 0.05}} {
			if inSparkle(u-s.x, v-s.y, s.r) {
				return white, 1
			}
		}
	}
	return bg, 1
}

func inRoundedSquare(u, v, m, r float64) bool {
	lo, hi := m+r, 1-m-r
	dx := math.Max(0, math.Max(lo-u, u-hi))
	dy := math.Max(0, math.Max(lo-v, v-hi))
	return u >= m && u <= 1-m && v >= m && v <= 1-m && dx*dx+dy*dy <= r*r
}

func inSlice(dx, dy float64) bool {
	a := math.Atan2(-dy, dx)
	return a >= sliceFrom && a <= sliceTo
}

// ball draws the flat disco ball at (u, v): a disc cut into tiles by straight horizontal lines and
// curved meridians, each tile one flat tone. slice selects the pulled-out wedge (true) or the
// rest of the ball (false); ok is false when the point is not on that part.
func ball(u, v float64, st style, slice bool) (rgb, bool) {
	dx, dy := (u-ballX)/st.ballR, (v-st.ballY)/st.ballR
	rr := dx*dx + dy*dy
	if rr > 1 || inSlice(dx, dy) != slice {
		return rgb{}, false
	}
	// Tile grid, evenly spaced: straight horizontal bands, and meridians that divide each row's
	// width into equal parts (so they curve with the outline).
	w := math.Sqrt(math.Max(1-dy*dy, 1e-9))
	q := clamp(dx/w, -1, 1)
	row := min(int((dy+1)/2*float64(st.rows)), st.rows-1)
	col := min(int((q+1)/2*float64(st.cols)), st.cols-1)

	if st.line > 0 {
		for _, k := range []int{row, row + 1} {
			if k > 0 && k < st.rows && math.Abs(dy-(2*float64(k)/float64(st.rows)-1)) < st.line {
				return bg, true
			}
		}
		for _, k := range []int{col, col + 1} {
			if k > 0 && k < st.cols && math.Abs(dx-w*(2*float64(k)/float64(st.cols)-1)) < st.line {
				return bg, true
			}
		}
	}

	// Flat tone per tile from where its centre faces: light top-left, dark bottom-right.
	cy := 2*(float64(row)+0.5)/float64(st.rows) - 1
	cx := math.Sqrt(1-cy*cy) * (2*(float64(col)+0.5)/float64(st.cols) - 1)
	facing := -0.7*cx - 0.7*cy
	if slice {
		if facing > -0.1 {
			return sliceLite, true
		}
		return sliceMain, true
	}
	if st.accents {
		if h := hash(row, col); h > 0.88 {
			return accents[int(hash(col, row)*2)%2], true
		}
	}
	switch {
	case facing > 0.35:
		return tileLight, true
	case facing > -0.35:
		return tileMid, true
	default:
		return tileDark, true
	}
}

// inSparkle reports whether (dx, dy) is inside a four-point star (astroid) of radius r.
func inSparkle(dx, dy, r float64) bool {
	return math.Pow(math.Abs(dx)/r, 2.0/3)+math.Pow(math.Abs(dy)/r, 2.0/3) < 1
}

func hash(a, b int) float64 {
	x := uint32(a)*73856093 ^ uint32(b)*19349663
	x ^= x >> 13
	x *= 0x5bd1e995
	x ^= x >> 15
	return float64(x) / math.MaxUint32
}
