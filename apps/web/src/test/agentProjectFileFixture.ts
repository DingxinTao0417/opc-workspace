import type {
  AgentRunFileCandidate,
  AgentRunReworkInputFile,
} from "../types/models";
import type { AiActionProposal } from "../api/aiWorkspaceActions";
import { restartProposal } from "./agentRunRestartFixture";
export const projectFile = {
  sourceKind: "project_task_artifact",
  id: "018f0000-0000-7000-8000-000000006101",
  name: "accepted.md",
  mime: "text/markdown",
  sizeBytes: 120,
  sha256: "c".repeat(64),
  eligible: true,
  errorCode: null,
  createdAt: "2026-09-21T12:00:00Z",
  sourceTask: {
    project_id: "018f0000-0000-7000-8000-000000006102",
    task_id: "018f0000-0000-7000-8000-000000006103",
    task_title: "已验收的来源任务",
    task_version: 12,
    submission_id: "018f0000-0000-7000-8000-000000006104",
    submission_sequence: 3,
  },
} satisfies AgentRunFileCandidate;
export const projectWireFile = {
  source_kind: projectFile.sourceKind,
  id: projectFile.id,
  name: projectFile.name,
  mime: projectFile.mime,
  size_bytes: projectFile.sizeBytes,
  sha256: projectFile.sha256,
  source_task: projectFile.sourceTask,
} satisfies AgentRunReworkInputFile;
export function projectFileProposal(anthropic = false): AiActionProposal {
  const proposal = restartProposal() as unknown as AiActionProposal;
  const start = proposal.preview.agent_run_start!;
  start.execution_contract_version = 6;
  start.task.project_id = projectFile.sourceTask.project_id;
  start.provider.leaves_device = true;
  start.provider.kind = "remote";
  start.provider.protocol = anthropic ? "anthropic_messages" : "openai_chat";
  if (anthropic) start.runtime_limits.max_output_tokens = 8192;
  start.input_files = [structuredClone(projectWireFile)];
  start.input_file_total_bytes = projectFile.sizeBytes;
  start.input_files_leave_device = true;
  start.output_contract = { type: "text" };
  start.file_limits = {
    max_files: 4,
    max_file_bytes: 65536,
    max_total_bytes: 131072,
    max_result_bytes: 65536,
  };
  proposal.action.changes.input_file_candidate_ids = [
    "018f0000-0000-7000-8000-000000006105",
  ];
  return proposal;
}
