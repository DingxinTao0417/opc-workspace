package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
)

const defaultDevelopmentDatabase = ".local/dev-data/opc-workspace.db"

var defaultOrigins = []string{
	"tauri://localhost",
	"http://tauri.localhost",
	"https://tauri.localhost",
	"http://localhost:1420",
	"http://127.0.0.1:1420",
}

type Config struct {
	DatabasePath   string
	ArtifactDir    string
	BackupDir      string
	LogDir         string
	Port           int
	SessionToken   string
	AllowedOrigins []string
	DevMode        bool
	Seed           bool
}

type getenvFunc func(string) string

func Parse(args []string, getenv getenvFunc) (Config, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}

	portValue := firstNonEmpty(getenv("OPC_PORT"), getenv("OPC_SIDECAR_PORT"))
	port, err := envInt(portValue, 0)
	if err != nil {
		return Config{}, fmt.Errorf("OPC_PORT: %w", err)
	}
	seed, err := envBool(getenv("OPC_DEV_SEED"), false)
	if err != nil {
		return Config{}, fmt.Errorf("OPC_DEV_SEED: %w", err)
	}
	dev, err := envBool(getenv("OPC_DEV"), false)
	if err != nil {
		return Config{}, fmt.Errorf("OPC_DEV: %w", err)
	}

	originsDefault := strings.TrimSpace(getenv("OPC_ALLOWED_ORIGINS"))
	if originsDefault == "" {
		originsDefault = strings.Join(defaultOrigins, ",")
	}

	fs := flag.NewFlagSet("opc-sidecar", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	databaseDefault := firstNonEmpty(getenv("OPC_DB_PATH"), getenv("OPC_DATABASE_PATH"))
	databasePath := fs.String("db", strings.TrimSpace(databaseDefault), "SQLite database path")
	artifactDefault := strings.TrimSpace(getenv("OPC_ARTIFACT_DIR"))
	artifactDir := fs.String("artifacts", artifactDefault, "controlled Task Artifact directory")
	backupDefault := strings.TrimSpace(getenv("OPC_BACKUP_DIR"))
	backupDir := fs.String("backups", backupDefault, "local verified backup package directory")
	logDefault := strings.TrimSpace(getenv("OPC_LOG_DIR"))
	logDir := fs.String("logs", logDefault, "local diagnostic directory")
	portFlag := fs.Int("port", port, "loopback port; 0 selects a free port")
	devFlag := fs.Bool("dev", dev, "enable explicit development-only relaxations")
	seedFlag := fs.Bool("seed", seed, "seed idempotent development data (requires --dev)")
	originsFlag := fs.String("allowed-origins", originsDefault, "comma-separated exact allowed origins")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if len(fs.Args()) != 0 {
		return Config{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}
	if *portFlag < 0 || *portFlag > 65535 {
		return Config{}, fmt.Errorf("port must be between 0 and 65535")
	}
	if *seedFlag && !*devFlag {
		return Config{}, errors.New("--seed is development-only and requires --dev")
	}

	path := strings.TrimSpace(*databasePath)
	if path == "" && *devFlag {
		path = defaultDevelopmentDatabase
	}
	if path == "" {
		return Config{}, errors.New("database path is required via --db or OPC_DB_PATH")
	}
	if path != ":memory:" {
		path, err = filepath.Abs(path)
		if err != nil {
			return Config{}, fmt.Errorf("resolve database path: %w", err)
		}
	}

	artifacts := strings.TrimSpace(*artifactDir)
	if artifacts == "" {
		if path == ":memory:" {
			return Config{}, errors.New("artifact directory is required with an in-memory database")
		}
		artifacts = filepath.Join(filepath.Dir(path), "artifacts")
	}
	artifacts, err = filepath.Abs(artifacts)
	if err != nil {
		return Config{}, fmt.Errorf("resolve artifact directory: %w", err)
	}
	if path != ":memory:" && strings.EqualFold(filepath.Clean(artifacts), filepath.Clean(path)) {
		return Config{}, errors.New("artifact directory must not be the database file")
	}

	backups := strings.TrimSpace(*backupDir)
	if backups == "" {
		if path == ":memory:" {
			return Config{}, errors.New("backup directory is required with an in-memory database")
		}
		backups = filepath.Join(filepath.Dir(path), "backups")
	}
	backups, err = filepath.Abs(backups)
	if err != nil {
		return Config{}, fmt.Errorf("resolve backup directory: %w", err)
	}
	if path != ":memory:" && strings.EqualFold(filepath.Clean(backups), filepath.Clean(path)) {
		return Config{}, errors.New("backup directory must not be the database file")
	}
	if path != ":memory:" && pathsOverlap(backups, path) {
		return Config{}, errors.New("backup directory must not contain the database file")
	}
	if pathsOverlap(backups, artifacts) {
		return Config{}, errors.New("backup directory and artifact directory must not overlap")
	}

	logs := strings.TrimSpace(*logDir)
	if logs == "" {
		if path == ":memory:" {
			return Config{}, errors.New("log directory is required with an in-memory database")
		}
		logs = filepath.Join(filepath.Dir(path), "logs")
	}
	logs, err = filepath.Abs(logs)
	if err != nil {
		return Config{}, fmt.Errorf("resolve log directory: %w", err)
	}
	if path != ":memory:" && strings.EqualFold(filepath.Clean(logs), filepath.Clean(path)) {
		return Config{}, errors.New("log directory must not be the database file")
	}
	if pathsOverlap(logs, artifacts) || pathsOverlap(logs, backups) {
		return Config{}, errors.New("log directory must not overlap artifact or backup directories")
	}

	host := strings.TrimSpace(getenv("OPC_HOST"))
	if host != "" && host != "127.0.0.1" {
		return Config{}, errors.New("OPC_HOST must be 127.0.0.1; the Sidecar never binds to a public interface")
	}
	token := strings.TrimSpace(firstNonEmpty(getenv("OPC_SESSION_TOKEN"), getenv("OPC_SIDECAR_TOKEN")))
	if token == "" && !*devFlag {
		return Config{}, errors.New("OPC_SESSION_TOKEN is required outside --dev")
	}

	origins, err := parseOrigins(*originsFlag)
	if err != nil {
		return Config{}, err
	}

	return Config{
		DatabasePath:   path,
		ArtifactDir:    artifacts,
		BackupDir:      backups,
		LogDir:         logs,
		Port:           *portFlag,
		SessionToken:   token,
		AllowedOrigins: origins,
		DevMode:        *devFlag,
		Seed:           *seedFlag,
	}, nil
}

func pathsOverlap(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	for _, pair := range [][2]string{{left, right}, {right, left}} {
		relative, err := filepath.Rel(pair[0], pair[1])
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func envInt(value string, fallback int) (int, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("must be an integer")
	}
	return parsed, nil
}

func envBool(value string, fallback bool) (bool, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("must be a boolean")
	}
	return parsed, nil
}

func parseOrigins(raw string) ([]string, error) {
	seen := make(map[string]struct{})
	origins := make([]string, 0)
	for _, item := range strings.Split(raw, ",") {
		origin := strings.TrimSpace(item)
		if origin == "" {
			continue
		}
		if origin == "*" || strings.Contains(origin, "*") {
			return nil, errors.New("allowed origins must be exact; wildcards are not permitted")
		}
		if _, exists := seen[origin]; exists {
			continue
		}
		seen[origin] = struct{}{}
		origins = append(origins, origin)
	}
	if len(origins) == 0 {
		return nil, errors.New("at least one allowed origin is required")
	}
	return origins, nil
}
