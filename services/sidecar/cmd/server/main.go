package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/api"
	"github.com/opc-workspace/opc-sidecar/internal/config"
	"github.com/opc-workspace/opc-sidecar/internal/database"
)

var (
	appVersion = "0.1.0"
	commit     = "unknown"
)

type readyEvent struct {
	Event         string `json:"event"`
	Status        string `json:"status"`
	Host          string `json:"host"`
	Address       string `json:"address"`
	URL           string `json:"url"`
	Port          int    `json:"port"`
	PID           int    `json:"pid"`
	Version       string `json:"version"`
	AppVersion    string `json:"app_version"`
	APIVersion    string `json:"api_version"`
	SchemaVersion int    `json:"schema_version"`
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	logger := log.New(os.Stderr, "sidecar ", log.Ldate|log.Ltime|log.LUTC)

	cfg, err := config.Parse(args, os.Getenv)
	if err != nil {
		logger.Printf("configuration error: %v", err)
		return 2
	}
	latestSchema, err := database.LatestSchemaVersion()
	if err != nil {
		logger.Printf("read embedded schema version failed: %v", err)
		return 1
	}
	restoreResult, err := api.ApplyPendingRestore(cfg.BackupDir, cfg.DatabasePath, cfg.ArtifactDir, latestSchema)
	if err != nil {
		logger.Printf("pending restore failed safely before database startup: %v", err)
		return 1
	}
	if restoreResult.Applied {
		logger.Printf(
			"pending restore applied backup_id=%s rollback_backup_id=%s",
			restoreResult.BackupID,
			restoreResult.RollbackBackupID,
		)
		if restoreResult.CleanupWarning != "" {
			logger.Printf("pending restore cleanup warning: %s", restoreResult.CleanupWarning)
		}
	}

	store, migrationGate, err := database.OpenBeforeDestructiveMigrations(cfg.DatabasePath)
	if err != nil {
		logger.Printf("database initialization failed: %v", err)
		return 1
	}
	if migrationGate != nil {
		backupID, backupErr := api.CreatePreMigrationBackup(store.DB, api.Options{
			AppVersion:    appVersion,
			Commit:        commit,
			SchemaVersion: store.SchemaVersion,
			ArtifactDir:   cfg.ArtifactDir,
			DatabasePath:  cfg.DatabasePath,
			BackupDir:     cfg.BackupDir,
		}, migrationGate.TargetVersion)
		if backupErr != nil {
			_ = store.Close()
			logger.Printf(
				"pre-migration backup failed; schema remains at v%d and migration was not started: %v",
				migrationGate.CurrentVersion,
				backupErr,
			)
			return 1
		}
		logger.Printf(
			"verified pre-migration backup created backup_id=%s schema=%d target_schema=%d",
			backupID,
			migrationGate.CurrentVersion,
			migrationGate.TargetVersion,
		)
		if err := store.Close(); err != nil {
			logger.Printf("close database before destructive migration failed: %v", err)
			return 1
		}
		store, err = database.Open(cfg.DatabasePath)
		if err != nil {
			logger.Printf("database migration failed after verified rollback backup %s: %v", backupID, err)
			return 1
		}
	}
	defer func() {
		if err := store.Close(); err != nil {
			logger.Printf("database close failed: %v", err)
		}
	}()

	if cfg.Seed {
		if err := database.SeedDevelopmentData(store.DB); err != nil {
			logger.Printf("development seed failed: %v", err)
			return 1
		}
	}

	router, err := api.NewRouter(store.DB, api.Options{
		AppVersion:     appVersion,
		Commit:         commit,
		SchemaVersion:  store.SchemaVersion,
		SessionToken:   cfg.SessionToken,
		DevMode:        cfg.DevMode,
		AllowedOrigins: cfg.AllowedOrigins,
		Logger:         logger,
		ArtifactDir:    cfg.ArtifactDir,
		DatabasePath:   cfg.DatabasePath,
		BackupDir:      cfg.BackupDir,
	})
	if err != nil {
		logger.Printf("router initialization failed: %v", err)
		return 1
	}
	defer func() {
		if err := router.Close(); err != nil {
			logger.Printf("router close failed: %v", err)
		}
	}()

	listener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.Port)))
	if err != nil {
		logger.Printf("listen failed: %v", err)
		return 1
	}

	tcpAddress, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		_ = listener.Close()
		logger.Printf("unexpected listener address: %s", listener.Addr())
		return 1
	}

	httpServer := &http.Server{
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       3 * time.Minute,
		WriteTimeout:      3 * time.Minute,
		IdleTimeout:       60 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- httpServer.Serve(listener)
	}()

	ready := readyEvent{
		Event:         "ready",
		Status:        "ok",
		Host:          "127.0.0.1",
		Address:       listener.Addr().String(),
		URL:           fmt.Sprintf("http://127.0.0.1:%d", tcpAddress.Port),
		Port:          tcpAddress.Port,
		PID:           os.Getpid(),
		Version:       appVersion,
		AppVersion:    appVersion,
		APIVersion:    api.Version,
		SchemaVersion: store.SchemaVersion,
	}
	if err := json.NewEncoder(os.Stdout).Encode(ready); err != nil {
		logger.Printf("ready event failed: %v", err)
		shutdownServer(httpServer, logger)
		return 1
	}

	signalContext, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	stdinShutdown := watchStdinForShutdown(os.Stdin, logger)

	select {
	case <-signalContext.Done():
		logger.Printf("shutdown requested")
	case <-stdinShutdown:
		logger.Printf("shutdown requested by parent process")
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Printf("server stopped unexpectedly: %v", err)
			return 1
		}
		return 0
	}

	shutdownErr := shutdownServer(httpServer, logger)
	checkpointErr := store.Checkpoint()
	if checkpointErr != nil {
		logger.Printf("WAL checkpoint failed: %v", checkpointErr)
	}
	if shutdownErr != nil || checkpointErr != nil {
		return 1
	}
	return 0
}

func watchStdinForShutdown(input io.Reader, logger *log.Logger) <-chan struct{} {
	shutdown := make(chan struct{}, 1)
	go func() {
		scanner := bufio.NewScanner(input)
		for scanner.Scan() {
			if strings.EqualFold(strings.TrimSpace(scanner.Text()), "shutdown") {
				shutdown <- struct{}{}
				return
			}
			logger.Printf("ignored unknown stdin control message")
		}
		if err := scanner.Err(); err != nil {
			logger.Printf("stdin control channel failed: %v", err)
		}
	}()
	return shutdown
}

func shutdownServer(server *http.Server, logger *log.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Printf("graceful HTTP shutdown failed: %v", err)
		_ = server.Close()
		return err
	}
	return nil
}
