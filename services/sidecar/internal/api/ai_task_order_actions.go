package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const aiTaskMoveAction = "task.move"

type aiTaskMoveChanges struct {
	AnchorTaskID          string `json:"anchor_task_id"`
	ExpectedAnchorVersion int64  `json:"expected_anchor_version"`
	Placement             string `json:"placement"`
}

type aiTaskOrderPreview struct {
	TaskID           string  `json:"task_id"`
	Title            string  `json:"title"`
	AnchorTaskID     string  `json:"anchor_task_id"`
	AnchorTitle      string  `json:"anchor_title"`
	Placement        string  `json:"placement"`
	PlannedDate      *string `json:"planned_date"`
	GroupCount       int     `json:"group_count"`
	ActiveCount      int     `json:"active_count"`
	BeforePosition   int     `json:"before_position"`
	AfterPosition    int     `json:"after_position"`
	GroupFingerprint string  `json:"group_fingerprint"`
}

type aiTaskOrderRow struct {
	ID          string  `gorm:"column:id" json:"id"`
	Title       string  `gorm:"column:title" json:"title"`
	Status      string  `gorm:"column:status" json:"status"`
	PlannedDate *string `gorm:"column:planned_date" json:"planned_date"`
	ManualOrder *int    `gorm:"column:manual_order" json:"manual_order"`
	Version     int64   `gorm:"column:version" json:"version"`
}

func addAITaskOrderSchema(properties map[string]any) {
	action := properties["action"].(map[string]any)
	action["enum"] = append(action["enum"].([]any), aiTaskMoveAction)
	changes := properties["changes"].(map[string]any)
	changes["description"] = changes["description"].(string) + " Task move (work+actions): task.move takes a real top-level task_id and expected_version plus changes.anchor_task_id, expected_anchor_version and placement before|after. Both tasks must be active in the same exact planned-date group. Read their current versions first. The server computes and checks the entire group; approval makes one atomic manual reorder, preserving done/cancelled slots. No date or status change."
	fields := changes["properties"].(map[string]any)
	fields["anchor_task_id"] = map[string]any{"type": "string", "format": "uuid"}
	fields["expected_anchor_version"] = map[string]any{"type": "integer", "minimum": 1}
	fields["placement"] = map[string]any{"type": "string", "enum": []string{"before", "after"}}
}

func parseAITaskMoveAction(input aiWorkspaceAction, fields map[string]json.RawMessage, raw map[string]json.RawMessage) (aiWorkspaceAction, error) {
	if len(raw) != 4 || len(fields) != 3 {
		return input, errors.New("task.move requires only action, task_id, expected_version and three move fields")
	}
	for _, key := range []string{"action", "task_id", "expected_version", "changes"} {
		if _, ok := raw[key]; !ok {
			return input, errors.New("task.move requires task_id and expected_version")
		}
	}
	for _, key := range []string{"anchor_task_id", "expected_anchor_version", "placement"} {
		if _, ok := fields[key]; !ok {
			return input, errors.New("task.move requires anchor_task_id, expected_anchor_version and placement")
		}
	}
	if id, err := uuid.Parse(input.TaskID); err != nil || id.String() != input.TaskID || input.ExpectedVersion < 1 {
		return input, errors.New("task.move requires a canonical task_id and positive expected_version")
	}
	var changes aiTaskMoveChanges
	if err := decodeStrictToolArguments(input.Changes, &changes); err != nil {
		return input, err
	}
	if id, err := uuid.Parse(changes.AnchorTaskID); err != nil || id.String() != changes.AnchorTaskID || changes.AnchorTaskID == input.TaskID {
		return input, errors.New("anchor_task_id must be another canonical Task UUID")
	}
	if changes.ExpectedAnchorVersion < 1 || (changes.Placement != "before" && changes.Placement != "after") {
		return input, errors.New("task.move requires a positive anchor version and before or after placement")
	}
	input.Changes, _ = json.Marshal(changes)
	return input, nil
}

func loadAITaskOrderRows(tx *gorm.DB, plannedDate *string) ([]aiTaskOrderRow, error) {
	query := tx.Model(&models.Task{}).Select("tasks.id, tasks.title, tasks.status, tasks.planned_date, tasks.manual_order, tasks.version")
	if plannedDate == nil {
		query = query.Where("tasks.planned_date IS NULL")
	} else {
		query = query.Where("tasks.planned_date = ?", *plannedDate)
	}
	query, _ = applyTaskSort(query, "manual_order")
	var rows []aiTaskOrderRow
	if err := query.Limit(1001).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) > 1000 {
		return nil, newProjectRequestError(http.StatusUnprocessableEntity, "TASK_REORDER_TOO_LARGE", "This planned-date group has more than 1000 tasks")
	}
	return rows, nil
}

func taskOrderActive(status string) bool {
	return status != "done" && status != "cancelled"
}

func calculateAITaskMove(tx *gorm.DB, input aiWorkspaceAction) (aiTaskOrderPreview, []string, map[string]int64, error) {
	var preview aiTaskOrderPreview
	var changes aiTaskMoveChanges
	if err := decodeStrictToolArguments(input.Changes, &changes); err != nil {
		return preview, nil, nil, err
	}
	var source models.Task
	if err := tx.Select("id, title, status, planned_date, version").First(&source, "id = ?", input.TaskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return preview, nil, nil, newProjectRequestError(http.StatusConflict, "TASK_REORDER_SET_CHANGED", "The task no longer exists")
		}
		return preview, nil, nil, err
	}
	if source.Version != input.ExpectedVersion {
		return preview, nil, nil, taskVersionConflict()
	}
	if !taskOrderActive(source.Status) {
		return preview, nil, nil, newProjectRequestError(http.StatusUnprocessableEntity, "TASK_REORDER_INACTIVE", "Only active tasks can be moved")
	}
	rows, err := loadAITaskOrderRows(tx, source.PlannedDate)
	if err != nil {
		return preview, nil, nil, err
	}
	var anchor *aiTaskOrderRow
	active := make([]string, 0, len(rows))
	versions := make(map[string]int64, len(rows))
	for index := range rows {
		row := &rows[index]
		versions[row.ID] = row.Version
		if taskOrderActive(row.Status) {
			active = append(active, row.ID)
		}
		if row.ID == changes.AnchorTaskID {
			anchor = row
		}
	}
	if anchor == nil || !taskOrderActive(anchor.Status) {
		return preview, nil, nil, newProjectRequestError(http.StatusUnprocessableEntity, "TASK_REORDER_ANCHOR_INVALID", "Anchor must be an active task in the same planned-date group")
	}
	if anchor.Version != changes.ExpectedAnchorVersion {
		return preview, nil, nil, taskVersionConflict()
	}
	beforePosition := -1
	withoutSource := make([]string, 0, len(active)-1)
	for index, id := range active {
		if id == input.TaskID {
			beforePosition = index + 1
		} else {
			withoutSource = append(withoutSource, id)
		}
	}
	if beforePosition < 1 {
		return preview, nil, nil, newProjectRequestError(http.StatusConflict, "TASK_REORDER_SET_CHANGED", "The task left its planned-date group")
	}
	insertAt := -1
	for index, id := range withoutSource {
		if id == changes.AnchorTaskID {
			insertAt = index
			if changes.Placement == "after" {
				insertAt++
			}
			break
		}
	}
	if insertAt < 0 {
		return preview, nil, nil, newProjectRequestError(http.StatusConflict, "TASK_REORDER_SET_CHANGED", "The anchor left its planned-date group")
	}
	if insertAt+1 == beforePosition {
		return preview, nil, nil, newProjectRequestError(http.StatusUnprocessableEntity, "TASK_REORDER_NO_CHANGE", "The task is already in that position")
	}
	orderedActive := append([]string{}, withoutSource[:insertAt]...)
	orderedActive = append(orderedActive, input.TaskID)
	orderedActive = append(orderedActive, withoutSource[insertAt:]...)
	orderedIDs := make([]string, len(rows))
	activeIndex := 0
	for index, row := range rows {
		if taskOrderActive(row.Status) {
			orderedIDs[index] = orderedActive[activeIndex]
			activeIndex++
		} else {
			orderedIDs[index] = row.ID
		}
	}
	encoded, err := json.Marshal(rows)
	if err != nil {
		return preview, nil, nil, err
	}
	digest := sha256.Sum256(encoded)
	preview = aiTaskOrderPreview{
		TaskID: input.TaskID, Title: source.Title,
		AnchorTaskID: anchor.ID, AnchorTitle: anchor.Title,
		Placement: changes.Placement, PlannedDate: source.PlannedDate,
		GroupCount: len(rows), ActiveCount: len(active),
		BeforePosition: beforePosition, AfterPosition: insertAt + 1,
		GroupFingerprint: hex.EncodeToString(digest[:]),
	}
	return preview, orderedIDs, versions, nil
}

func previewAITaskMoveAction(tx *gorm.DB, input aiWorkspaceAction) (aiActionPreview, error) {
	result := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	order, _, _, err := calculateAITaskMove(tx, input)
	if err != nil {
		return result, err
	}
	result.Label = order.Title
	result.TaskOrder = &order
	return result, nil
}

func executeAITaskMoveAction(tx *gorm.DB, input aiWorkspaceAction, now time.Time) (aiActionResult, error) {
	order, ids, versions, err := calculateAITaskMove(tx, input)
	if err != nil {
		return aiActionResult{}, err
	}
	response, err := reorderTaskGroupInTransaction(tx, order.PlannedDate, "manual", ids, versions, now)
	if err != nil {
		return aiActionResult{}, err
	}
	for _, task := range response.Tasks {
		if task.ID == input.TaskID {
			return aiActionResult{ID: task.ID, Version: task.Version}, nil
		}
	}
	return aiActionResult{}, fmt.Errorf("reordered Task %s missing from command result", input.TaskID)
}
