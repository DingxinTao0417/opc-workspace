package config

import (
	"path/filepath"
	"strings"
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
	if cfg.InvoicePDFDir != filepath.Join(filepath.Dir(cfg.DatabasePath), "invoices") {
		t.Fatalf("InvoicePDFDir = %q, want database sibling invoices", cfg.InvoicePDFDir)
	}
	if cfg.BackupDir != filepath.Join(filepath.Dir(cfg.DatabasePath), "backups") {
		t.Fatalf("BackupDir = %q, want database sibling backups", cfg.BackupDir)
	}
	if cfg.LogDir != filepath.Join(filepath.Dir(cfg.DatabasePath), "logs") {
		t.Fatalf("LogDir = %q, want database sibling logs", cfg.LogDir)
	}
	if cfg.ExitOnStdinClose {
		t.Fatal("ExitOnStdinClose = true, want false by default")
	}
}

func TestParseReadsExitOnStdinCloseEnvironment(t *testing.T) {
	cfg, err := Parse([]string{"--dev"}, func(key string) string {
		if key == "OPC_EXIT_ON_STDIN_CLOSE" {
			return "true"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !cfg.ExitOnStdinClose {
		t.Fatal("ExitOnStdinClose = false, want true")
	}
}

func TestParseRejectsInvalidExitOnStdinCloseEnvironment(t *testing.T) {
	_, err := Parse([]string{"--dev"}, func(key string) string {
		if key == "OPC_EXIT_ON_STDIN_CLOSE" {
			return "enabled"
		}
		return ""
	})
	if err == nil {
		t.Fatal("expected invalid OPC_EXIT_ON_STDIN_CLOSE error")
	}
	if !strings.Contains(err.Error(), "OPC_EXIT_ON_STDIN_CLOSE: must be a boolean") {
		t.Fatalf("Parse() error = %q, want strict boolean error", err)
	}
}

func TestParseAcceptsExplicitBackupDirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "verified-backups")
	cfg, err := Parse([]string{"--dev", "--backups", directory}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	want, _ := filepath.Abs(directory)
	if cfg.BackupDir != want {
		t.Fatalf("BackupDir = %q, want %q", cfg.BackupDir, want)
	}
}

func TestParseRejectsOverlappingBackupAndArtifactDirectories(t *testing.T) {
	root := t.TempDir()
	_, err := Parse([]string{
		"--dev",
		"--artifacts", filepath.Join(root, "artifacts"),
		"--backups", filepath.Join(root, "artifacts", "backups"),
	}, func(string) string { return "" })
	if err == nil {
		t.Fatal("expected overlapping directories error")
	}
}

func TestParseRejectsBackupDirectoryContainingDatabase(t *testing.T) {
	root := t.TempDir()
	_, err := Parse([]string{
		"--dev",
		"--db", filepath.Join(root, "data", "workspace.db"),
		"--artifacts", filepath.Join(root, "artifacts"),
		"--backups", root,
	}, func(string) string { return "" })
	if err == nil {
		t.Fatal("expected backup directory containing database error")
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

func TestParseAcceptsExplicitInvoicePDFDirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "generated-invoices")
	cfg, err := Parse([]string{"--dev", "--invoices", directory}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	want, _ := filepath.Abs(directory)
	if cfg.InvoicePDFDir != want {
		t.Fatalf("InvoicePDFDir = %q, want %q", cfg.InvoicePDFDir, want)
	}
}

func TestParseReadsInvoicePDFDirectoryEnvironment(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "invoices-from-env")
	cfg, err := Parse([]string{"--dev"}, func(key string) string {
		if key == "OPC_INVOICE_DIR" {
			return directory
		}
		return ""
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	want, _ := filepath.Abs(directory)
	if cfg.InvoicePDFDir != want {
		t.Fatalf("InvoicePDFDir = %q, want %q", cfg.InvoicePDFDir, want)
	}
}

func TestParseRejectsOverlappingInvoiceAndArtifactDirectories(t *testing.T) {
	root := t.TempDir()
	_, err := Parse([]string{
		"--dev",
		"--artifacts", filepath.Join(root, "assets"),
		"--invoices", filepath.Join(root, "assets", "invoices"),
	}, func(string) string { return "" })
	if err == nil {
		t.Fatal("expected overlapping invoice PDF and Artifact directories error")
	}
}

func TestParseRejectsInvoiceDirectoryOverlappingBackupOrLogRoots(t *testing.T) {
	root := t.TempDir()
	for _, test := range []struct {
		name string
		args []string
	}{
		{
			name: "backup",
			args: []string{
				"--backups", filepath.Join(root, "invoices", "backups"),
				"--logs", filepath.Join(root, "logs"),
			},
		},
		{
			name: "log",
			args: []string{
				"--backups", filepath.Join(root, "backups"),
				"--logs", filepath.Join(root, "invoices", "logs"),
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := []string{
				"--dev",
				"--db", filepath.Join(root, test.name, "workspace.db"),
				"--artifacts", filepath.Join(root, "artifacts"),
				"--invoices", filepath.Join(root, "invoices"),
			}
			_, err := Parse(append(args, test.args...), func(string) string { return "" })
			if err == nil {
				t.Fatalf("expected invoice PDF directory overlapping %s root error", test.name)
			}
		})
	}
}

func TestParseRejectsInvoiceDirectoryContainingDatabase(t *testing.T) {
	root := t.TempDir()
	_, err := Parse([]string{
		"--dev",
		"--db", filepath.Join(root, "data", "workspace.db"),
		"--artifacts", filepath.Join(root, "artifacts"),
		"--invoices", root,
	}, func(string) string { return "" })
	if err == nil {
		t.Fatal("expected invoice PDF directory containing database error")
	}
}

func TestParseRequiresInvoicePDFDirectoryForInMemoryDatabase(t *testing.T) {
	root := t.TempDir()
	_, err := Parse([]string{
		"--dev",
		"--db", ":memory:",
		"--artifacts", filepath.Join(root, "artifacts"),
		"--backups", filepath.Join(root, "backups"),
		"--logs", filepath.Join(root, "logs"),
	}, func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "invoice PDF directory is required") {
		t.Fatalf("Parse() error = %v, want required invoice PDF directory", err)
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

func TestParseReadsLogDirectoryEnvironment(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "diagnostics")
	cfg, err := Parse([]string{"--dev"}, func(key string) string {
		if key == "OPC_LOG_DIR" {
			return directory
		}
		return ""
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	want, _ := filepath.Abs(directory)
	if cfg.LogDir != want {
		t.Fatalf("LogDir = %q, want %q", cfg.LogDir, want)
	}
}

func TestParseRejectsLogDirectoryOverlappingControlledRoots(t *testing.T) {
	root := t.TempDir()
	_, err := Parse([]string{
		"--dev",
		"--artifacts", filepath.Join(root, "artifacts"),
		"--logs", filepath.Join(root, "artifacts", "logs"),
	}, func(string) string { return "" })
	if err == nil {
		t.Fatal("expected overlapping log and Artifact directories error")
	}
}

func TestParseRejectsWildcardOrigin(t *testing.T) {
	_, err := Parse([]string{"--dev", "--allowed-origins", "*"}, func(string) string { return "" })
	if err == nil {
		t.Fatal("expected wildcard origin error")
	}
}
