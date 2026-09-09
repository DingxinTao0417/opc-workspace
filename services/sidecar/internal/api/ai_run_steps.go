package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const aiMaxDetailedRunSteps = 62

type aiRunStepCollector struct {
	mu    sync.Mutex
	steps []harness.RunStep
}

func (c *aiRunStepCollector) add(step harness.RunStep) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.steps) >= aiMaxDetailedRunSteps {
		return
	}
	c.steps = append(c.steps, step)
}

func (c *aiRunStepCollector) snapshot() []harness.RunStep {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]harness.RunStep, len(c.steps))
	copy(result, c.steps)
	return result
}

func createAIRunRootStep(tx *gorm.DB, generation models.AIGeneration) error {
	return tx.Create(&models.AIRunStep{
		ID: uuid.NewString(), GenerationID: generation.ID, Sequence: 1,
		Kind: "generation", Status: "running", StartedAt: generation.CreatedAt, CreatedAt: generation.CreatedAt,
	}).Error
}

func persistAIRunSteps(
	tx *gorm.DB,
	generation models.AIGeneration,
	steps []harness.RunStep,
	rootStatus, rootErrorCode, completedAt string,
) error {
	inputBytes, outputBytes := 0, 0
	inputTokens, outputTokens := 0, 0
	usageSteps := 0
	providerUsageComplete := true
	rows := make([]models.AIRunStep, 0, len(steps)+1)
	for _, step := range steps {
		if step.Kind != "model_turn" && step.Kind != "tool_call" && step.Kind != "self_check" && step.Kind != "citation_validation" {
			continue
		}
		if step.Status != "succeeded" && step.Status != "failed" && step.Status != "cancelled" {
			continue
		}
		started := step.StartedAt.UTC()
		completed := step.CompletedAt.UTC()
		if started.IsZero() {
			started, _ = time.Parse(time.RFC3339Nano, generation.CreatedAt)
		}
		if completed.IsZero() || completed.Before(started) {
			completed = started
		}
		duration := step.DurationMS
		if duration < 0 {
			duration = 0
		}
		input := step.InputBytes
		if input < 0 {
			input = 0
		}
		output := step.OutputBytes
		if output < 0 {
			output = 0
		}
		inputBytes += input
		outputBytes += output
		var stepInputTokens, stepOutputTokens *int
		var tokenSource *string
		if step.Kind == "model_turn" || (step.Kind == "self_check" && (step.InputBytes > 0 || step.OutputBytes > 0 || step.UsageAvailable)) {
			usageSteps++
			if step.UsageAvailable && step.InputTokens >= 0 && step.OutputTokens >= 0 {
				inputValue, outputValue, sourceValue := step.InputTokens, step.OutputTokens, "provider"
				stepInputTokens, stepOutputTokens, tokenSource = &inputValue, &outputValue, &sourceValue
				inputTokens += inputValue
				outputTokens += outputValue
			} else {
				providerUsageComplete = false
			}
		}
		var turnIndex *int
		if step.Kind == "model_turn" || step.Kind == "self_check" {
			value := step.TurnIndex
			if value < 1 {
				value = 1
			}
			turnIndex = &value
		}
		var toolName *string
		if step.Kind == "tool_call" {
			value := strings.TrimSpace(step.ToolName)
			if value == "" {
				value = "unknown_tool"
			}
			toolName = &value
		}
		var errorCode *string
		if step.Status == "failed" {
			value := strings.TrimSpace(step.ErrorCode)
			if value == "" {
				value = "AI_RUN_STEP_FAILED"
			}
			errorCode = &value
		}
		completedText := completed.Format(time.RFC3339Nano)
		durationValue := duration
		rows = append(rows, models.AIRunStep{
			ID: uuid.NewString(), GenerationID: generation.ID, Sequence: len(rows) + 2,
			Kind: step.Kind, Status: step.Status, TurnIndex: turnIndex, ToolName: toolName,
			StartedAt: started.Format(time.RFC3339Nano), CompletedAt: &completedText, DurationMS: &durationValue,
			InputBytes: input, OutputBytes: output, ErrorCode: errorCode,
			InputTokens: stepInputTokens, OutputTokens: stepOutputTokens, TokenSource: tokenSource,
			CreatedAt: completedText,
		})
	}
	rootDuration := aiRunDurationMilliseconds(generation.CreatedAt, completedAt)
	rootUpdates := map[string]any{
		"status": rootStatus, "completed_at": completedAt, "duration_ms": rootDuration,
		"input_bytes": inputBytes, "output_bytes": outputBytes,
	}
	if rootStatus == "failed" {
		rootUpdates["error_code"] = rootErrorCode
	}
	if usageSteps > 0 && providerUsageComplete {
		rootUpdates["input_tokens"] = inputTokens
		rootUpdates["output_tokens"] = outputTokens
		rootUpdates["token_source"] = "provider"
	}
	result := tx.Model(&models.AIRunStep{}).
		Where("generation_id = ? AND sequence = 1 AND kind = 'generation' AND status = 'running'", generation.ID).
		Updates(rootUpdates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("AI generation root step is unavailable")
	}
	if len(rows) > 0 {
		if err := tx.Create(&rows).Error; err != nil {
			return err
		}
	}
	persistenceCompleted := completedAt
	zero := int64(0)
	return tx.Create(&models.AIRunStep{
		ID: uuid.NewString(), GenerationID: generation.ID, Sequence: len(rows) + 2,
		Kind: "persistence", Status: "succeeded", StartedAt: completedAt,
		CompletedAt: &persistenceCompleted, DurationMS: &zero, CreatedAt: completedAt,
	}).Error
}

func aiRunDurationMilliseconds(startedAt, completedAt string) int64 {
	started, startErr := time.Parse(time.RFC3339Nano, startedAt)
	completed, completedErr := time.Parse(time.RFC3339Nano, completedAt)
	if startErr != nil || completedErr != nil || completed.Before(started) {
		return 0
	}
	return completed.Sub(started).Milliseconds()
}

func (a *API) listAIRunSteps(c *gin.Context) {
	generationID := strings.TrimSpace(c.Param("id"))
	parsed, err := uuid.Parse(generationID)
	if err != nil || parsed.String() != generationID {
		writeError(c, http.StatusBadRequest, "INVALID_AI_GENERATION_ID", "AI generation id must be a canonical UUID")
		return
	}
	var generation models.AIGeneration
	var steps []models.AIRunStep
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&generation, "id = ?", generationID).Error; err != nil {
			return err
		}
		return tx.Where("generation_id = ?", generationID).Order("sequence ASC").Find(&steps).Error
	}, &sql.TxOptions{ReadOnly: true})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(c, http.StatusNotFound, "AI_GENERATION_NOT_FOUND", "AI generation not found")
		return
	}
	if err != nil {
		writeDatabaseError(c)
		return
	}
	inputBytes, outputBytes, durationMS := 0, 0, int64(0)
	var inputTokens, outputTokens *int
	var tokenSource *string
	status := generation.Status
	if len(steps) > 0 {
		inputBytes, outputBytes = steps[0].InputBytes, steps[0].OutputBytes
		if steps[0].DurationMS != nil {
			durationMS = *steps[0].DurationMS
		}
		inputTokens, outputTokens, tokenSource = steps[0].InputTokens, steps[0].OutputTokens, steps[0].TokenSource
	}
	c.JSON(http.StatusOK, gin.H{"data": steps, "meta": gin.H{
		"generation_id": generationID, "status": status, "total": len(steps),
		"input_bytes": inputBytes, "output_bytes": outputBytes, "duration_ms": durationMS,
		"input_tokens": inputTokens, "output_tokens": outputTokens, "token_source": tokenSource,
	}})
}
