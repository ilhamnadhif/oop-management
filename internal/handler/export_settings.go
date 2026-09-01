package handler

import (
	"net/http"
	"strconv"
	"strings"

	"opp-management/internal/model"
)

// exportLabels word each configurable export the way its own page is titled,
// so the settings screen and the sidebar name the same thing.
var exportLabels = map[model.ExportTypeKey]string{
	model.ExportProduksi: "Laporan Produksi",
	model.ExportNota:     "Laporan Nota",
	model.ExportUnitDT:   "Daftar Unit DT",
	model.ExportUnitA2B:  "Daftar Unit A2B",
	model.ExportInputHM:  "Input Hour Meter",
	model.ExportAbsensi:  "Rekap Absensi Bulanan",
}

// exportLedes say what each report contains, so somebody switching one off can
// tell what they are taking away.
var exportLedes = map[model.ExportTypeKey]string{
	model.ExportProduksi: "Ritase harian per unit, lengkap dengan tonase dan lokasinya.",
	model.ExportNota:     "Rincian nota per item, dengan filter metode pembayaran.",
	model.ExportUnitDT:   "Register dump truck: ukuran bak dan drivernya.",
	model.ExportUnitA2B:  "Register alat berat: kapasitas tangki, konsumsi per jam, lokasi.",
	model.ExportInputHM:  "Pembacaan hour meter per bulan, dengan PA dan UA.",
	model.ExportAbsensi:  "Matriks absensi bulanan per karyawan.",
}

// ExportChoice is one export type as the settings form shows it: whether the
// project lets it be downloaded, and the closing block its PDF prints.
type ExportChoice struct {
	Key   string
	Label string
	Lede  string
	Aktif bool
	// TTDCount is how many signatures print: 1, 2 or 3.
	TTDCount int
	// Slots are left, centre and right, in that order. The form always shows
	// three; the ones past TTDCount simply do not print.
	Slots []ExportSlotChoice
}

// ExportSlotChoice is one signature position, with a flag the template uses to
// dim the positions this export does not print.
type ExportSlotChoice struct {
	// Nomor is the slot's number in the form field names: 1, 2, 3.
	Nomor int
	// Posisi words the position for the person filling the form in.
	Posisi  string
	Nama    string
	Jabatan string
	// Dipakai says this slot prints at the current TTD count.
	Dipakai bool
}

// slotPositions word the three positions left to right.
var slotPositions = [3]string{"Kiri", "Tengah", "Kanan"}

// exportChoicesFor lists every configurable export with this project's setting
// against it, in the order model declares them. An export the project has
// never configured shows its defaults, which is what it has always printed.
func exportChoicesFor(project model.Project) []ExportChoice {
	choices := make([]ExportChoice, 0, len(model.ExportTypeKeys))
	for _, key := range model.ExportTypeKeys {
		config := project.ExportConfigFor(key)
		count := config.TTDCount
		if count < 1 || count > 3 {
			count = 1
		}
		choice := ExportChoice{
			Key:      string(key),
			Label:    exportLabels[key],
			Lede:     exportLedes[key],
			Aktif:    config.Aktif,
			TTDCount: count,
		}
		for index, posisi := range slotPositions {
			choice.Slots = append(choice.Slots, ExportSlotChoice{
				Nomor:   index + 1,
				Posisi:  posisi,
				Nama:    config.Slots[index].Nama,
				Jabatan: config.Slots[index].Jabatan,
				Dipakai: slotPrints(index, count),
			})
		}
		choices = append(choices, choice)
	}
	return choices
}

// slotPrints answers whether one position prints at a given count. The
// positions fill from the edges, the same rule signatoriesFor lays out with:
// one signs on the right, two on left and right, three fill every position.
func slotPrints(index, count int) bool {
	switch count {
	case 2:
		return index == 0 || index == 2
	case 3:
		return true
	default:
		return index == 2
	}
}

// exportConfigFromForm reads one export's settings out of a submitted form.
// Validation of the count and of the export key itself belongs to the service,
// which is the one place that decides what a stored row may say.
func exportConfigFromForm(r *http.Request, projectID string) model.ExportConfig {
	config := model.ExportConfig{
		ProjectID: strings.TrimSpace(projectID),
		ExportKey: strings.TrimSpace(r.FormValue("export_key")),
		Aktif:     r.FormValue("export_aktif") == "1",
	}
	count, err := strconv.Atoi(strings.TrimSpace(r.FormValue("ttd_count")))
	if err != nil {
		// Nothing chosen reads as the single signature every report started
		// with, rather than as a refusal.
		count = 1
	}
	config.TTDCount = count
	for index := range config.Slots {
		suffix := strconv.Itoa(index + 1)
		config.Slots[index] = model.ExportSlot{
			Nama:    strings.TrimSpace(r.FormValue("slot" + suffix + "_nama")),
			Jabatan: strings.TrimSpace(r.FormValue("slot" + suffix + "_jabatan")),
		}
	}
	return config
}

// exportAktif says whether the project this request is bound to allows one
// report to be downloaded.
func (s *Server) exportAktif(key model.ExportTypeKey) bool {
	return s.project.ExportConfigFor(key).Aktif
}

// requireExportAktif refuses a download of a report the project has switched
// off. The pages hide the buttons, but the URL is guessable and the setting has
// to hold there too.
func (s *Server) requireExportAktif(w http.ResponseWriter, key model.ExportTypeKey) bool {
	if s.exportAktif(key) {
		return true
	}
	label := exportLabels[key]
	if label == "" {
		label = "Laporan ini"
	}
	http.Error(w, label+" dinonaktifkan untuk project ini", http.StatusForbidden)
	return false
}
