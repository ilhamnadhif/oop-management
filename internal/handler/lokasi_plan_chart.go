package handler

import (
	"strconv"

	"opp-management/internal/service"
)

// formatPercent writes the figure the way the rest of the dashboard writes
// percentages: two decimals, a dot, and the sign attached.
func formatPercent(value float64) string {
	return strconv.FormatFloat(value, 'f', 2, 64) + "%"
}

// The per-location chart is two horizontal bars per row: the plan drawn at full
// width, and the realisation drawn against it. Reading one bar against another
// says "how far along is this" at a glance, which a single bar and a number
// does not.
//
// It is an SVG rather than styled <div>s because the content security policy
// allows no inline styles, and a bar whose width is data cannot be expressed in
// a stylesheet. Geometry is computed here for the same reason the other charts
// compute theirs here: the template language has no arithmetic.
const (
	lokasiChartWidth = 520.0
	// The gutter holds the percentage printed past the end of the bar, so a bar
	// at 100% still has room for its own label.
	lokasiChartGutter = 62.0
	lokasiRowHeight   = 58.0
	lokasiBarHeight   = 9.0
	lokasiTopPadding  = 4.0
	// The total is set off from the locations rather than stacked flush against
	// them: it is a different reading of the same track, and a row that looks
	// like the ones above it gets counted as another location.
	lokasiTotalGap = 22.0
)

// lokasiTotalLabel names the row that measures the whole period rather than one
// segment. It is capitalised because the row is a summary, not a place.
const lokasiTotalLabel = "SEMUA LOKASI"

type LokasiPlanBar struct {
	Lokasi string
	Detail string
	Value  string
	// Full marks a plan that has been met, which is drawn in a different colour
	// so a bar at the end of its track is not confused with one merely long.
	Full bool

	NameY   float64
	PlanY   float64
	ActualY float64
	ActualW float64
	PlanW   float64
	ValueX  float64
	ValueY  float64
}

type LokasiPlanChart struct {
	Width  float64
	Height float64
	Bars   []LokasiPlanBar
	// Unplanned lists the locations produced without a target. They are kept
	// out of the chart on purpose: their bar would measure a share of the
	// period, and two different meanings on one axis is how a chart lies.
	Unplanned []service.LokasiShare
	// Total is the whole period against the whole plan, drawn under a rule at
	// the foot of the chart. It is nil when nothing has been planned, because
	// then there is no target to measure against.
	Total *LokasiPlanBar
	// DividerY is where the rule above the total row is drawn.
	DividerY float64
}

func (c *LokasiPlanChart) HasBars() bool { return c != nil && len(c.Bars) > 0 }

func buildLokasiPlanChart(shares []service.LokasiShare, totalVolume, totalRencana float64) *LokasiPlanChart {
	chart := &LokasiPlanChart{Width: lokasiChartWidth}
	trackWidth := lokasiChartWidth - lokasiChartGutter

	for _, share := range shares {
		if !share.AdaRencana {
			chart.Unplanned = append(chart.Unplanned, share)
			continue
		}
		top := lokasiTopPadding + float64(len(chart.Bars))*lokasiRowHeight
		chart.Bars = append(chart.Bars, lokasiPlanRow(
			share.Lokasi, share.Volume, share.Rencana, share.Capaian, top, trackWidth))
	}
	chart.Height = lokasiTopPadding*2 + float64(len(chart.Bars))*lokasiRowHeight

	// The total is drawn whenever anything was planned, even for a single
	// location, so the row keeps its place as the date filter narrows what is
	// above it. Its volume is the whole period rather than the sum of the bars:
	// production booked to an unplanned location still counts against the plan.
	if totalRencana > 0 {
		capaian := totalVolume / totalRencana * 100
		top := chart.Height - lokasiTopPadding + lokasiTotalGap
		chart.DividerY = top - lokasiTotalGap/2
		total := lokasiPlanRow(
			lokasiTotalLabel, totalVolume, totalRencana, capaian, top, trackWidth)
		chart.Total = &total
		chart.Height = top + lokasiRowHeight
	}
	return chart
}

// lokasiPlanRow lays out one row of the chart. The total shares it with the
// locations on purpose: the two are read against each other, so a difference in
// geometry between them would be read as a difference in meaning.
func lokasiPlanRow(lokasi string, volume, rencana, capaian, top, trackWidth float64) LokasiPlanBar {
	// The bar stops at the end of its track; the figure beside it keeps
	// counting past 100, which is where the overshoot is actually read.
	ratio := capaian / 100
	if ratio > 1 {
		ratio = 1
	}
	if ratio < 0 {
		ratio = 0
	}
	actualWidth := trackWidth * ratio

	return LokasiPlanBar{
		Lokasi:  lokasi,
		Detail:  formatVolume(volume) + " / " + formatVolume(rencana) + " m³",
		Value:   formatPercent(capaian),
		Full:    capaian >= 100,
		NameY:   top + 11,
		ActualY: top + 19,
		PlanY:   top + 32,
		ActualW: actualWidth,
		PlanW:   trackWidth,
		ValueX:  actualWidth + 8,
		ValueY:  top + 30,
	}
}
