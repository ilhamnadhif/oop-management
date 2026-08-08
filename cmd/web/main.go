package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"

	"opp-management/internal/config"
	"opp-management/internal/export"
	"opp-management/internal/handler"
	"opp-management/internal/repository"
	"opp-management/internal/service"
	"opp-management/internal/session"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := buildStore(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}
	setupCtx, cancelSetup := context.WithTimeout(ctx, 30*time.Second)
	if err := store.EnsureSchema(setupCtx); err != nil {
		cancelSetup()
		log.Fatal(err)
	}
	cancelSetup()

	now := func() time.Time { return time.Now().In(cfg.Timezone) }
	authService := service.NewAuthService(store, cfg.Timezone, now)
	attendanceService := service.NewAttendanceService(store, cfg.Timezone, now)
	unitDTService := service.NewUnitDTService(store, cfg.Timezone, now)
	produksiService := service.NewProduksiService(store, cfg.Timezone, now)
	overviewService := service.NewOverviewService(store, cfg.Timezone, now)
	unitA2BService := service.NewUnitA2BService(store, cfg.Timezone, now)
	notaService := service.NewNotaService(store, cfg.Timezone, now)
	sessions := session.NewManager(cfg.SessionTTL, cfg.SessionCookieSecure)
	webServer, err := handler.NewServer(authService, attendanceService, unitDTService, produksiService, overviewService, unitA2BService, notaService, sessions, cfg.Timezone, now, cfg.MaxUploadBytes, cfg.MaxPhotoChars,
		handler.Branding{
			Company: cfg.CompanyName,
			Signatory: export.Signatory{
				Name:  cfg.SignatoryName,
				Title: cfg.SignatoryTitle,
				Place: cfg.SignatoryPlace,
			},
		})
	if err != nil {
		log.Fatal(err)
	}

	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           webServer.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
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

func buildStore(ctx context.Context, cfg config.Config) (repository.Store, error) {
	serviceClient, err := sheets.NewService(ctx,
		option.WithCredentialsFile(cfg.GoogleCredentialsFile),
		option.WithScopes(sheets.SpreadsheetsScope),
	)
	if err != nil {
		return nil, err
	}
	return repository.NewGoogleSheetsRepository(serviceClient, cfg.GoogleSpreadsheetID, cfg.Timezone), nil
}
