package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func aiRoadmapMilestonesSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"filters":{"type":"object","additionalProperties":false,"description":"The same year/quarter/status/project/archive filters as the native roadmap list. A quarter without a year spans matching quarters in all years.","properties":{"year":{"type":"integer","minimum":2000,"maximum":2100},"quarter":{"type":"integer","minimum":1,"maximum":4},"status":{"type":"string","enum":["planned","active","achieved","archived"]},"project_id":{"type":"string","format":"uuid"},"include_archived":{"type":"boolean"}}},"sort":{"type":"string","enum":["target_date"],"description":"Omit for native quarter/manual order; target_date sorts across quarters by date then ID."},"limit":{"type":"integer","minimum":1,"maximum":20},"offset":{"type":"integer","minimum":0,"maximum":1000}}}`)
}

type aiRoadmapMilestonesQuery struct {
	Filters json.RawMessage `json:"filters"`
	Sort    *string         `json:"sort"`
	Limit   *int            `json:"limit"`
	Offset  int             `json:"offset"`
}

type aiRoadmapMilestoneFilters struct {
	Year            *int    `json:"year"`
	Quarter         *int    `json:"quarter"`
	Status          *string `json:"status"`
	ProjectID       *string `json:"project_id"`
	IncludeArchived bool    `json:"include_archived"`
}

// Reject duplicate, unknown and null keys before Go's JSON decoder can hide
// them through last-write-wins or zero values.
func decodeAIRoadmapObject(raw json.RawMessage, allowed map[string]bool) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return errors.New("roadmap query must be an object")
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || !allowed[key] || seen[key] {
			return errors.New("roadmap query requires exact unique fields")
		}
		seen[key] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errors.New("roadmap query fields must be non-null")
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return errors.New("roadmap query is invalid")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("roadmap query has trailing data")
	}
	return nil
}

func parseAIRoadmapMilestonesQuery(raw json.RawMessage) (aiRoadmapMilestonesQuery, aiRoadmapMilestoneFilters, error) {
	var input aiRoadmapMilestonesQuery
	var filters aiRoadmapMilestoneFilters
	if err := decodeAIRoadmapObject(raw, map[string]bool{"filters": true, "sort": true, "limit": true, "offset": true}); err != nil {
		return input, filters, err
	}
	if err := decodeStrictToolArguments(raw, &input); err != nil {
		return input, filters, err
	}
	if len(input.Filters) > 0 {
		if err := decodeAIRoadmapObject(input.Filters, map[string]bool{"year": true, "quarter": true, "status": true, "project_id": true, "include_archived": true}); err != nil {
			return input, filters, err
		}
		if err := decodeStrictToolArguments(input.Filters, &filters); err != nil {
			return input, filters, err
		}
	}
	if filters.Year != nil && (*filters.Year < 2000 || *filters.Year > 2100) {
		return input, filters, errors.New("year must be between 2000 and 2100")
	}
	if filters.Quarter != nil && (*filters.Quarter < 1 || *filters.Quarter > 4) {
		return input, filters, errors.New("quarter must be between 1 and 4")
	}
	if filters.Status != nil {
		if _, ok := roadmapMilestoneStatuses[*filters.Status]; !ok {
			return input, filters, errors.New("roadmap status is invalid")
		}
	}
	if filters.ProjectID != nil {
		id, err := uuid.Parse(*filters.ProjectID)
		if err != nil || id.String() != *filters.ProjectID {
			return input, filters, errors.New("project_id must be a canonical UUID")
		}
	}
	if input.Sort != nil && *input.Sort != "target_date" {
		return input, filters, errors.New("sort must be target_date or omitted")
	}
	return input, filters, nil
}

type aiRoadmapMilestoneListItem struct {
	ID                 string             `json:"id"`
	Title              string             `json:"title"`
	Year               int                `json:"year"`
	Quarter            int                `json:"quarter"`
	TargetDate         string             `json:"target_date"`
	Status             string             `json:"status"`
	ManualOrder        int64              `json:"manual_order"`
	ArchivedFromStatus *string            `json:"archived_from_status"`
	Version            int64              `json:"version"`
	ProjectCount       int                `json:"project_count"`
	TaskSummary        roadmapTaskSummary `json:"task_summary"`
	Route              string             `json:"route"`
}

func (t *aiWorkspaceTool) roadmapMilestones(ctx context.Context, args json.RawMessage) (any, error) {
	if err := t.policy.Require("work"); err != nil {
		return nil, err
	}
	input, filters, err := parseAIRoadmapMilestonesQuery(args)
	if err != nil {
		return nil, err
	}
	limit, err := workspacePaging(input.Limit, input.Offset)
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	err = t.api.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result["as_of"] = formatInboxTimestamp(t.api.options.Now())
		year, quarter, status, projectID := 0, 0, "", ""
		if filters.Year != nil {
			year = *filters.Year
		}
		if filters.Quarter != nil {
			quarter = *filters.Quarter
		}
		if filters.Status != nil {
			status = *filters.Status
		}
		if filters.ProjectID != nil {
			projectID = *filters.ProjectID
		}
		query := roadmapMilestoneFilteredQuery(tx, year, quarter, status, projectID, filters.IncludeArchived)
		var total int64
		if err := query.Count(&total).Error; err != nil {
			return err
		}
		if input.Sort != nil && *input.Sort == "target_date" {
			query = query.Order("roadmap_milestones.target_date ASC").Order("roadmap_milestones.id ASC")
		} else {
			query = query.Order("roadmap_milestones.year ASC").Order("roadmap_milestones.quarter ASC").
				Order("roadmap_milestones.manual_order ASC").Order("roadmap_milestones.target_date ASC").Order("roadmap_milestones.id ASC")
		}
		milestones := []models.RoadmapMilestone{}
		if err := query.Offset(input.Offset).Limit(limit + 1).Find(&milestones).Error; err != nil {
			return err
		}
		more := len(milestones) > limit
		if more {
			milestones = milestones[:limit]
		}
		responses, err := loadRoadmapMilestoneResponses(tx, milestones)
		if err != nil {
			return err
		}
		items := make([]aiRoadmapMilestoneListItem, 0, len(responses))
		for _, response := range responses {
			items = append(items, aiRoadmapMilestoneListItem{
				ID: response.ID, Title: response.Title, Year: response.Year, Quarter: response.Quarter,
				TargetDate: response.TargetDate, Status: response.Status, ManualOrder: response.ManualOrder,
				ArchivedFromStatus: response.ArchivedFromStatus, Version: response.Version,
				ProjectCount: len(response.Projects), TaskSummary: response.TaskSummary,
				Route: searchRoute("roadmap_milestone", response.ID),
			})
		}
		var next *int
		if more && input.Offset+limit <= 1000 {
			value := input.Offset + limit
			next = &value
		}
		result["items"] = items
		result["total_items"], result["has_more"], result["next_offset"], result["window_limited"] = total, more, next, more && next == nil
		return nil
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, safeAIWorkspaceError(ctx, err)
	}
	return result, nil
}
