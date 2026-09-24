package harness

import "errors"

// ErrPermissionDenied is deliberately content-free: denied tool arguments may
// contain sensitive data and must not escape through logs or run-step errors.
var ErrPermissionDenied = errors.New("harness: capability not granted for this run")

// Capabilities is an immutable, run-scoped allowlist owned by the caller, never
// by a model response, memory entry, tool result or conversation transcript.
type Capabilities struct{ allowed map[string]struct{} }

func NewCapabilities(names ...string) Capabilities {
	allowed := make(map[string]struct{}, len(names))
	for _, name := range names {
		allowed[name] = struct{}{}
	}
	return Capabilities{allowed: allowed}
}

func (p Capabilities) Allows(name string) bool {
	_, ok := p.allowed[name]
	return ok
}

func (p Capabilities) Require(name string) error {
	if !p.Allows(name) {
		return ErrPermissionDenied
	}
	return nil
}
