// Package treemap lays out sizes as nested rectangles using the squarified
// algorithm (Bruls, Huizing, van Wijk, 2000), which keeps rectangles close
// to square so they are easy to compare and click.
package treemap

import "math"

// Rect is an axis-aligned rectangle.
type Rect struct {
	X, Y, W, H float64
}

// Squarify returns one rectangle per value, in the same order, filling
// bounds with areas proportional to the values. Values should be sorted
// largest first for the best result. Non-positive values get an empty
// rectangle at the bounds' origin.
func Squarify(values []int64, bounds Rect) []Rect {
	out := make([]Rect, len(values))
	var total float64
	var idx []int
	for i, v := range values {
		out[i] = Rect{X: bounds.X, Y: bounds.Y}
		if v > 0 {
			total += float64(v)
			idx = append(idx, i)
		}
	}
	if total == 0 || bounds.W <= 0 || bounds.H <= 0 {
		return out
	}

	// Scale values to areas.
	scale := bounds.W * bounds.H / total
	areas := make([]float64, len(idx))
	for k, i := range idx {
		areas[k] = float64(values[i]) * scale
	}

	free := bounds
	for start := 0; start < len(areas); {
		side := math.Min(free.W, free.H)
		// Grow the row while it improves the worst aspect ratio.
		end := start + 1
		for end < len(areas) && worst(areas[start:end+1], side) <= worst(areas[start:end], side) {
			end++
		}
		row := areas[start:end]
		var rowArea float64
		for _, a := range row {
			rowArea += a
		}
		// Lay the row along the shorter side of the free space.
		thick := rowArea / side
		pos := 0.0
		for k, a := range row {
			length := a / thick
			var r Rect
			if free.W >= free.H { // row is a column on the left
				r = Rect{X: free.X, Y: free.Y + pos, W: thick, H: length}
			} else { // row is a strip on top
				r = Rect{X: free.X + pos, Y: free.Y, W: length, H: thick}
			}
			out[idx[start+k]] = r
			pos += length
		}
		if free.W >= free.H {
			free.X += thick
			free.W -= thick
		} else {
			free.Y += thick
			free.H -= thick
		}
		start = end
	}
	return out
}

// worst is the largest aspect ratio in a row of areas laid along side.
func worst(row []float64, side float64) float64 {
	var sum, lo, hi float64
	lo = math.Inf(1)
	for _, a := range row {
		sum += a
		lo = math.Min(lo, a)
		hi = math.Max(hi, a)
	}
	s2, sum2 := side*side, sum*sum
	return math.Max(s2*hi/sum2, sum2/(s2*lo))
}
