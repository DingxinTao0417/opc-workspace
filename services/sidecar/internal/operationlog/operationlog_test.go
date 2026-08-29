package operationlog

import (
	"bytes"
	"errors"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type failingWriteCloser struct{}

func (failingWriteCloser) Write([]byte) (int, error) { return 0, errors.New("disk unavailable") }
func (failingWriteCloser) Close() error              { return nil }

func TestLoggerWritesStderrAndSanitizedFile(t *testing.T) {
	directory := t.TempDir()
	var stderr bytes.Buffer
	logger, closer, err := open(directory, &stderr, 1024, 2, "session-secret")
	if err != nil {
		t.Fatalf("open logger: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })

	logger.Printf("request complete token=%s authorization=Bearer another-secret", "session-secret")
	if err := closer.Close(); err != nil {
		t.Fatalf("close logger: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(directory, FileName))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	for name, value := range map[string]string{"file": string(content), "stderr": stderr.String()} {
		if strings.Contains(value, "session-secret") || strings.Contains(value, "another-secret") {
			t.Fatalf("%s log leaked a secret: %s", name, value)
		}
		if strings.Count(value, "[REDACTED]") != 2 {
			t.Fatalf("%s redaction count = %d, want 2: %s", name, strings.Count(value, "[REDACTED]"), value)
		}
	}
}

func TestLoggerRotatesAndBoundsArchives(t *testing.T) {
	directory := t.TempDir()
	logger, closer, err := open(directory, &bytes.Buffer{}, 90, 2)
	if err != nil {
		t.Fatalf("open logger: %v", err)
	}
	for index := 0; index < 8; index++ {
		logger.Printf("entry=%d payload=abcdefghijklmnopqrstuvwxyz", index)
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("close logger: %v", err)
	}
	for _, name := range []string{FileName, FileName + ".1", FileName + ".2"} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(directory, FileName+".3")); !os.IsNotExist(err) {
		t.Fatalf("archive beyond retention exists or could not be checked: %v", err)
	}
}

func TestFileWriteFailureFallsBackToStderrOnce(t *testing.T) {
	var stderr bytes.Buffer
	writer := &resilientWriter{stderr: &stderr, file: failingWriteCloser{}}
	logger := log.New(writer, "", 0)

	logger.Print("first entry")
	logger.Print("second entry")

	output := stderr.String()
	if !strings.Contains(output, "first entry") || !strings.Contains(output, "second entry") {
		t.Fatalf("stderr fallback lost entries: %s", output)
	}
	if strings.Count(output, "operational file logging disabled") != 1 {
		t.Fatalf("fallback warning count = %d, want 1: %s", strings.Count(output, "operational file logging disabled"), output)
	}
}

func TestLoggerRestrictsCurrentFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows reports inherited ACL permissions rather than POSIX mode bits")
	}
	directory := t.TempDir()
	_, closer, err := open(directory, &bytes.Buffer{}, 1024, 2)
	if err != nil {
		t.Fatalf("open logger: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })
	info, err := os.Stat(filepath.Join(directory, FileName))
	if err != nil {
		t.Fatalf("stat log: %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("log permissions = %o, want no group/other access", info.Mode().Perm())
	}
}

func TestLoggerRefusesSymlinkCurrentFile(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target.log")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(directory, FileName)); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, _, err := open(directory, &bytes.Buffer{}, 1024, 2); err == nil {
		t.Fatal("expected symlink log file to be rejected")
	}
}
