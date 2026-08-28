package api

import (
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

const (
	searchDefaultPageSize = 20
	searchMaximumPageSize = 100
)

var searchResourceTypes = []string{"task", "project", "client", "inbox_item"}

type searchRow struct {
	ResourceType string  `gorm:"column:resource_type"`
	ResourceID   string  `gorm:"column:resource_id"`
	Title        string  `gorm:"column:title"`
	Subtitle     *string `gorm:"column:subtitle"`
	Status       string  `gorm:"column:status"`
	UpdatedAt    string  `gorm:"column:updated_at"`
	PrimaryText  string  `gorm:"column:primary_text"`
	Secondary1   *string `gorm:"column:secondary_1"`
	Secondary2   *string `gorm:"column:secondary_2"`
	Secondary3   *string `gorm:"column:secondary_3"`
}

type searchResultResponse struct {
	ResourceType  string   `json:"resource_type"`
	ResourceID    string   `json:"resource_id"`
	Title         string   `json:"title"`
	Subtitle      string   `json:"subtitle"`
	MatchedFields []string `json:"matched_fields"`
	Route         string   `json:"route"`
	Status        string   `json:"status"`
	UpdatedAt     string   `json:"updated_at"`
}

type searchSelect struct {
	statement string
	arguments func(string, string, string) []any
}

func (a *API) search(c *gin.Context) {
	query := strings.TrimSpace(c.Query("q"))
	if query == "" {
		writeError(c, http.StatusBadRequest, "INVALID_QUERY", "q is required")
		return
	}
	if utf8.RuneCountInString(query) > 200 {
		writeError(c, http.StatusBadRequest, "INVALID_QUERY", "q cannot exceed 200 characters")
		return
	}
	page, ok := queryInt(c, "page", 1, 1, 1_000_000)
	if !ok {
		return
	}
	pageSize, ok := queryInt(c, "page_size", searchDefaultPageSize, 1, searchMaximumPageSize)
	if !ok {
		return
	}
	types, ok := parseSearchTypes(c)
	if !ok {
		return
	}

	escaped := escapeLike(query)
	contains := "%" + escaped + "%"
	prefix := escaped + "%"
	selects := searchSelects()
	statements := make([]string, 0, len(types))
	arguments := make([]any, 0, len(types)*6)
	for _, resourceType := range types {
		selected := selects[resourceType]
		statements = append(statements, selected.statement)
		arguments = append(arguments, selected.arguments(query, prefix, contains)...)
	}
	union := strings.Join(statements, " UNION ALL ")
	var total int64
	if err := a.db.WithContext(c.Request.Context()).Raw("SELECT COUNT(*) FROM ("+union+") AS search_matches", arguments...).Scan(&total).Error; err != nil {
		writeDatabaseError(c)
		return
	}

	listArguments := append(append([]any{}, arguments...), pageSize, (page-1)*pageSize)
	var rows []searchRow
	statement := "SELECT resource_type, resource_id, title, subtitle, status, updated_at, primary_text, secondary_1, secondary_2, secondary_3 FROM (" + union + ") AS search_matches ORDER BY relevance_rank ASC, julianday(updated_at) DESC, updated_at DESC, resource_type ASC, resource_id ASC LIMIT ? OFFSET ?"
	if err := a.db.WithContext(c.Request.Context()).Raw(statement, listArguments...).Scan(&rows).Error; err != nil {
		writeDatabaseError(c)
		return
	}

	results := make([]searchResultResponse, 0, len(rows))
	for _, row := range rows {
		results = append(results, searchResponse(row, query))
	}
	c.JSON(http.StatusOK, gin.H{
		"data": results,
		"meta": pageMeta{Page: page, PageSize: pageSize, Total: total},
	})
}

func parseSearchTypes(c *gin.Context) ([]string, bool) {
	rawValues, present := c.GetQueryArray("types")
	if !present {
		return append([]string(nil), searchResourceTypes...), true
	}
	valid := make(map[string]struct{}, len(searchResourceTypes))
	for _, resourceType := range searchResourceTypes {
		valid[resourceType] = struct{}{}
	}
	seen := make(map[string]struct{}, len(searchResourceTypes))
	for _, raw := range rawValues {
		for _, value := range strings.Split(raw, ",") {
			resourceType := strings.TrimSpace(value)
			if _, exists := valid[resourceType]; !exists {
				writeError(c, http.StatusBadRequest, "INVALID_TYPES", "types must contain only task, project, client, or inbox_item")
				return nil, false
			}
			seen[resourceType] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for _, resourceType := range searchResourceTypes {
		if _, exists := seen[resourceType]; exists {
			result = append(result, resourceType)
		}
	}
	if len(result) == 0 {
		writeError(c, http.StatusBadRequest, "INVALID_TYPES", "types cannot be empty")
		return nil, false
	}
	return result, true
}

func searchSelects() map[string]searchSelect {
	return map[string]searchSelect{
		"task": {
			statement: `SELECT 'task' AS resource_type, tasks.id AS resource_id, tasks.title AS title,
				COALESCE(projects.name, '') AS subtitle, tasks.status AS status, tasks.updated_at AS updated_at,
				tasks.title AS primary_text, tasks.description AS secondary_1, NULL AS secondary_2, NULL AS secondary_3,
				CASE WHEN tasks.title = ? COLLATE NOCASE THEN 0 WHEN tasks.title LIKE ? ESCAPE '\' THEN 1 WHEN tasks.title LIKE ? ESCAPE '\' THEN 2 ELSE 3 END AS relevance_rank
			 FROM tasks LEFT JOIN projects ON projects.id = tasks.project_id
			 WHERE tasks.title LIKE ? ESCAPE '\' OR tasks.description LIKE ? ESCAPE '\'`,
			arguments: func(query, prefix, contains string) []any {
				return []any{query, prefix, contains, contains, contains}
			},
		},
		"project": {
			statement: `SELECT 'project' AS resource_type, projects.id AS resource_id, projects.name AS title,
				COALESCE(clients.name, '') AS subtitle, projects.status AS status, projects.updated_at AS updated_at,
				projects.name AS primary_text, projects.description AS secondary_1, NULL AS secondary_2, NULL AS secondary_3,
				CASE WHEN projects.name = ? COLLATE NOCASE THEN 0 WHEN projects.name LIKE ? ESCAPE '\' THEN 1 WHEN projects.name LIKE ? ESCAPE '\' THEN 2 ELSE 3 END AS relevance_rank
			 FROM projects LEFT JOIN clients ON clients.id = projects.client_id
			 WHERE projects.status <> 'archived' AND (projects.name LIKE ? ESCAPE '\' OR projects.description LIKE ? ESCAPE '\')`,
			arguments: func(query, prefix, contains string) []any {
				return []any{query, prefix, contains, contains, contains}
			},
		},
		"client": {
			statement: `SELECT 'client' AS resource_type, clients.id AS resource_id, clients.name AS title,
				COALESCE(clients.contact_name, clients.email, '') AS subtitle, clients.status AS status, clients.updated_at AS updated_at,
				clients.name AS primary_text, clients.contact_name AS secondary_1, clients.email AS secondary_2, clients.phone AS secondary_3,
				CASE WHEN clients.name = ? COLLATE NOCASE THEN 0 WHEN clients.name LIKE ? ESCAPE '\' THEN 1 WHEN clients.name LIKE ? ESCAPE '\' THEN 2 ELSE 3 END AS relevance_rank
			 FROM clients
			 WHERE clients.name LIKE ? ESCAPE '\' OR clients.contact_name LIKE ? ESCAPE '\' OR clients.email LIKE ? ESCAPE '\' OR clients.phone LIKE ? ESCAPE '\'`,
			arguments: func(query, prefix, contains string) []any {
				return []any{query, prefix, contains, contains, contains, contains, contains}
			},
		},
		"inbox_item": {
			statement: `SELECT 'inbox_item' AS resource_type, inbox_items.id AS resource_id, inbox_items.title AS title,
				inbox_items.kind AS subtitle, inbox_items.status AS status, inbox_items.updated_at AS updated_at,
				inbox_items.title AS primary_text, inbox_items.summary AS secondary_1, NULL AS secondary_2, NULL AS secondary_3,
				CASE WHEN inbox_items.title = ? COLLATE NOCASE THEN 0 WHEN inbox_items.title LIKE ? ESCAPE '\' THEN 1 WHEN inbox_items.title LIKE ? ESCAPE '\' THEN 2 ELSE 3 END AS relevance_rank
			 FROM inbox_items
			 WHERE inbox_items.status IN ('open', 'tracking') AND (inbox_items.title LIKE ? ESCAPE '\' OR inbox_items.summary LIKE ? ESCAPE '\')`,
			arguments: func(query, prefix, contains string) []any {
				return []any{query, prefix, contains, contains, contains}
			},
		},
	}
}

func searchResponse(row searchRow, query string) searchResultResponse {
	fieldsByType := map[string][]string{
		"task":       {"title", "description", "", ""},
		"project":    {"name", "description", "", ""},
		"client":     {"name", "contact_name", "email", "phone"},
		"inbox_item": {"title", "summary", "", ""},
	}
	fields := fieldsByType[row.ResourceType]
	values := []*string{&row.PrimaryText, row.Secondary1, row.Secondary2, row.Secondary3}
	matched := make([]string, 0, len(values))
	for index, value := range values {
		if value != nil && fields[index] != "" && containsFold(*value, query) {
			matched = append(matched, fields[index])
		}
	}
	subtitle := ""
	if row.Subtitle != nil {
		subtitle = *row.Subtitle
	}
	return searchResultResponse{
		ResourceType: row.ResourceType, ResourceID: row.ResourceID, Title: row.Title,
		Subtitle: subtitle, MatchedFields: matched, Route: searchRoute(row.ResourceType, row.ResourceID),
		Status: row.Status, UpdatedAt: normalizeTimestamp(row.UpdatedAt),
	}
}

func containsFold(value, query string) bool {
	return strings.Contains(strings.ToLower(value), strings.ToLower(query))
}

func searchRoute(resourceType, resourceID string) string {
	prefixes := map[string]string{
		"task": "/tasks/", "project": "/projects/", "client": "/clients/", "inbox_item": "/inbox/",
	}
	return fmt.Sprintf("%s%s", prefixes[resourceType], resourceID)
}
