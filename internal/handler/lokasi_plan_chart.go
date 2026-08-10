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
)

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
}

func (c *LokasiPlanChart) HasBars() bool { return c != nil && len(c.Bars) > 0 }

func buildLokasiPlanChart(shares []service.LokasiShare) *LokasiPlanChart {
	chart := &LokasiPlanChart{Width: lokasiChartWidth}
	trackWidth := lokasiChartWidth - lokasiChartGutter

	for _, share := range shares {
		if !share.AdaRencana {
			chart.Unplanned = append(chart.Unplanned, share)
			continue
		}
		top := lokasiTopPadding + float64(len(chart.Bars))*lokasiRowHeight

		// The bar stops at the end of its track; the figure beside it keeps
		// counting past 100, which is where the overshoot is actually read.
		ratio := share.Capaian / 100
		if ratio > 1 {
			ratio = 1
		}
		if ratio < 0 {
			ratio = 0
		}
		actualWidth := trackWidth * ratio

		chart.Bars = append(chart.Bars, LokasiPlanBar{
			Lokasi:  share.Lokasi,
			Detail:  formatVolume(share.Volume) + " / " + formatVolume(share.Rencana) + " m³",
			Value:   formatPercent(share.Capaian),
			Full:    share.Capaian >= 100,
			NameY:   top + 11,
			ActualY: top + 19,
			PlanY:   top + 32,
			ActualW: actualWidth,
			PlanW:   trackWidth,
			ValueX:  actualWidth + 8,
			ValueY:  top + 30,
		})
	}
	chart.Height = lokasiTopPadding*2 + float64(len(chart.Bars))*lokasiRowHeight
	return chart
}
