import { apiRequest, ApiError, normalizeTaskSubmission } from "./client";
import { isWorkspaceIdentity } from "../lib/focusReportLocation";
import type { Task, TaskSubmission } from "../types/models";

export interface TaskSubmissionLocation {
  task: Pick<
    Task,
    "id" | "title" | "status" | "version" | "currentSubmissionId"
  >;
  submission: TaskSubmission;
}

const object = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === "object" && !Array.isArray(v);

export async function getTaskSubmissionLocation(
  taskId: string,
  submissionId: string,
  signal?: AbortSignal,
): Promise<TaskSubmissionLocation> {
  if (!isWorkspaceIdentity(taskId) || !isWorkspaceIdentity(submissionId)) {
    throw new ApiError("提交批次链接无效。", { code: "INVALID_SUBMISSION_ID" });
  }
  const value = await apiRequest<unknown>(
    `/api/v1/tasks/${taskId}/submissions/${submissionId}`,
    { signal },
  );
  if (!object(value) || !object(value.data) || !object(value.data.task)) {
    throw new ApiError("提交批次响应格式无效。", { code: "INVALID_RESPONSE" });
  }
  const task = value.data.task;
  const submission = normalizeTaskSubmission(value.data.submission);
  if (
    task.id !== taskId ||
    submission.id !== submissionId ||
    submission.taskId !== taskId ||
    typeof task.title !== "string" ||
    !task.title.trim() ||
    ![
      "todo",
      "in_progress",
      "blocked",
      "waiting_review",
      "done",
      "cancelled",
    ].includes(String(task.status)) ||
    typeof task.version !== "number" ||
    !Number.isSafeInteger(task.version) ||
    task.version < 1 ||
    (task.current_submission_id !== null &&
      !isWorkspaceIdentity(task.current_submission_id as string))
  ) {
    throw new ApiError("提交批次与任务身份不一致。", {
      code: "TASK_SUBMISSION_IDENTITY_MISMATCH",
    });
  }
  return {
    task: {
      id: taskId,
      title: task.title,
      status: task.status as Task["status"],
      version: task.version,
      currentSubmissionId: task.current_submission_id as string | null,
    },
    submission,
  };
}
