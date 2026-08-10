package handler

import (
	"fmt"
	"strings"
	"testing"
)

// rotatedLabelTail is roughly how far a slanted date label reaches below its
// anchor: a label rotated -45 with text-anchor="end" runs down and to the left
// in SVG coordinates, so a five-character date needs this much clearance.
const rotatedLabelTail = 40.0

func assertWithinViewBox(t *testing.T, name string, chart *Chart) {
	t.Helper()
	for _, bar := range chart.Bars {
		if bar.X < 0 || bar.X+bar.Width > chart.Width {
			t.Fatalf("%s: bar spans %.1f..%.1f, outside 0..%.1f", name, bar.X, bar.X+bar.Width, chart.Width)
		}
		if bar.Y < 0 || bar.Y+bar.Height > chart.Height {
			t.Fatalf("%s: bar spans %.1f..%.1f vertically, outside 0..%.1f", name, bar.Y, bar.Y+bar.Height, chart.Height)
		}
	}
	for _, label := range chart.XLabels {
		if label.X < 0 || label.X > chart.Width {
			t.Fatalf("%s: label %q sits at x=%.1f, outside the view box", name, label.Text, label.X)
		}
		// Anything the viewBox does not contain is silently clipped, which is
		// how the dates went missing.
		if label.Angle != 0 && label.Y+rotatedLabelTail > chart.Height {
			t.Fatalf("%s: slanted label %q ends at y=%.1f, past the view box height %.1f",
				name, label.Text, label.Y+rotatedLabelTail, chart.Height)
		}
	}
}

func TestChartsStayInsideTheViewBox(t *testing.T) {
	labels := []string{"01/08", "02/08", "03/08", "04/08", "05/08", "06/08"}
	values := []float64{2280.4, 1720.9, 860.2, 1660.1, 3775.45, 550}
	// The ritase split is two counts against each other, drawn by the same
	// grouped builder as the volumes.
	kecil := []float64{160, 65, 22, 120, 310, 40}
	besar := []float64{14, 26, 15, 8, 9, 3}

	assertWithinViewBox(t, "value", BuildValueChart(labels, values, 0))
	assertWithinViewBox(t, "ritase", BuildGroupedChart(labels, kecil, besar,
		GroupedSeries{Name: "kecil", Label: "DT Kecil"},
		GroupedSeries{Name: "besar", Label: "DT Besar"}))
	assertWithinViewBox(t, "grouped", BuildGroupedChart(labels, values, []float64{2000, 1500, 800, 1600, 3500, 500},
		GroupedSeries{Name: "real", Label: "Volume Real", Decimals: 2},
		GroupedSeries{Name: "opp", Label: "Volume OPP", Decimals: 2}))

	// A long unfiltered run must survive the same check.
	many := make([]string, 60)
	manyValues := make([]float64, 60)
	for i := range many {
		many[i] = "01/08"
		manyValues[i] = float64(i * 17)
	}
	assertWithinViewBox(t, "long value", BuildValueChart(many, manyValues, 0))
}

// The volume chart is a line, not bars: no <rect> should be produced, the path
// must be curved rather than straight segments, and it must never dip below the
// baseline into volume that was never produced.
func TestLineChartIsSmoothAndStaysAboveTheBaseline(t *testing.T) {
	labels := []string{"01/08", "02/08", "03/08", "04/08", "05/08", "06/08"}
	values := []float64{2280.4, 1720.9, 60.2, 1660.1, 3775.45, 550}
	chart := BuildLineChart(labels, values, 0)

	if len(chart.Bars) != 0 {
		t.Fatalf("line chart produced %d bars", len(chart.Bars))
	}
	if !strings.Contains(chart.Path, " C ") {
		t.Fatalf("path has no curve segments: %q", chart.Path)
	}
	if len(chart.Dots) != len(values) {
		t.Fatalf("got %d dots for %d values", len(chart.Dots), len(values))
	}
	for _, dot := range chart.Dots {
		if dot.Y > chart.PlotBottom+0.01 || dot.Y < chart.PlotTop-0.01 {
			t.Fatalf("dot for %s at y=%.1f is outside the plot %.1f..%.1f", dot.Label, dot.Y, chart.PlotTop, chart.PlotBottom)
		}
	}
	// Every control point is clamped to the baseline, so a dip after a spike
	// cannot render below zero. Coordinates alternate x, y through the path, so
	// only the odd positions are heights.
	numbers := strings.Fields(strings.NewReplacer("M", " ", "C", " ", ",", " ").Replace(chart.Path))
	if len(numbers)%2 != 0 {
		t.Fatalf("path has an odd number of coordinates: %q", chart.Path)
	}
	for i := 1; i < len(numbers); i += 2 {
		var y float64
		if _, err := fmt.Sscanf(numbers[i], "%f", &y); err != nil {
			t.Fatalf("coordinate %q is not a number", numbers[i])
		}
		if y > chart.PlotBottom+0.01 {
			t.Fatalf("path height %.1f falls below the baseline %.1f", y, chart.PlotBottom)
		}
	}
}

// Badges must not be clipped by the top of the view box.
func TestLineChartBadgesFitInsideTheViewBox(t *testing.T) {
	chart := BuildLineChart([]string{"01/08", "02/08"}, []float64{10, 4000}, 0)
	for _, dot := range chart.Dots {
		if !dot.Badge {
			continue
		}
		if dot.BadgeY < 0 {
			t.Fatalf("badge for %s starts at y=%.1f, above the view box", dot.Label, dot.BadgeY)
		}
		if dot.BadgeX < 0 || dot.BadgeX+dot.BadgeWidth > chart.Width {
			t.Fatalf("badge for %s spans %.1f..%.1f, outside 0..%.1f", dot.Label, dot.BadgeX, dot.BadgeX+dot.BadgeWidth, chart.Width)
		}
	}
}

// badgesCollide reports whether two plates overlap on screen, which needs both
// axes: two numbers above bars of different heights sit clear of each other.
func badgesCollide(a, b ChartBar) bool {
	const height = 16.0
	horizontal := a.BadgeX < b.BadgeX+b.BadgeWidth && b.BadgeX < a.BadgeX+a.BadgeWidth
	vertical := a.BadgeY < b.BadgeY+height && b.BadgeY < a.BadgeY+height
	return horizontal && vertical
}

// Every chart carries value badges, and none of them may be clipped by the
// view box or overlap its neighbour.
func TestBarChartsCarryBadgesThatFit(t *testing.T) {
	labels := []string{"Jun 2026", "Jul 2026", "Agu 2026"}

	charts := map[string]*Chart{
		"value": BuildValueChart(labels, []float64{1200, 0, 3775}, 0),
		"grouped": BuildGroupedChart(labels, []float64{1200, 0, 3775}, []float64{1100, 0, 3500},
			GroupedSeries{Name: "real", Label: "Volume Real", Decimals: 2},
			GroupedSeries{Name: "opp", Label: "Volume OPP", Decimals: 2}),
	}

	for name, chart := range charts {
		badges := 0
		var placed []ChartBar
		for _, bar := range chart.Bars {
			if !bar.Badge {
				continue
			}
			badges++
			if bar.BadgeY < 0 {
				t.Fatalf("%s: badge %q starts at y=%.1f, above the view box", name, bar.Value, bar.BadgeY)
			}
			if bar.BadgeX < 0 || bar.BadgeX+bar.BadgeWidth > chart.Width {
				t.Fatalf("%s: badge %q spans %.1f..%.1f, outside 0..%.1f",
					name, bar.Value, bar.BadgeX, bar.BadgeX+bar.BadgeWidth, chart.Width)
			}
			for _, other := range placed {
				if badgesCollide(bar, other) {
					t.Fatalf("%s: badge %q overlaps badge %q", name, bar.Value, other.Value)
				}
			}
			placed = append(placed, bar)
		}
		if badges == 0 {
			t.Fatalf("%s: no badges rendered", name)
		}
	}
}

// A crowded axis must drop badges rather than print them on top of each other.
func TestBadgesAreDroppedWhenBarsAreTooClose(t *testing.T) {
	labels := make([]string, 90)
	values := make([]float64, 90)
	for i := range labels {
		labels[i] = "01/08"
		values[i] = 1234.5
	}

	chart := BuildValueChart(labels, values, 0)
	var placed []ChartBar
	for _, bar := range chart.Bars {
		if !bar.Badge {
			continue
		}
		for _, other := range placed {
			if badgesCollide(bar, other) {
				t.Fatalf("badges overlap on a crowded axis: %q", bar.Value)
			}
		}
		placed = append(placed, bar)
	}
}

func TestChartTallestBarReachesThePlotTop(t *testing.T) {
	chart := BuildValueChart([]string{"a", "b"}, []float64{50, 100}, 0)
	tallest := chart.Bars[1]
	if tallest.Y < chart.PlotTop-0.01 {
		t.Fatalf("tallest bar starts at %.1f, above the plot top %.1f", tallest.Y, chart.PlotTop)
	}
	if tallest.Y+tallest.Height != chart.PlotBottom {
		t.Fatalf("bars do not sit on the baseline: %.1f vs %.1f", tallest.Y+tallest.Height, chart.PlotBottom)
	}
}

// Panels inside a grid are siblings, so the stacked-panel margin would offset
// every card but the first and shorten it inside its row.
func TestGridPanelsDoNotInheritStackingMargin(t *testing.T) {
	testServer := newTestServer(t)
	stylesheet := fetchPage(t, testServer.URL+"/static/css/style.css")

	if !strings.Contains(stylesheet, ".kpi-grid > .panel, .chart-grid > .panel { margin-top: 0; }") {
		t.Fatal("grid panels still inherit the stacked-panel margin")
	}
}
