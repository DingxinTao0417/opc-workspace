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
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/api"
	"github.com/opc-workspace/opc-sidecar/internal/config"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/operationlog"
	"github.com/opc-workspace/opc-sidecar/internal/runlease"
)

var (
	appVersion              = "0.1.1"
	commit                  = "unknown"
	syncInvoicePDFDirectory = syncStartupDirectory
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

type startupEvent struct {
	Event string `json:"event"`
	Stage string `json:"stage"`
}

func writeStartupStage(writer io.Writer, stage string) error {
	return json.NewEncoder(writer).Encode(startupEvent{Event: "startup", Stage: stage})
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
	fileLogger, logCloser, logErr := operationlog.Open(cfg.LogDir, os.Stderr, cfg.SessionToken)
	if logErr != nil {
		logger.Printf("operational file logging unavailable; continuing with stderr: %v", logErr)
	} else {
		logger = fileLogger
		defer func() {
			if err := logCloser.Close(); err != nil {
				_, _ = fmt.Fprintln(os.Stderr, "sidecar operational log close failed")
			}
		}()
	}
	if err := prepareInvoicePDFDirectory(cfg.InvoicePDFDir); err != nil {
		logger.Printf("invoice PDF directory initialization failed: %v", err)
		recordStartupFailure(cfg.LogDir, api.StartupIncidentSidecarStartup, logger)
		return 1
	}
	if err := writeStartupStage(os.Stdout, "acquiring_workspace_lock"); err != nil {
		logger.Printf("startup progress event failed: %v", err)
		return 1
	}
	runLease, err := runlease.Acquire(cfg.DatabasePath)
	if err != nil {
		if errors.Is(err, runlease.ErrAlreadyHeld) {
			logger.Printf("database run lease unavailable: %v", err)
			return 1
		}
		logger.Printf("database initialization failed before opening database: %v", err)
		recordStartupFailure(cfg.LogDir, api.StartupIncidentDatabaseStartup, logger)
		return 1
	}
	defer func() {
		if err := runLease.Close(); err != nil {
			logger.Printf("database run lease close failed: %v", err)
		}
	}()

	latestSchema, err := database.LatestSchemaVersion()
	if err != nil {
		logger.Printf("read embedded schema version failed: %v", err)
		recordStartupFailure(cfg.LogDir, api.StartupIncidentSidecarStartup, logger)
		return 1
	}
	restoreResult, err := api.ApplyPendingRestoreWithInvoicePDFsAndProgress(
		cfg.BackupDir, cfg.DatabasePath, cfg.ArtifactDir, cfg.InvoicePDFDir, latestSchema,
		func(stage api.StartupRestoreStage) {
			if progressErr := writeStartupStage(os.Stdout, string(stage)); progressErr != nil {
				logger.Printf("startup progress event failed: %v", progressErr)
			}
		},
	)
	if err != nil {
		logger.Printf("pending restore failed safely before database startup: %v", err)
		recordStartupFailure(cfg.LogDir, api.StartupIncidentSidecarStartup, logger)
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

	if err := writeStartupStage(os.Stdout, "opening_database"); err != nil {
		logger.Printf("startup progress event failed: %v", err)
		return 1
	}
	store, migrationGate, err := database.OpenBeforeDestructiveMigrations(cfg.DatabasePath)
	if err != nil {
		logger.Printf("database initialization failed: %v", err)
		recordStartupFailure(cfg.LogDir, api.StartupIncidentDatabaseStartup, logger)
		return 1
	}
	if migrationGate != nil {
		if err := writeStartupStage(os.Stdout, "creating_migration_rollback"); err != nil {
			_ = store.Close()
			logger.Printf("startup progress event failed: %v", err)
			return 1
		}
		backupID, backupErr := api.CreatePreMigrationBackup(store.DB, api.Options{
			AppVersion:    appVersion,
			Commit:        commit,
			SchemaVersion: store.SchemaVersion,
			ArtifactDir:   cfg.ArtifactDir,
			InvoicePDFDir: cfg.InvoicePDFDir,
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
			recordStartupFailure(cfg.LogDir, api.StartupIncidentDatabaseMigration, logger)
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
			recordStartupFailure(cfg.LogDir, api.StartupIncidentDatabaseMigration, logger)
			return 1
		}
		if err := writeStartupStage(os.Stdout, "applying_database_migration"); err != nil {
			logger.Printf("startup progress event failed: %v", err)
			return 1
		}
		store, err = database.Open(cfg.DatabasePath)
		if err != nil {
			logger.Printf("database migration failed after verified rollback backup %s: %v", backupID, err)
			recordStartupFailure(cfg.LogDir, api.StartupIncidentDatabaseMigration, logger)
			return 1
		}
	}
	defer func() {
		if err := store.Close(); err != nil {
			logger.Printf("database close failed: %v", err)
		}
	}()

	if err := writeStartupStage(os.Stdout, "initializing_workspace"); err != nil {
		logger.Printf("startup progress event failed: %v", err)
		return 1
	}
	if cfg.Seed {
		if err := database.SeedDevelopmentData(store.DB); err != nil {
			logger.Printf("development seed failed: %v", err)
			recordStartupFailure(cfg.LogDir, api.StartupIncidentSidecarStartup, logger)
			return 1
		}
	}
	if err := api.ReplayStartupIncidents(store.DB, cfg.LogDir, logger); err != nil {
		logger.Printf("startup incident replay deferred: %v", err)
	}

	if err := writeStartupStage(os.Stdout, "starting_local_api"); err != nil {
		logger.Printf("startup progress event failed: %v", err)
		return 1
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
		InvoicePDFDir:  cfg.InvoicePDFDir,
		DatabasePath:   cfg.DatabasePath,
		BackupDir:      cfg.BackupDir,
		LogDir:         cfg.LogDir,
		StartupRestore: restoreResult,
	})
	if err != nil {
		logger.Printf("router initialization failed: %v", err)
		recordStartupFailure(cfg.LogDir, api.StartupIncidentSidecarStartup, logger)
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
		recordStartupFailure(cfg.LogDir, api.StartupIncidentSidecarStartup, logger)
		return 1
	}

	tcpAddress, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		_ = listener.Close()
		logger.Printf("unexpected listener address: %s", listener.Addr())
		recordStartupFailure(cfg.LogDir, api.StartupIncidentSidecarStartup, logger)
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
		recordStartupFailure(cfg.LogDir, api.StartupIncidentSidecarStartup, logger)
		return 1
	}

	signalContext, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	stdinShutdown := watchStdinForShutdown(os.Stdin, logger, cfg.ExitOnStdinClose)

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

func prepareInvoicePDFDirectory(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("invoice PDF directory is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve invoice PDF directory: %w", err)
	}
	absolute = filepath.Clean(absolute)
	if err := rejectInvoicePDFPathTraversal(absolute); err != nil {
		return err
	}
	missing, err := missingInvoicePDFDirectories(absolute)
	if err != nil {
		return err
	}
	prepared := false
	defer func() {
		if prepared {
			return
		}
		// Only remove directories this call observed as absent. os.Remove is
		// deliberately non-recursive, so concurrent or user-created contents
		// cannot be discarded during startup error compensation.
		for _, directory := range missing {
			if err := os.Remove(directory); err == nil {
				_ = syncInvoicePDFDirectory(filepath.Dir(directory))
			}
		}
	}()
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return fmt.Errorf("create invoice PDF directory: %w", err)
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return fmt.Errorf("inspect invoice PDF directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("invoice PDF directory must not be a symbolic link")
	}
	if !info.IsDir() {
		return errors.New("invoice PDF path is not a directory")
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return fmt.Errorf("resolve invoice PDF directory links: %w", err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return fmt.Errorf("resolve canonical invoice PDF directory: %w", err)
	}
	resolved = filepath.Clean(resolved)
	if !sameFilesystemPath(absolute, resolved) {
		return errors.New("invoice PDF directory must not traverse a symbolic link or directory junction")
	}
	// Persist every newly created directory entry from the nearest existing
	// ancestor down to the invoice PDF root. The store later syncs the root
	// itself when it creates its marker and internal directories.
	for index := len(missing) - 1; index >= 0; index-- {
		if err := syncInvoicePDFDirectory(filepath.Dir(missing[index])); err != nil {
			return fmt.Errorf("sync invoice PDF directory parent: %w", err)
		}
	}
	prepared = true
	return nil
}

func missingInvoicePDFDirectories(path string) ([]string, error) {
	missing := make([]string, 0, 1)
	candidate := path
	for {
		if _, err := os.Lstat(candidate); err == nil {
			return missing, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("inspect invoice PDF directory ancestor: %w", err)
		}
		missing = append(missing, candidate)
		parent := filepath.Dir(candidate)
		if sameFilesystemPath(parent, candidate) {
			return nil, errors.New("invoice PDF directory has no accessible parent")
		}
		candidate = parent
	}
}

func rejectInvoicePDFPathTraversal(path string) error {
	candidate := path
	for {
		info, err := os.Lstat(candidate)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return errors.New("invoice PDF directory must not traverse a symbolic link or directory junction")
			}
			resolved, err := filepath.EvalSymlinks(candidate)
			if err != nil {
				return fmt.Errorf("resolve existing invoice PDF directory ancestor: %w", err)
			}
			resolved, err = filepath.Abs(resolved)
			if err != nil {
				return fmt.Errorf("resolve canonical invoice PDF directory ancestor: %w", err)
			}
			if !sameFilesystemPath(candidate, resolved) {
				return errors.New("invoice PDF directory must not traverse a symbolic link or directory junction")
			}
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect invoice PDF directory ancestor: %w", err)
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return errors.New("invoice PDF directory has no accessible existing ancestor")
		}
		candidate = parent
	}
}

func sameFilesystemPath(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func recordStartupFailure(logDir string, kind api.StartupIncidentKind, logger *log.Logger) {
	if err := api.RecordStartupIncident(logDir, kind, time.Now()); err != nil && logger != nil {
		logger.Printf("record safe startup incident failed: %v", err)
	}
}

func watchStdinForShutdown(input io.Reader, logger *log.Logger, exitOnEOF bool) <-chan struct{} {
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
			return
		}
		if exitOnEOF {
			shutdown <- struct{}{}
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
