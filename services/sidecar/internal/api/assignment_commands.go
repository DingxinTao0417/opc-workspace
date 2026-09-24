package api

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// Shared by human API and AI approval inside the caller-owned transaction.
func createAssignmentInTransaction(tx *gorm.DB, taskIDValue string, expectedVersion int64, role, actorIDValue, requestID, now string) (assignmentMutationResponse, error) {
	var response assignmentMutationResponse
	task, err := loadAssignmentTask(tx, taskIDValue, expectedVersion, true)
	if err != nil {
		return response, err
	}
	if err := requireAssignmentActor(tx, actorIDValue, role); err != nil {
		return response, err
	}
	var activeCount int64
	if err := tx.Model(&models.TaskAssignment{}).
		Where("task_id = ? AND role = ? AND unassigned_at IS NULL", taskIDValue, role).
		Count(&activeCount).Error; err != nil {
		return response, err
	}
	if activeCount > 0 {
		return response, assignmentAlreadyActiveError()
	}

	if err := bumpTaskForAssignment(tx, taskIDValue, expectedVersion, now); err != nil {
		return response, err
	}
	assignment := models.TaskAssignment{
		ID: uuid.NewString(), TaskID: taskIDValue, ActorID: actorIDValue, Role: role,
		AssignedByActorID: models.BuiltinOwnerActorID, AssignedAt: now, Reason: "",
	}
	if err := tx.Create(&assignment).Error; err != nil {
		return response, mapAssignmentConstraintError(err)
	}
	created, err := loadAssignmentResponse(tx, assignment.ID)
	if err != nil {
		return response, err
	}
	loadedTask, err := loadTask(tx, task.ID)
	if err != nil {
		return response, err
	}
	response = assignmentMutationResponse{Assignment: created, Task: loadedTask}
	if err := recordAssignmentWorkflowEvent(
		tx, "assignment_created", taskIDValue, assignment.ID, nil, created,
		requestID, now,
	); err != nil {
		return response, err
	}
	loadedTask, err = reconcileTaskParentProgress(tx, taskIDValue, requestID, now)
	if err != nil {
		return response, taskParentProgressError("reconcile assigned parent Task", err)
	}
	response.Task = loadedTask
	return response, nil
}

func reassignInTransaction(tx *gorm.DB, taskIDValue string, expectedVersion int64, role, actorIDValue, reason, requestID, now string) (reassignMutationResponse, error) {
	var response reassignMutationResponse
	task, err := loadAssignmentTask(tx, taskIDValue, expectedVersion, true)
	if err != nil {
		return response, err
	}
	if err := requireAssignmentActor(tx, actorIDValue, role); err != nil {
		return response, err
	}
	var current models.TaskAssignment
	if err := tx.Where("task_id = ? AND role = ? AND unassigned_at IS NULL", taskIDValue, role).
		Take(&current).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response, newProjectRequestError(http.StatusConflict, "ASSIGNMENT_NOT_ACTIVE", "There is no active assignment for this role")
		}
		return response, err
	}
	if current.ActorID == actorIDValue {
		return response, newProjectRequestError(http.StatusConflict, "ASSIGNMENT_UNCHANGED", "The selected actor already has this assignment")
	}
	previous, err := loadAssignmentResponse(tx, current.ID)
	if err != nil {
		return response, err
	}

	if err := bumpTaskForAssignment(tx, taskIDValue, expectedVersion, now); err != nil {
		return response, err
	}
	result := tx.Model(&models.TaskAssignment{}).
		Where("id = ? AND unassigned_at IS NULL", current.ID).
		Updates(map[string]any{"unassigned_at": now, "reason": reason})
	if result.Error != nil {
		return response, mapAssignmentConstraintError(result.Error)
	}
	if result.RowsAffected == 0 {
		return response, newProjectRequestError(http.StatusConflict, "ASSIGNMENT_NOT_ACTIVE", "The assignment is no longer active")
	}
	newAssignment := models.TaskAssignment{
		ID: uuid.NewString(), TaskID: taskIDValue, ActorID: actorIDValue, Role: role,
		AssignedByActorID: models.BuiltinOwnerActorID, AssignedAt: now, Reason: "",
	}
	if err := tx.Create(&newAssignment).Error; err != nil {
		return response, mapAssignmentConstraintError(err)
	}
	ended, err := loadAssignmentResponse(tx, current.ID)
	if err != nil {
		return response, err
	}
	created, err := loadAssignmentResponse(tx, newAssignment.ID)
	if err != nil {
		return response, err
	}
	loadedTask, err := loadTask(tx, task.ID)
	if err != nil {
		return response, err
	}
	response = reassignMutationResponse{PreviousAssignment: ended, Assignment: created, Task: loadedTask}
	if err := recordAssignmentWorkflowEvent(
		tx, "assignment_reassigned", taskIDValue, newAssignment.ID,
		previous, reassignEventCurrent{EndedAssignment: ended, Assignment: created},
		requestID, now,
	); err != nil {
		return response, err
	}
	loadedTask, err = reconcileTaskParentProgress(tx, taskIDValue, requestID, now)
	if err != nil {
		return response, taskParentProgressError("reconcile reassigned parent Task", err)
	}
	response.Task = loadedTask
	return response, nil
}

func endAssignmentInTransaction(tx *gorm.DB, assignmentIDValue string, expectedVersion int64, reason, requestID, now string) (assignmentMutationResponse, error) {
	var response assignmentMutationResponse
	var assignment models.TaskAssignment
	if err := tx.First(&assignment, "id = ?", assignmentIDValue).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response, newProjectRequestError(http.StatusNotFound, "ASSIGNMENT_NOT_FOUND", "Assignment not found")
		}
		return response, err
	}
	task, err := loadAssignmentTask(tx, assignment.TaskID, expectedVersion, false)
	if err != nil {
		return response, err
	}
	if assignment.UnassignedAt != nil {
		return response, newProjectRequestError(http.StatusConflict, "ASSIGNMENT_NOT_ACTIVE", "The assignment is no longer active")
	}
	previous, err := loadAssignmentResponse(tx, assignment.ID)
	if err != nil {
		return response, err
	}

	if err := bumpTaskForAssignment(tx, task.ID, expectedVersion, now); err != nil {
		return response, err
	}
	result := tx.Model(&models.TaskAssignment{}).
		Where("id = ? AND unassigned_at IS NULL", assignment.ID).
		Updates(map[string]any{"unassigned_at": now, "reason": reason})
	if result.Error != nil {
		return response, mapAssignmentConstraintError(result.Error)
	}
	if result.RowsAffected == 0 {
		return response, newProjectRequestError(http.StatusConflict, "ASSIGNMENT_NOT_ACTIVE", "The assignment is no longer active")
	}
	ended, err := loadAssignmentResponse(tx, assignment.ID)
	if err != nil {
		return response, err
	}
	loadedTask, err := loadTask(tx, task.ID)
	if err != nil {
		return response, err
	}
	response = assignmentMutationResponse{Assignment: ended, Task: loadedTask}
	if err := recordAssignmentWorkflowEvent(
		tx, "assignment_ended", task.ID, assignment.ID, previous, ended,
		requestID, now,
	); err != nil {
		return response, err
	}
	loadedTask, err = reconcileTaskParentProgress(tx, task.ID, requestID, now)
	if err != nil {
		return response, taskParentProgressError("reconcile unassigned parent Task", err)
	}
	response.Task = loadedTask
	return response, nil
}
