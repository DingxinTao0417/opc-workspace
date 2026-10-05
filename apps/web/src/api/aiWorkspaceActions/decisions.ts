import { validFinanceExport } from "../aiFinanceExport";
import { validTaskOutputProposal } from "../aiTaskOutputActions";
import { apiRequest } from "../client";
import {
  validAgentDelegationProposal,
  validAgentFollowupProposal,
} from "./agentDelegationProposals";
import {
  validAgentRunCancelManyProposal,
  validAgentRunCancelProposal,
  validAgentRunStartProposal,
} from "./agentRunProposals";
import { validClientProposal } from "./clientProposals";
import { validContentItemProposal } from "./contentProposals";
import { validKnowledgeProposal } from "./knowledgeProposals";
import { parseAiActionProposal } from "./parse";
import { exactKeys, invalid, object, uuid } from "./primitives";
import {
  validProjectDeleteProposal,
  validRoadmapProposal,
} from "./projectProposals";
import {
  validTagProposal,
  validTaskDeleteProposal,
  validTaskViewProposal,
} from "./taskProposals";
import type { AiActionProposal } from "./types";

export async function decideAiTaskOutput(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  consent: boolean,
) {
  if (!validTaskOutputProposal(proposal)) return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm" ? { confirm_task_output: consent } : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== proposal.action.action ||
    result.action.task_id !== proposal.action.task_id
  )
    return invalid();
  return result;
}

export async function decideAiAgentDelegation(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  consent: boolean,
) {
  if (
    proposal.action.action !== "agent_delegate.spawn" ||
    !validAgentDelegationProposal(
      proposal as unknown as Record<string, unknown>,
    )
  )
    return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm"
          ? { confirm_agent_delegation: consent }
          : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== proposal.action.action ||
    JSON.stringify(result.action.changes) !==
      JSON.stringify(proposal.action.changes)
  )
    return invalid();
  return result;
}

export async function confirmAiAgentDelegationBatch(
  generationId: string,
  proposals: AiActionProposal[],
  consent: boolean,
): Promise<AiActionProposal[]> {
  if (
    !uuid.test(generationId) ||
    !Array.isArray(proposals) ||
    proposals.length < 2 ||
    proposals.length > 4 ||
    consent !== true
  )
    return invalid();
  const ids = new Set<string>();
  const fingerprints = new Set<string>();
  let parentSessionId = "";
  for (const proposal of proposals) {
    if (
      !proposal ||
      proposal.generation_id !== generationId ||
      proposal.action.action !== "agent_delegate.spawn" ||
      proposal.status !== "pending" ||
      !proposal.can_confirm ||
      !validAgentDelegationProposal(
        proposal as unknown as Record<string, unknown>,
      ) ||
      ids.has(proposal.id) ||
      fingerprints.has(proposal.fingerprint)
    )
      return invalid();
    const parent = proposal.preview.agent_delegation?.parent_session_id;
    if (!parent || (parentSessionId && parentSessionId !== parent))
      return invalid();
    parentSessionId = parent;
    ids.add(proposal.id);
    fingerprints.add(proposal.fingerprint);
  }
  const payload = await apiRequest<unknown>(
    `/api/v1/ai/generations/${encodeURIComponent(generationId)}/agent-delegations/confirm`,
    {
      method: "POST",
      body: JSON.stringify({
        confirm_agent_delegations: true,
        items: proposals.map(({ id, fingerprint }) => ({
          proposal_id: id,
          fingerprint,
        })),
      }),
    },
  );
  if (
    !object(payload) ||
    !exactKeys(payload, ["data"]) ||
    !Array.isArray(payload.data)
  )
    return invalid();
  if (payload.data.length !== proposals.length) return invalid();
  const results = payload.data.map(parseAiActionProposal);
  for (const [index, result] of results.entries()) {
    const selected = proposals[index];
    if (
      result.id !== selected.id ||
      result.generation_id !== generationId ||
      result.fingerprint !== selected.fingerprint ||
      result.action.action !== "agent_delegate.spawn" ||
      JSON.stringify(result.action.changes) !==
        JSON.stringify(selected.action.changes) ||
      result.status !== "confirmed" ||
      result.result_id !==
        selected.preview.agent_delegation?.child_session_id ||
      result.agent_delegation_result?.session_id !==
        selected.preview.agent_delegation?.child_session_id
    )
      return invalid();
  }
  return results;
}

export async function decideAiAgentFollowup(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  consent: boolean,
) {
  if (
    proposal.action.action !== "agent_delegate.followup" ||
    !validAgentFollowupProposal(proposal as unknown as Record<string, unknown>)
  )
    return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm"
          ? { confirm_agent_delegation: consent }
          : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== proposal.action.action ||
    JSON.stringify(result.action.changes) !==
      JSON.stringify(proposal.action.changes) ||
    JSON.stringify(result.preview.agent_followup) !==
      JSON.stringify(proposal.preview.agent_followup)
  )
    return invalid();
  return result;
}

export async function decideAiAgentRun(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  confirmAgentExecution: boolean,
  confirmAgentFiles?: boolean,
  confirmAgentRework?: boolean,
  confirmAgentRestart?: boolean,
  confirmAgentProjectFiles?: boolean,
  confirmAgentPredecessorInput?: boolean,
) {
  if (
    !["agent_run.start", "agent_run.retry"].includes(proposal.action.action) ||
    !validAgentRunStartProposal(proposal as unknown as Record<string, unknown>)
  )
    return invalid();
  const hasInputFiles =
    (proposal.preview.agent_run_start?.input_files?.length ?? 0) > 0;
  const hasProjectFiles = proposal.preview.agent_run_start?.input_files?.some(
    (file) => file.source_kind === "project_task_artifact",
  );
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        ...(decision === "confirm" && hasProjectFiles
          ? { confirm_agent_project_files: confirmAgentProjectFiles }
          : {}),
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm"
          ? { confirm_agent_execution: confirmAgentExecution }
          : {}),
        ...(decision === "confirm" && hasInputFiles
          ? { confirm_agent_files: confirmAgentFiles }
          : {}),
        ...(decision === "confirm" &&
        proposal.preview.agent_run_start?.rework_context
          ? { confirm_agent_rework: confirmAgentRework }
          : {}),
        ...(decision === "confirm" && proposal.preview.agent_run_start?.restart
          ? { confirm_agent_restart: confirmAgentRestart }
          : {}),
        ...(decision === "confirm" &&
        proposal.preview.agent_run_start?.start_gate?.input_slot
          ? { confirm_agent_predecessor_input: confirmAgentPredecessorInput }
          : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== proposal.action.action ||
    result.action.agent_run_id !== proposal.action.agent_run_id ||
    result.action.task_id !== proposal.action.task_id ||
    result.action.changes.restart_of_run_id !==
      proposal.action.changes.restart_of_run_id
  )
    return invalid();
  return result;
}

export async function decideAiAgentRunCancel(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  confirmed: boolean,
) {
  if (
    !["agent_run.cancel", "agent_run.cancel_many"].includes(
      proposal.action.action,
    ) ||
    !(proposal.action.action === "agent_run.cancel"
      ? validAgentRunCancelProposal(
          proposal as unknown as Record<string, unknown>,
        )
      : validAgentRunCancelManyProposal(
          proposal as unknown as Record<string, unknown>,
        ))
  )
    return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm" ? { confirm_agent_cancel: confirmed } : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== proposal.action.action ||
    JSON.stringify(result.action.changes) !==
      JSON.stringify(proposal.action.changes) ||
    (proposal.action.action === "agent_run.cancel" &&
      (result.action.agent_run_id !== proposal.action.agent_run_id ||
        result.action.task_id !== proposal.action.task_id))
  )
    return invalid();
  return result;
}

export async function decideAiAgentRunRecovery(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  consent: boolean,
) {
  if (
    proposal.action.action !== "agent_run.recover_output" ||
    !validAgentRunCancelProposal(proposal as unknown as Record<string, unknown>)
  )
    return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm"
          ? { confirm_agent_output_recovery: consent }
          : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== proposal.action.action ||
    result.action.task_id !== proposal.action.task_id ||
    result.action.agent_run_id !== proposal.action.agent_run_id
  )
    return invalid();
  return result;
}

export async function decideAiTaskDelete(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  consent: boolean,
) {
  if (
    proposal.action.action !== "task.delete" ||
    !validTaskDeleteProposal(proposal as unknown as Record<string, unknown>)
  )
    return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm" ? { confirm_task_delete: consent } : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== "task.delete" ||
    result.action.task_id !== proposal.action.task_id ||
    result.action.expected_version !== proposal.action.expected_version
  )
    return invalid();
  return result;
}

export async function decideAiProjectDelete(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  consent: boolean,
) {
  if (
    proposal.action.action !== "project.delete" ||
    !validProjectDeleteProposal(proposal as unknown as Record<string, unknown>)
  )
    return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm" ? { confirm_project_delete: consent } : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== "project.delete" ||
    result.action.project_id !== proposal.action.project_id ||
    result.action.expected_version !== proposal.action.expected_version
  )
    return invalid();
  return result;
}

export async function decideAiClientDelete(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  consent: boolean,
) {
  if (
    proposal.action.action !== "client.delete" ||
    !validClientProposal(proposal as unknown as Record<string, unknown>)
  )
    return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm" ? { confirm_client_delete: consent } : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== "client.delete" ||
    result.action.client_id !== proposal.action.client_id ||
    result.action.expected_version !== proposal.action.expected_version
  )
    return invalid();
  return result;
}

export async function decideAiContentItemDelete(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  consent: boolean,
) {
  if (
    proposal.action.action !== "content_item.delete" ||
    !validContentItemProposal(proposal as unknown as Record<string, unknown>)
  )
    return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm"
          ? { confirm_content_item_delete: consent }
          : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== "content_item.delete" ||
    result.action.content_item_id !== proposal.action.content_item_id ||
    result.action.expected_version !== proposal.action.expected_version
  )
    return invalid();
  return result;
}

export async function decideAiContentPublished(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  consent: boolean,
) {
  if (
    proposal.action.action !== "content_item.publish" ||
    !validContentItemProposal(proposal as unknown as Record<string, unknown>)
  )
    return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm"
          ? { confirm_content_published: consent }
          : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== "content_item.publish" ||
    result.action.content_item_id !== proposal.action.content_item_id ||
    result.action.expected_version !== proposal.action.expected_version
  )
    return invalid();
  return result;
}

export async function decideAiRoadmapMilestoneDelete(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  consent: boolean,
) {
  if (
    proposal.action.action !== "roadmap_milestone.delete" ||
    !validRoadmapProposal(proposal as unknown as Record<string, unknown>)
  )
    return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm"
          ? { confirm_roadmap_milestone_delete: consent }
          : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== "roadmap_milestone.delete" ||
    result.action.roadmap_milestone_id !==
      proposal.action.roadmap_milestone_id ||
    result.action.expected_version !== proposal.action.expected_version
  )
    return invalid();
  return result;
}

export async function decideAiTaskViewAction(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  consent: boolean,
) {
  const action = proposal.action.action;
  if (
    !["task_view.create", "task_view.update", "task_view.delete"].includes(
      action,
    ) ||
    !validTaskViewProposal(proposal as unknown as Record<string, unknown>)
  )
    return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm" && action === "task_view.delete"
          ? { confirm_task_view_delete: consent }
          : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== action ||
    result.action.task_saved_view_id !== proposal.action.task_saved_view_id ||
    result.action.expected_version !== proposal.action.expected_version
  )
    return invalid();
  return result;
}

export async function decideAiKnowledgeAction(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  consent: boolean,
) {
  const action = proposal.action.action;
  if (
    ![
      "knowledge_source.create",
      "knowledge_source.reindex",
      "knowledge_source.delete",
      "knowledge_index_job.retry",
      "knowledge_index_job.cancel",
    ].includes(action) ||
    !validKnowledgeProposal(proposal as unknown as Record<string, unknown>)
  )
    return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm" && action === "knowledge_source.delete"
          ? { confirm_knowledge_source_delete: consent }
          : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== action ||
    result.action.knowledge_source_id !== proposal.action.knowledge_source_id ||
    result.action.knowledge_index_job_id !==
      proposal.action.knowledge_index_job_id
  )
    return invalid();
  return result;
}

export async function decideAiTagDelete(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  consent: boolean,
) {
  if (
    proposal.action.action !== "tag.delete" ||
    !validTagProposal(proposal as unknown as Record<string, unknown>)
  )
    return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm" ? { confirm_tag_delete: consent } : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== "tag.delete" ||
    result.action.tag_id !== proposal.action.tag_id ||
    result.action.expected_version !== proposal.action.expected_version
  )
    return invalid();
  return result;
}

export async function decideAiWorkspaceAction(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  confirmIncompleteTasks?: boolean,
  confirmForceResolve?: boolean,
  focusConsent?: { start?: boolean; includeGap?: boolean },
  confirmFollowupCompleted?: boolean,
  confirmActivityDelete?: boolean,
  confirmAutomationEffects?: boolean,
  confirmAutomationRetry?: boolean,
  confirmFinancialEffects?: boolean,
  confirmInvoiceEffects?: boolean,
  confirmInvoiceDelete?: boolean,
  confirmInvoicePdf?: boolean,
) {
  const result = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm" &&
        proposal.action.action === "invoice.delete" &&
        confirmInvoiceDelete !== undefined
          ? { confirm_invoice_delete: confirmInvoiceDelete }
          : {}),
        ...(decision === "confirm" &&
        proposal.action.action === "invoice.generate_pdf" &&
        confirmInvoicePdf !== undefined
          ? { confirm_invoice_pdf: confirmInvoicePdf }
          : {}),
        ...(decision === "confirm" &&
        proposal.action.action.startsWith("invoice.") &&
        confirmInvoiceEffects !== undefined
          ? { confirm_invoice_effects: confirmInvoiceEffects }
          : {}),
        ...(decision === "confirm" &&
        proposal.action.action.startsWith("financial_entry.") &&
        confirmFinancialEffects !== undefined
          ? { confirm_financial_effects: confirmFinancialEffects }
          : {}),
        ...(decision === "confirm" &&
        proposal.action.action === "automation.retry" &&
        confirmAutomationRetry !== undefined
          ? { confirm_automation_retry: confirmAutomationRetry }
          : {}),
        ...(decision === "confirm" &&
        needsAutomationEffectsConsent(proposal) &&
        confirmAutomationEffects !== undefined
          ? { confirm_automation_effects: confirmAutomationEffects }
          : {}),
        ...(decision === "confirm" &&
        proposal.action.action === "client_activity.delete" &&
        confirmActivityDelete !== undefined
          ? { confirm_activity_delete: confirmActivityDelete }
          : {}),
        ...(decision === "confirm" &&
        proposal.action.action === "client_followup.complete" &&
        confirmFollowupCompleted !== undefined
          ? { confirm_followup_completed: confirmFollowupCompleted }
          : {}),
        ...(decision === "confirm" &&
        proposal.action.action === "project.complete" &&
        confirmIncompleteTasks !== undefined
          ? { confirm_incomplete_tasks: confirmIncompleteTasks }
          : {}),
        ...(decision === "confirm" &&
        proposal.action.action === "inbox.force_resolve" &&
        confirmForceResolve !== undefined
          ? { confirm_force_resolve: confirmForceResolve }
          : {}),
        ...(decision === "confirm" &&
        proposal.action.action === "focus.start" &&
        focusConsent?.start !== undefined
          ? { confirm_focus_start: focusConsent.start }
          : {}),
        ...(decision === "confirm" &&
        proposal.action.action === "focus.recover" &&
        proposal.action.changes.recovery_action === "include_gap_resume" &&
        focusConsent?.includeGap !== undefined
          ? { confirm_focus_gap: focusConsent.includeGap }
          : {}),
      }),
    },
  );
  const parsed = parseAiActionProposal(result.data);
  if (
    parsed.id !== proposal.id ||
    parsed.fingerprint !== proposal.fingerprint ||
    parsed.generation_id !== proposal.generation_id
  )
    return invalid();
  return parsed;
}

export function needsAutomationEffectsConsent(proposal: AiActionProposal) {
  return (
    proposal.action.action === "automation.enable" ||
    (proposal.action.action === "automation.update" &&
      proposal.preview.after.rule_enabled === true)
  );
}

export async function decideAiFinanceExport(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  consent: boolean,
) {
  if (
    proposal.action.action !== "finance.export_csv" ||
    !validFinanceExport(proposal)
  )
    return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm" ? { confirm_finance_export: consent } : {}),
      }),
    },
  );
  const result = parseAiActionProposal(response.data);
  if (
    result.id !== proposal.id ||
    result.generation_id !== proposal.generation_id ||
    result.fingerprint !== proposal.fingerprint ||
    result.action.action !== "finance.export_csv" ||
    result.status !== (decision === "confirm" ? "confirmed" : "rejected") ||
    !Object.entries(proposal.preview.after).every(
      ([key, value]) => result.preview.after[key] === value,
    )
  )
    return invalid();
  return result;
}

export async function decideAiProjectNote(
  proposal: AiActionProposal,
  decision: "confirm" | "reject",
  confirmDelete: boolean,
) {
  const result = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/actions/${proposal.id}/decision`,
    {
      method: "POST",
      body: JSON.stringify({
        fingerprint: proposal.fingerprint,
        decision,
        ...(decision === "confirm" &&
        proposal.action.action === "project_note.delete"
          ? { confirm_project_note_delete: confirmDelete }
          : {}),
      }),
    },
  );
  return parseAiActionProposal(result.data);
}
