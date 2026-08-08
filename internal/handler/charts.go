package handler

import (
	"fmt"
	"math"
	"strings"
)

// Charts are drawn as plain SVG built here rather than by a JavaScript library:
// the CSP allows same-origin assets only, so every charting library would have
// to be vendored, and these shapes are simple enough not to warrant it.
//
// All geometry is computed in a fixed 0..chartWidth x 0..chartHeight space and
// the SVG scales itself to the panel through viewBox.
const (
	chartWidth = 820.0
	// A label rotated -45 with text-anchor="end" runs down and to the left in
	// SVG coordinates, so the base padding has to clear its tail or the dates
	// are cut off by the viewBox.
	chartHeight  = 300.0
	chartPadLeft = 58.0
	chartPadBase = 74.0
	chartPadTop  = 14.0
)

type ChartBar struct {
	X, Y, Width, Height float64
	Label               string
	Value               string
	Series              string
	// Badge describes the small plate of text above the bar. It is omitted
	// wherever the bars sit too close for the numbers to stay legible.
	Badge      bool
	BadgeX     float64
	BadgeY     float64
	BadgeWidth float64
}

type ChartTick struct {
	Y     float64
	Value string
}

// ChartDot is one plotted point: a marker, and optionally a small badge with
// the value above it.
type ChartDot struct {
	X, Y  float64
	Value string
	Label string
	Badge bool
	// BadgeX and BadgeWidth describe the rounded plate drawn behind the number.
	BadgeX     float64
	BadgeY     float64
	BadgeWidth float64
}

type ChartLabel struct {
	X, Y  float64
	Text  string
	Angle float64
}

// Chart is one rendered figure: bars, an optional line through them, gridlines
// and axis labels.
type Chart struct {
	Width       float64
	Height      float64
	PlotTop     float64
	PlotBottom  float64
	PlotLeft    float64
	PlotRight   float64
	Bars        []ChartBar
	Line        string
	Path        string
	Dots        []ChartDot
	Ticks       []ChartTick
	XLabels     []ChartLabel
	Empty       bool
	LabelStride int
}

func newChart() *Chart {
	return &Chart{
		Width:      chartWidth,
		Height:     chartHeight,
		PlotTop:    chartPadTop,
		PlotBottom: chartHeight - chartPadBase,
		PlotLeft:   chartPadLeft,
		PlotRight:  chartWidth - 12,
	}
}

// niceMax rounds an axis top up to something a person would choose, so the
// gridline labels are readable numbers instead of 4713.9.
func niceMax(value float64) float64 {
	if value <= 0 {
		return 1
	}
	magnitude := math.Pow(10, math.Floor(math.Log10(value)))
	for _, step := range []float64{1, 1.5, 2, 2.5, 3, 4, 5, 7.5, 10} {
		if value <= step*magnitude {
			return step * magnitude
		}
	}
	return 10 * magnitude
}

func (c *Chart) addTicks(max float64, decimals int) {
	const divisions = 4
	height := c.PlotBottom - c.PlotTop
	for i := 0; i <= divisions; i++ {
		fraction := float64(i) / divisions
		c.Ticks = append(c.Ticks, ChartTick{
			Y:     c.PlotBottom - fraction*height,
			Value: trimNumber(max*fraction, decimals),
		})
	}
}

func trimNumber(value float64, decimals int) string {
	text := fmt.Sprintf("%.*f", decimals, value)
	if strings.Contains(text, ".") {
		text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
	}
	return text
}

// badgeHeight is the plate height the template draws.
const badgeHeight = 16.0

// badgeWidth approximates the plate needed for a number at the badge font size.
func badgeWidth(text string) float64 {
	return float64(len(text))*6.0 + 10
}

// badgeFits decides whether a number can sit above a bar without colliding with
// its neighbour. A badge that overlaps the next one is worse than no badge:
// the figures become unreadable smears.
func badgeFits(text string, available float64) bool {
	return badgeWidth(text)+4 <= available
}

// labelStride keeps the x axis readable: with dozens of days, printing every
// label turns the axis into a smear.
func labelStride(count int) int {
	switch {
	case count <= 12:
		return 1
	case count <= 30:
		return 2
	case count <= 60:
		return 4
	default:
		return count/15 + 1
	}
}

// BuildValueChart draws one bar per entry with a line across their tops.
func BuildValueChart(labels []string, values []float64, decimals int) *Chart {
	chart := newChart()
	if len(values) == 0 {
		chart.Empty = true
		return chart
	}
	max := 0.0
	for _, value := range values {
		if value > max {
			max = value
		}
	}
	max = niceMax(max)
	chart.PlotTop += 16
	chart.addTicks(max, decimals)

	plotWidth := chart.PlotRight - chart.PlotLeft
	plotHeight := chart.PlotBottom - chart.PlotTop
	slot := plotWidth / float64(len(values))
	barWidth := math.Max(2, math.Min(28, slot*0.62))
	stride := labelStride(len(values))
	chart.LabelStride = stride

	points := make([]string, 0, len(values))
	for i, value := range values {
		centre := chart.PlotLeft + slot*(float64(i)+0.5)
		height := 0.0
		if max > 0 {
			height = value / max * plotHeight
		}
		text := trimNumber(value, decimals)
		bar := ChartBar{
			X:      centre - barWidth/2,
			Y:      chart.PlotBottom - height,
			Width:  barWidth,
			Height: height,
			Label:  labels[i],
			Value:  text,
		}
		if i%stride == 0 && badgeFits(text, slot*float64(stride)) {
			bar.Badge = true
			bar.BadgeWidth = badgeWidth(text)
			bar.BadgeX = centre - bar.BadgeWidth/2
			bar.BadgeY = bar.Y - 20
		}
		chart.Bars = append(chart.Bars, bar)
		points = append(points, fmt.Sprintf("%.1f,%.1f", centre, chart.PlotBottom-height))
		if i%stride == 0 {
			chart.XLabels = append(chart.XLabels, ChartLabel{X: centre, Y: chart.PlotBottom + 14, Text: labels[i], Angle: -45})
		}
	}
	chart.Line = strings.Join(points, " ")
	return chart
}

// BuildStackedChart stacks two counts per entry, which is how the ritase split
// reads as one total rather than two competing bars.
func BuildStackedChart(labels []string, lower, upper []int) *Chart {
	chart := newChart()
	if len(labels) == 0 {
		chart.Empty = true
		return chart
	}
	max := 0.0
	for i := range labels {
		if total := float64(lower[i] + upper[i]); total > max {
			max = total
		}
	}
	max = niceMax(max)
	chart.PlotTop += 16
	chart.addTicks(max, 0)

	plotWidth := chart.PlotRight - chart.PlotLeft
	plotHeight := chart.PlotBottom - chart.PlotTop
	slot := plotWidth / float64(len(labels))
	barWidth := math.Max(2, math.Min(24, slot*0.62))
	stride := labelStride(len(labels))
	chart.LabelStride = stride

	for i := range labels {
		centre := chart.PlotLeft + slot*(float64(i)+0.5)
		lowerHeight := float64(lower[i]) / max * plotHeight
		upperHeight := float64(upper[i]) / max * plotHeight
		top := ChartBar{
			X: centre - barWidth/2, Y: chart.PlotBottom - lowerHeight - upperHeight,
			Width: barWidth, Height: upperHeight,
			Label: labels[i], Value: fmt.Sprintf("%d", upper[i]), Series: "besar",
		}
		// The badge carries the combined total: two numbers stacked on one thin
		// bar would overlap, and the total is what the eye is after anyway.
		total := fmt.Sprintf("%d", lower[i]+upper[i])
		if i%stride == 0 && badgeFits(total, slot*float64(stride)) {
			top.Badge = true
			top.BadgeWidth = badgeWidth(total)
			top.BadgeX = centre - top.BadgeWidth/2
			top.BadgeY = top.Y - 20
			top.Value = total
		}
		chart.Bars = append(chart.Bars,
			ChartBar{
				X: centre - barWidth/2, Y: chart.PlotBottom - lowerHeight,
				Width: barWidth, Height: lowerHeight,
				Label: labels[i], Value: fmt.Sprintf("%d", lower[i]), Series: "kecil",
			},
			top,
		)
		if i%stride == 0 {
			chart.XLabels = append(chart.XLabels, ChartLabel{X: centre, Y: chart.PlotBottom + 14, Text: labels[i], Angle: -45})
		}
	}
	return chart
}

// BuildGroupedChart puts two bars side by side in each slot, which is how a
// realised figure reads against its nominal: they are alternatives to compare,
// not parts of one total to stack.
func BuildGroupedChart(labels []string, first, second []float64) *Chart {
	chart := newChart()
	if len(labels) == 0 {
		chart.Empty = true
		return chart
	}
	max := 0.0
	for i := range labels {
		if first[i] > max {
			max = first[i]
		}
		if second[i] > max {
			max = second[i]
		}
	}
	max = niceMax(max)
	chart.PlotTop += 16
	chart.addTicks(max, 0)

	plotWidth := chart.PlotRight - chart.PlotLeft
	plotHeight := chart.PlotBottom - chart.PlotTop
	slot := plotWidth / float64(len(labels))
	barWidth := math.Max(2, math.Min(26, slot*0.34))
	gap := barWidth * 0.12
	stride := labelStride(len(labels))
	chart.LabelStride = stride

	for i := range labels {
		centre := chart.PlotLeft + slot*(float64(i)+0.5)
		firstHeight := first[i] / max * plotHeight
		secondHeight := second[i] / max * plotHeight
		firstText := trimNumber(first[i], 0)
		secondText := trimNumber(second[i], 0)
		realBar := ChartBar{
			X: centre - barWidth - gap/2, Y: chart.PlotBottom - firstHeight,
			Width: barWidth, Height: firstHeight,
			Label: labels[i] + " · Volume Real", Value: trimNumber(first[i], 2), Series: "real",
		}
		oppBar := ChartBar{
			X: centre + gap/2, Y: chart.PlotBottom - secondHeight,
			Width: barWidth, Height: secondHeight,
			Label: labels[i] + " · Volume OPP", Value: trimNumber(second[i], 2), Series: "opp",
		}
		// Two badges share one slot here, so each only gets half the room
		// against the neighbouring slot.
		half := slot * float64(stride) / 2
		if i%stride == 0 && badgeFits(firstText, half) && badgeFits(secondText, half) {
			realBar.Badge = true
			realBar.BadgeWidth = badgeWidth(firstText)
			realBar.BadgeX = realBar.X + barWidth/2 - realBar.BadgeWidth/2
			realBar.BadgeY = realBar.Y - 20
			realBar.Value = firstText

			oppBar.Badge = true
			oppBar.BadgeWidth = badgeWidth(secondText)
			oppBar.BadgeX = oppBar.X + barWidth/2 - oppBar.BadgeWidth/2
			oppBar.BadgeY = oppBar.Y - 20
			oppBar.Value = secondText

			// The pair of bars is narrower than the pair of badges, so the two
			// numbers only stay apart when the bars differ in height. Stack them
			// when they do not, and drop the second if stacking would run off
			// the top of the chart.
			if realBar.BadgeX < oppBar.BadgeX+oppBar.BadgeWidth &&
				oppBar.BadgeX < realBar.BadgeX+realBar.BadgeWidth &&
				math.Abs(realBar.BadgeY-oppBar.BadgeY) < badgeHeight+2 {
				oppBar.BadgeY = math.Min(realBar.BadgeY, oppBar.BadgeY) - badgeHeight - 3
				if oppBar.BadgeY < 0 {
					oppBar.Badge = false
				}
			}
		}
		chart.Bars = append(chart.Bars, realBar, oppBar)
		if i%stride == 0 {
			chart.XLabels = append(chart.XLabels, ChartLabel{X: centre, Y: chart.PlotBottom + 14, Text: labels[i], Angle: -45})
		}
	}
	return chart
}

// smoothPath turns the points into a Catmull-Rom spline expressed as cubic
// Beziers, so the line eases through each date instead of turning a sharp
// corner at every one.
//
// The tension is deliberately gentle: at full strength the curve overshoots
// after a spike and can dip below the baseline, drawing volume that was never
// produced.
func smoothPath(points []ChartDot, floor float64) string {
	if len(points) == 0 {
		return ""
	}
	if len(points) == 1 {
		return fmt.Sprintf("M %.1f %.1f", points[0].X, points[0].Y)
	}

	const tension = 0.4
	clamp := func(y float64) float64 {
		if y > floor {
			return floor
		}
		return y
	}

	var path strings.Builder
	fmt.Fprintf(&path, "M %.1f %.1f", points[0].X, points[0].Y)
	for i := 0; i < len(points)-1; i++ {
		previous := points[max(i-1, 0)]
		current := points[i]
		next := points[i+1]
		after := points[min(i+2, len(points)-1)]

		c1x := current.X + (next.X-previous.X)/6*tension
		c1y := clamp(current.Y + (next.Y-previous.Y)/6*tension)
		c2x := next.X - (after.X-current.X)/6*tension
		c2y := clamp(next.Y - (after.Y-current.Y)/6*tension)
		fmt.Fprintf(&path, " C %.1f %.1f, %.1f %.1f, %.1f %.1f", c1x, c1y, c2x, c2y, next.X, next.Y)
	}
	return path.String()
}

// BuildLineChart plots values as a single smoothed line with a marker and a
// small value badge on each point. No bars: the shape of the trend is the
// message here, not the individual magnitudes.
func BuildLineChart(labels []string, values []float64, decimals int) *Chart {
	chart := newChart()
	if len(values) == 0 {
		chart.Empty = true
		return chart
	}
	max := 0.0
	for _, value := range values {
		if value > max {
			max = value
		}
	}
	max = niceMax(max)
	// Leave headroom so a badge on the tallest point is not clipped.
	chart.PlotTop += 16
	chart.addTicks(max, decimals)

	plotWidth := chart.PlotRight - chart.PlotLeft
	plotHeight := chart.PlotBottom - chart.PlotTop
	slot := plotWidth / float64(len(values))
	stride := labelStride(len(values))
	chart.LabelStride = stride

	for i, value := range values {
		centre := chart.PlotLeft + slot*(float64(i)+0.5)
		y := chart.PlotBottom
		if max > 0 {
			y = chart.PlotBottom - value/max*plotHeight
		}
		text := trimNumber(value, decimals)
		// Roughly 6px per character at the badge font size, plus padding.
		width := float64(len(text))*6.0 + 10
		chart.Dots = append(chart.Dots, ChartDot{
			X: centre, Y: y,
			Value: text, Label: labels[i],
			Badge:      i%stride == 0,
			BadgeX:     centre - width/2,
			BadgeY:     y - 22,
			BadgeWidth: width,
		})
		if i%stride == 0 {
			chart.XLabels = append(chart.XLabels, ChartLabel{X: centre, Y: chart.PlotBottom + 14, Text: labels[i], Angle: -45})
		}
	}
	chart.Path = smoothPath(chart.Dots, chart.PlotBottom)
	return chart
}
