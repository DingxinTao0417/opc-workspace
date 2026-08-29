package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type StartupIncidentKind string

const (
	StartupIncidentDatabaseStartup   StartupIncidentKind = "database_startup"
	StartupIncidentDatabaseMigration StartupIncidentKind = "database_migration"
	StartupIncidentSidecarStartup    StartupIncidentKind = "sidecar_startup"
	StartupIncidentDatabaseRuntime   StartupIncidentKind = "database_runtime"

	startupIncidentJournalName = "startup-incidents-v1.json"
	startupIncidentFormat      = 1
	maxStartupIncidentBytes    = 64 << 10
	maxStartupIncidentRecords  = 16
)

var startupIncidentJournalMu sync.Mutex

type startupIncidentJournal struct {
	FormatVersion int                     `json:"format_version"`
	Incidents     []startupIncidentRecord `json:"incidents"`
}

type startupIncidentRecord struct {
	IncidentID string              `json:"incident_id"`
	Kind       StartupIncidentKind `json:"kind"`
	OccurredAt string              `json:"occurred_at"`
}

func startupIncidentDefinition(kind StartupIncidentKind) (systemMaintenanceIncident, bool) {
	switch kind {
	case StartupIncidentDatabaseStartup:
		return databaseStartupMaintenanceIncident, true
	case StartupIncidentDatabaseMigration:
		return databaseMigrationMaintenanceIncident, true
	case StartupIncidentSidecarStartup:
		return sidecarStartupMaintenanceIncident, true
	case StartupIncidentDatabaseRuntime:
		return runtimeDatabaseMaintenanceIncident, true
	default:
		return systemMaintenanceIncident{}, false
	}
}

// RecordStartupIncident persists only a whitelisted incident identity and time.
// Raw startup errors, paths, tokens and request data never enter this journal.
func RecordStartupIncident(logDir string, kind StartupIncidentKind, now time.Time) error {
	startupIncidentJournalMu.Lock()
	defer startupIncidentJournalMu.Unlock()
	if _, ok := startupIncidentDefinition(kind); !ok {
		return errors.New("unsupported startup incident")
	}
	path, err := startupIncidentJournalPath(logDir)
	if err != nil {
		return err
	}
	journal, err := readStartupIncidentJournal(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			if quarantineErr := quarantineStartupIncidentJournal(path); quarantineErr != nil {
				return errors.Join(err, quarantineErr)
			}
		}
		journal = startupIncidentJournal{FormatVersion: startupIncidentFormat}
	}
	for _, record := range journal.Incidents {
		if record.Kind == kind {
			return nil
		}
	}
	if len(journal.Incidents) >= maxStartupIncidentRecords {
		journal.Incidents = journal.Incidents[len(journal.Incidents)-maxStartupIncidentRecords+1:]
	}
	journal.Incidents = append(journal.Incidents, startupIncidentRecord{
		IncidentID: uuid.NewString(),
		Kind:       kind,
		OccurredAt: now.UTC().Format(time.RFC3339Nano),
	})
	return writeStartupIncidentJournal(path, journal)
}

// ReplayStartupIncidents projects durable pre-database failures after the next
// successful open. Stable incident IDs make retries safe even if journal cleanup
// failed after a prior projection.
func ReplayStartupIncidents(db *gorm.DB, logDir string, logger *log.Logger) error {
	startupIncidentJournalMu.Lock()
	defer startupIncidentJournalMu.Unlock()
	path, err := startupIncidentJournalPath(logDir)
	if err != nil {
		return err
	}
	journal, err := readStartupIncidentJournal(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.Join(err, quarantineStartupIncidentJournal(path))
	}
	projector := &API{db: db, options: Options{Now: time.Now, Logger: logger}}
	for _, record := range journal.Incidents {
		incident, ok := startupIncidentDefinition(record.Kind)
		if !ok {
			return errors.New("startup incident journal contains an unsupported incident")
		}
		if err := projector.projectSystemMaintenanceFailureAt(incident, "startup-replay", record.OccurredAt, record.IncidentID); err != nil {
			return fmt.Errorf("project startup incident: %w", err)
		}
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove replayed startup incident journal: %w", err)
	}
	return syncArtifactDirectory(filepath.Dir(path))
}

func startupIncidentJournalPath(logDir string) (string, error) {
	dir := strings.TrimSpace(logDir)
	if dir == "" {
		return "", errors.New("startup incident log directory is required")
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve startup incident log directory: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return "", fmt.Errorf("create startup incident log directory: %w", err)
	}
	return filepath.Join(absolute, startupIncidentJournalName), nil
}

func readStartupIncidentJournal(path string) (startupIncidentJournal, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return startupIncidentJournal{}, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > maxStartupIncidentBytes {
		return startupIncidentJournal{}, errors.New("startup incident journal is not a bounded regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return startupIncidentJournal{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxStartupIncidentBytes+1))
	if err != nil || len(data) > maxStartupIncidentBytes {
		return startupIncidentJournal{}, errors.New("startup incident journal could not be read safely")
	}
	var journal startupIncidentJournal
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&journal); err != nil {
		return startupIncidentJournal{}, errors.New("startup incident journal is invalid")
	}
	if decoder.Decode(&struct{}{}) != io.EOF || journal.FormatVersion != startupIncidentFormat ||
		len(journal.Incidents) == 0 || len(journal.Incidents) > maxStartupIncidentRecords {
		return startupIncidentJournal{}, errors.New("startup incident journal has an invalid envelope")
	}
	seenKinds := make(map[StartupIncidentKind]struct{}, len(journal.Incidents))
	seenIDs := make(map[string]struct{}, len(journal.Incidents))
	for _, record := range journal.Incidents {
		parsedID, idErr := uuid.Parse(record.IncidentID)
		occurredAt, timeErr := time.Parse(time.RFC3339Nano, record.OccurredAt)
		_, supported := startupIncidentDefinition(record.Kind)
		_, duplicateKind := seenKinds[record.Kind]
		_, duplicateID := seenIDs[record.IncidentID]
		if idErr != nil || parsedID.String() != record.IncidentID || timeErr != nil ||
			occurredAt.Location() != time.UTC || !supported || duplicateKind || duplicateID {
			return startupIncidentJournal{}, errors.New("startup incident journal contains an invalid record")
		}
		seenKinds[record.Kind] = struct{}{}
		seenIDs[record.IncidentID] = struct{}{}
	}
	return journal, nil
}

func writeStartupIncidentJournal(path string, journal startupIncidentJournal) error {
	data, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	if len(data) > maxStartupIncidentBytes {
		return errors.New("startup incident journal is too large")
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".startup-incidents-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := replaceFileAtomically(temporaryPath, path); err != nil {
		return err
	}
	return syncArtifactDirectory(filepath.Dir(path))
}

func quarantineStartupIncidentJournal(path string) error {
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	destination := filepath.Join(filepath.Dir(path), ".startup-incidents-invalid-"+uuid.NewString()+".json")
	if err := os.Rename(path, destination); err != nil {
		return err
	}
	return syncArtifactDirectory(filepath.Dir(path))
}
