package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/agentexec"
	"github.com/opc-workspace/opc-sidecar/internal/agentrunner"
	"github.com/opc-workspace/opc-sidecar/internal/api/filestore"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

var agentRunFinalizationRetryDelays = [...]time.Duration{
	50 * time.Millisecond,
	200 * time.Millisecond,
	1 * time.Second,
	5 * time.Second,
}

func (a *API) finalizeAgentRunObserved(run models.AgentRun, resultText, errorCode, completedAt string) {
	a.finalizeAgentRunOutcomeObserved(run, agentexec.ResultTypeText, resultText, errorCode, completedAt)
}

func (a *API) finalizeAgentRunOutcomeObserved(
	run models.AgentRun,
	resultType string,
	resultText string,
	errorCode string,
	completedAt string,
) {
	a.finalizeAgentRunOutcomeObservedContext(
		a.agentRunLifecycle(), run, resultType, resultText, errorCode, completedAt,
	)
}

func (a *API) finalizeAgentRunObservedContext(ctx context.Context, run models.AgentRun, resultText, errorCode, completedAt string) {
	a.finalizeAgentRunOutcomeObservedContext(
		ctx, run, agentexec.ResultTypeText, resultText, errorCode, completedAt,
	)
}

func (a *API) finalizeAgentRunOutcomeObservedContext(
	ctx context.Context,
	run models.AgentRun,
	resultType string,
	resultText string,
	errorCode string,
	completedAt string,
) {
	loggedPersistenceFailure := false
	for attempt := 0; ; attempt++ {
		// Execution never holds the maintenance read lock while waiting on the
		// model. Re-enter it only for the complete observable finalization
		// attempt, including the pending fallback, then release it during
		// backoff so backup/import/restore writers can make progress.
		err := a.withAgentRunStorageLease(func() error {
			return a.finalizeAgentRunOutcome(run, resultType, resultText, errorCode, completedAt)
		})
		if err == nil || errors.Is(err, errAgentRunOutputDeliveryPending) {
			a.wakeAIContinuationsAfterFactCommit()
			return
		}
		if errors.Is(err, errAgentRunRestorePending) {
			return
		}
		if !loggedPersistenceFailure && a.options.Logger != nil {
			// The result and storage error may contain user or database content.
			// Keep fallback observation limited to a stable message and Run ID.
			a.options.Logger.Printf("Agent Run finalization could not be persisted safely run_id=%s", run.ID)
			loggedPersistenceFailure = true
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return
		}
		delayIndex := attempt
		if delayIndex >= len(agentRunFinalizationRetryDelays) {
			delayIndex = len(agentRunFinalizationRetryDelays) - 1
		}
		timer := time.NewTimer(agentRunFinalizationRetryDelays[delayIndex])
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-timer.C:
		}
	}
}

func (a *API) finalizeAgentRun(run models.AgentRun, resultText, errorCode, completedAt string) error {
	return a.finalizeAgentRunOutcome(run, agentexec.ResultTypeText, resultText, errorCode, completedAt)
}

func (a *API) finalizeAgentRunOutcome(
	run models.AgentRun,
	resultType string,
	resultText string,
	errorCode string,
	completedAt string,
) error {
	if errorCode == "" && resultText != "" {
		contract, err := agentRunOutputContract(run)
		if err != nil || resultType != contract.Type {
			errorCode = agentrunner.CodeProtocolInvalid
			resultText = ""
		}
	}
	if errorCode == "" && resultText != "" {
		err := a.finalizeSuccessfulAgentRun(run.ID, resultText, completedAt)
		if err == nil {
			return nil
		}
		if pendingErr := a.persistPendingAgentRunOutput(run.ID, resultText, completedAt); pendingErr != nil {
			return fmt.Errorf("finalize Agent Run output: %v; persist recovery state: %w", err, pendingErr)
		}
		return errAgentRunOutputDeliveryPending
	}
	updates := map[string]any{"status": "failed", "error_code": errorCode, "completed_at": completedAt}
	event := "agent_run_failed"
	if errorCode == agentrunner.CodeCancelled {
		updates = map[string]any{
			"status": "cancelled", "error_code": nil, "completed_at": completedAt,
		}
		event = "agent_run_cancelled"
	}
	err := a.db.Transaction(func(tx *gorm.DB) error {
		if settled, err := settleRequestedAgentRunCancellation(tx, run.ID, completedAt); err != nil || settled {
			return err
		}
		result := tx.Model(&models.AgentRun{}).
			Where("id = ? AND status = 'running' AND output_delivery_status = ?", run.ID, agentRunOutputNotReady).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return nil
		}
		return recordAgentRunWorkflowEvent(tx, event, run.ID, map[string]any{"error_code": errorCode}, "", completedAt)
	})
	if err == nil && event == "agent_run_failed" {
		a.consumeAutomationEventDeliveriesBestEffort("agent-run-failed")
	}
	return err
}

// finalizeSuccessfulAgentRun gives a successful model result one observable
// disposition. The normal submission chain and the Run terminal transition
// commit together; deterministic business drift retains the bounded result on
// the Run. Unexpected storage failures roll this transaction back so the
// caller can persist a running+pending recovery record without ever exposing
// succeeded output that has no disposition.
func (a *API) finalizeSuccessfulAgentRun(runID, resultText, completedAt string) error {
	deliveryFiles := agentRunOutputFileDelivery{api: a, store: a.artifactStore}
	err := withAgentRunOutputTransaction(a.db, func(tx *gorm.DB) error {
		if settled, err := settleRequestedAgentRunCancellation(tx, runID, completedAt); err != nil || settled {
			return err
		}
		var run models.AgentRun
		if err := tx.First(&run, "id = ?", runID).Error; err != nil {
			return err
		}
		if run.Status == "succeeded" &&
			(run.OutputDeliveryStatus == agentRunOutputSubmitted || run.OutputDeliveryStatus == agentRunOutputRetained) {
			return nil
		}
		if run.Status != "running" {
			return nil
		}
		if run.OutputDeliveryStatus == agentRunOutputPending {
			if run.OutputDeliveryPendingText == nil || run.OutputDeliveryPendingCompletedAt == nil {
				return errors.New("Agent Run pending output is incomplete")
			}
			resultText = *run.OutputDeliveryPendingText
			completedAt = *run.OutputDeliveryPendingCompletedAt
		} else if run.OutputDeliveryStatus != agentRunOutputNotReady {
			return errors.New("Agent Run output delivery state is invalid")
		}
		return a.completeAgentRunOutputInTransaction(tx, run, resultText, completedAt,
			a.options.Now().UTC().Format(time.RFC3339Nano), &deliveryFiles)
	})
	return deliveryFiles.finish(err)
}

// withAgentRunOutputTransaction keeps file-store compensation aligned with
// the actual outer SQLite commit. database/sql marks sql.Tx done after a
// failed Commit, while SQLite can still require ROLLBACK (notably for a
// deferred constraint); GORM's normal Transaction helper can therefore return
// the sole pooled connection in an active transaction. Pinning one connection
// and issuing BEGIN/COMMIT/ROLLBACK on that same connection guarantees that a
// commit failure is released before pending-output recovery starts.
func withAgentRunOutputTransaction(db *gorm.DB, operation func(*gorm.DB) error) error {
	return db.Connection(func(tx *gorm.DB) (transactionErr error) {
		clean := func() *gorm.DB {
			return tx.Session(&gorm.Session{NewDB: true, SkipDefaultTransaction: true})
		}
		if transactionErr = clean().Exec("BEGIN").Error; transactionErr != nil {
			return fmt.Errorf("begin Agent Run output transaction: %w", transactionErr)
		}
		committed := false
		rollback := func() error {
			// HTTP cancellation must not prevent releasing a begun SQLite
			// transaction before the pinned connection returns to the pool.
			return clean().WithContext(context.WithoutCancel(tx.Statement.Context)).Exec("ROLLBACK").Error
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				_ = rollback()
				panic(recovered)
			}
			if committed {
				return
			}
			if rollbackErr := rollback(); rollbackErr != nil && transactionErr == nil {
				transactionErr = rollbackErr
			}
		}()
		if transactionErr = operation(clean()); transactionErr != nil {
			return fmt.Errorf("execute Agent Run output transaction: %w", transactionErr)
		}
		if transactionErr = clean().Exec("COMMIT").Error; transactionErr != nil {
			return fmt.Errorf("commit Agent Run output transaction: %w", transactionErr)
		}
		committed = true
		return nil
	})
}

func withAgentRunOutputSavepoint(tx *gorm.DB, operation func(*gorm.DB) error) error {
	const savepoint = "agent_run_output_delivery"
	clean := func() *gorm.DB {
		return tx.Session(&gorm.Session{NewDB: true, SkipDefaultTransaction: true})
	}
	if err := clean().Exec("SAVEPOINT " + savepoint).Error; err != nil {
		return fmt.Errorf("create Agent Run output savepoint: %w", err)
	}
	operationErr := operation(clean())
	if operationErr != nil {
		if rollbackErr := clean().Exec("ROLLBACK TO SAVEPOINT " + savepoint).Error; rollbackErr != nil {
			return fmt.Errorf("Agent Run output delivery failed: %v; rollback savepoint: %w", operationErr, rollbackErr)
		}
		if releaseErr := clean().Exec("RELEASE SAVEPOINT " + savepoint).Error; releaseErr != nil {
			return fmt.Errorf("Agent Run output delivery failed: %v; release savepoint: %w", operationErr, releaseErr)
		}
		return operationErr
	}
	if err := clean().Exec("RELEASE SAVEPOINT " + savepoint).Error; err != nil {
		return fmt.Errorf("release Agent Run output savepoint: %w", err)
	}
	return nil
}

type agentRunOutputFileDelivery struct {
	api                   *API
	store                 *filestore.ArtifactStore
	staged                *filestore.StagedArtifactFile
	taskID                string
	artifactID            string
	committedRelativePath string
	stagedFiles           []filestore.StagedArtifactFile
	committedFiles        []agentRunCommittedOutputFile
}

type agentRunCommittedOutputFile struct {
	taskID       string
	artifactID   string
	relativePath string
}

func (delivery *agentRunOutputFileDelivery) finish(transactionErr error) error {
	if delivery.staged != nil && delivery.store != nil {
		delivery.store.DiscardStagedFile(*delivery.staged)
	}
	if delivery.store != nil {
		for _, staged := range delivery.stagedFiles {
			delivery.store.DiscardStagedFile(staged)
		}
	}
	if transactionErr == nil || delivery.store == nil {
		return transactionErr
	}
	committed := append([]agentRunCommittedOutputFile(nil), delivery.committedFiles...)
	if delivery.committedRelativePath != "" {
		committed = append(committed, agentRunCommittedOutputFile{
			taskID: delivery.taskID, artifactID: delivery.artifactID, relativePath: delivery.committedRelativePath,
		})
	}
	for _, file := range committed {
		if delivery.api == nil || file.taskID == "" || file.artifactID == "" || file.relativePath == "" {
			// Without database evidence an ambiguous COMMIT must preserve the
			// object for the startup reconciler rather than risk a durable Artifact
			// row pointing at a missing file.
			continue
		}
		var count int64
		result := delivery.api.db.Model(&models.TaskArtifact{}).
			Where(
				"id = ? AND task_id = ? AND storage_kind = 'file' AND relative_path = ? AND deleted_at IS NULL",
				file.artifactID, file.taskID, file.relativePath,
			).
			Count(&count)
		if result.Error != nil {
			if delivery.api.options.Logger != nil {
				delivery.api.options.Logger.Printf(
					"Agent Run Artifact compensation deferred task_id=%s artifact_id=%s",
					file.taskID, file.artifactID,
				)
			}
			continue
		}
		if count != 0 {
			continue
		}
		if cleanupErr := delivery.store.DiscardCommittedFile(file.relativePath); cleanupErr != nil {
			return fmt.Errorf("%v; compensate Agent Run file output: %w", transactionErr, cleanupErr)
		}
	}
	delivery.committedRelativePath = ""
	delivery.committedFiles = nil
	return transactionErr
}

func (delivery *agentRunOutputFileDelivery) discardCommittedFiles() error {
	if delivery == nil || delivery.store == nil {
		return nil
	}
	paths := make([]string, 0, len(delivery.committedFiles)+1)
	if delivery.committedRelativePath != "" {
		paths = append(paths, delivery.committedRelativePath)
	}
	for _, file := range delivery.committedFiles {
		if file.relativePath != "" {
			paths = append(paths, file.relativePath)
		}
	}
	for _, path := range paths {
		if err := delivery.store.DiscardCommittedFile(path); err != nil {
			return err
		}
	}
	delivery.committedRelativePath = ""
	delivery.committedFiles = nil
	return nil
}

func (a *API) completeAgentRunOutputInTransaction(
	tx *gorm.DB,
	run models.AgentRun,
	resultText string,
	completedAt string,
	deliveryAt string,
	deliveryFiles *agentRunOutputFileDelivery,
) error {
	var output submitOutputResponse
	var primaryArtifactID string
	var artifactIDs []string
	if deliveryFiles == nil {
		return errors.New("Agent Run output file delivery state is missing")
	}
	deliveryErr := withAgentRunOutputSavepoint(tx, func(deliveryTx *gorm.DB) error {
		frozenTask, err := validateAgentRunOutputIdentity(deliveryTx, run)
		if err != nil {
			return fmt.Errorf("validate Agent Run output identity: %w", err)
		}
		contract, err := agentRunOutputContract(run)
		if err != nil {
			return fmt.Errorf("load Agent Run output contract: %w", agentRunIdentityChangedError())
		}
		bodies, bodyErr := agentRunOutputBodies(contract, resultText)
		if bodyErr != nil {
			return fmt.Errorf("load Agent Run output bodies: %w", agentRunIdentityChangedError())
		}
		inputArtifacts := make([]submitArtifactInput, 0, len(bodies))
		artifacts := make([]preparedArtifact, 0, len(bodies))
		isFileOutput := contract.Type == agentexec.ResultTypeFile || contract.Type == agentexec.ResultTypeFiles
		if isFileOutput && a.artifactStore == nil {
			return newProjectRequestError(http.StatusServiceUnavailable,
				agentRunFileStorageUnavailableCode, "Controlled Artifact storage is unavailable")
		}
		var commitFile func(preparedArtifact) error
		if isFileOutput {
			commitFile = func(prepared preparedArtifact) error {
				if prepared.StagedFile == nil {
					return errors.New("Agent Run file output staging is missing")
				}
				if err := a.artifactStore.CommitStagedFile(*prepared.StagedFile); err != nil {
					return err
				}
				tracked := agentRunCommittedOutputFile{
					taskID: run.TaskID, artifactID: prepared.ID, relativePath: prepared.StagedFile.RelativePath,
				}
				deliveryFiles.committedFiles = append(deliveryFiles.committedFiles, tracked)
				return nil
			}
		}
		for index, body := range bodies {
			artifactID := uuid.NewString()
			clientRef := fmt.Sprintf("agent-run-result-%d", index+1)
			if primaryArtifactID == "" {
				primaryArtifactID = artifactID
			}
			artifactIDs = append(artifactIDs, artifactID)
			if !isFileOutput {
				name := fmt.Sprintf("agent-run-attempt-%d%s", run.Attempt, deriveArtifactExtension(frozenTask, body))
				inputArtifacts = append(inputArtifacts, submitArtifactInput{
					ClientRef: clientRef, StorageKind: "text", Name: name, ContentText: &body,
				})
				artifacts = append(artifacts, preparedArtifact{
					ClientRef: clientRef, ID: artifactID, Position: index + 1,
					StorageKind: "text", Name: name, ContentText: &body,
				})
				continue
			}
			outputFile := agentRunOutputFileAt(contract, index)
			stagedFile, stageErr := a.artifactStore.StageMultipartFileWithLimit(
				bytes.NewReader([]byte(body)), artifactID, int64(agentexec.MaxResultBytes),
			)
			if stageErr != nil {
				return stageErr
			}
			deliveryFiles.stagedFiles = append(deliveryFiles.stagedFiles, stagedFile)
			mimeType := outputFile.MIME
			sizeBytes := stagedFile.SizeBytes
			sha := stagedFile.SHA256
			relativePath := stagedFile.RelativePath
			inputArtifacts = append(inputArtifacts, submitArtifactInput{
				ClientRef: clientRef, StorageKind: "file", Name: outputFile.Name,
			})
			artifacts = append(artifacts, preparedArtifact{
				ClientRef: clientRef, ID: artifactID, Position: index + 1,
				StorageKind: "file", Name: outputFile.Name, RelativePath: &relativePath,
				MimeType: &mimeType, SizeBytes: &sizeBytes, SHA256: &sha,
				StagedFile: &stagedFile,
			})
		}
		input := submitOutputRequest{
			Summary:   fmt.Sprintf("Agent Run 交付（attempt %d）", run.Attempt),
			Artifacts: inputArtifacts,
		}
		output, err = submitTaskOutputInTransactionAs(
			deliveryTx, run.TaskID, run.TaskVersion, input, artifacts, commitFile,
			"agent-run-output:"+run.ID, deliveryAt,
			taskOutputAttribution{
				SubmittedByActorID: run.ActorID, RecordedByActorID: models.BuiltinSystemActorID,
				EventActorID: models.BuiltinSystemActorID, ExpectedProducerActorID: run.ActorID,
				AgentRunID: &run.ID,
			},
		)
		if err != nil {
			return fmt.Errorf("submit Agent Run output: %w", err)
		}
		return nil
	})
	if deliveryErr != nil {
		if cleanupErr := deliveryFiles.discardCommittedFiles(); cleanupErr != nil {
			return fmt.Errorf("compensate Agent Run file output: %w", cleanupErr)
		}
		var requestErr *projectRequestError
		if !errors.As(deliveryErr, &requestErr) {
			return deliveryErr
		}
		if err := transitionAgentRunOutput(tx, run.ID, resultText, completedAt, agentRunOutputRetained,
			requestErr.code, nil, nil); err != nil {
			return err
		}
		if err := recordAgentRunWorkflowEvent(tx, "agent_run_succeeded", run.ID,
			map[string]any{"output_delivery_status": agentRunOutputRetained}, "", completedAt); err != nil {
			return err
		}
		return recordAgentRunWorkflowEvent(tx, "agent_run_output_retained", run.ID,
			map[string]any{"reason": requestErr.code}, "", deliveryAt)
	}
	if len(output.Artifacts) != len(artifactIDs) || primaryArtifactID == "" {
		return errors.New("Agent Run output command returned an inconsistent Artifact")
	}
	for index, artifact := range output.Artifacts {
		if artifact.ID != artifactIDs[index] {
			return errors.New("Agent Run output command returned reordered Artifacts")
		}
	}
	if err := transitionAgentRunOutput(tx, run.ID, resultText, completedAt, agentRunOutputSubmitted,
		"", &output.Submission.ID, &primaryArtifactID); err != nil {
		return err
	}
	if err := recordAgentRunWorkflowEvent(tx, "agent_run_succeeded", run.ID,
		map[string]any{"output_delivery_status": agentRunOutputSubmitted}, "", completedAt); err != nil {
		return err
	}
	return recordAgentRunWorkflowEvent(tx, "agent_run_output_submitted", run.ID, map[string]any{
		"submission_id": output.Submission.ID, "artifact_id": primaryArtifactID,
		"artifact_ids": artifactIDs,
		"task_version": output.Task.Version,
	}, "", deliveryAt)
}

func agentRunOutputBodies(contract agentexec.OutputContract, resultText string) ([]string, error) {
	if contract.Type == agentexec.ResultTypeFiles {
		return agentexec.DecodeFilesPayload(resultText, contract, agentexec.MaxResultBytes)
	}
	if _, err := agentexec.ResultPayload(
		agentexec.Result{Type: contract.Type, Text: resultText}, contract, agentexec.MaxResultBytes,
	); err != nil {
		return nil, err
	}
	return []string{resultText}, nil
}

func agentRunOutputFileAt(contract agentexec.OutputContract, index int) agentexec.OutputFileContract {
	if contract.Type == agentexec.ResultTypeFile {
		return agentexec.OutputFileContract{Name: contract.Name, MIME: contract.MIME}
	}
	return contract.Files[index]
}

func validateAgentRunOutputIdentity(tx *gorm.DB, run models.AgentRun) (models.Task, error) {
	var task models.Task
	if err := tx.First(&task, "id = ?", run.TaskID).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return models.Task{}, err
		}
		return models.Task{}, agentRunIdentityChangedError()
	}
	if task.Version != run.TaskVersion {
		return models.Task{}, agentRunIdentityChangedError()
	}
	var assignment models.TaskAssignment
	if err := tx.First(&assignment, "id = ?", run.AssignmentID).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return models.Task{}, err
		}
		return models.Task{}, agentRunIdentityChangedError()
	}
	if assignment.TaskID != run.TaskID || assignment.ActorID != run.ActorID ||
		assignment.Role != "assignee" || assignment.UnassignedAt != nil ||
		assignment.AssignedAt != run.AssignmentAssignedAt {
		return models.Task{}, agentRunIdentityChangedError()
	}
	var actor models.Actor
	if err := tx.First(&actor, "id = ?", run.ActorID).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return models.Task{}, err
		}
		return models.Task{}, agentRunIdentityChangedError()
	}
	if actor.Type != "agent" || actor.Status != "active" || actor.Version != run.ActorVersion ||
		actor.AgentAdapterID == nil || *actor.AgentAdapterID != run.AdapterID {
		return models.Task{}, agentRunIdentityChangedError()
	}
	return task, nil
}

func transitionAgentRunOutput(tx *gorm.DB, runID, resultText, completedAt, status, reason string, submissionID, artifactID *string) error {
	updates := map[string]any{
		"status": "succeeded", "result_text": resultText, "result_bytes": len(resultText),
		"error_code": nil, "completed_at": completedAt,
		"output_delivery_status": status, "output_delivery_error_code": nil,
		"submission_id": submissionID, "artifact_id": artifactID,
		"output_delivery_pending_text": nil, "output_delivery_pending_bytes": nil,
		"output_delivery_pending_completed_at": nil,
	}
	if reason != "" {
		updates["output_delivery_error_code"] = reason
	}
	result := tx.Model(&models.AgentRun{}).
		Where("id = ? AND status = 'running' AND output_delivery_status IN ?", runID,
			[]string{agentRunOutputNotReady, agentRunOutputPending}).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("Agent Run output transition lost its compare-and-swap")
	}
	return nil
}

func (a *API) persistPendingAgentRunOutput(runID, resultText, completedAt string) error {
	return a.db.Transaction(func(tx *gorm.DB) error {
		if settled, err := settleRequestedAgentRunCancellation(tx, runID, completedAt); err != nil || settled {
			return err
		}
		var run models.AgentRun
		if err := tx.First(&run, "id = ?", runID).Error; err != nil {
			return err
		}
		if run.Status == "succeeded" &&
			(run.OutputDeliveryStatus == agentRunOutputSubmitted || run.OutputDeliveryStatus == agentRunOutputRetained) {
			return nil
		}
		if run.Status != "running" {
			return errors.New("Agent Run is no longer active")
		}
		if run.OutputDeliveryStatus == agentRunOutputPending {
			if run.OutputDeliveryPendingText != nil && *run.OutputDeliveryPendingText == resultText &&
				run.OutputDeliveryPendingCompletedAt != nil && *run.OutputDeliveryPendingCompletedAt == completedAt {
				return nil
			}
			return errors.New("Agent Run already contains different pending output")
		}
		result := tx.Model(&models.AgentRun{}).
			Where("id = ? AND status = 'running' AND output_delivery_status = ?", runID, agentRunOutputNotReady).
			Updates(map[string]any{
				"output_delivery_status":               agentRunOutputPending,
				"output_delivery_error_code":           agentRunOutputPendingCode,
				"output_delivery_pending_text":         resultText,
				"output_delivery_pending_bytes":        len(resultText),
				"output_delivery_pending_completed_at": completedAt,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("Agent Run pending output transition lost its compare-and-swap")
		}
		return recordAgentRunWorkflowEvent(tx, "agent_run_output_delivery_pending", runID,
			map[string]any{"reason": agentRunOutputPendingCode}, "", completedAt)
	})
}

// deriveArtifactExtension resolves the deliverable extension from what the
// task asked for. Requirement keywords win; the produced content is only the
// fallback so a task asking for "html" always yields an .html artifact.
func deriveArtifactExtension(task models.Task, resultText string) string {
	requirements := strings.ToLower(task.CompletionCriteria + "\n" +
		task.Description + "\n" + task.Title)
	for _, keyword := range []struct {
		keyword   string
		extension string
	}{
		{"html", ".html"}, {"json", ".json"}, {"csv", ".csv"},
		{"xml", ".xml"}, {"yaml", ".yaml"}, {"yml", ".yml"},
		{"sql", ".sql"}, {"python", ".py"}, {"javascript", ".js"},
		{" markdown", ".md"}, {"markdown 文档", ".md"},
	} {
		if strings.Contains(requirements, keyword.keyword) {
			return keyword.extension
		}
	}
	lowerResult := strings.ToLower(resultText)
	if strings.Contains(lowerResult, "<!doctype html") || strings.Contains(lowerResult, "<html") {
		return ".html"
	}
	if strings.HasPrefix(strings.TrimSpace(resultText), "{") {
		return ".json"
	}
	return ".md"
}
