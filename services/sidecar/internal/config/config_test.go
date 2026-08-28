package config

import (
	"path/filepath"
	"testing"
)

func TestParseProductionRequiresSessionToken(t *testing.T) {
	_, err := Parse([]string{"--db", filepath.Join(t.TempDir(), "app.db")}, func(string) string { return "" })
	if err == nil {
		t.Fatal("expected missing token error")
	}
}

func TestParseAllowsDynamicPortAndDevDatabase(t *testing.T) {
	cfg, err := Parse([]string{"--dev", "--port", "0"}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.Port != 0 {
		t.Fatalf("Port = %d, want 0", cfg.Port)
	}
	if !filepath.IsAbs(cfg.DatabasePath) {
		t.Fatalf("DatabasePath = %q, want absolute path", cfg.DatabasePath)
	}
	if cfg.ArtifactDir != filepath.Join(filepath.Dir(cfg.DatabasePath), "artifacts") {
		t.Fatalf("ArtifactDir = %q, want database sibling artifacts", cfg.ArtifactDir)
	}
}

func TestParseAcceptsExplicitArtifactDirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "controlled-artifacts")
	cfg, err := Parse([]string{"--dev", "--artifacts", directory}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	want, _ := filepath.Abs(directory)
	if cfg.ArtifactDir != want {
		t.Fatalf("ArtifactDir = %q, want %q", cfg.ArtifactDir, want)
	}
}

func TestParseReadsArtifactDirectoryEnvironment(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "artifacts-from-env")
	cfg, err := Parse([]string{"--dev"}, func(key string) string {
		if key == "OPC_ARTIFACT_DIR" {
			return directory
		}
		return ""
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	want, _ := filepath.Abs(directory)
	if cfg.ArtifactDir != want {
		t.Fatalf("ArtifactDir = %q, want %q", cfg.ArtifactDir, want)
	}
}

func TestParseRejectsWildcardOrigin(t *testing.T) {
	_, err := Parse([]string{"--dev", "--allowed-origins", "*"}, func(string) string { return "" })
	if err == nil {
		t.Fatal("expected wildcard origin error")
	}
}
