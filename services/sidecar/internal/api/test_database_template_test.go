package api

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/opc-workspace/opc-sidecar/internal/database"
)

var apiTestDatabaseTemplatePath string

func prepareAPITestDatabaseTemplate() (func() error, error) {
	root, err := os.MkdirTemp("", "opc-api-test-database-")
	if err != nil {
		return nil, fmt.Errorf("create API test database template directory: %w", err)
	}
	cleanup := func() error { return os.RemoveAll(root) }
	fail := func(templateErr error) (func() error, error) {
		_ = cleanup()
		return nil, templateErr
	}

	templatePath := filepath.Join(root, "latest-schema.db")
	store, err := database.Open(templatePath)
	if err != nil {
		return fail(fmt.Errorf("create API test database template: %w", err))
	}
	var artifactStoreID sql.NullString
	if err := store.SQL.QueryRow(
		"SELECT artifact_store_id FROM workspace_identity WHERE singleton = 1",
	).Scan(&artifactStoreID); err != nil {
		_ = store.Close()
		return fail(fmt.Errorf("inspect API test database template identity: %w", err))
	}
	if artifactStoreID.Valid {
		_ = store.Close()
		return fail(fmt.Errorf("API test database template unexpectedly has an Artifact Store binding"))
	}
	if _, err := store.SQL.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		_ = store.Close()
		return fail(fmt.Errorf("checkpoint API test database template: %w", err))
	}
	if err := store.Close(); err != nil {
		return fail(fmt.Errorf("close API test database template: %w", err))
	}

	apiTestDatabaseTemplatePath = templatePath
	return func() error {
		apiTestDatabaseTemplatePath = ""
		return cleanup()
	}, nil
}

// openAPITestDatabase clones the closed latest-schema template for ordinary
// API behavior tests. Every caller still receives its own physical SQLite
// database and database.Open applies the normal connection/runtime setup; only
// the repeated replay of the complete migration history is skipped.
//
// Existing paths are opened in place so close/reopen tests preserve their
// database state. Tests that exercise first-open migrations or recovery from a
// malformed database intentionally keep calling database.Open directly.
// The immutable workspace_identity.database_id is copied with the template,
// so tests comparing two distinct workspaces must also use database.Open.
func openAPITestDatabase(path string) (*database.Store, error) {
	if _, err := os.Stat(path); err == nil {
		return database.Open(path)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect API test database destination: %w", err)
	}
	if apiTestDatabaseTemplatePath == "" {
		return nil, fmt.Errorf("API test database template is not initialized")
	}
	if err := copyAPITestDatabaseTemplate(apiTestDatabaseTemplatePath, path); err != nil {
		return nil, err
	}
	return database.Open(path)
}

func copyAPITestDatabaseTemplate(sourcePath, destinationPath string) (err error) {
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open API test database template: %w", err)
	}
	defer source.Close()

	destination, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create cloned API test database: %w", err)
	}
	complete := false
	defer func() {
		if closeErr := destination.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close cloned API test database: %w", closeErr)
		}
		if !complete || err != nil {
			_ = os.Remove(destinationPath)
		}
	}()

	if _, err = io.Copy(destination, source); err != nil {
		return fmt.Errorf("copy API test database template: %w", err)
	}
	complete = true
	return nil
}
