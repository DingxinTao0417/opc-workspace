package api

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const createTagEndpoint = "POST /api/v1/tags"

type createTagRequest struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type updateTagRequest struct {
	Name  *string `json:"name"`
	Color *string `json:"color"`
}

type deletedTagResponse struct {
	DeletedID     string `json:"deleted_id"`
	DetachedTasks int64  `json:"detached_tasks"`
}

func (a *API) listTags(c *gin.Context) {
	page, ok := queryInt(c, "page", 1, 1, 1_000_000)
	if !ok {
		return
	}
	pageSize, ok := queryInt(c, "page_size", 100, 1, 100)
	if !ok {
		return
	}

	query := a.db.WithContext(c.Request.Context()).Model(&models.Tag{})
	if search := strings.TrimSpace(c.Query("q")); search != "" {
		query = query.Where("name LIKE ? ESCAPE '\\'", "%"+escapeLike(search)+"%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	ordered, ok := applyTagSort(query, c.Query("sort"))
	if !ok {
		writeError(c, http.StatusBadRequest, "INVALID_SORT", "sort contains an unsupported field")
		return
	}
	var tags []models.Tag
	if err := ordered.Offset((page - 1) * pageSize).Limit(pageSize).Find(&tags).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	normalizeTags(tags)
	c.JSON(http.StatusOK, gin.H{"data": tags, "meta": pageMeta{Page: page, PageSize: pageSize, Total: total}})
}

func (a *API) createTag(c *gin.Context) {
	var input createTagRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	tag, err := tagFromCreateRequest(input)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}

	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if err := validateIdempotencyKey(idempotencyKey); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", err.Error())
		return
	}
	requestHash := ""
	if idempotencyKey != "" {
		requestHash = tagCreateRequestHash(tag)
	}

	statusCode := http.StatusCreated
	replayed := false
	var response models.Tag
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if idempotencyKey != "" {
			var existing models.IdempotencyKey
			err := tx.Where("key = ? AND endpoint = ?", idempotencyKey, createTagEndpoint).First(&existing).Error
			if err == nil {
				if existing.RequestHash == nil || existing.ResponseBody == nil || existing.ResponseStatus == nil {
					return newProjectRequestError(http.StatusConflict, "IDEMPOTENCY_REPLAY_UNAVAILABLE", "This legacy Idempotency-Key cannot be replayed safely; use a new key")
				}
				if *existing.RequestHash != requestHash {
					return newProjectRequestError(http.StatusConflict, "IDEMPOTENCY_CONFLICT", "Idempotency-Key was already used with a different tag request")
				}
				if err := json.Unmarshal([]byte(*existing.ResponseBody), &response); err != nil {
					return fmt.Errorf("decode idempotent tag response: %w", err)
				}
				statusCode = *existing.ResponseStatus
				replayed = true
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("read tag idempotency key: %w", err)
			}
		}
		if err := requireUniqueTagName(tx, tag.Name, ""); err != nil {
			return err
		}
		if err := tx.Create(&tag).Error; err != nil {
			return fmt.Errorf("create tag: %w", err)
		}
		response = tag
		normalizeTag(&response)
		if idempotencyKey != "" {
			encoded, err := json.Marshal(response)
			if err != nil {
				return fmt.Errorf("encode idempotent tag response: %w", err)
			}
			responseText := string(encoded)
			responseStatus := http.StatusCreated
			record := models.IdempotencyKey{
				Key: idempotencyKey, Endpoint: createTagEndpoint, ResourceID: tag.ID,
				RequestHash: &requestHash, ResponseBody: &responseText, ResponseStatus: &responseStatus,
				CreatedAt: tag.CreatedAt,
			}
			if err := tx.Create(&record).Error; err != nil {
				return fmt.Errorf("record tag idempotency key: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, response.Version)
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	c.JSON(statusCode, gin.H{"data": response})
}

func (a *API) updateTag(c *gin.Context) {
	id, ok := tagID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input updateTagRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}

	var response models.Tag
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var tag models.Tag
		if err := tx.First(&tag, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "TAG_NOT_FOUND", "Tag not found")
			}
			return err
		}
		if tag.Version != expectedVersion {
			return taskVersionConflict()
		}

		updates := make(map[string]any)
		if input.Name != nil {
			name, err := validateTagName(*input.Name)
			if err != nil {
				return newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
			}
			if err := requireUniqueTagName(tx, name, id); err != nil {
				return err
			}
			updates["name"] = name
		}
		if input.Color != nil {
			color, err := validateTagColor(*input.Color)
			if err != nil {
				return newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
			}
			updates["color"] = color
		}
		if len(updates) == 0 {
			return newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "at least one editable tag field is required")
		}
		updates["version"] = gorm.Expr("version + 1")
		result := tx.Model(&models.Tag{}).Where("id = ? AND version = ?", id, expectedVersion).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return taskVersionConflict()
		}
		if err := bumpTasksForTag(tx, id); err != nil {
			return err
		}
		return tx.First(&response, "id = ?", id).Error
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	normalizeTag(&response)
	setProjectETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) deleteTag(c *gin.Context) {
	id, ok := tagID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	if c.Query("confirm") != "true" {
		writeError(c, http.StatusUnprocessableEntity, "CONFIRMATION_REQUIRED", "Permanent tag deletion requires confirm=true")
		return
	}

	response := deletedTagResponse{DeletedID: id}
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var tag models.Tag
		if err := tx.First(&tag, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "TAG_NOT_FOUND", "Tag not found")
			}
			return err
		}
		if tag.Version != expectedVersion {
			return taskVersionConflict()
		}
		var taskIDs []string
		if err := tx.Table("task_tags").Where("tag_id = ?", id).Pluck("task_id", &taskIDs).Error; err != nil {
			return err
		}
		response.DetachedTasks = int64(len(taskIDs))
		result := tx.Delete(&models.Tag{}, "id = ? AND version = ?", id, expectedVersion)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return taskVersionConflict()
		}
		if len(taskIDs) > 0 {
			if err := bumpTaskVersions(tx, taskIDs); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func tagFromCreateRequest(input createTagRequest) (models.Tag, error) {
	name, err := validateTagName(input.Name)
	if err != nil {
		return models.Tag{}, err
	}
	color, err := validateTagColor(input.Color)
	if err != nil {
		return models.Tag{}, err
	}
	return models.Tag{
		ID: uuid.NewString(), Name: name, Color: color, Version: 1,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}, nil
}

func validateTagName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if length := utf8.RuneCountInString(name); length < 1 || length > 50 {
		return "", errors.New("name must contain 1 to 50 characters")
	}
	return name, nil
}

func validateTagColor(value string) (string, error) {
	color := strings.ToUpper(strings.TrimSpace(value))
	if !projectColorPattern.MatchString(color) {
		return "", errors.New("color must use #RRGGBB")
	}
	return color, nil
}

func requireUniqueTagName(db *gorm.DB, name, exceptID string) error {
	query := db.Model(&models.Tag{}).Where("LOWER(name) = LOWER(?)", name)
	if exceptID != "" {
		query = query.Where("id <> ?", exceptID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return newProjectRequestError(http.StatusConflict, "TAG_NAME_CONFLICT", "A tag with this name already exists")
	}
	return nil
}

func tagCreateRequestHash(tag models.Tag) string {
	encoded, _ := json.Marshal(struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}{Name: tag.Name, Color: tag.Color})
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", digest)
}

func tagID(c *gin.Context) (string, bool) {
	id := strings.TrimSpace(c.Param("id"))
	if _, err := uuid.Parse(id); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_TAG_ID", "Tag id must be a UUID")
		return "", false
	}
	return id, true
}

func applyTagSort(query *gorm.DB, raw string) (*gorm.DB, bool) {
	if strings.TrimSpace(raw) == "" {
		return query.Order("LOWER(tags.name) ASC").Order("tags.id ASC"), true
	}
	allowed := map[string]string{"name": "LOWER(tags.name)", "created_at": "tags.created_at"}
	for _, part := range strings.Split(raw, ",") {
		field := strings.TrimSpace(part)
		direction := "ASC"
		if strings.HasPrefix(field, "-") {
			direction = "DESC"
			field = strings.TrimPrefix(field, "-")
		}
		column, ok := allowed[field]
		if !ok {
			return query, false
		}
		query = query.Order(column + " " + direction)
	}
	return query.Order("tags.id ASC"), true
}

func bumpTasksForTag(db *gorm.DB, tagID string) error {
	return db.Exec(`
		UPDATE tasks
		SET version = version + 1, updated_at = ?
		WHERE id IN (SELECT task_id FROM task_tags WHERE tag_id = ?)
	`, time.Now().UTC().Format(time.RFC3339Nano), tagID).Error
}

func bumpTaskVersions(db *gorm.DB, taskIDs []string) error {
	if len(taskIDs) == 0 {
		return nil
	}
	return db.Model(&models.Task{}).
		Where("id IN ?", taskIDs).
		Updates(map[string]any{
			"version":    gorm.Expr("version + 1"),
			"updated_at": time.Now().UTC().Format(time.RFC3339Nano),
		}).Error
}

func normalizeTags(tags []models.Tag) {
	for index := range tags {
		normalizeTag(&tags[index])
	}
}

func normalizeTag(tag *models.Tag) {
	tag.CreatedAt = normalizeTimestamp(tag.CreatedAt)
}

func taskVersionConflict() error {
	return newProjectRequestError(http.StatusConflict, "VERSION_CONFLICT", "The resource has changed; reload it before retrying")
}
