import { create } from "zustand";
import { workspaceApi } from "../api/workspace";
import { useWorkspaceFileChanges } from "./workspaceFileChanges";
import { useFileOperationReminders } from "./fileOperationReminders";
import type { AiProjectFileContext } from "../api/ai";
import type {
  AiChatStreamMeta,
  AiProvider,
  AiProjectFileProposal,
} from "../types/models";
import type { Approval, Selection } from "./aiProjectFiles";

type Provider = Pick<AiProvider, "id" | "version">;
interface Review {
  selection: Selection;
  targetFile: Selection["files"][number] | null;
  approval: Approval;
  providerLabel: string;
  requestId: string | null;
  generationId: string | null;
  proposal: AiProjectFileProposal | null;
  status:
    | "receiving"
    | "ready"
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
interface State {
  review: Review | null;
  error: string | null;
  begin: (
    selection: Selection,
    approval: Approval,
    provider: AiProvider,
  ) => void;
  bind: (requestId: string, files: AiProjectFileContext | undefined) => void;
  meta: (requestId: string, meta: AiChatStreamMeta) => void;
  receive: (
    requestId: string,
    generationId: string,
    proposal: AiProjectFileProposal,
  ) => void;
  finish: (requestId: string, completed: boolean) => void;
  validate: (sessionId: string, provider: Provider) => Promise<void>;
  apply: (
    sessionId: string,
    provider: Provider,
    proposalId: string,
  ) => Promise<void>;
  clear: (error?: string) => void;
}
const canonicalId =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
function release(review: Review | null) {
  review?.selection.files.forEach(({ snapshot }) => {
    void workspaceApi
      .releaseFileSnapshot(review.selection.rootId, snapshot.id)
      .catch(() => {});
  });
}
function current(review: Review, sessionId: string, provider: Provider) {
  return (
    review.approval.sessionId === sessionId &&
    review.approval.providerId === provider.id &&
    review.approval.providerVersion === provider.version &&
    review.selection.expiresAt > Date.now()
  );
}

// One in-memory review. No persistence, replay or transferable approval.
// Only apply() after a human click invokes the independent native writer.
// Failure/uncertain generation completion cannot recover it from chat history.
export const useAiProjectFileReview = create<State>((set, get) => ({
  review: null,
  error: null,
  clear: (error) => {
    release(get().review);
    set({ review: null, error: error ?? null });
  },
  begin: (selection, approval, provider) => {
    get().clear();
    set({
      review: {
        selection,
        targetFile: null,
        approval,
        providerLabel: `${provider.name} · ${provider.model}`,
        requestId: null,
        generationId: null,
        proposal: null,
        status: "receiving",
        error: null,
      },
    });
  },
  bind: (requestId, files) => {
    const review = get().review;
    if (!review) return;
    if (
      !files ||
      review.requestId ||
      !current(review, files.session_id, {
        id: files.provider_id,
        version: files.provider_version,
      }) ||
      !files.confirmed ||
      files.files.length !== review.selection.files.length ||
      Date.parse(files.expires_at) !== review.selection.expiresAt ||
      files.files.some((file, index) => {
        const selected = review.selection.files[index]?.snapshot;
        return (
          !selected ||
          file.path !== selected.path ||
          file.sha256 !== selected.sha256 ||
          file.content !== selected.content
        );
      })
    ) {
      get().clear();
      return;
    }
    set({ review: { ...review, requestId } });
  },
  meta: (requestId, meta) => {
    const review = get().review;
    if (!review || review.requestId !== requestId) return;
    if (
      review.approval.sessionId !== meta.session_id ||
      review.approval.providerId !== meta.provider_id ||
      !canonicalId.test(meta.generation_id) ||
      (review.generationId && review.generationId !== meta.generation_id) ||
      review.selection.expiresAt <= Date.now()
    ) {
      get().clear("文件建议的生成身份或期限不一致，请重新读取");
      return;
    }
    set({ review: { ...review, generationId: meta.generation_id } });
  },
  receive: (requestId, generationId, proposal) => {
    const review = get().review;
    if (!review || review.requestId !== requestId) return;
    const targetFile = review.selection.files.find(
      ({ snapshot }) =>
        proposal.path === snapshot.path &&
        proposal.baseSHA256 === snapshot.sha256,
    );
    if (
      review.status !== "receiving" ||
      review.generationId !== generationId ||
      !review.generationId ||
      review.selection.expiresAt <= Date.now() ||
      !targetFile ||
      !canonicalId.test(proposal.id) ||
      typeof proposal.content !== "string" ||
      new TextEncoder().encode(proposal.content).length > 32 * 1024 ||
      proposal.content.includes("\0") ||
      proposal.content === targetFile.snapshot.content ||
      (review.proposal &&
        JSON.stringify(review.proposal) !== JSON.stringify(proposal))
    ) {
      get().clear("文件建议与本次精确基线不一致，未接收；请重新读取");
      return;
    }
    set({ review: { ...review, targetFile, proposal } });
  },
  finish: (requestId, completed) => {
    const review = get().review;
    if (!review || review.requestId !== requestId) return;
    if (
      !completed ||
      !review.proposal ||
      !review.targetFile ||
      review.selection.expiresAt <= Date.now()
    ) {
      get().clear(
        review.proposal
          ? "生成未完整完成或文件基线已过期，建议已丢弃；请重新读取"
          : undefined,
      );
      return;
    }
    set({ review: { ...review, status: "ready" } });
  },
  validate: async (sessionId, provider) => {
    const review = get().review;
    if (
      !review ||
      !review.proposal ||
      !review.targetFile ||
      (review.status !== "ready" && review.status !== "checked")
    )
      return;
    if (!current(review, sessionId, provider)) {
      get().clear("对话、模型或文件期限已变化，请重新读取");
      return;
    }
    const checking: Review = { ...review, status: "checking", error: null };
    set({ review: checking });
    try {
      await workspaceApi.validateFileSnapshot(
        review.selection.rootId,
        review.targetFile.snapshot.id,
      );
      if (get().review !== checking) return;
      if (!current(checking, sessionId, provider)) {
        get().clear("文件基线已过期，请重新读取");
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
          error:
            "当前文件已变化或无法安全读取。未写入任何内容，请重新选择文件并生成建议。",
        },
      });
    }
  },
  apply: async (sessionId, provider, proposalId) => {
    const review = get().review;
    if (
      !review?.proposal ||
      !review.targetFile ||
      review.proposal.id !== proposalId ||
      !current(review, sessionId, provider) ||
      (review.status !== "ready" && review.status !== "checked")
    )
      return;
    const writing: Review = { ...review, status: "writing", error: null };
    set({ review: writing });
    const reminder = useFileOperationReminders
      .getState()
      .begin("ai_project_file", sessionId);
    try {
      // This invocation must only follow the explicit UI confirmation. Native
      // code compares again under exclusive handles; no separate check grants it.
      const result = await workspaceApi.applyFileEdit(
        review.selection.rootId,
        review.targetFile.snapshot.id,
        proposalId,
        review.proposal.content,
      );
      useWorkspaceFileChanges.getState().changed();
      if (
        result.id !== proposalId ||
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
              "本次原生确认或系统授权已取消，旧文件审查已消费；请重新选择文件后再生成建议。",
          },
        });
        return;
      }
      set({
        review: {
          ...writing,
          status:
            result.status === "already_recorded" ? "uncertain" : result.status,
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
              "写入被拒绝或结果未确定。不要重复提交；请在右侧文件面板的“文件恢复”中核对记录和当前文件，必要时重新读取。",
          },
        });
    }
  },
}));
