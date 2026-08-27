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
}

func TestParseRejectsWildcardOrigin(t *testing.T) {
	_, err := Parse([]string{"--dev", "--allowed-origins", "*"}, func(string) string { return "" })
	if err == nil {
		t.Fatal("expected wildcard origin error")
	}
}
