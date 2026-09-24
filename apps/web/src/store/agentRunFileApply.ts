import { create } from "zustand";
import {
  workspaceApi,
  type WorkspaceFileSnapshot,
  type WorkspaceRoot,
} from "../api/workspace";
import type { AgentRun } from "../types/models";
import { getAgentRun } from "../api/client";
import { useWorkspaceFileChanges } from "./workspaceFileChanges";
import { useFileOperationReminders } from "./fileOperationReminders";

const canonicalId =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const sha256 = /^[0-9a-f]{64}$/;
const maxEditBytes = 32 * 1024;

export interface AgentRunFileApplyCandidate {
  runId: string;
  taskId: string;
  taskTitle: string;
  submissionId: string;
  artifactId: string;
  attempt: number;
  index: number;
  name: string;
  mime: string;
  content: string;
}

export interface AgentRunFileApplyReview {
  candidate: AgentRunFileApplyCandidate;
  root: WorkspaceRoot;
  path: string;
  snapshot: WorkspaceFileSnapshot;
  operationId: string;
  expiresAt: number;
  status:
    | "ready"
    | "verifying_source"
    | "checking"
    | "checked"
    | "conflict"
    | "writing"
    | "applied"
    | "recovery_required"
    | "uncertain";
  backupPath?: string;
  error: string | null;
}

interface AgentRunFileApplyState {
  candidate: AgentRunFileApplyCandidate | null;
  review: AgentRunFileApplyReview | null;
  loading: boolean;
  error: string | null;
  select: (candidate: AgentRunFileApplyCandidate) => boolean;
  stageTarget: (root: WorkspaceRoot, path: string) => Promise<void>;
  validate: () => Promise<void>;
  apply: () => Promise<void>;
  clear: (error?: string) => void;
}

let revision = 0;

function contentBytes(content: string) {
  return new TextEncoder().encode(content).byteLength;
}

function validCandidate(candidate: AgentRunFileApplyCandidate) {
  return (
    canonicalId.test(candidate.runId) &&
    canonicalId.test(candidate.taskId) &&
    canonicalId.test(candidate.submissionId) &&
    canonicalId.test(candidate.artifactId) &&
    Number.isSafeInteger(candidate.attempt) &&
    candidate.attempt >= 1 &&
    Number.isSafeInteger(candidate.index) &&
    candidate.index >= 0 &&
    candidate.name.trim().length > 0 &&
    contentBytes(candidate.name) <= 255 &&
    !/[\u0000-\u001f]/.test(candidate.name) &&
    candidate.mime.trim().length > 0 &&
    candidate.mime.length <= 100 &&
    !candidate.content.includes("\0") &&
    contentBytes(candidate.content) <= maxEditBytes
  );
}

function validSnapshot(snapshot: WorkspaceFileSnapshot, path: string) {
  return (
    snapshot.path === path &&
    canonicalId.test(snapshot.id) &&
    sha256.test(snapshot.sha256) &&
    snapshot.size === contentBytes(snapshot.content) &&
    snapshot.size <= maxEditBytes &&
    !snapshot.content.includes("\0") &&
    Number.isSafeInteger(snapshot.expiresInSeconds) &&
    snapshot.expiresInSeconds >= 1 &&
    snapshot.expiresInSeconds <= 600
  );
}

function sameCandidate(
  current: AgentRunFileApplyCandidate | undefined,
  expected: AgentRunFileApplyCandidate,
) {
  return (
    current?.runId === expected.runId &&
    current.taskId === expected.taskId &&
    current.submissionId === expected.submissionId &&
    current.artifactId === expected.artifactId &&
    current.attempt === expected.attempt &&
    current.index === expected.index &&
    current.name === expected.name &&
    current.mime === expected.mime &&
    current.content === expected.content
  );
}

async function candidateIsCurrent(candidate: AgentRunFileApplyCandidate) {
  const exact = await getAgentRun(candidate.runId);
  return sameCandidate(
    agentRunFileApplyCandidates(exact, candidate.taskTitle).find(
      (item) => item.index === candidate.index,
    ),
    candidate,
  );
}

function release(review: AgentRunFileApplyReview | null) {
  if (!review) return;
  void workspaceApi
    .releaseFileSnapshot(review.root.id, review.snapshot.id)
    .catch(() => {});
}

export function agentRunFileApplyCandidates(
  run: AgentRun,
  taskTitle: string,
): AgentRunFileApplyCandidate[] {
  if (
    run.status !== "succeeded" ||
    run.outputDeliveryStatus !== "submitted" ||
    !run.submissionId ||
    !run.artifactId ||
    !run.resultText
  )
    return [];
  const output = run.outputContract;
  let files: Array<{ name: string; mime: string; content: string }> = [];
  if (output?.type === "file") {
    files = [{ name: output.name, mime: output.mime, content: run.resultText }];
  } else if (output?.type === "files") {
    try {
      const bodies: unknown = JSON.parse(run.resultText);
      if (
        !Array.isArray(bodies) ||
        bodies.length !== output.files.length ||
        !bodies.every((body) => typeof body === "string")
      )
        return [];
      files = output.files.map((file, index) => ({
        ...file,
        content: bodies[index] as string,
      }));
    } catch {
      return [];
    }
  }
  return files
    .map((file, index) => ({
      runId: run.id,
      taskId: run.taskId,
      taskTitle,
      submissionId: run.submissionId!,
      artifactId: run.artifactId!,
      attempt: run.attempt,
      index,
      ...file,
    }))
    .filter(validCandidate);
}

// This state is deliberately memory-only. The Agent result does not grant a
// file path or a durable write permission; one human-selected target snapshot
// and one fresh native operation ID are required for every replacement.
export const useAgentRunFileApply = create<AgentRunFileApplyState>(
  (set, get) => ({
    candidate: null,
    review: null,
    loading: false,
    error: null,
    select: (candidate) => {
      if (!validCandidate(candidate)) {
        set({ error: "该 Agent 文件产出不符合单文件替换范围，未创建写入候选" });
        return false;
      }
      ++revision;
      release(get().review);
      set({ candidate, review: null, loading: false, error: null });
      return true;
    },
    stageTarget: async (root, path) => {
      const candidate = get().candidate;
      if (!candidate || !path) return;
      const request = ++revision;
      release(get().review);
      set({ review: null, loading: true, error: null });
      let snapshot: WorkspaceFileSnapshot | null = null;
      try {
        if (!(await candidateIsCurrent(candidate)))
          throw new Error(
            "Agent 文件产出的提交身份、契约或正文已经变化，请重新打开执行记录选择",
          );
        if (request !== revision || get().candidate !== candidate) return;
        snapshot = await workspaceApi.fileSnapshot(root.id, path);
        if (request !== revision || get().candidate !== candidate) {
          await workspaceApi.releaseFileSnapshot(root.id, snapshot.id);
          return;
        }
        if (!validSnapshot(snapshot, path))
          throw new Error(
            "目标文件无法形成完整、未截断且不超过 32 KiB 的精确基线",
          );
        if (snapshot.content === candidate.content)
          throw new Error("目标文件已与所选 Agent 产出一致，无需替换");
        set({
          loading: false,
          review: {
            candidate,
            root,
            path,
            snapshot,
            operationId: crypto.randomUUID(),
            expiresAt: Date.now() + snapshot.expiresInSeconds * 1000,
            status: "ready",
            error: null,
          },
        });
      } catch (reason) {
        if (snapshot)
          void workspaceApi
            .releaseFileSnapshot(root.id, snapshot.id)
            .catch(() => {});
        if (request === revision)
          set({
            loading: false,
            review: null,
            error: reason instanceof Error ? reason.message : String(reason),
          });
      }
    },
    validate: async () => {
      const review = get().review;
      if (!review || (review.status !== "ready" && review.status !== "checked"))
        return;
      if (review.expiresAt <= Date.now()) {
        get().clear("文件审查基线已过期，请重新选择 Agent 产出和目标文件");
        return;
      }
      const checking = { ...review, status: "checking" as const, error: null };
      set({ review: checking, error: null });
      try {
        await workspaceApi.validateFileSnapshot(
          review.root.id,
          review.snapshot.id,
        );
        if (get().review !== checking) return;
        if (checking.expiresAt <= Date.now()) {
          get().clear("文件审查基线已过期，请重新选择目标文件");
          return;
        }
        set({ review: { ...checking, status: "checked" } });
      } catch {
        if (get().review !== checking) return;
        release(checking);
        set({
          review: {
            ...checking,
            status: "conflict",
            error: "目标文件已变化或无法安全读取；未写入任何内容，请重新选择",
          },
        });
      }
    },
    apply: async () => {
      const review = get().review;
      if (!review || (review.status !== "ready" && review.status !== "checked"))
        return;
      if (review.expiresAt <= Date.now()) {
        get().clear("文件审查基线已过期，请重新选择目标文件");
        return;
      }
      const verifying = {
        ...review,
        status: "verifying_source" as const,
        error: null,
      };
      set({ review: verifying, error: null });
      try {
        if (!(await candidateIsCurrent(review.candidate))) {
          if (get().review !== verifying) return;
          release(verifying);
          set({
            review: {
              ...verifying,
              status: "conflict",
              error:
                "Agent 文件产出的提交身份、契约或正文已经变化；未发起写入，请重新选择产出",
            },
          });
          return;
        }
      } catch {
        if (get().review !== verifying) return;
        set({
          review: {
            ...verifying,
            status: review.status,
            error: "无法重新核对 Agent 文件产出；未发起写入，请重试",
          },
        });
        return;
      }
      if (get().review !== verifying) return;
      if (verifying.expiresAt <= Date.now()) {
        get().clear("文件审查基线已过期，请重新选择目标文件");
        return;
      }
      const writing = {
        ...verifying,
        status: "writing" as const,
        error: null,
      };
      set({ review: writing, error: null });
      const reminder = useFileOperationReminders
        .getState()
        .begin("agent_output_file");
      try {
        const result = await workspaceApi.applyFileEdit(
          review.root.id,
          review.snapshot.id,
          review.operationId,
          review.candidate.content,
        );
        useWorkspaceFileChanges.getState().changed();
        if (
          result.id !== review.operationId ||
          ![
            "applied",
            "cancelled",
            "recovery_required",
            "already_recorded",
          ].includes(result.status)
        )
          throw new Error("写入回执无效");
        useFileOperationReminders
          .getState()
          .settle(
            reminder,
            result.status === "applied" || result.status === "cancelled"
              ? "confirmed"
              : result.status === "recovery_required"
                ? "recovery_required"
                : "uncertain",
          );
        if (get().review !== writing) return;
        if (result.status === "cancelled") {
          set({
            review: {
              ...writing,
              status: "conflict",
              error:
                "本次原生确认或系统授权已取消；旧审查已消费，请重新选择目标文件",
            },
          });
          return;
        }
        set({
          review: {
            ...writing,
            status:
              result.status === "already_recorded"
                ? "uncertain"
                : result.status,
            backupPath: result.backupPath,
          },
        });
      } catch {
        useWorkspaceFileChanges.getState().changed();
        useFileOperationReminders.getState().settle(reminder, "uncertain");
        if (get().review === writing)
          set({
            review: {
              ...writing,
              status: "uncertain",
              error:
                "写入被拒绝或结果未确定。不要重复提交；请在文件恢复中核对记录和当前文件",
            },
          });
      }
    },
    clear: (error) => {
      ++revision;
      release(get().review);
      set({
        candidate: null,
        review: null,
        loading: false,
        error: error ?? null,
      });
    },
  }),
);
