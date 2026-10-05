// The AI workspace action contract is split under ./aiWorkspaceActions. This
// module keeps the original import path and re-exports exactly the same public
// names.

export { parseNativeAgentRunStartPreview } from "./aiWorkspaceActions/agentRunProposals";
export { aiActionLabels, aiActionFields } from "./aiWorkspaceActions/catalog";
export {
  decideAiTaskOutput,
  decideAiAgentDelegation,
  confirmAiAgentDelegationBatch,
  decideAiAgentFollowup,
  decideAiAgentRun,
  decideAiAgentRunCancel,
  decideAiAgentRunRecovery,
  decideAiTaskDelete,
  decideAiProjectDelete,
  decideAiClientDelete,
  decideAiContentItemDelete,
  decideAiContentPublished,
  decideAiRoadmapMilestoneDelete,
  decideAiTaskViewAction,
  decideAiKnowledgeAction,
  decideAiTagDelete,
  decideAiWorkspaceAction,
  needsAutomationEffectsConsent,
  decideAiFinanceExport,
  decideAiProjectNote,
} from "./aiWorkspaceActions/decisions";
export {
  parseAiActionProposal,
  getAiWorkspaceActions,
} from "./aiWorkspaceActions/parse";
export {
  type AiActionProposal,
  type AiAgentDelegationPreview,
  type AiAgentDelegationResult,
  type AiAgentFollowupPreview,
  type AiAgentFollowupResult,
  type AiTaskBatchCreateItemPreview,
  type AiTaskBatchCreatePreview,
  type AiTaskBatchCreateResult,
  type AiKnowledgeJobPreview,
  type AiKnowledgeProposalPreview,
  type AiTaskViewPreview,
  type AiTaskBatchItemPreview,
  type AiTaskBatchActorPreview,
  type AiTaskBatchPreview,
  type AiTaskOrderPreview,
  type AiRoadmapOrderPreview,
  type AiAgentRunCancelPreview,
  type AiAgentRunCancelManyPreview,
  type AiAgentRunCancelManyResult,
  type AiAgentRunStartPreview,
  type AiAgentRunStartGatePreview,
  type AiAgentRunInputFilePreview,
  type AiAgentRunOutputContract,
  type AiAgentRunResult,
  type AiTaskOption,
  type AiInboxSplitDraft,
} from "./aiWorkspaceActions/types";
