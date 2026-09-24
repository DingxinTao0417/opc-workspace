package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const aiRoadmapMilestoneMoveAction = "roadmap_milestone.move"

type aiRoadmapMilestoneMoveChanges struct {
	AnchorMilestoneID     string `json:"anchor_milestone_id"`
	ExpectedAnchorVersion int64  `json:"expected_anchor_version"`
	Placement             string `json:"placement"`
}

type aiRoadmapOrderPreview struct {
	MilestoneID      string `json:"milestone_id"`
	Title            string `json:"title"`
	AnchorID         string `json:"anchor_id"`
	AnchorTitle      string `json:"anchor_title"`
	Placement        string `json:"placement"`
	Year             int    `json:"year"`
	Quarter          int    `json:"quarter"`
	GroupCount       int    `json:"group_count"`
	BeforePosition   int    `json:"before_position"`
	AfterPosition    int    `json:"after_position"`
	GroupFingerprint string `json:"group_fingerprint"`
}

var aiRoadmapMilestoneChangeFields = map[string]bool{
	"title": true, "description": true, "year": true, "quarter": true,
	"target_date": true, "status": true, "project_ids": true,
}

func addAIRoadmapMilestoneSchema(properties map[string]any) {
	action := properties["action"].(map[string]any)
	action["enum"] = append(action["enum"].([]any),
		"roadmap_milestone.create", "roadmap_milestone.update",
		"roadmap_milestone.archive", "roadmap_milestone.restore", "roadmap_milestone.delete",
		aiRoadmapMilestoneMoveAction,
	)
	properties["roadmap_milestone_id"] = map[string]any{
		"type": "string", "format": "uuid",
		"description": "Required for an existing roadmap milestone; read the exact ID and expected_version with workspace_get first",
	}
	changes := properties["changes"].(map[string]any)
	changes["description"] = changes["description"].(string) + " Roadmap milestone create requires title, year, quarter and target_date; status defaults to planned. Update accepts only changed fields. target_date must fall in the supplied year/quarter. project_ids are existing Project UUIDs. Archive/restore/delete require empty changes. Delete only proposes permanent deletion of an archived milestone and always requires separate human consent. Move takes a real source milestone ID/version and changes.anchor_milestone_id/expected_anchor_version/placement before|after in the same non-archived quarter; the server computes the complete group of at most 100 and requires human approval."
	fields := changes["properties"].(map[string]any)
	fields["year"] = map[string]any{"type": "integer", "minimum": 2000, "maximum": 2100}
	fields["quarter"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 4}
	fields["target_date"] = map[string]any{"type": "string", "description": "Roadmap milestone YYYY-MM-DD within year and quarter"}
	fields["project_ids"] = map[string]any{"type": "array", "maxItems": 100, "uniqueItems": true, "items": map[string]any{"type": "string", "format": "uuid"}}
	fields["anchor_milestone_id"] = map[string]any{"type": "string", "format": "uuid"}
	fields["expected_anchor_version"] = map[string]any{"type": "integer", "minimum": 1}
	fields["placement"] = map[string]any{"type": "string", "enum": []string{"before", "after"}}
	// status is shared by the ledger schema. Keep the tool schema permissive
	// enough for both domains; the strict action parser enforces each enum.
	fields["status"] = map[string]any{"type": "string", "enum": []string{"planned", "active", "achieved", "pending", "confirmed"}}
}

func parseAIRoadmapMilestoneAction(input aiWorkspaceAction, fields, raw map[string]json.RawMessage) (aiWorkspaceAction, error) {
	for _, key := range []string{
		"agent_run_id", "project_note_id", "invoice_id", "financial_entry_id",
		"project_id", "task_id", "inbox_item_id", "reminder_id", "focus_session_id",
		"client_followup_id", "client_activity_id", "automation_rule_id", "automation_run_id",
	} {
		if _, present := raw[key]; present {
			return input, errors.New("roadmap milestone actions only accept roadmap_milestone_id as target")
		}
	}
	switch input.Action {
	case "roadmap_milestone.create":
		if _, present := raw["roadmap_milestone_id"]; present || input.ExpectedVersion != 0 {
			return input, errors.New("create does not accept a roadmap milestone ID/version")
		}
	case "roadmap_milestone.update", "roadmap_milestone.archive", "roadmap_milestone.restore", "roadmap_milestone.delete", aiRoadmapMilestoneMoveAction:
		parsed, err := uuid.Parse(strings.TrimSpace(input.RoadmapMilestoneID))
		if err != nil || parsed.String() != input.RoadmapMilestoneID || input.ExpectedVersion < 1 {
			return input, errors.New("use a real roadmap milestone ID and expected_version from workspace_get")
		}
	default:
		return input, errors.New("unsupported roadmap milestone action")
	}
	if input.Action == aiRoadmapMilestoneMoveAction {
		if len(raw) != 4 || len(fields) != 3 {
			return input, errors.New("roadmap_milestone.move requires only source ID/version and three move fields")
		}
		for _, key := range []string{"action", "roadmap_milestone_id", "expected_version", "changes"} {
			if _, ok := raw[key]; !ok {
				return input, errors.New("roadmap_milestone.move requires source ID and version")
			}
		}
		for _, key := range []string{"anchor_milestone_id", "expected_anchor_version", "placement"} {
			if _, ok := fields[key]; !ok {
				return input, errors.New("roadmap_milestone.move requires anchor ID, version and placement")
			}
		}
		var move aiRoadmapMilestoneMoveChanges
		if err := decodeStrictToolArguments(input.Changes, &move); err != nil {
			return input, err
		}
		if id, err := uuid.Parse(move.AnchorMilestoneID); err != nil || id.String() != move.AnchorMilestoneID || move.AnchorMilestoneID == input.RoadmapMilestoneID || move.ExpectedAnchorVersion < 1 || (move.Placement != "before" && move.Placement != "after") {
			return input, errors.New("roadmap_milestone.move requires another canonical anchor ID, positive version and before or after placement")
		}
		input.Changes, _ = json.Marshal(move)
		return input, nil
	}
	if input.Action == "roadmap_milestone.create" || input.Action == "roadmap_milestone.update" {
		for field := range fields {
			if !aiRoadmapMilestoneChangeFields[field] {
				return input, errors.New("unsupported roadmap milestone field: " + field)
			}
		}
		if input.Action == "roadmap_milestone.create" {
			var create createRoadmapMilestoneRequest
			if err := decodeStrictToolArguments(input.Changes, &create); err != nil {
				return input, err
			}
			if _, _, err := roadmapMilestoneFromCreateRequest(create); err != nil {
				return input, err
			}
		} else {
			if len(fields) == 0 {
				return input, errors.New("at least one roadmap milestone field is required")
			}
			var update updateRoadmapMilestoneRequest
			if err := decodeStrictToolArguments(input.Changes, &update); err != nil {
				return input, err
			}
			if !roadmapMilestoneUpdateHasFields(update) {
				return input, errors.New("at least one roadmap milestone field is required")
			}
		}
	} else if len(fields) != 0 {
		return input, errors.New("roadmap milestone archive/restore/delete requires empty changes")
	}
	input.Changes, _ = json.Marshal(fields)
	return input, nil
}

func calculateAIRoadmapMilestoneMove(tx *gorm.DB, input aiWorkspaceAction) (aiRoadmapOrderPreview, []reorderRoadmapMilestoneItem, error) {
	var preview aiRoadmapOrderPreview
	var changes aiRoadmapMilestoneMoveChanges
	if err := decodeStrictToolArguments(input.Changes, &changes); err != nil {
		return preview, nil, err
	}
	var source models.RoadmapMilestone
	if err := tx.Select("id, title, status, year, quarter, version").First(&source, "id = ?", input.RoadmapMilestoneID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return preview, nil, newProjectRequestError(http.StatusConflict, "ROADMAP_REORDER_SET_CHANGED", "The milestone no longer exists")
		}
		return preview, nil, err
	}
	if source.Version != input.ExpectedVersion {
		return preview, nil, roadmapMilestoneVersionConflict()
	}
	if source.Status == "archived" {
		return preview, nil, newProjectRequestError(http.StatusConflict, "ROADMAP_MILESTONE_ARCHIVED", "Archived roadmap milestones cannot be reordered")
	}
	var rows []models.RoadmapMilestone
	if err := tx.Select("id, title, status, year, quarter, target_date, manual_order, version").
		Where("year = ? AND quarter = ? AND status <> 'archived'", source.Year, source.Quarter).
		Order("manual_order ASC").Order("target_date ASC").Order("id ASC").Limit(101).Find(&rows).Error; err != nil {
		return preview, nil, err
	}
	if len(rows) > 100 {
		return preview, nil, newProjectRequestError(http.StatusUnprocessableEntity, "ROADMAP_REORDER_TOO_LARGE", "The quarter has more than 100 active milestones")
	}
	beforePosition, insertAt := -1, -1
	var anchor models.RoadmapMilestone
	withoutSource := make([]models.RoadmapMilestone, 0, len(rows))
	for index, row := range rows {
		if row.ID == input.RoadmapMilestoneID {
			beforePosition = index + 1
		} else {
			withoutSource = append(withoutSource, row)
		}
		if row.ID == changes.AnchorMilestoneID {
			anchor = row
		}
	}
	if beforePosition < 1 {
		return preview, nil, newProjectRequestError(http.StatusConflict, "ROADMAP_REORDER_SET_CHANGED", "The milestone left its quarter")
	}
	if anchor.ID == "" {
		return preview, nil, newProjectRequestError(http.StatusUnprocessableEntity, "ROADMAP_REORDER_ANCHOR_INVALID", "Anchor must be in the same non-archived quarter")
	}
	if anchor.Version != changes.ExpectedAnchorVersion {
		return preview, nil, roadmapMilestoneVersionConflict()
	}
	for index, row := range withoutSource {
		if row.ID == anchor.ID {
			insertAt = index
			if changes.Placement == "after" {
				insertAt++
			}
			break
		}
	}
	if insertAt < 0 || insertAt+1 == beforePosition {
		return preview, nil, newProjectRequestError(http.StatusUnprocessableEntity, "ROADMAP_REORDER_NO_CHANGE", "The milestone is already in that position")
	}
	var moving models.RoadmapMilestone
	for _, row := range rows {
		if row.ID == input.RoadmapMilestoneID {
			moving = row
			break
		}
	}
	ordered := append([]models.RoadmapMilestone{}, withoutSource[:insertAt]...)
	ordered = append(ordered, moving)
	ordered = append(ordered, withoutSource[insertAt:]...)
	items := make([]reorderRoadmapMilestoneItem, len(ordered))
	for index, row := range ordered {
		items[index] = reorderRoadmapMilestoneItem{ID: row.ID, ExpectedVersion: row.Version}
	}
	encoded, err := json.Marshal(rows)
	if err != nil {
		return preview, nil, err
	}
	digest := sha256.Sum256(encoded)
	preview = aiRoadmapOrderPreview{
		MilestoneID: moving.ID, Title: moving.Title, AnchorID: anchor.ID, AnchorTitle: anchor.Title,
		Placement: changes.Placement, Year: source.Year, Quarter: source.Quarter,
		GroupCount: len(rows), BeforePosition: beforePosition, AfterPosition: insertAt + 1,
		GroupFingerprint: hex.EncodeToString(digest[:]),
	}
	return preview, items, nil
}

func previewAIRoadmapMilestoneMove(tx *gorm.DB, input aiWorkspaceAction) (aiActionPreview, error) {
	preview := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	order, _, err := calculateAIRoadmapMilestoneMove(tx, input)
	if err != nil {
		return preview, err
	}
	preview.Label = order.Title
	preview.RoadmapOrder = &order
	return preview, nil
}

func executeAIRoadmapMilestoneMove(tx *gorm.DB, input aiWorkspaceAction, now time.Time) (aiActionResult, error) {
	_, items, err := calculateAIRoadmapMilestoneMove(tx, input)
	if err != nil {
		return aiActionResult{}, err
	}
	rows, err := reorderRoadmapMilestonesInTransaction(tx, items, now)
	if err != nil {
		return aiActionResult{}, err
	}
	for _, row := range rows {
		if row.ID == input.RoadmapMilestoneID {
			return aiActionResult{ID: row.ID, Version: row.Version}, nil
		}
	}
	return aiActionResult{}, errors.New("reordered milestone missing from result")
}

func aiRoadmapMilestoneFields(response roadmapMilestoneResponse) map[string]any {
	projectIDs := make([]string, len(response.Projects))
	for i, project := range response.Projects {
		projectIDs[i] = project.ID
	}
	return map[string]any{
		"title": response.Title, "description": response.Description,
		"year": response.Year, "quarter": response.Quarter, "target_date": response.TargetDate,
		"status": response.Status, "project_ids": projectIDs,
	}
}

func aiRoadmapMilestoneModelFields(milestone models.RoadmapMilestone, projectIDs []string) map[string]any {
	return map[string]any{
		"title": milestone.Title, "description": milestone.Description,
		"year": milestone.Year, "quarter": milestone.Quarter, "target_date": milestone.TargetDate,
		"status": milestone.Status, "project_ids": projectIDs,
	}
}

func previewAIRoadmapMilestoneAction(tx *gorm.DB, input aiWorkspaceAction) (aiActionPreview, error) {
	preview := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	if input.Action == "roadmap_milestone.create" {
		var create createRoadmapMilestoneRequest
		_ = json.Unmarshal(input.Changes, &create)
		milestone, projectIDs, err := roadmapMilestoneFromCreateRequest(create)
		if err != nil {
			return preview, err
		}
		if err := requireRoadmapProjects(tx, projectIDs); err != nil {
			return preview, err
		}
		preview.Label = milestone.Title
		preview.After = aiRoadmapMilestoneModelFields(milestone, projectIDs)
		return preview, nil
	}
	response, err := loadRoadmapMilestoneResponse(tx, input.RoadmapMilestoneID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return preview, newProjectRequestError(http.StatusNotFound, "ROADMAP_MILESTONE_NOT_FOUND", "Roadmap milestone not found")
	}
	if err != nil {
		return preview, err
	}
	if response.Version != input.ExpectedVersion {
		return preview, roadmapMilestoneVersionConflict()
	}
	preview.Label = response.Title
	if input.Action == "roadmap_milestone.delete" {
		_, impact, err := loadRoadmapMilestoneDeletionImpact(tx, response.ID, response.Version)
		if err != nil {
			return preview, err
		}
		preview.Before = map[string]any{
			"status":                    response.Status,
			"roadmap_milestone_version": response.Version,
		}
		preview.After = map[string]any{
			"roadmap_milestone_deleted":               true,
			"roadmap_milestone_project_links_deleted": impact.ProjectLinks,
			"roadmap_milestone_inbox_sources_marked":  impact.InboxSourcesMarked,
		}
		return preview, nil
	}
	if input.Action == "roadmap_milestone.update" {
		if response.Status == "archived" {
			return preview, newProjectRequestError(http.StatusConflict, "ROADMAP_MILESTONE_ARCHIVED", "Restore the roadmap milestone before editing it")
		}
		var current models.RoadmapMilestone
		if err := tx.First(&current, "id = ?", response.ID).Error; err != nil {
			return preview, err
		}
		var update updateRoadmapMilestoneRequest
		_ = json.Unmarshal(input.Changes, &update)
		updates, projectIDs, replaceProjects, err := roadmapMilestoneUpdates(current, update)
		if err != nil {
			return preview, err
		}
		if update.Status.Set && update.Status.Value != nil && *update.Status.Value == "archived" {
			return preview, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Use roadmap_milestone.archive to archive a roadmap milestone")
		}
		if replaceProjects {
			if err := requireRoadmapProjects(tx, projectIDs); err != nil {
				return preview, err
			}
		}
		before := aiRoadmapMilestoneFields(response)
		var changed map[string]json.RawMessage
		_ = json.Unmarshal(input.Changes, &changed)
		for field := range changed {
			preview.Before[field] = before[field]
			if field == "project_ids" {
				preview.After[field] = projectIDs
			} else {
				preview.After[field] = updates[field]
			}
		}
		return preview, nil
	}
	if input.Action == "roadmap_milestone.archive" {
		if response.Status == "archived" {
			return preview, newProjectRequestError(http.StatusConflict, "ROADMAP_MILESTONE_STATE_INVALID", "Roadmap milestone is already archived")
		}
		projectIDs := aiRoadmapMilestoneFields(response)["project_ids"]
		preview.Before["status"], preview.After["status"] = response.Status, "archived"
		preview.Before["project_ids"], preview.After["project_ids"] = projectIDs, projectIDs
		return preview, nil
	}
	if response.Status != "archived" || response.ArchivedFromStatus == nil {
		return preview, newProjectRequestError(http.StatusConflict, "ROADMAP_MILESTONE_STATE_INVALID", "Roadmap milestone is not archived")
	}
	projectIDs := aiRoadmapMilestoneFields(response)["project_ids"]
	preview.Before["status"], preview.After["status"] = response.Status, *response.ArchivedFromStatus
	preview.Before["project_ids"], preview.After["project_ids"] = projectIDs, projectIDs
	return preview, nil
}

func (a *API) executeAIRoadmapMilestoneAction(tx *gorm.DB, input aiWorkspaceAction, requestID string, clock time.Time) (aiActionResult, error) {
	now := formatInboxTimestamp(clock.UTC())
	if input.Action == "roadmap_milestone.delete" {
		return aiActionResult{}, errors.New("roadmap milestone deletion must use the guarded approval transaction")
	}
	if input.Action == "roadmap_milestone.create" {
		var create createRoadmapMilestoneRequest
		_ = json.Unmarshal(input.Changes, &create)
		milestone, projectIDs, err := roadmapMilestoneFromCreateRequest(create)
		if err != nil {
			return aiActionResult{}, err
		}
		milestone.CreatedAt, milestone.UpdatedAt = now, now
		if err := requireRoadmapProjects(tx, projectIDs); err != nil {
			return aiActionResult{}, err
		}
		order, err := nextRoadmapMilestoneOrder(tx, milestone.Year, milestone.Quarter)
		if err != nil {
			return aiActionResult{}, err
		}
		milestone.ManualOrder = order
		if err := tx.Create(&milestone).Error; err != nil {
			return aiActionResult{}, err
		}
		if err := replaceRoadmapMilestoneProjects(tx, milestone.ID, projectIDs, now); err != nil {
			return aiActionResult{}, err
		}
		eventType := "due"
		if milestone.Status == "achieved" {
			eventType = "achieved"
		}
		if err := a.projectRoadmapMilestoneInboxEvent(tx, milestone, eventType, requestID, clock); err != nil {
			return aiActionResult{}, err
		}
		return aiActionResult{ID: milestone.ID, Version: milestone.Version}, nil
	}
	var milestone models.RoadmapMilestone
	if err := tx.First(&milestone, "id = ?", input.RoadmapMilestoneID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return aiActionResult{}, newProjectRequestError(http.StatusNotFound, "ROADMAP_MILESTONE_NOT_FOUND", "Roadmap milestone not found")
		}
		return aiActionResult{}, err
	}
	if milestone.Version != input.ExpectedVersion {
		return aiActionResult{}, roadmapMilestoneVersionConflict()
	}
	if input.Action == "roadmap_milestone.update" {
		if milestone.Status == "archived" {
			return aiActionResult{}, newProjectRequestError(http.StatusConflict, "ROADMAP_MILESTONE_ARCHIVED", "Restore the roadmap milestone before editing it")
		}
		var update updateRoadmapMilestoneRequest
		_ = json.Unmarshal(input.Changes, &update)
		updates, projectIDs, replaceProjects, err := roadmapMilestoneUpdates(milestone, update)
		if err != nil {
			return aiActionResult{}, err
		}
		if update.Status.Set && update.Status.Value != nil && *update.Status.Value == "archived" {
			return aiActionResult{}, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Use roadmap_milestone.archive to archive a roadmap milestone")
		}
		if updates["year"] != milestone.Year || updates["quarter"] != milestone.Quarter {
			order, err := nextRoadmapMilestoneOrder(tx, updates["year"].(int), updates["quarter"].(int))
			if err != nil {
				return aiActionResult{}, err
			}
			updates["manual_order"] = order
		}
		if replaceProjects {
			if err := requireRoadmapProjects(tx, projectIDs); err != nil {
				return aiActionResult{}, err
			}
		}
		updates["updated_at"], updates["version"] = now, gorm.Expr("version + 1")
		result := tx.Model(&models.RoadmapMilestone{}).Where("id = ? AND version = ?", milestone.ID, input.ExpectedVersion).Updates(updates)
		if result.Error != nil {
			return aiActionResult{}, result.Error
		}
		if result.RowsAffected == 0 {
			return aiActionResult{}, roadmapMilestoneVersionConflict()
		}
		if replaceProjects {
			if err := replaceRoadmapMilestoneProjects(tx, milestone.ID, projectIDs, now); err != nil {
				return aiActionResult{}, err
			}
		}
		var updated models.RoadmapMilestone
		if err := tx.First(&updated, "id = ?", milestone.ID).Error; err != nil {
			return aiActionResult{}, err
		}
		if milestone.TargetDate != updated.TargetDate {
			if err := resolveRoadmapMilestoneInboxSources(tx, milestone.ID, "due", "roadmap_milestone_rescheduled", requestID, now); err != nil {
				return aiActionResult{}, err
			}
		}
		if roadmapMilestoneStatusInvalidatesInboxSource(milestone.Status, updated.Status) {
			eventType := "due"
			if milestone.Status == "achieved" {
				eventType = "achieved"
			}
			if err := resolveRoadmapMilestoneInboxSources(tx, milestone.ID, eventType, "roadmap_milestone_status_changed", requestID, now); err != nil {
				return aiActionResult{}, err
			}
		}
		if milestone.Status != "achieved" && updated.Status == "achieved" {
			if err := a.projectRoadmapMilestoneInboxEvent(tx, updated, "achieved", requestID, clock); err != nil {
				return aiActionResult{}, err
			}
		} else if (milestone.TargetDate != updated.TargetDate || milestone.Status != updated.Status) && (updated.Status == "planned" || updated.Status == "active") {
			if err := a.projectRoadmapMilestoneInboxEvent(tx, updated, "due", requestID, clock); err != nil {
				return aiActionResult{}, err
			}
		}
		return aiActionResult{ID: updated.ID, Version: updated.Version}, nil
	}
	updates := map[string]any{"updated_at": now, "version": gorm.Expr("version + 1")}
	if input.Action == "roadmap_milestone.archive" {
		if milestone.Status == "archived" {
			return aiActionResult{}, newProjectRequestError(http.StatusConflict, "ROADMAP_MILESTONE_STATE_INVALID", "Roadmap milestone is already archived")
		}
		updates["status"], updates["archived_from_status"] = "archived", milestone.Status
	} else {
		if milestone.Status != "archived" || milestone.ArchivedFromStatus == nil {
			return aiActionResult{}, newProjectRequestError(http.StatusConflict, "ROADMAP_MILESTONE_STATE_INVALID", "Roadmap milestone is not archived")
		}
		updates["status"], updates["archived_from_status"] = *milestone.ArchivedFromStatus, nil
	}
	result := tx.Model(&models.RoadmapMilestone{}).Where("id = ? AND version = ?", milestone.ID, input.ExpectedVersion).Updates(updates)
	if result.Error != nil {
		return aiActionResult{}, result.Error
	}
	if result.RowsAffected == 0 {
		return aiActionResult{}, roadmapMilestoneVersionConflict()
	}
	if input.Action == "roadmap_milestone.archive" {
		if err := resolveRoadmapMilestoneInboxSources(tx, milestone.ID, "", "roadmap_milestone_archived", requestID, now); err != nil {
			return aiActionResult{}, err
		}
	} else {
		var restored models.RoadmapMilestone
		if err := tx.First(&restored, "id = ?", milestone.ID).Error; err != nil {
			return aiActionResult{}, err
		}
		if restored.Status != "achieved" {
			if err := a.projectRoadmapMilestoneInboxEvent(tx, restored, "due", requestID, clock); err != nil {
				return aiActionResult{}, err
			}
		}
	}
	var updated models.RoadmapMilestone
	if err := tx.First(&updated, "id = ?", milestone.ID).Error; err != nil {
		return aiActionResult{}, err
	}
	return aiActionResult{ID: updated.ID, Version: updated.Version}, nil
}
