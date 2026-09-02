package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"

	"opp-management/internal/config"
	"opp-management/internal/export"
	"opp-management/internal/handler"
	"opp-management/internal/model"
	"opp-management/internal/receipt"
	"opp-management/internal/repository"
	"opp-management/internal/service"
	"opp-management/internal/session"
	"opp-management/internal/tally"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sheetsClient, err := sheets.NewService(ctx,
		option.WithCredentialsFile(cfg.GoogleCredentialsFile),
		option.WithScopes(sheets.SpreadsheetsScope),
	)
	if err != nil {
		log.Fatal(err)
	}

	// The master spreadsheet holds the accounts and the list of projects. It is
	// usually also the first project's own spreadsheet, which is what lets a
	// deployment that predates projects carry on with nothing moved.
	master := repository.NewGoogleSheetsRepository(sheetsClient, cfg.GoogleSpreadsheetID, cfg.Timezone)
	setupCtx, cancelSetup := context.WithTimeout(ctx, 60*time.Second)
	if err := master.EnsureMasterSchema(setupCtx); err != nil {
		cancelSetup()
		log.Fatal(err)
	}
	if err := master.EnsureSchema(setupCtx); err != nil {
		cancelSetup()
		log.Fatal(err)
	}

	now := func() time.Time { return time.Now().In(cfg.Timezone) }
	defaults := model.ProjectSettings{
		WorkStart:            cfg.WorkStart,
		WorkEnd:              cfg.WorkEnd,
		LateToleranceMinutes: cfg.LateToleranceMinutes,
		A2BWorkMinutes:       cfg.A2BWorkMinutes,
		Company:              cfg.CompanyName,
		SignatoryName:        cfg.SignatoryName,
		SignatoryTitle:       cfg.SignatoryTitle,
		SignatoryPlace:       cfg.SignatoryPlace,
	}
	projectService := service.NewProjectService(master, defaults, cfg.Timezone, now)
	// The first start after this release writes the project the app has always
	// been keeping books for, pointing at the spreadsheet it has always used.
	first, err := projectService.EnsureFirst(setupCtx, cfg.ProjectName, cfg.GoogleSpreadsheetID)
	if err != nil {
		cancelSetup()
		log.Fatal(err)
	}
	cancelSetup()
	log.Printf("project pertama: %s (%s)", first.Nama, first.ProjectID)

	authService := service.NewAuthService(master, cfg.Timezone, now)
	sessions := session.NewManager(cfg.SessionTTL, cfg.SessionCookieSecure)

	// Each project's services are built the first time that project is opened,
	// over a store pointed at its own spreadsheet.
	factory := func(ctx context.Context, project model.Project) (*handler.ProjectServices, error) {
		store := repository.Store(master)
		if !strings.EqualFold(strings.TrimSpace(project.SpreadsheetID), cfg.GoogleSpreadsheetID) {
			projectStore := repository.NewGoogleSheetsRepository(sheetsClient, project.SpreadsheetID, cfg.Timezone)
			if err := projectStore.EnsureSchema(ctx); err != nil {
				return nil, err
			}
			store = projectStore
		}
		return buildProjectServices(store, projectService, project, cfg.Timezone, now)
	}

	// Naming a project is what prepares its spreadsheet. Doing it here rather
	// than on first open means a file that was never shared with the service
	// account is reported while the person is still on the screen that named it.
	provision := func(ctx context.Context, spreadsheetID string) error {
		// A deadline of its own, comfortably inside the server's write timeout,
		// so a Google that has stopped answering ends as a message on the page
		// rather than as a connection the browser watches die.
		ctx, cancel := context.WithTimeout(ctx, provisionTimeout)
		defer cancel()
		store := repository.NewGoogleSheetsRepository(sheetsClient, spreadsheetID, cfg.Timezone)
		if err := store.EnsureSchema(ctx); err != nil {
			return spreadsheetProblem(err)
		}
		return nil
	}

	webServer, err := handler.NewServer(handler.Deps{
		Auth:           authService,
		Projects:       projectService,
		Services:       factory,
		Provision:      provision,
		Sessions:       sessions,
		Location:       cfg.Timezone,
		Now:            now,
		MaxUploadBytes: cfg.MaxUploadBytes,
		MaxPhotoChars:  cfg.MaxPhotoChars,
		Branding: handler.Branding{
			Company: cfg.CompanyName,
			Signatory: export.Signatory{
				Name:  cfg.SignatoryName,
				Title: cfg.SignatoryTitle,
				Place: cfg.SignatoryPlace,
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	if cfg.MiMoAPIKey != "" {
		scanner, err := receipt.NewMiMoScanner(
			cfg.MiMoAPIKey,
			cfg.MiMoBaseURL,
			cfg.MiMoModel,
			&http.Client{Timeout: cfg.MiMoTimeout},
		)
		if err != nil {
			log.Fatalf("konfigurasi scan struk MiMo tidak valid: %v", err)
		}
		webServer.WithReceiptScanner(scanner)

		// The tally sheet is read by the same provider and key, with its own
		// prompt and its own token budget: a page of handwriting needs far more
		// room than a receipt.
		sheetScanner, err := tally.NewMiMoScanner(
			cfg.MiMoAPIKey,
			cfg.MiMoBaseURL,
			cfg.MiMoModel,
			&http.Client{Timeout: cfg.MiMoSheetTimeout},
		)
		if err != nil {
			log.Fatalf("konfigurasi scan lembar produksi MiMo tidak valid: %v", err)
		}
		webServer.WithTallyScanner(sheetScanner, cfg.MiMoSheetTimeout)
		log.Printf("Scan struk (%s) dan lembar produksi (%s) MiMo aktif (model=%s)",
			cfg.MiMoTimeout, cfg.MiMoSheetTimeout, cfg.MiMoModel)
	} else {
		log.Printf("Scan MiMo nonaktif: MIMO_API_KEY belum diatur")
	}

	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           webServer.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		// Thirty seconds is right for every page here. The sheet scan is the one
		// request that legitimately runs longer, and it lifts its own deadline
		// for itself rather than loosening the limit for everything.
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("OPP HRIS Absensi listening on http://localhost:%s (Google Sheets, timezone=%s)", cfg.Port, cfg.TimezoneName)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown: %v", err)
	}
}

// provisionTimeout bounds preparing one project's spreadsheet. Setting it up is
// a handful of batched requests; anything past this is Google not answering
// rather than Google being slow.
const provisionTimeout = 20 * time.Second

// spreadsheetProblem turns what the Sheets API says into what the person who
// pasted the id has to do about it. Every branch here is a failure somebody can
// act on; anything else is passed through and logged as itself rather than
// dressed up in a guess.
func spreadsheetProblem(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("Google Sheets tidak merespons dalam %s. Coba lagi sebentar", provisionTimeout)
	}
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) {
		switch apiErr.Code {
		case http.StatusNotFound:
			return fmt.Errorf("Spreadsheet dengan ID itu tidak ditemukan. Periksa lagi bagian di antara /d/ dan /edit pada URL")
		case http.StatusForbidden:
			return fmt.Errorf("Spreadsheet itu belum dibagikan ke service account aplikasi. Buka Share di Google Sheets, tambahkan email service account sebagai Editor, lalu coba lagi")
		case http.StatusUnauthorized:
			return fmt.Errorf("Kredensial service account ditolak Google. Periksa file credentials aplikasi")
		case http.StatusTooManyRequests:
			return fmt.Errorf("Kuota Google Sheets sedang penuh. Coba lagi satu menit lagi")
		}
		if apiErr.Code >= 500 {
			return fmt.Errorf("Google Sheets sedang bermasalah (kode %d). Coba lagi sebentar", apiErr.Code)
		}
	}
	// Not an API answer at all: no route to Google, DNS, TLS, a proxy.
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return fmt.Errorf("Tidak bisa menghubungi Google Sheets. Periksa koneksi internet server")
	}
	return err
}

// buildProjectServices wires one project's services over its own store. The
// figures each is given come from that project's settings, already merged with
// the deployment defaults, so a project that configures nothing behaves exactly
// as the app did before it could be configured at all.
func buildProjectServices(store repository.Store, projects *service.ProjectService, project model.Project, location *time.Location, now service.NowFunc) (*handler.ProjectServices, error) {
	schedule, err := service.NewSchedule(project.Settings.WorkStart, project.Settings.WorkEnd, project.Settings.LateToleranceMinutes)
	if err != nil {
		return nil, fmt.Errorf("jadwal absensi project %s tidak valid: %w", project.Nama, err)
	}
	// Accounts live in the master spreadsheet, so the two services that read
	// them are pointed at this project's members rather than at their own store.
	members := projects.MembersOf(project.Nama)
	return &handler.ProjectServices{
		Attendance:   service.NewAttendanceService(store, location, now).WithSchedule(schedule).WithUsers(members),
		UnitDT:       service.NewUnitDTService(store, location, now),
		Produksi:     service.NewProduksiService(store, location, now),
		Overview:     service.NewOverviewService(store, location, now),
		UnitA2B:      service.NewUnitA2BService(store, location, now),
		Nota:         service.NewNotaService(store, location, now),
		Leave:        service.NewLeaveService(store, location, now).WithSchedule(schedule).WithUsers(members),
		UnitOverview: service.NewUnitOverviewService(store, location, now),
		FuelMasuk:    service.NewFuelMasukService(store, location, now),
		FuelKeluar:   service.NewFuelKeluarService(store, location, now),
		HourMeter:    service.NewHourMeterService(store, location, now).WithWorkMinutes(project.Settings.A2BWorkMinutes),
		Company:      project.Settings.Company,
		Signatory: export.Signatory{
			Name:  project.Settings.SignatoryName,
			Title: project.Settings.SignatoryTitle,
			Place: project.Settings.SignatoryPlace,
		},
	}, nil
}
