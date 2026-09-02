package handler

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"opp-management/internal/model"

	"opp-management/internal/photo"
)

// projectMarks is what one settings save says about the project's artwork: a
// newly uploaded mark, or an instruction to go back to the app's own.
//
// The three are separate uploads on purpose. A mark that reads well in a
// sidebar is not always the one a letterhead wants, and a favicon has to be
// square to look like anything at all in a browser tab.
type projectMarks struct {
	LogoSistem      string
	LogoExport      string
	Favicon         string
	ClearLogoSistem bool
	ClearLogoExport bool
	ClearFavicon    bool
}

// readProjectMarks reads the three upload fields. An empty field means the
// stored mark is left alone: somebody editing the working hours has not
// uploaded a logo, and their save must not wipe one. Removing a mark is its own
// checkbox, so "no file" and "go back to the default" are never confused.
func (s *Server) readProjectMarks(r *http.Request) (projectMarks, error) {
	marks := projectMarks{
		ClearLogoSistem: r.FormValue("hapus_logo_sistem") == "1",
		ClearLogoExport: r.FormValue("hapus_logo_export") == "1",
		ClearFavicon:    r.FormValue("hapus_favicon") == "1",
	}
	var err error
	if marks.LogoSistem, err = s.readOptionalLogo(r, "logo_sistem", "Logo sistem"); err != nil {
		return projectMarks{}, err
	}
	if marks.LogoExport, err = s.readOptionalLogo(r, "logo_export", "Logo export"); err != nil {
		return projectMarks{}, err
	}
	// The favicon takes .ico as well: it is the one mark an organisation
	// usually already has in that format.
	if marks.Favicon, err = s.readOptionalMark(r, "favicon", "Favicon", photo.NormalizeFavicon); err != nil {
		return projectMarks{}, err
	}
	return marks, nil
}

// readOptionalLogo reads an upload that must be a PNG or a JPEG.
func (s *Server) readOptionalLogo(r *http.Request, field, label string) (string, error) {
	return s.readOptionalMark(r, field, label, photo.NormalizeLogo)
}

// readOptionalMark turns one uploaded file into the data URL the sheet stores,
// using whichever normalizer that field accepts. It says which field failed,
// because three uploads on one form make "format tidak didukung" on its own a
// guessing game.
func (s *Server) readOptionalMark(r *http.Request, field, label string, normalize func([]byte, int) (string, error)) (string, error) {
	file, _, err := r.FormFile(field)
	if errors.Is(err, http.ErrMissingFile) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("%s gagal dibaca", label)
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, s.maxUploadBytes+1))
	if err != nil {
		return "", fmt.Errorf("%s gagal dibaca", label)
	}
	if int64(len(raw)) > s.maxUploadBytes {
		return "", fmt.Errorf("%s maksimal %d MB", label, s.maxUploadBytes/(1024*1024))
	}
	value, err := normalize(raw, s.maxPhotoChars)
	if err != nil {
		if errors.Is(err, photo.ErrTooLarge) {
			return "", fmt.Errorf("%s terlalu besar untuk disimpan, coba gambar yang lebih kecil", label)
		}
		return "", fmt.Errorf("%s harus berupa gambar %s", label, markFormats(field))
	}
	return value, nil
}

// markFormats names what one field takes, so a refusal says what to try next.
func markFormats(field string) string {
	if field == "favicon" {
		return "PNG, JPG, atau ICO"
	}
	return "PNG atau JPG"
}

// ProjectMark is one upload as the settings form shows it: what it is called,
// what it is for, and whether the project has one yet.
type ProjectMark struct {
	// Field is the form field, and also the suffix of the checkbox that clears
	// it. Naming it once keeps the form and the reader from drifting apart.
	Field string
	Label string
	Lede  string
	Icon  string
	// Accept is what the file picker offers, since the favicon takes a format
	// the other two do not.
	Accept string
	// Ada says the project has uploaded this mark; URL is where the preview
	// fetches it.
	Ada bool
	URL string
}

// projectMarksFor words the three uploads for one project. The preview URLs
// carry the project's own updated_at, so a mark replaced a moment ago is the
// one shown rather than the one the browser cached.
func projectMarksFor(project model.Project) []ProjectMark {
	version := project.UpdatedAt.Unix()
	return []ProjectMark{
		{
			Field: "logo_sistem", Label: "Logo sistem", Icon: "layers",
			Accept: "image/png,image/jpeg",
			Lede:   "Dipakai di sidebar dan header aplikasi.",
			Ada:    strings.TrimSpace(project.Settings.LogoSistem) != "",
			URL:    brandMarkURL(brandLogoPath, project.Settings.LogoSistem, version),
		},
		{
			Field: "logo_export", Label: "Logo export", Icon: "save",
			Accept: "image/png,image/jpeg",
			Lede:   "Dipakai di kop laporan XLSX dan PDF.",
			Ada:    strings.TrimSpace(project.Settings.LogoExport) != "",
			URL:    brandMarkURL(brandExportLogoPath, project.Settings.LogoExport, version),
		},
		{
			Field: "favicon", Label: "Favicon", Icon: "pin",
			Accept: "image/png,image/jpeg,image/x-icon,.ico",
			Lede:   "Icon di tab browser. PNG, JPG, atau ICO; gambar persegi paling rapi.",
			Ada:    strings.TrimSpace(project.Settings.Favicon) != "",
			URL:    brandMarkURL(brandFaviconPath, project.Settings.Favicon, version),
		},
	}
}
