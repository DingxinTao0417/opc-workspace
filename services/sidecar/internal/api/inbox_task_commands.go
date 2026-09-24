package api

import (
	"errors"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// HTTP and AI approvals own their transaction and durable replay identity.
// All relationship writes, history and automatic resolution use these commands.
func prepareInboxTaskMutation(tx *gorm.DB, inboxID string, expectedVersion int64) (models.InboxItem, error) {
	current, err := loadInboxItem(tx, inboxID)
	if err != nil {
		return current, inboxItemLoadError(err)
	}
	if current.Version != expectedVersion {
		return current, inboxVersionConflict()
	}
	if inboxItemTerminal(current.Status) {
		return current, inboxTerminalConflict("Archived Inbox Items must be reopened before changing Task relations")
	}
	return current, nil
}

func mutateInboxTaskInTransaction(tx *gorm.DB, inboxID, command string, input inboxTaskCommandHash, requestID string, now time.Time) (response inboxTaskMutationResponse, err error) {
	current, err := prepareInboxTaskMutation(tx, inboxID, input.ExpectedVersion)
	if err != nil {
		return response, err
	}
	nowText := formatInboxTimestamp(now)
	switch command {
	case "link":
		if input.IsRequired == nil {
			return response, errors.New("is_required is required")
		}
		err = createInboxTaskRelation(tx, requestID, current, input.TaskID, *input.IsRequired, now, nowText, &response)
	case "requirement":
		if input.IsRequired == nil {
			return response, errors.New("is_required is required")
		}
		err = changeInboxTaskRequirement(tx, requestID, current, input.TaskID, *input.IsRequired, now, nowText, &response)
	case "unlink":
		err = softUnlinkInboxTask(tx, requestID, current, input.TaskID, input.Reason, now, nowText, &response)
	default:
		err = errors.New("unsupported Inbox Task command")
	}
	return response, err
}
