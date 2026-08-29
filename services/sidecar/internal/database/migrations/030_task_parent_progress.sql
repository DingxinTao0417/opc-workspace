ALTER TABLE task_submissions
ADD COLUMN origin TEXT NOT NULL DEFAULT 'manual'
CHECK (origin IN ('manual', 'child_rollup'));

CREATE TRIGGER trg_task_submissions_child_rollup_insert
BEFORE INSERT ON task_submissions
WHEN NEW.origin = 'child_rollup'
  AND (
      NEW.submitted_by_actor_id <> '00000000-0000-5000-8000-000000000002'
      OR NEW.is_inferred <> 0
  )
BEGIN
    SELECT RAISE(ABORT, 'TASK_CHILD_ROLLUP_SUBMISSION_INVALID');
END;

DROP TRIGGER trg_task_submissions_protect_history_update;

CREATE TRIGGER trg_task_submissions_protect_history_update
BEFORE UPDATE ON task_submissions
WHEN NEW.id IS NOT OLD.id
    OR NEW.task_id IS NOT OLD.task_id
    OR NEW.sequence IS NOT OLD.sequence
    OR NEW.summary IS NOT OLD.summary
    OR NEW.submitted_by_actor_id IS NOT OLD.submitted_by_actor_id
    OR NEW.submitted_at IS NOT OLD.submitted_at
    OR NEW.is_inferred IS NOT OLD.is_inferred
    OR NEW.origin IS NOT OLD.origin
    OR OLD.status <> 'pending_review'
    OR NEW.status NOT IN ('accepted', 'changes_requested', 'withdrawn')
BEGIN
    SELECT RAISE(ABORT, 'TASK_SUBMISSION_HISTORY_IMMUTABLE');
END;

CREATE TRIGGER trg_task_artifacts_reject_child_rollup_insert
BEFORE INSERT ON task_artifacts
WHEN EXISTS (
    SELECT 1
    FROM task_submissions
    WHERE id = NEW.submission_id
      AND origin = 'child_rollup'
)
BEGIN
    SELECT RAISE(ABORT, 'TASK_CHILD_ROLLUP_ARTIFACT_FORBIDDEN');
END;
