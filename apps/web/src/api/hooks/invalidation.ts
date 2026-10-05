import { agentRunNeedsPolling } from "../../lib/agentRunDelivery";
import type { AgentRun, InboxItem } from "../../types/models";
import {
  activeContinuationsKey,
  recentContinuationsKey,
} from "../aiPlanContinuation";
import type { AiActionProposal } from "../aiWorkspaceActions";
import { ApiError, resetRuntimeConnection } from "../client";
import type { Query, QueryClient } from "@tanstack/react-query";
import {
  actorQueryKey,
  agentRunQueryRootKey,
  aiAgentInboxQueryKey,
  aiWorkPlanInboxQueryKey,
  automationQueryKey,
  clientActivityQueryKey,
  clientDetailQueryKey,
  clientQueryKey,
  contentItemQueryKey,
  financialEntryQueryKey,
  focusReportQueryKey,
  focusSessionHistoryQueryKey,
  focusSessionQueryKey,
  inboxDetailQueryKey,
  inboxEventQueryKey,
  inboxQueryKey,
  inboxTaskRelationQueryKey,
  incomeStatsQueryKey,
  invoiceDetailQueryKey,
  invoiceQueryKey,
  projectDetailQueryKey,
  projectNoteQueryKey,
  projectQueryKey,
  reminderDetailQueryKey,
  reminderQueryKey,
  roadmapMilestoneQueryKey,
  searchQueryKey,
  taskAgentRunsQueryKey,
  taskArtifactDetailQueryRootKey,
  taskArtifactQueryRootKey,
  taskAssignmentQueryKey,
  taskAssignmentQueryRootKey,
  taskDetailQueryKey,
  taskEventQueryRootKey,
  taskQueryKey,
  taskSubmissionQueryRootKey,
} from "./queryKeys";

export function invalidateUnifiedSearch(
  queryClient: QueryClient,
): Promise<void> {
  return queryClient.invalidateQueries({ queryKey: searchQueryKey });
}

const backupIncidentErrorCodes = new Set([
  "BACKUP_CREATE_FAILED",
  "BACKUP_VERIFY_FAILED",
  "BACKUP_DRILL_FAILED",
  "BACKUP_NOT_RESTORABLE",
  "RESTORE_PENDING_INVALID",
  "RESTORE_SCHEDULE_FAILED",
  "RESTORE_ROLLBACK_BACKUP_FAILED",
]);

function backupErrorMayProjectInbox(error: unknown): boolean {
  return error instanceof ApiError && backupIncidentErrorCodes.has(error.code);
}

export async function invalidateBackupIncident(
  queryClient: QueryClient,
  error: unknown,
): Promise<void> {
  if (backupErrorMayProjectInbox(error)) {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: inboxQueryKey }),
      queryClient.invalidateQueries({ queryKey: aiAgentInboxQueryKey }),
    ]);
  }
}

export async function invalidateActorReferences(queryClient: QueryClient) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: clientQueryKey }),
    queryClient.invalidateQueries({ queryKey: projectQueryKey }),
    queryClient.invalidateQueries({ queryKey: inboxQueryKey }),
    queryClient.invalidateQueries({ queryKey: taskAssignmentQueryRootKey }),
    queryClient.invalidateQueries({ queryKey: taskEventQueryRootKey }),
    queryClient.invalidateQueries({ queryKey: taskSubmissionQueryRootKey }),
    queryClient.invalidateQueries({ queryKey: taskArtifactQueryRootKey }),
  ]);
}

export async function invalidateActorOptions(queryClient: QueryClient) {
  await Promise.all([
    queryClient.invalidateQueries({
      queryKey: [...actorQueryKey, "assignment-options"],
    }),
    queryClient.invalidateQueries({
      queryKey: [...actorQueryKey, "client-contact-options"],
    }),
    queryClient.invalidateQueries({
      queryKey: [...actorQueryKey, "client-followup-options"],
    }),
  ]);
}

export function invalidateClientQueries(
  queryClient: QueryClient,
  preserveActiveDetailId?: string,
): Array<Promise<unknown>> {
  return preserveActiveDetailId
    ? [
        queryClient.invalidateQueries({
          queryKey: clientQueryKey,
          predicate: (query) =>
            !(
              query.queryKey.length === 3 &&
              query.queryKey[1] === "detail" &&
              query.queryKey[2] === preserveActiveDetailId
            ),
        }),
        queryClient.invalidateQueries({
          queryKey: clientDetailQueryKey(preserveActiveDetailId),
          exact: true,
          refetchType: "none",
        }),
      ]
    : [queryClient.invalidateQueries({ queryKey: clientQueryKey })];
}

export async function invalidateClientEditFacts(
  queryClient: QueryClient,
  preserveActiveDetailId?: string,
): Promise<void> {
  await Promise.all([
    ...invalidateClientQueries(queryClient, preserveActiveDetailId),
    queryClient.invalidateQueries({ queryKey: projectQueryKey }),
    queryClient.invalidateQueries({ queryKey: invoiceQueryKey }),
    queryClient.invalidateQueries({ queryKey: financialEntryQueryKey }),
    invalidateUnifiedSearch(queryClient),
  ]);
}

export async function invalidateFinancialActionFacts(queryClient: QueryClient) {
  const keys = [financialEntryQueryKey, incomeStatsQueryKey];
  await Promise.all(
    keys.map((queryKey) => queryClient.cancelQueries({ queryKey })),
  );
  await invalidateFinancialReadModels(queryClient);
}

export async function invalidateFinancialReadModels(queryClient: QueryClient) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: financialEntryQueryKey }),
    queryClient.invalidateQueries({ queryKey: incomeStatsQueryKey }),
  ]);
}

export async function invalidateInvoiceActionFacts(
  queryClient: QueryClient,
  mayTriggerAutomation: boolean,
) {
  await Promise.all(
    [
      invoiceQueryKey,
      financialEntryQueryKey,
      incomeStatsQueryKey,
      projectQueryKey,
      inboxQueryKey,
      searchQueryKey,
    ].map((queryKey) => queryClient.cancelQueries({ queryKey })),
  );
  await invalidateInvoiceReadModels(queryClient);
  if (mayTriggerAutomation)
    await invalidateAutomationActionFacts(queryClient, true);
}

export async function invalidateInvoiceReadModels(
  queryClient: QueryClient,
  preserveActiveDetailId?: string,
) {
  const invoiceQueries = preserveActiveDetailId
    ? [
        queryClient.invalidateQueries({
          queryKey: invoiceQueryKey,
          predicate: (query) =>
            !(
              query.queryKey.length === 3 &&
              query.queryKey[1] === "detail" &&
              query.queryKey[2] === preserveActiveDetailId
            ),
        }),
        queryClient.invalidateQueries({
          queryKey: invoiceDetailQueryKey(preserveActiveDetailId),
          exact: true,
          refetchType: "none",
        }),
      ]
    : [queryClient.invalidateQueries({ queryKey: invoiceQueryKey })];
  await Promise.all([
    ...invoiceQueries,
    queryClient.invalidateQueries({ queryKey: financialEntryQueryKey }),
    queryClient.invalidateQueries({ queryKey: incomeStatsQueryKey }),
    queryClient.invalidateQueries({ queryKey: projectQueryKey }),
    queryClient.invalidateQueries({ queryKey: inboxQueryKey }),
    invalidateUnifiedSearch(queryClient),
  ]);
}

export async function invalidateClientActivityActionFacts(
  queryClient: QueryClient,
): Promise<void> {
  // Client detail, timeline, recent cards and navigation badges share this prefix.
  const keys = [clientQueryKey, searchQueryKey];
  await Promise.all(
    keys.map((queryKey) => queryClient.cancelQueries({ queryKey })),
  );
  await Promise.all(
    keys.map((queryKey) => queryClient.invalidateQueries({ queryKey })),
  );
}

export async function invalidateClientActionFacts(
  queryClient: QueryClient,
): Promise<void> {
  // A client name/status/contact edit changes client lists and can change the
  // labels projected into related project, invoice and ledger read models.
  const keys = [
    clientQueryKey,
    projectQueryKey,
    invoiceQueryKey,
    financialEntryQueryKey,
    searchQueryKey,
  ];
  await Promise.all(
    keys.map((queryKey) => queryClient.cancelQueries({ queryKey })),
  );
  await Promise.all(
    keys.map((queryKey) => queryClient.invalidateQueries({ queryKey })),
  );
}

export async function invalidateClientContactActionFacts(
  queryClient: QueryClient,
): Promise<void> {
  // The relationship is projected in Client detail and constrains whether the
  // selected person can be deactivated. Search can also show Client facts.
  const keys = [clientQueryKey, actorQueryKey, searchQueryKey];
  await Promise.all(
    keys.map((queryKey) => queryClient.cancelQueries({ queryKey })),
  );
  await Promise.all(
    keys.map((queryKey) => queryClient.invalidateQueries({ queryKey })),
  );
}

export async function invalidatePersonActionFacts(
  queryClient: QueryClient,
): Promise<void> {
  // Person names/status are shared by responsibility, Client-contact and
  // follow-up projections. Cancel stale reads before refreshing every owner.
  const keys = [
    actorQueryKey,
    taskQueryKey,
    clientQueryKey,
    inboxQueryKey,
    searchQueryKey,
  ];
  await Promise.all(
    keys.map((queryKey) => queryClient.cancelQueries({ queryKey })),
  );
  await Promise.all(
    keys.map((queryKey) => queryClient.invalidateQueries({ queryKey })),
  );
}

export async function invalidateClientFollowupActionFacts(
  queryClient: QueryClient,
): Promise<void> {
  // Approval can finish after navigation or with a lost response. Cancel old
  // snapshots first, including inactive queries, before refreshing shared facts.
  const keys = [clientQueryKey, inboxQueryKey, actorQueryKey, searchQueryKey];
  await Promise.all(
    keys.map((queryKey) => queryClient.cancelQueries({ queryKey })),
  );
  await Promise.all(
    keys.map((queryKey) => queryClient.invalidateQueries({ queryKey })),
  );
}

export async function invalidateClientActivityConflict(
  queryClient: QueryClient,
  clientId: string,
): Promise<void> {
  const activityKey = clientActivityQueryKey(clientId);
  await Promise.all([
    queryClient.invalidateQueries({
      queryKey: clientQueryKey,
      predicate: (query) =>
        !activityKey.every((part, index) => query.queryKey[index] === part),
    }),
    queryClient.invalidateQueries({
      queryKey: activityKey,
      refetchType: "none",
    }),
  ]);
}

export async function invalidateReminderActionFacts(
  queryClient: QueryClient,
): Promise<void> {
  // A scheduled occurrence may have fired while its approval card was open.
  await Promise.all([
    invalidateReminderFacts(queryClient),
    queryClient.invalidateQueries({ queryKey: inboxQueryKey }),
    queryClient.invalidateQueries({ queryKey: searchQueryKey }),
  ]);
}

export async function invalidateReminderFacts(
  queryClient: QueryClient,
  id?: string,
): Promise<void> {
  if (!id) {
    await queryClient.invalidateQueries({ queryKey: reminderQueryKey });
    return;
  }
  await Promise.all([
    queryClient.invalidateQueries({
      queryKey: reminderQueryKey,
      predicate: (query) =>
        !(query.queryKey[1] === "detail" && query.queryKey[2] === id),
    }),
    queryClient.invalidateQueries({
      queryKey: reminderDetailQueryKey(id),
      exact: true,
    }),
  ]);
}

export async function invalidateAutomationActionFacts(
  queryClient: QueryClient,
  retryAttempt = false,
): Promise<void> {
  const keys: readonly (readonly string[])[] = [
    automationQueryKey,
    inboxQueryKey,
    reminderQueryKey,
    ...(retryAttempt
      ? [
          taskQueryKey,
          projectQueryKey,
          roadmapMilestoneQueryKey,
          contentItemQueryKey,
          searchQueryKey,
          ["stats", "today"],
        ]
      : []),
  ];
  await Promise.all(
    keys.map((queryKey) => queryClient.cancelQueries({ queryKey })),
  );
  await invalidateAutomationFacts(queryClient);
  if (retryAttempt) await invalidateTaskFacts(queryClient);
}

export async function invalidateAutomationFacts(
  queryClient: QueryClient,
): Promise<void> {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: automationQueryKey }),
    queryClient.invalidateQueries({ queryKey: inboxQueryKey }),
    queryClient.invalidateQueries({ queryKey: reminderQueryKey }),
  ]);
}

async function refreshProjectFacts(queryClient: QueryClient): Promise<void> {
  await queryClient.cancelQueries({ queryKey: projectQueryKey });
  await queryClient.invalidateQueries({ queryKey: projectQueryKey });
}

// AI proposals do not fetch raw source payloads into the approval card, so a
// cached Inbox detail may be absent. Refresh all Project follow-up read models.
export function invalidateInboxActionFacts(queryClient: QueryClient) {
  return invalidateInboxFacts(queryClient, undefined, undefined, {
    projectsMayChange: true,
  });
}

// Snapshot read-all changes only Inbox read state. Keep this narrower than the
// general AI Inbox invalidation, whose other commands may also affect Projects.
export async function invalidateInboxReadAllFacts(queryClient: QueryClient) {
  await queryClient.cancelQueries({ queryKey: inboxQueryKey });
  await invalidateInboxFacts(queryClient);
}

export async function invalidateInboxFacts(
  queryClient: QueryClient,
  id?: string,
  item?: InboxItem,
  options: {
    projectsMayChange?: boolean;
    preserveActiveDetailId?: string;
    preserveActiveTaskRelationsItemId?: string;
  } = {},
): Promise<void> {
  const sourceItem =
    item ??
    (id
      ? queryClient.getQueryData<InboxItem>(inboxDetailQueryKey(id))
      : undefined);
  const projectFactsMayChange =
    options.projectsMayChange ||
    (sourceItem &&
      (sourceItem.sourceEntityType === "task_artifact" ||
        sourceItem.sourceEntityType === "task" ||
        sourceItem.sourceEntityType === "task_due" ||
        sourceItem.sourceEntityType === "project_completion"));
  const excludeFromInboxRootInvalidation = (query: Query): boolean =>
    (Boolean(id) &&
      query.queryKey[1] === "events" &&
      query.queryKey[2] === id) ||
    (Boolean(options.preserveActiveDetailId) &&
      query.queryKey[1] === "detail" &&
      query.queryKey[2] === options.preserveActiveDetailId) ||
    (Boolean(options.preserveActiveTaskRelationsItemId) &&
      query.queryKey[1] === "tasks" &&
      query.queryKey[2] === options.preserveActiveTaskRelationsItemId);
  const invalidations: Array<Promise<unknown>> = [
    id ||
    options.preserveActiveDetailId ||
    options.preserveActiveTaskRelationsItemId
      ? queryClient.invalidateQueries({
          queryKey: inboxQueryKey,
          predicate: (query) => !excludeFromInboxRootInvalidation(query),
        })
      : queryClient.invalidateQueries({ queryKey: inboxQueryKey }),
    invalidateUnifiedSearch(queryClient),
    queryClient.invalidateQueries({ queryKey: aiAgentInboxQueryKey }),
  ];
  if (options.preserveActiveDetailId) {
    invalidations.push(
      queryClient.invalidateQueries({
        queryKey: inboxDetailQueryKey(options.preserveActiveDetailId),
        exact: true,
        refetchType: "none",
      }),
    );
  }
  if (options.preserveActiveTaskRelationsItemId) {
    invalidations.push(
      queryClient.invalidateQueries({
        queryKey: inboxTaskRelationQueryKey(
          options.preserveActiveTaskRelationsItemId,
        ),
        refetchType: "none",
      }),
    );
  }
  if (id) {
    invalidations.push(
      queryClient.invalidateQueries({ queryKey: inboxEventQueryKey(id) }),
    );
  }
  if (projectFactsMayChange) {
    invalidations.push(refreshProjectFacts(queryClient));
  }
  await Promise.all(invalidations);
}

type AgentRunTerminalState = Pick<
  AgentRun,
  | "id"
  | "taskId"
  | "status"
  | "outputDeliveryStatus"
  | "outputDeliveryErrorCode"
  | "submissionId"
  | "artifactId"
>;

export function agentRunTerminalSignature(
  run: AgentRunTerminalState,
): string | null {
  if (agentRunNeedsPolling(run)) return null;
  return [
    run.id,
    run.taskId,
    run.status,
    run.outputDeliveryStatus,
    run.outputDeliveryErrorCode ?? "",
    run.submissionId ?? "",
    run.artifactId ?? "",
  ].join(":");
}

const refreshedAgentRunTerminalStates = new WeakMap<QueryClient, Set<string>>();

export async function invalidateAgentRunTerminalFactsOnce(
  queryClient: QueryClient,
  runs: readonly AgentRunTerminalState[],
) {
  let refreshed = refreshedAgentRunTerminalStates.get(queryClient);
  if (!refreshed) {
    refreshed = new Set<string>();
    refreshedAgentRunTerminalStates.set(queryClient, refreshed);
  }
  const taskIds = new Set<string>();
  for (const run of runs) {
    const signature = agentRunTerminalSignature(run);
    if (!signature || refreshed.has(signature)) continue;
    refreshed.add(signature);
    taskIds.add(run.taskId);
  }
  if (taskIds.size > 0) {
    await invalidateAgentRunDeliveryFactsForTasks(queryClient, [...taskIds]);
  }
}

export async function invalidateTaskAssignments(
  queryClient: QueryClient,
  preserveConflictTaskId?: string,
): Promise<void> {
  await invalidateTaskAggregates(
    queryClient,
    preserveConflictTaskId
      ? {
          preserveActiveAssignmentTaskId: preserveConflictTaskId,
          preserveActiveDetailId: preserveConflictTaskId,
        }
      : undefined,
  );
}

export async function invalidateFocusReadModels(
  queryClient: QueryClient,
  scopes: { history?: boolean; report?: boolean },
): Promise<void> {
  const invalidations: Array<Promise<unknown>> = [];
  if (scopes.report) {
    invalidations.push(
      queryClient.invalidateQueries({ queryKey: focusReportQueryKey }),
    );
  }
  if (scopes.history) {
    invalidations.push(
      queryClient.invalidateQueries({ queryKey: focusSessionHistoryQueryKey }),
    );
  }
  await Promise.all(invalidations);
}

export async function invalidateFocusDependents(queryClient: QueryClient) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: focusSessionQueryKey }),
    queryClient.invalidateQueries({ queryKey: taskQueryKey }),
    queryClient.invalidateQueries({ queryKey: projectQueryKey }),
    queryClient.invalidateQueries({ queryKey: ["stats", "today"] }),
    queryClient.invalidateQueries({ queryKey: focusReportQueryKey }),
    queryClient.invalidateQueries({ queryKey: focusSessionHistoryQueryKey }),
  ]);
}

// An approval may have succeeded even if its response was lost. Cancel old
// snapshots before refetching authoritative facts; never replay local cycle
// transitions from a historical decision card.
export async function invalidateFocusActionFacts(queryClient: QueryClient) {
  await Promise.all([
    queryClient.cancelQueries({ queryKey: ["focus-sessions"] }),
    queryClient.cancelQueries({ queryKey: focusReportQueryKey }),
  ]);
  await invalidateFocusDependents(queryClient);
}

// Submission/review can change ancestor tasks and Inbox follow-up projections.
// Cancel even inactive queries first: an old response must not restore the
// pre-decision status after confirmation or an ambiguous network failure.
export async function invalidateTaskOutputActionFacts(
  queryClient: QueryClient,
) {
  const keys = [
    taskQueryKey,
    taskSubmissionQueryRootKey,
    taskArtifactQueryRootKey,
    taskArtifactDetailQueryRootKey,
    taskAssignmentQueryRootKey,
    taskEventQueryRootKey,
    projectQueryKey,
    inboxQueryKey,
    searchQueryKey,
    roadmapMilestoneQueryKey,
    contentItemQueryKey,
    ["stats", "today"],
  ];
  await Promise.all(
    keys.map((queryKey) => queryClient.cancelQueries({ queryKey })),
  );
  await Promise.all([
    invalidateTaskAggregates(queryClient),
    queryClient.invalidateQueries({ queryKey: taskArtifactDetailQueryRootKey }),
  ]);
}

function aiActionQueriesForAgentRunTasks(taskIds: string[]) {
  const changedTasks = new Set(taskIds);
  return {
    queryKey: ["ai", "actions"] as const,
    predicate: (query: Query) => {
      const rows = query.state.data as AiActionProposal[] | undefined;
      // Run metadata has no owning generation ID. Reuse the task association
      // of already validated cached proposals to refresh every relevant group,
      // not just a currently visible card. An initial/empty/incomplete read
      // cannot exclude that association, so conservatively refresh it too.
      if (!Array.isArray(rows) || rows.length === 0) return true;
      return rows.some((proposal) => {
        if (
          !proposal ||
          proposal.generation_id !== query.queryKey[2] ||
          !proposal.action ||
          typeof proposal.action.action !== "string" ||
          (proposal.status === "pending" && !proposal.can_confirm)
        )
          return true;
        return (
          proposal.action.action.startsWith("agent_run.") &&
          (!proposal.action.task_id ||
            changedTasks.has(proposal.action.task_id))
        );
      });
    },
  };
}

async function invalidateAgentRunDeliveryFactsForTasks(
  queryClient: QueryClient,
  taskIds: string[],
) {
  const runListKeys = [...new Set(taskIds)].map(taskAgentRunsQueryKey);
  const actionQueries = aiActionQueriesForAgentRunTasks(taskIds);
  // A background Run transition changes the Task-side facts as well as the
  // derived plan and continuation surfaces. Refreshing those projections here
  // avoids showing stale queued/running/pending state until their next poll.
  // This is cache invalidation only: it never creates a plan, Proposal, scope
  // grant, Run action or recovery attempt.
  const aiDerivedKeys = [
    aiAgentInboxQueryKey,
    aiWorkPlanInboxQueryKey,
    ["ai", "work-plan"] as const,
  ];
  await Promise.all([
    // A late pre-terminal receipt must not replace the fresh group used by
    // post-approval continuation. This never fabricates a proposal outcome.
    queryClient.cancelQueries(actionQueries),
    ...runListKeys.map((queryKey) => queryClient.cancelQueries({ queryKey })),
    ...aiDerivedKeys.map((queryKey) => queryClient.cancelQueries({ queryKey })),
    queryClient.cancelQueries({
      queryKey: agentRunQueryRootKey,
      predicate: (query) => query.queryKey[1] !== "detail",
    }),
  ]);
  await Promise.all([
    queryClient.invalidateQueries(actionQueries),
    invalidateTaskOutputActionFacts(queryClient),
    ...runListKeys.map((queryKey) =>
      queryClient.invalidateQueries({ queryKey }),
    ),
    ...aiDerivedKeys.map((queryKey) =>
      queryClient.invalidateQueries({ queryKey }),
    ),
    queryClient.invalidateQueries({
      queryKey: agentRunQueryRootKey,
      predicate: (query) => query.queryKey[1] !== "detail",
    }),
  ]);
}

export async function invalidateAgentRunDeliveryFacts(
  queryClient: QueryClient,
  taskId: string,
) {
  await invalidateAgentRunDeliveryFactsForTasks(queryClient, [taskId]);
}

export async function invalidateTaskAggregates(
  queryClient: QueryClient,
  options: {
    preserveActiveAssignmentTaskId?: string;
    preserveActiveDetailId?: string;
  } = {},
): Promise<void> {
  const assignmentQueries = options.preserveActiveAssignmentTaskId
    ? [
        queryClient.invalidateQueries({
          queryKey: taskAssignmentQueryRootKey,
          predicate: (query) =>
            !(
              query.queryKey.length >= 2 &&
              query.queryKey[1] === options.preserveActiveAssignmentTaskId
            ),
        }),
        queryClient.invalidateQueries({
          queryKey: taskAssignmentQueryKey(
            options.preserveActiveAssignmentTaskId,
          ),
          refetchType: "none",
        }),
      ]
    : [
        queryClient.invalidateQueries({
          queryKey: taskAssignmentQueryRootKey,
        }),
      ];
  await Promise.all([
    invalidateTaskFacts(queryClient, options),
    queryClient.invalidateQueries({ queryKey: taskSubmissionQueryRootKey }),
    queryClient.invalidateQueries({ queryKey: taskArtifactQueryRootKey }),
    queryClient.invalidateQueries({ queryKey: aiAgentInboxQueryKey }),
    ...assignmentQueries,
    queryClient.invalidateQueries({ queryKey: taskEventQueryRootKey }),
  ]);
}

export async function invalidateAiPlanReviewProjections(
  queryClient: QueryClient,
) {
  const queryKeys = [
    ["ai", "work-plan"] as const,
    aiWorkPlanInboxQueryKey,
    ["ai", "plan-continuation"] as const,
    activeContinuationsKey,
    recentContinuationsKey,
  ];
  await Promise.all(
    queryKeys.map((queryKey) => queryClient.cancelQueries({ queryKey })),
  );
  await Promise.all(
    queryKeys.map((queryKey) => queryClient.invalidateQueries({ queryKey })),
  );
}

export async function invalidateTaskFacts(
  queryClient: QueryClient,
  options: { preserveActiveDetailId?: string } = {},
): Promise<void> {
  const taskQueries = options.preserveActiveDetailId
    ? [
        queryClient.invalidateQueries({
          queryKey: taskQueryKey,
          predicate: (query) =>
            !(
              query.queryKey.length === 2 &&
              query.queryKey[1] === options.preserveActiveDetailId
            ),
        }),
        queryClient.invalidateQueries({
          queryKey: taskDetailQueryKey(options.preserveActiveDetailId),
          exact: true,
          refetchType: "none",
        }),
      ]
    : [queryClient.invalidateQueries({ queryKey: taskQueryKey })];
  await Promise.all([
    ...taskQueries,
    invalidateUnifiedSearch(queryClient),
    refreshProjectFacts(queryClient),
    queryClient.invalidateQueries({ queryKey: roadmapMilestoneQueryKey }),
    queryClient.invalidateQueries({ queryKey: contentItemQueryKey }),
    queryClient.invalidateQueries({ queryKey: ["stats", "today"] }),
    queryClient.invalidateQueries({ queryKey: inboxQueryKey }),
  ]);
}

export async function invalidateRoadmapReadModels(
  queryClient: QueryClient,
  inboxMayChange = false,
): Promise<void> {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: roadmapMilestoneQueryKey }),
    invalidateUnifiedSearch(queryClient),
    ...(inboxMayChange
      ? [queryClient.invalidateQueries({ queryKey: inboxQueryKey })]
      : []),
  ]);
}

export async function invalidateRoadmapActionFacts(
  queryClient: QueryClient,
): Promise<void> {
  await invalidateRoadmapReadModels(queryClient, true);
}

export async function invalidateContentItemActionFacts(
  queryClient: QueryClient,
): Promise<void> {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: contentItemQueryKey }),
    queryClient.invalidateQueries({ queryKey: inboxQueryKey }),
    invalidateUnifiedSearch(queryClient),
  ]);
}

export async function invalidateProjectNoteConflict(
  queryClient: QueryClient,
  projectId: string,
): Promise<void> {
  const noteKey = projectNoteQueryKey(projectId);
  await Promise.all([
    queryClient.invalidateQueries({
      queryKey: projectQueryKey,
      predicate: (query) =>
        !noteKey.every((part, index) => query.queryKey[index] === part),
    }),
    queryClient.invalidateQueries({
      queryKey: noteKey,
      refetchType: "active",
    }),
  ]);
}

export function invalidateProjectQueries(
  queryClient: QueryClient,
  preserveActiveDetailId?: string,
): Array<Promise<unknown>> {
  return preserveActiveDetailId
    ? [
        queryClient.invalidateQueries({
          queryKey: projectQueryKey,
          predicate: (query) =>
            !(
              query.queryKey.length === 3 &&
              query.queryKey[1] === "detail" &&
              query.queryKey[2] === preserveActiveDetailId
            ),
        }),
        queryClient.invalidateQueries({
          queryKey: projectDetailQueryKey(preserveActiveDetailId),
          exact: true,
          refetchType: "none",
        }),
      ]
    : [queryClient.invalidateQueries({ queryKey: projectQueryKey })];
}

export async function invalidateProjectEditFacts(
  queryClient: QueryClient,
  preserveActiveDetailId?: string,
): Promise<void> {
  await Promise.all([
    ...invalidateProjectQueries(queryClient, preserveActiveDetailId),
    queryClient.invalidateQueries({ queryKey: taskQueryKey }),
    queryClient.invalidateQueries({ queryKey: clientQueryKey }),
    queryClient.invalidateQueries({ queryKey: invoiceQueryKey }),
    queryClient.invalidateQueries({ queryKey: financialEntryQueryKey }),
    queryClient.invalidateQueries({ queryKey: roadmapMilestoneQueryKey }),
    invalidateFocusReadModels(queryClient, { report: true }),
    invalidateUnifiedSearch(queryClient),
  ]);
}

export async function invalidateProjectNoteActionFacts(
  queryClient: QueryClient,
) {
  // Cancel even consumers that do not forward AbortSignal before invalidation.
  await queryClient.cancelQueries({ queryKey: projectQueryKey });
  await queryClient.invalidateQueries({ queryKey: projectQueryKey });
}

export async function invalidateProjectActionFacts(queryClient: QueryClient) {
  await Promise.all([
    invalidateProjectEditFacts(queryClient),
    invalidateProjectCompletionFacts(queryClient),
    invalidateTaskAggregates(queryClient),
  ]);
}

export async function invalidateProjectCompletionFacts(
  queryClient: QueryClient,
) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: inboxQueryKey }),
    queryClient.invalidateQueries({ queryKey: automationQueryKey }),
  ]);
}

export function resetApiAndRefetch(refetch: () => Promise<unknown>): void {
  resetRuntimeConnection();
  void refetch();
}
