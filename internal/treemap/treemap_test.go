package treemap

import (
	"math"
	"testing"
)

func area(r Rect) float64 { return r.W * r.H }

// Proves: each rectangle's area is proportional to its value, all of them
// fit inside the bounds without overlapping, and together they fill it.
func TestSquarifyAreasAndBounds(t *testing.T) {
	values := []int64{600, 300, 200, 100, 60, 40, 20, 10}
	bounds := Rect{X: 10, Y: 20, W: 400, H: 300}
	rects := Squarify(values, bounds)
	if len(rects) != len(values) {
		t.Fatalf("got %d rects for %d values", len(rects), len(values))
	}

	var total int64
	for _, v := range values {
		total += v
	}
	var sum float64
	for i, r := range rects {
		want := float64(values[i]) / float64(total) * area(bounds)
		if math.Abs(area(r)-want) > 0.5 {
			t.Errorf("rect %d area %.2f, want %.2f", i, area(r), want)
		}
		const eps = 1e-6
		if r.X < bounds.X-eps || r.Y < bounds.Y-eps || r.X+r.W > bounds.X+bounds.W+eps || r.Y+r.H > bounds.Y+bounds.H+eps {
			t.Errorf("rect %d %+v outside bounds %+v", i, r, bounds)
		}
		sum += area(r)
		for j := 0; j < i; j++ {
			if overlap(r, rects[j]) > 1e-6 {
				t.Errorf("rects %d and %d overlap", i, j)
			}
		}
	}
	if math.Abs(sum-area(bounds)) > 1 {
		t.Errorf("total area %.2f, want %.2f", sum, area(bounds))
	}
}

func overlap(a, b Rect) float64 {
	w := math.Min(a.X+a.W, b.X+b.W) - math.Max(a.X, b.X)
	h := math.Min(a.Y+a.H, b.Y+b.H) - math.Max(a.Y, b.Y)
	if w <= 0 || h <= 0 {
		return 0
	}
	return w * h
}

// Proves: the layout keeps rectangles close to square (the point of
// squarifying), far better than slicing the bounds into strips.
func TestSquarifyAspectRatio(t *testing.T) {
	values := []int64{6, 6, 4, 3, 2, 2, 1}
	rects := Squarify(values, Rect{W: 600, H: 400})
	worst := 0.0
	for _, r := range rects {
		ratio := math.Max(r.W/r.H, r.H/r.W)
		worst = math.Max(worst, ratio)
	}
	if worst > 3 {
		t.Errorf("worst aspect ratio %.2f, want <= 3", worst)
	}
}

// Proves: zero and negative values get empty rects and do not break the
// layout; empty input and empty bounds are handled.
func TestSquarifyEdgeCases(t *testing.T) {
	rects := Squarify([]int64{100, 0, -5, 50}, Rect{W: 100, H: 100})
	if len(rects) != 4 || area(rects[1]) != 0 || area(rects[2]) != 0 {
		t.Errorf("rects = %+v, want zero-area rects for non-positive values", rects)
	}
	if area(rects[0]) <= area(rects[3]) {
		t.Errorf("larger value got smaller area: %+v", rects)
	}
	if len(Squarify(nil, Rect{W: 10, H: 10})) != 0 {
		t.Error("nil values should give no rects")
	}
	for _, r := range Squarify([]int64{1, 2}, Rect{W: 0, H: 10}) {
		if area(r) != 0 {
			t.Errorf("zero-width bounds gave %+v", r)
		}
	}
}
