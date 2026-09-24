import type { AiWorkspaceRecordNavigationType } from "../types/models";
import { taskArtifactHref, taskSubmissionHref } from "./taskSubmissionLocation";
import { clientRecordHref } from "./clientRecordLocation";
import { projectNoteHref } from "./projectNoteLocation";

const recordNavigationTypes = new Set<AiWorkspaceRecordNavigationType>([
  "task",
  "task_saved_view",
  "project",
  "client",
  "client_activity",
  "client_followup",
  "project_note",
  "financial_entry",
  "invoice",
  "inbox_item",
  "reminder",
  "roadmap_milestone",
  "content_item",
  "agent_run",
  "task_submission",
  "task_artifact",
]);

const canonicalUuid =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

export function isAiWorkspaceRecordNavigationTarget(
  recordType: string,
  recordId: string,
): recordType is AiWorkspaceRecordNavigationType {
  return (
    recordNavigationTypes.has(recordType as AiWorkspaceRecordNavigationType) &&
    canonicalUuid.test(recordId)
  );
}

export function isAiWorkspaceRecordNavigationTaskId(
  recordType: AiWorkspaceRecordNavigationType,
  taskId: unknown,
): taskId is string {
  return (
    (recordType === "agent_run" ||
      recordType === "task_submission" ||
      recordType === "task_artifact") &&
    typeof taskId === "string" &&
    canonicalUuid.test(taskId)
  );
}

export function isAiWorkspaceRecordNavigationSubmissionId(
  recordType: AiWorkspaceRecordNavigationType,
  submissionId: unknown,
): submissionId is string {
  return (
    recordType === "task_artifact" &&
    typeof submissionId === "string" &&
    canonicalUuid.test(submissionId)
  );
}

export function isAiWorkspaceRecordNavigationParentId(
  recordType: AiWorkspaceRecordNavigationType,
  parentId: unknown,
): parentId is string {
  return (
    (recordType === "project_note" ||
      recordType === "client_activity" ||
      recordType === "client_followup") &&
    typeof parentId === "string" &&
    canonicalUuid.test(parentId)
  );
}

export function aiWorkspaceRecordNavigationRoute(
  recordType: AiWorkspaceRecordNavigationType,
  recordId: string,
  taskId?: string,
  parentId?: string,
  submissionId?: string,
): string {
  switch (recordType) {
    case "task":
      return `/tasks/${recordId}`;
    case "task_saved_view":
      return `/tasks?task_view=${recordId}`;
    case "project":
      return `/projects/${recordId}`;
    case "client":
      return `/clients/${recordId}`;
    case "project_note":
      return isAiWorkspaceRecordNavigationParentId(recordType, parentId)
        ? projectNoteHref(parentId, recordId)
        : "";
    case "client_activity":
      return isAiWorkspaceRecordNavigationParentId(recordType, parentId)
        ? clientRecordHref(parentId, "activity", recordId)
        : "";
    case "client_followup":
      return isAiWorkspaceRecordNavigationParentId(recordType, parentId)
        ? clientRecordHref(parentId, "followup", recordId)
        : "";
    case "financial_entry":
      return `/income/${recordId}`;
    case "invoice":
      return `/invoices/${recordId}`;
    case "inbox_item":
      return `/inbox/${recordId}`;
    case "reminder":
      return `/inbox?reminder=${recordId}`;
    case "roadmap_milestone":
      return `/roadmap?milestone=${recordId}`;
    case "content_item":
      return `/content-calendar?item=${recordId}`;
    case "agent_run":
      return isAiWorkspaceRecordNavigationTaskId(recordType, taskId)
        ? `/tasks/${taskId}?agent_run=${recordId}`
        : "";
    case "task_submission":
      return isAiWorkspaceRecordNavigationTaskId(recordType, taskId)
        ? taskSubmissionHref(taskId, recordId)
        : "";
    case "task_artifact":
      return isAiWorkspaceRecordNavigationTaskId(recordType, taskId) &&
        isAiWorkspaceRecordNavigationSubmissionId(recordType, submissionId)
        ? taskArtifactHref(taskId, submissionId, recordId)
        : "";
  }
}

export const aiWorkspaceRecordNavigationLabels: Record<
  AiWorkspaceRecordNavigationType,
  string
> = {
  task: "任务",
  task_saved_view: "任务保存视图",
  project: "项目",
  client: "客户",
  client_activity: "客户活动",
  client_followup: "客户回访",
  project_note: "项目笔记",
  financial_entry: "收支记录",
  invoice: "发票",
  inbox_item: "收件箱事项",
  reminder: "提醒",
  roadmap_milestone: "路线图里程碑",
  content_item: "内容条目",
  agent_run: "Agent 执行",
  task_submission: "提交批次",
  task_artifact: "任务产出",
};
