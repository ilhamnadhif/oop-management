package model

import "time"

// A2B Baru is the heavy equipment fuel book as the site keeps it in its own
// workbook, "Monitoring Konsumsi BBM": one register of machines, the purchase
// orders the tank is filled from, and every dispense with the hour meters read
// beside it. It lives on sheets of its own, next to the older A2B module rather
// than inside it.

// A2BBaruUnit is one machine fuel is booked against.
type A2BBaruUnit struct {
	IDUnit    string
	NamaUnit  string
	CreatedBy string
	CreatedAt time.Time
}

// A2BBaruFuelMasuk is one purchase order and what of it reached the site.
// Every amount is derived from the two volumes and the two prices, so only
// those are stored.
type A2BBaruFuelMasuk struct {
	FuelInID string
	// Phase is the site's own name for the order, such as "PO-3".
	Phase   string
	Tanggal string
	NoPO    string

	POVolume        float64
	POHargaSatuan   float64
	SiteVolume      float64
	SiteHargaSatuan float64

	CreatedBy string
	CreatedAt time.Time
}

// POJumlah is what the order costs.
func (f A2BBaruFuelMasuk) POJumlah() float64 { return f.POVolume * f.POHargaSatuan }

// SiteJumlah is what the fuel that arrived is worth.
func (f A2BBaruFuelMasuk) SiteJumlah() float64 { return f.SiteVolume * f.SiteHargaSatuan }

// DeviasiVolume is how much of the order never reached the tank. A negative
// figure is more arriving than was ordered.
func (f A2BBaruFuelMasuk) DeviasiVolume() float64 { return f.POVolume - f.SiteVolume }

// DeviasiJumlah is the money the shortfall stands for.
func (f A2BBaruFuelMasuk) DeviasiJumlah() float64 { return f.POJumlah() - f.SiteJumlah() }

// A2BBaruFuelKeluar is one dispense into a machine.
//
// The workbook's older rows read the machine's own hour meter at the start and
// end of the shift; the newer ones read the pump's flow meter instead and the
// machine's meter only when it is filled. Both sets are kept, each empty when
// it was not read.
type A2BBaruFuelKeluar struct {
	FuelOutID string
	Tanggal   string
	IDUnit    string
	NamaUnit  string

	HMAwalA2B  *float64
	HMAkhirA2B *float64
	HMAwalFM   *float64
	HMAkhirFM  *float64
	FuelLiter  float64
	// HMSaatIsi is the machine's hour meter when it was filled.
	HMSaatIsi *float64
	// JamKerja is the hours worked that the fuel is set against. Zero is a
	// machine with no meter, such as a genset, and has no ratio.
	JamKerja float64
	Operator string

	CreatedBy string
	CreatedAt time.Time

	// Foto is the photo of the closing flow meter reading, as a data URL. It
	// is written with the row but never listed, since a page of base64 images
	// is megabytes; HasFoto says whether one is there to fetch.
	Foto    string
	HasFoto bool
}

// Ratio is litres per working hour, and false when there are no hours to
// divide by.
func (f A2BBaruFuelKeluar) Ratio() (float64, bool) {
	if f.JamKerja <= 0 {
		return 0, false
	}
	return f.FuelLiter / f.JamKerja, true
}
