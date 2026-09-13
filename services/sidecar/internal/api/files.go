package api

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// controlledFileScopeSet lists the read-only controlled-file scopes the files
// index exposes. The workspace avatar is deliberately excluded: it is a single
// settings-controlled image surfaced through the settings module, not a
// browsable collection.
var controlledFileScopeSet = map[string]struct{}{
	"artifact":           {},
	"client_attachment":  {},
	"project_attachment": {},
	"knowledge_document": {},
}

type controlledFileResponse struct {
	ID           string  `json:"id"`
	Scope        string  `json:"scope"`
	Name         string  `json:"name"`
	MimeType     *string `json:"mime_type"`
	SizeBytes    *int64  `json:"size_bytes"`
	SHA256       *string `json:"sha256"`
	OwnerLabel   string  `json:"owner_label"`
	ContentRoute string  `json:"content_route"`
	UpdatedAt    string  `json:"updated_at"`
}

type controlledFileRow struct {
	ID           string  `gorm:"column:id"`
	Scope        string  `gorm:"column:scope"`
	Name         string  `gorm:"column:name"`
	MimeType     *string `gorm:"column:mime_type"`
	SizeBytes    *int64  `gorm:"column:size_bytes"`
	SHA256       *string `gorm:"column:sha256"`
	OwnerLabel   string  `gorm:"column:owner_label"`
	ContentRoute string  `gorm:"column:content_route"`
	UpdatedAt    string  `gorm:"column:updated_at"`
}

// controlledFilesUnionSQL projects every browsable controlled file into one
// read-only shape. It returns metadata and a routed content reference only;
// bodies stay behind the existing authenticated content endpoints.
const controlledFilesUnionSQL = `
SELECT id, scope, name, mime_type, size_bytes, sha256, owner_label, content_route, updated_at FROM (
  SELECT a.id AS id, 'artifact' AS scope, a.name AS name, a.mime_type AS mime_type,
         a.size_bytes AS size_bytes, a.sha256 AS sha256,
         COALESCE(task.title, '') AS owner_label,
         '/api/v1/artifacts/' || a.id AS content_route,
         a.created_at AS updated_at
  FROM task_artifacts AS a
  LEFT JOIN tasks AS task ON task.id = a.task_id
  WHERE a.deleted_at IS NULL
  UNION ALL
  SELECT ca.id, 'client_attachment', ca.name, ca.mime_type, ca.size_bytes, ca.sha256,
         COALESCE(client.name, ''),
         '/api/v1/client-attachments/' || ca.id || '/content',
         ca.created_at
  FROM client_attachments AS ca
  LEFT JOIN clients AS client ON client.id = ca.client_id
  WHERE ca.deleted_at IS NULL
  UNION ALL
  SELECT pa.id, 'project_attachment', pa.name, pa.mime_type, pa.size_bytes, pa.sha256,
         COALESCE(project.name, ''),
         '/api/v1/project-attachments/' || pa.id || '/content',
         pa.created_at
  FROM project_attachments AS pa
  LEFT JOIN projects AS project ON project.id = pa.project_id
  WHERE pa.deleted_at IS NULL
  UNION ALL
  SELECT kd.id, 'knowledge_document', kd.title, 'text/plain', NULL, kd.content_sha256,
         COALESCE(ks.name, ks.title, ''),
         '/api/v1/knowledge/documents/' || kd.id,
         kd.updated_at
  FROM knowledge_documents AS kd
  LEFT JOIN knowledge_sources AS ks ON ks.id = kd.source_id
)
`

func (a *API) listControlledFiles(c *gin.Context) {
	page, ok := queryInt(c, "page", 1, 1, 1000)
	if !ok {
		return
	}
	pageSize, ok := queryInt(c, "page_size", 50, 1, 100)
	if !ok {
		return
	}
	scope := strings.TrimSpace(c.Query("scope"))
	if scope != "" {
		if _, allowed := controlledFileScopeSet[scope]; !allowed {
			writeError(c, http.StatusBadRequest, "INVALID_FILE_SCOPE",
				"scope must be one of artifact, client_attachment, project_attachment, knowledge_document")
			return
		}
	}
	where := ""
	args := []any{}
	if scope != "" {
		where = " WHERE scope = ?"
		args = append(args, scope)
	}
	var rows []controlledFileRow
	var total int64
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		countSQL := "SELECT COUNT(*) FROM (" + controlledFilesUnionSQL + ")" + where
		if err := tx.Raw(countSQL, args...).Scan(&total).Error; err != nil {
			return err
		}
		listSQL := "SELECT * FROM (" + controlledFilesUnionSQL + ")" + where +
			" ORDER BY updated_at DESC, id DESC LIMIT ? OFFSET ?"
		listArgs := append(append([]any{}, args...), pageSize, (page-1)*pageSize)
		return tx.Raw(listSQL, listArgs...).Scan(&rows).Error
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		writeDatabaseError(c)
		return
	}
	items := make([]controlledFileResponse, len(rows))
	for index := range rows {
		row := rows[index]
		items[index] = controlledFileResponse{
			ID:           row.ID,
			Scope:        row.Scope,
			Name:         row.Name,
			MimeType:     row.MimeType,
			SizeBytes:    row.SizeBytes,
			SHA256:       row.SHA256,
			OwnerLabel:   row.OwnerLabel,
			ContentRoute: row.ContentRoute,
			UpdatedAt:    row.UpdatedAt,
		}
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"data": items,
		"meta": pageMeta{Page: page, PageSize: pageSize, Total: total},
	})
}
