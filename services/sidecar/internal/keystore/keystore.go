package keystore

import (
	"errors"

	"github.com/zalando/go-keyring"
)

// ErrNotFound is returned when no secret exists for the requested entry.
var ErrNotFound = errors.New("keystore: secret not found")

// Store persists secrets in the operating system secure storage. The
// production implementation uses the OS credential manager (Windows
// Credential Manager / macOS Keychain / Linux Secret Service); a memory
// implementation is provided for tests.
type Store interface {
	Set(service, account, secret string) error
	Get(service, account string) (string, error)
	Delete(service, account string) error
}

type OSStore struct{}

// NewOSStore returns the production store backed by the OS credential
// manager. Unsupported platforms surface the underlying error on first use
// rather than falling back to insecure storage.
func NewOSStore() Store { return OSStore{} }

func (OSStore) Set(service, account, secret string) error {
	return keyring.Set(service, account, secret)
}

func (OSStore) Get(service, account string) (string, error) {
	secret, err := keyring.Get(service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	return secret, err
}

func (OSStore) Delete(service, account string) error {
	err := keyring.Delete(service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return ErrNotFound
	}
	return err
}

// MemoryStore is a test-only store; it must never be used in production.
type MemoryStore struct {
	values map[string]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{values: make(map[string]string)}
}

func (m *MemoryStore) entryKey(service, account string) string {
	return service + "\x00" + account
}

func (m *MemoryStore) Set(service, account, secret string) error {
	m.values[m.entryKey(service, account)] = secret
	return nil
}

func (m *MemoryStore) Get(service, account string) (string, error) {
	secret, ok := m.values[m.entryKey(service, account)]
	if !ok {
		return "", ErrNotFound
	}
	return secret, nil
}

func (m *MemoryStore) Delete(service, account string) error {
	delete(m.values, m.entryKey(service, account))
	return nil
}
