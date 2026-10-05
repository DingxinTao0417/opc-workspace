import type {
  AgentRunListParams,
  ControlledFileListParams,
  TaskListParams,
} from "../../types/models";
import type { AiSessionDelegatedRunListParams } from "../aiSessionAgentRuns";

export const settingsQueryKey = ["settings"] as const;

export const searchQueryKey = ["search"] as const;

export const backupQueryKey = ["backups"] as const;

export const scheduledBackupPolicyQueryKey = ["backups", "policy"] as const;

export const restoreDiagnosticsQueryKey = ["restore-diagnostics"] as const;

export const storageCapacityQueryKey = ["diagnostics", "storage"] as const;

export const storageCapacityHistoryQueryKey = [
  "diagnostics",
  "storage-history",
] as const;

export const actorQueryKey = ["actors"] as const;

export const actorDetailQueryKey = (id: string) =>
  [...actorQueryKey, "detail", id] as const;

export const clientQueryKey = ["clients"] as const;

export const clientDetailQueryKey = (id: string) =>
  [...clientQueryKey, "detail", id] as const;

export const financialEntryQueryKey = ["financial-entries"] as const;

export const financialEntryDetailQueryKey = (id: string) =>
  [...financialEntryQueryKey, "detail", id] as const;

export const incomeStatsQueryKey = ["income-stats"] as const;

export const invoiceQueryKey = ["invoices"] as const;

export const invoiceDetailQueryKey = (id: string) =>
  [...invoiceQueryKey, "detail", id] as const;

export const invoicePdfQueryKey = (id: string) =>
  [...invoiceDetailQueryKey(id), "pdf"] as const;

export const clientActivityQueryKey = (clientId: string) =>
  [...clientDetailQueryKey(clientId), "activities"] as const;

export const recentClientActivityQueryKey = [
  ...clientQueryKey,
  "activities",
  "recent",
] as const;

export const clientFollowupQueryKey = (clientId: string) =>
  [...clientDetailQueryKey(clientId), "followups"] as const;

export const clientAttachmentQueryKey = (clientId: string) =>
  [...clientDetailQueryKey(clientId), "attachments"] as const;

export const clientActorLinkQueryKey = (clientId: string) =>
  [...clientDetailQueryKey(clientId), "actor-links"] as const;

export const inboxQueryKey = ["inbox-items"] as const;

export const inboxStatsQueryKey = [...inboxQueryKey, "stats"] as const;

export const INBOX_LIST_REFRESH_INTERVAL_MS = 15_000;

export const inboxDetailQueryKey = (id: string) =>
  [...inboxQueryKey, "detail", id] as const;

export const inboxEventQueryKey = (id: string) =>
  [...inboxQueryKey, "events", id] as const;

export const inboxTaskRelationQueryKey = (id: string) =>
  [...inboxQueryKey, "tasks", id] as const;

export const reminderQueryKey = ["reminders"] as const;

export const reminderDetailQueryKey = (id: string) =>
  [...reminderQueryKey, "detail", id] as const;

export const automationQueryKey = ["automations"] as const;

export const automationRulesQueryKey = [
  ...automationQueryKey,
  "rules",
] as const;

export const automationRunsQueryKey = [...automationQueryKey, "runs"] as const;

export const automationRunDetailQueryKey = (id: string) =>
  [...automationRunsQueryKey, "detail", id] as const;

export const agentAdaptersQueryKey = ["agent-adapters"] as const;

export const taskQueryKey = ["tasks"] as const;

export const taskDetailQueryKey = (id: string) => ["tasks", id] as const;

export const taskPageQueryKey = (input: TaskListParams) =>
  [...taskQueryKey, "page", input] as const;

export const taskSavedViewQueryKey = ["task-saved-views"] as const;

export const taskAgentRunsQueryKey = (taskId: string) =>
  ["task-agent-runs", taskId] as const;

export const agentRunQueryRootKey = ["agent-runs"] as const;

export const agentRunDetailQueryKey = (runId: string, taskId: string) =>
  [...agentRunQueryRootKey, "detail", runId, taskId] as const;

export const agentRunListQueryKey = (input: AgentRunListParams = {}) =>
  [...agentRunQueryRootKey, "list", input] as const;

/** Keyed by the slot status too, so a change of state is read again. */
export const agentRunStartGateInputsQueryKey = (
  runId: string,
  slotStatus = "unknown",
) => [...agentRunQueryRootKey, "start-gate-inputs", runId, slotStatus] as const;

export const aiAgentFamilyQueryKey = (sessionId: string) =>
  ["ai", "sessions", sessionId, "agent-family"] as const;

export const aiSessionDelegatedRunsQueryKey = (
  sessionId: string,
  input: AiSessionDelegatedRunListParams = {},
) => ["ai", "sessions", sessionId, "delegated-runs", input] as const;

export const controlledFileListQueryKey = (
  input: ControlledFileListParams = {},
) => ["files", "list", input] as const;

export const taskAssignmentQueryRootKey = ["task-assignments"] as const;

export const taskAssignmentQueryKey = (taskId: string) =>
  [...taskAssignmentQueryRootKey, taskId] as const;

export const taskEventQueryRootKey = ["task-events"] as const;

export const taskEventQueryKey = (taskId: string) =>
  [...taskEventQueryRootKey, taskId] as const;

export const taskSubmissionQueryRootKey = ["task-submissions"] as const;

export const taskSubmissionQueryKey = (taskId: string) =>
  [...taskSubmissionQueryRootKey, taskId] as const;

export const taskArtifactQueryRootKey = ["task-artifacts"] as const;

export const taskArtifactQueryKey = (taskId: string) =>
  [...taskArtifactQueryRootKey, taskId] as const;

export const taskArtifactDetailQueryRootKey = ["task-artifact"] as const;

export const taskArtifactDetailQueryKey = (artifactId: string) =>
  [...taskArtifactDetailQueryRootKey, artifactId] as const;

export const focusSessionQueryKey = ["focus-sessions", "active"] as const;

export const focusSessionHistoryQueryKey = [
  "focus-sessions",
  "history",
] as const;

export const focusReportQueryKey = ["stats", "focus"] as const;

export const tagQueryKey = ["tags"] as const;

export const projectQueryKey = ["projects"] as const;

export const roadmapMilestoneQueryKey = ["roadmap", "milestones"] as const;

export const roadmapMilestoneDetailQueryKey = (id: string) =>
  [...roadmapMilestoneQueryKey, "detail", id] as const;

export const contentItemQueryKey = ["content-items"] as const;

export const contentItemListQueryKey = [
  ...contentItemQueryKey,
  "infinite-list",
] as const;

export const contentItemDetailQueryKey = (id: string) =>
  [...contentItemQueryKey, "detail", id] as const;

export const projectDetailQueryKey = (id: string) =>
  [...projectQueryKey, "detail", id] as const;

export const projectEventQueryKey = (id: string) =>
  [...projectDetailQueryKey(id), "events"] as const;

export const projectArtifactQueryKey = (id: string) =>
  [...projectDetailQueryKey(id), "artifacts"] as const;

export const projectNoteQueryKey = (id: string) =>
  [...projectDetailQueryKey(id), "notes"] as const;

export const projectAttachmentQueryKey = (id: string) =>
  [...projectDetailQueryKey(id), "attachments"] as const;

export const aiProvidersQueryKey = ["ai", "providers"] as const;

export const aiMemoriesQueryKey = ["ai", "memories"] as const;

export const aiMemoryProposalsQueryKey = ["ai", "memory-proposals"] as const;

export const aiSessionsQueryKey = ["ai", "sessions"] as const;

export const aiAgentInboxQueryKey = ["ai", "agent-inbox"] as const;

export const aiWorkPlanInboxQueryKey = [
  "ai",
  "work-plans",
  "attention",
] as const;

export const aiMessagesQueryKey = (sessionId: string) =>
  ["ai", "messages", sessionId] as const;

export const aiUsageSummaryQueryKey = (sessionId: string, trendDays = 7) =>
  ["ai", "usage-summary", sessionId, trendDays] as const;

export const aiUsageSummarySessionQueryKey = (sessionId: string) =>
  ["ai", "usage-summary", sessionId] as const;

export const aiEvaluationsQueryKey = ["ai", "evaluations"] as const;

export const aiEvaluationQueryKey = (id: string) =>
  ["ai", "evaluations", id] as const;

export const aiEvaluationSummaryQueryKey = [
  "ai",
  "evaluation-summary",
] as const;

export const aiEvaluationReviewsQueryKey = [
  "ai",
  "evaluation-reviews",
] as const;
