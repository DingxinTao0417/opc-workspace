package api

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// The preflight predicts an import from the package and the target's keys.
// Rules that live in the database itself (triggers, constraints) were only met
// when the rows were written, after the rollback backup had been created. The
// rehearsal writes the same rows through the same code inside a transaction
// that is always rolled back, so whatever would reject the import is known
// before anything is touched. ADR-032, S5.

const importBlockerApplyRehearsal = "apply_rehearsal_failed"

var errBusinessImportRehearsed = errors.New("business import rehearsal complete")

// businessImportRehearsalFailure names the rule that would reject the import.
// It carries a stable token only: never row values or the database's message.
type businessImportRehearsalFailure struct {
	Code string `json:"code"`
}

// A trigger aborts with a constant such as TASK_DUE_INBOX_SOURCE_NOT_CURRENT.
var businessImportRuleToken = regexp.MustCompile(`[A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+`)

// rehearseBusinessImport reports why the plan cannot be written, or nil when
// it can. An error means the rehearsal itself could not run to a verdict.
func (a *API) rehearseBusinessImport(c *gin.Context, plan *businessImportPlan, applyMode string) (*businessImportRehearsalFailure, error) {
	err := a.applyBusinessTables(c, plan.apply, applyMode, func(tx *gorm.DB) error {
		if err := plan.verifyUntouched(tx); err != nil {
			return err
		}
		return errBusinessImportRehearsed
	})
	switch {
	case errors.Is(err, errBusinessImportRehearsed):
		return nil, nil
	case err == nil:
		// The hook above ends every rehearsal with its own error.
		return nil, errors.New("business import rehearsal was not rolled back")
	case businessImportRehearsalInterrupted(err):
		return nil, err
	}
	return &businessImportRehearsalFailure{Code: businessImportRehearsalCode(err)}, nil
}

// businessImportRehearsalInterrupted separates "this import would be rejected"
// from "the database could not be asked right now".
func businessImportRehearsalInterrupted(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	text := strings.ToLower(err.Error())
	for _, marker := range []string{
		"database is locked", "database table is locked", "interrupted", "disk i/o error",
		"database or disk is full", "out of memory",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func businessImportRehearsalCode(err error) string {
	text := err.Error()
	for _, token := range businessImportRuleToken.FindAllString(text, -1) {
		// The driver names its own result code next to the trigger's token.
		if !strings.HasPrefix(token, "SQLITE_") && len(token) <= 80 {
			return token
		}
	}
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "unique constraint failed"):
		return "UNIQUE_CONSTRAINT"
	case strings.Contains(lower, "foreign key"):
		return "FOREIGN_KEY_CONSTRAINT"
	case strings.Contains(lower, "check constraint failed"):
		return "CHECK_CONSTRAINT"
	case strings.Contains(lower, "not null constraint failed"):
		return "NOT_NULL_CONSTRAINT"
	}
	return "INTEGRITY_VALIDATION"
}
