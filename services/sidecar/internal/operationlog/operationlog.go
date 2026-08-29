package operationlog

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

const (
	FileName         = "opc-sidecar.log"
	DefaultMaxBytes  = 5 * 1024 * 1024
	DefaultRetention = 3
)

var bearerPattern = regexp.MustCompile(`(?i)(bearer[[:space:]]+)[^[:space:]]+`)

// Open creates the Sidecar operational logger. Every entry is written to stderr
// and, while available, to a size-bounded rotating file in logDir.
func Open(logDir string, stderr io.Writer, secrets ...string) (*log.Logger, io.Closer, error) {
	return open(logDir, stderr, DefaultMaxBytes, DefaultRetention, secrets...)
}

func open(logDir string, stderr io.Writer, maxBytes int64, retention int, secrets ...string) (*log.Logger, io.Closer, error) {
	if stderr == nil {
		stderr = io.Discard
	}
	fileWriter, err := newRotatingWriter(logDir, FileName, maxBytes, retention)
	if err != nil {
		return nil, nil, err
	}
	writer := &resilientWriter{stderr: stderr, file: fileWriter, secrets: normalizedSecrets(secrets)}
	return log.New(writer, "sidecar ", log.Ldate|log.Ltime|log.LUTC), writer, nil
}

type resilientWriter struct {
	mu      sync.Mutex
	stderr  io.Writer
	file    io.WriteCloser
	secrets []string
}

func (w *resilientWriter) Write(input []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	output := sanitize(input, w.secrets)
	_, stderrErr := w.stderr.Write(output)
	if w.file != nil {
		if _, err := w.file.Write(output); err != nil {
			_ = w.file.Close()
			w.file = nil
			_, _ = fmt.Fprintln(w.stderr, "sidecar operational file logging disabled after write failure")
		}
	}
	if stderrErr != nil {
		return 0, stderrErr
	}
	return len(input), nil
}

func (w *resilientWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func sanitize(input []byte, secrets []string) []byte {
	value := string(input)
	for _, secret := range secrets {
		value = strings.ReplaceAll(value, secret, "[REDACTED]")
	}
	value = bearerPattern.ReplaceAllString(value, "${1}[REDACTED]")
	return []byte(value)
}

func normalizedSecrets(secrets []string) []string {
	result := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		if value := strings.TrimSpace(secret); value != "" {
			result = append(result, value)
		}
	}
	return result
}

type rotatingWriter struct {
	mu        sync.Mutex
	directory string
	path      string
	file      *os.File
	size      int64
	maxBytes  int64
	retention int
}

func newRotatingWriter(directory, name string, maxBytes int64, retention int) (*rotatingWriter, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("maximum log size must be positive")
	}
	if retention < 1 {
		return nil, fmt.Errorf("log retention must be at least one")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	cleanDirectory, err := filepath.Abs(directory)
	if err != nil {
		return nil, fmt.Errorf("resolve log directory: %w", err)
	}
	writer := &rotatingWriter{
		directory: cleanDirectory,
		path:      filepath.Join(cleanDirectory, name),
		maxBytes:  maxBytes,
		retention: retention,
	}
	if err := writer.openCurrent(); err != nil {
		return nil, err
	}
	return writer, nil
}

func (w *rotatingWriter) Write(input []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return 0, os.ErrClosed
	}
	if w.size > 0 && w.size+int64(len(input)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	written, err := w.file.Write(input)
	w.size += int64(written)
	return written, err
}

func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *rotatingWriter) openCurrent() error {
	if info, err := os.Lstat(w.path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refuse symbolic link log file")
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("inspect log file: %w", err)
	}
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("restrict log file permissions: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("read log file size: %w", err)
	}
	w.file = file
	w.size = info.Size()
	return nil
}

func (w *rotatingWriter) rotate() error {
	if err := w.file.Close(); err != nil {
		return fmt.Errorf("close log before rotation: %w", err)
	}
	w.file = nil

	oldest := w.archivePath(w.retention)
	if err := os.Remove(oldest); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove oldest log archive: %w", err)
	}
	for index := w.retention - 1; index >= 1; index-- {
		source := w.archivePath(index)
		destination := w.archivePath(index + 1)
		if err := os.Remove(destination); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("replace log archive: %w", err)
		}
		if err := os.Rename(source, destination); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("rotate log archive: %w", err)
		}
	}
	if err := os.Rename(w.path, w.archivePath(1)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("archive current log: %w", err)
	}
	return w.openCurrent()
}

func (w *rotatingWriter) archivePath(index int) string {
	return fmt.Sprintf("%s.%d", w.path, index)
}
