package api

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func isAIAssignmentAction(action string) bool {
	return action == "task.assign" || action == "task.reassign" || action == "task.unassign"
}

type aiAssignmentChanges struct {
	Role         string `json:"role"`
	ActorID      string `json:"actor_id,omitempty"`
	AssignmentID string `json:"assignment_id,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

func addAIAssignmentSchema(p map[string]any) {
	action := p["action"].(map[string]any)
	action["enum"] = append(action["enum"].([]any), "task.assign", "task.reassign", "task.unassign")
	changes := p["changes"].(map[string]any)
	changes["description"] = changes["description"].(string) + " Task assign requires role and real actor_id; reassign also requires current assignment_id and reason (1-1000 chars); unassign requires role, current assignment_id and reason, no actor_id. All bind task_id/expected_version from workspace_task_assignments. Query candidates using workspace_task_options type actor and role; reviewer is owner only. Preserves history and reconciles parent review. Assignment never starts Agent execution or sends a message."
	fields := changes["properties"].(map[string]any)
	fields["role"] = map[string]any{"type": "string", "enum": []string{"assignee", "reviewer"}}
	fields["actor_id"] = map[string]any{"type": "string", "format": "uuid"}
	fields["assignment_id"] = map[string]any{"type": "string", "format": "uuid", "description": "Current active Assignment ID, not Task/Actor ID"}
}

func parseAIAssignmentAction(input aiWorkspaceAction, fields, raw map[string]json.RawMessage) (aiWorkspaceAction, error) {
	for k := range raw {
		if k != "action" && k != "task_id" && k != "expected_version" && k != "changes" {
			return input, errors.New("unsupported assignment action field: " + k)
		}
	}
	id, e := uuid.Parse(input.TaskID)
	if e != nil || id.String() != input.TaskID || input.ExpectedVersion < 1 {
		return input, errors.New("use canonical task_id and current expected_version")
	}
	keys := map[string]bool{"role": true}
	if input.Action != "task.unassign" {
		keys["actor_id"] = true
	}
	if input.Action != "task.assign" {
		keys["assignment_id"] = true
		keys["reason"] = true
	}
	if len(fields) != len(keys) {
		return input, errors.New("assignment changes require explicit role, actor/assignment identity and reason as appropriate")
	}
	for k, v := range fields {
		if !keys[k] || string(v) == "null" {
			return input, errors.New("unsupported or null assignment field: " + k)
		}
	}
	var c aiAssignmentChanges
	if e := decodeStrictToolArguments(input.Changes, &c); e != nil {
		return input, e
	}
	if c.Role != "assignee" && c.Role != "reviewer" {
		return input, errors.New("role must be assignee or reviewer")
	}
	for _, key := range []string{"actor_id", "assignment_id"} {
		if !keys[key] {
			continue
		}
		var value string
		_ = json.Unmarshal(fields[key], &value)
		if id, e := uuid.Parse(value); e != nil || id.String() != value {
			return input, errors.New(key + " must be a canonical UUID")
		}
	}
	if keys["reason"] {
		normalized, e := validateAssignmentReason(c.Reason)
		if e != nil {
			return input, e
		}
		c.Reason = normalized
	}
	input.Changes, _ = json.Marshal(c)
	return input, nil
}

// Read only public responsibility metadata. Historical reasons, Actor notes,
// contact links and Adapter configuration are intentionally excluded.
type aiAssignmentRow struct {
	TaskID       string  `json:"task_id,omitempty"`
	ID           string  `json:"id"`
	Role         string  `json:"role"`
	ActorID      string  `json:"actor_id"`
	ActorName    string  `json:"actor_name"`
	ActorType    string  `json:"actor_type"`
	ActorStatus  string  `json:"actor_status"`
	ActorVersion int64   `json:"actor_version"`
	AssignedAt   string  `json:"assigned_at"`
	UnassignedAt *string `json:"unassigned_at"`
}

func aiTaskAssignmentsSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"task_id":{"type":"string","format":"uuid","description":"One Task, including active or history paging"},"task_ids":{"type":"array","minItems":1,"maxItems":20,"uniqueItems":true,"items":{"type":"string","format":"uuid"},"description":"Exact 1-20 Task IDs, active responsibility only; mutually exclusive with task_id"},"state":{"type":"string","enum":["active","history"]},"role":{"type":"string","enum":["assignee","reviewer"]},"limit":{"type":"integer","minimum":1,"maximum":20},"offset":{"type":"integer","minimum":0,"maximum":1000}}}`)
}
func (t *aiWorkspaceTool) taskAssignments(ctx context.Context, args json.RawMessage) (any, error) {
	if err := t.policy.Require("work"); err != nil {
		return nil, err
	}
	var in struct {
		TaskID  string   `json:"task_id"`
		TaskIDs []string `json:"task_ids"`
		State   string   `json:"state"`
		Role    string   `json:"role"`
		Limit   *int     `json:"limit"`
		Offset  int      `json:"offset"`
	}
	if err := decodeStrictToolArguments(args, &in); err != nil {
		return nil, err
	}
	if len(in.TaskIDs) > 0 {
		if in.TaskID != "" || in.State != "" || in.Limit != nil || in.Offset != 0 || len(in.TaskIDs) > 20 {
			return nil, errors.New("task_ids requires 1-20 exact Tasks and cannot use task_id or paging")
		}
		if in.Role != "" && in.Role != "assignee" && in.Role != "reviewer" {
			return nil, errors.New("invalid assignment role")
		}
		seen := map[string]bool{}
		for _, value := range in.TaskIDs {
			id, err := uuid.Parse(value)
			if err != nil || id.String() != value || seen[value] {
				return nil, errors.New("task_ids must contain distinct canonical Task UUIDs")
			}
			seen[value] = true
		}
		return t.taskAssignmentsBatch(ctx, in.TaskIDs, in.Role)
	}
	if id, e := uuid.Parse(in.TaskID); e != nil || id.String() != in.TaskID {
		return nil, errors.New("task_id must be a canonical UUID")
	}
	if in.State == "" {
		in.State = "active"
	}
	if in.State != "active" && in.State != "history" {
		return nil, errors.New("state must be active or history")
	}
	if in.Role != "" && in.Role != "assignee" && in.Role != "reviewer" {
		return nil, errors.New("invalid assignment role")
	}
	limit, err := workspacePaging(in.Limit, in.Offset)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	err = t.api.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task models.Task
		if e := tx.Select("id", "version", "status", "review_policy").First(&task, "id=?", in.TaskID).Error; e != nil {
			return e
		}
		q := tx.Table("task_assignments r").Joins("JOIN actors a ON a.id=r.actor_id").
			Select("r.id,r.role,r.actor_id,a.display_name AS actor_name,a.type AS actor_type,a.status AS actor_status,a.version AS actor_version,r.assigned_at,r.unassigned_at").Where("r.task_id=?", task.ID)
		if in.State == "active" {
			q = q.Where("r.unassigned_at IS NULL")
		} else {
			q = q.Where("r.unassigned_at IS NOT NULL")
		}
		if in.Role != "" {
			q = q.Where("r.role=?", in.Role)
		}
		rows := []aiAssignmentRow{}
		if e := q.Order("r.assigned_at DESC,r.id DESC").Offset(in.Offset).Limit(limit + 1).Scan(&rows).Error; e != nil {
			return e
		}
		more := len(rows) > limit
		if more {
			rows = rows[:limit]
		}
		var next *int
		if more && in.Offset+limit <= 1000 {
			n := in.Offset + limit
			next = &n
		}
		result = map[string]any{"task_id": task.ID, "task_version": task.Version, "task_status": task.Status, "review_policy": task.ReviewPolicy, "state": in.State, "items": rows, "has_more": more, "next_offset": next, "window_limited": more && next == nil, "route": searchRoute("task", task.ID)}
		return nil
	})
	if err != nil {
		return nil, safeAIWorkspaceError(ctx, err)
	}
	return result, nil
}

// The batch view is one live read snapshot of exact selected Tasks. It is not
// a frozen selection, a bulk-write grant, or a scan of unrelated Assignments.
func (t *aiWorkspaceTool) taskAssignmentsBatch(ctx context.Context, taskIDs []string, role string) (any, error) {
	var result map[string]any
	err := t.api.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var tasks []models.Task
		if err := tx.Select("id", "title", "version", "status", "review_policy").Where("id IN ?", taskIDs).Find(&tasks).Error; err != nil {
			return err
		}
		if len(tasks) != len(taskIDs) {
			return newProjectRequestError(404, "TASK_NOT_FOUND", "One or more selected Tasks no longer exist")
		}
		byID := make(map[string]models.Task, len(tasks))
		for _, task := range tasks {
			byID[task.ID] = task
		}
		q := tx.Table("task_assignments r").Joins("JOIN actors a ON a.id=r.actor_id").
			Select("r.task_id,r.id,r.role,r.actor_id,a.display_name AS actor_name,a.type AS actor_type,a.status AS actor_status,a.version AS actor_version,r.assigned_at,r.unassigned_at").
			Where("r.task_id IN ? AND r.unassigned_at IS NULL", taskIDs)
		if role != "" {
			q = q.Where("r.role=?", role)
		}
		var assignments []aiAssignmentRow
		if err := q.Order("r.task_id,r.role,r.id").Scan(&assignments).Error; err != nil {
			return err
		}
		byTask := map[string][]aiAssignmentRow{}
		for _, assignment := range assignments {
			byTask[assignment.TaskID] = append(byTask[assignment.TaskID], assignment)
		}
		items := make([]map[string]any, 0, len(taskIDs))
		for _, id := range taskIDs {
			task := byID[id]
			current := byTask[id]
			if current == nil {
				current = []aiAssignmentRow{}
			}
			items = append(items, map[string]any{"task_id": id, "task_title": task.Title, "task_version": task.Version,
				"task_status": task.Status, "review_policy": task.ReviewPolicy, "assignments": current, "route": searchRoute("task", id)})
		}
		result = map[string]any{"items": items, "count": len(items), "role": role, "state": "active"}
		return nil
	})
	if err != nil {
		return nil, safeAIWorkspaceError(ctx, err)
	}
	return result, nil
}

func previewAIAssignmentAction(tx *gorm.DB, input aiWorkspaceAction) (aiActionPreview, error) {
	p := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	var c aiAssignmentChanges
	_ = json.Unmarshal(input.Changes, &c)
	_, err := loadAssignmentTask(tx, input.TaskID, input.ExpectedVersion, input.Action != "task.unassign")
	if err != nil {
		return p, err
	}
	task, err := loadTask(tx, input.TaskID)
	if err != nil {
		return p, err
	}
	p.Label = task.Title
	var current models.TaskAssignment
	err = tx.Where("task_id=? AND role=? AND unassigned_at IS NULL", task.ID, c.Role).Take(&current).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return p, err
	}
	if input.Action == "task.assign" {
		if err == nil {
			return p, assignmentAlreadyActiveError()
		}
	} else if err != nil || current.ID != c.AssignmentID {
		return p, newProjectRequestError(409, "ASSIGNMENT_NOT_ACTIVE", "Read the current assignment for this task and role")
	}
	if input.Action == "task.reassign" && current.ActorID == c.ActorID {
		return p, newProjectRequestError(409, "ASSIGNMENT_UNCHANGED", "The selected actor already has this assignment")
	}
	// Bind names/versions as well as IDs: renaming or disabling a candidate must
	// not silently change the human's approval target.
	actorFields := func(id string) (map[string]any, error) {
		f := map[string]any{"actor_id": nil, "actor_name": nil, "actor_type": nil, "actor_version": nil}
		if id != "" {
			var a models.Actor
			if e := tx.Select("id", "display_name", "type", "version").First(&a, "id=?", id).Error; e != nil {
				return nil, e
			}
			f["actor_id"], f["actor_name"], f["actor_type"], f["actor_version"] = a.ID, a.DisplayName, a.Type, a.Version
		}
		return f, nil
	}
	p.Before, err = actorFields(current.ActorID)
	if err != nil {
		return p, err
	}
	if input.Action != "task.unassign" {
		if err = requireAssignmentActor(tx, c.ActorID, c.Role); err != nil {
			return p, err
		}
	}
	p.After, err = actorFields(c.ActorID)
	if err != nil {
		return p, err
	}
	for _, f := range []map[string]any{p.Before, p.After} {
		f["role"] = c.Role
		f["previous_assignment_id"] = nil
		if current.ID != "" {
			f["previous_assignment_id"] = current.ID
		}
		f["task_status"] = task.Status
		f["review_policy"] = task.ReviewPolicy
		f["subtask_total"], f["subtask_completed"], f["subtask_cancelled"] = task.SubtaskTotal, task.SubtaskCompleted, task.SubtaskCancelled
		f["current_submission_id"] = task.CurrentSubmissionID
	}
	if c.Reason != "" {
		p.After["reason"] = c.Reason
	}
	return p, nil
}
func executeAIAssignmentAction(tx *gorm.DB, in aiWorkspaceAction, requestID, now string) (models.Task, error) {
	var c aiAssignmentChanges
	_ = json.Unmarshal(in.Changes, &c)
	switch in.Action {
	case "task.assign":
		r, e := createAssignmentInTransaction(tx, in.TaskID, in.ExpectedVersion, c.Role, c.ActorID, requestID, now)
		return r.Task, e
	case "task.reassign":
		r, e := reassignInTransaction(tx, in.TaskID, in.ExpectedVersion, c.Role, c.ActorID, c.Reason, requestID, now)
		return r.Task, e
	default:
		r, e := endAssignmentInTransaction(tx, c.AssignmentID, in.ExpectedVersion, c.Reason, requestID, now)
		return r.Task, e
	}
}
