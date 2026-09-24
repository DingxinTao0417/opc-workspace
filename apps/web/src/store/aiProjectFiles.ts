import { create } from "zustand";
import {
  workspaceApi,
  type WorkspaceFileSnapshot,
  type WorkspaceRoot,
} from "../api/workspace";
import type { AiProjectFileContext } from "../api/ai";
import type { AiProvider } from "../types/models";
import { useAiProjectFileReview } from "./aiProjectFileReview";

export interface Selection {
  id: number;
  rootId: string;
  rootName: string;
  files: Array<{
    snapshot: WorkspaceFileSnapshot;
    expiresAt: number;
  }>;
  expiresAt: number;
  totalBytes: number;
}
export interface Approval {
  selectionId: number;
  sessionId: string;
  providerId: string;
  providerVersion: number;
}
type Provider = Pick<AiProvider, "id" | "version">;
interface State {
  selection: Selection | null;
  approval: Approval | null;
  loading: boolean;
  error: string | null;
  stage: (root: WorkspaceRoot, path: string) => Promise<void>;
  remove: (path: string) => void;
  approve: (sessionId: string, provider: Provider) => void;
  revoke: () => void;
  clear: () => void;
  consumeForReview: (provider: AiProvider) => void;
  prepare: (
    sessionId: string,
    provider: Provider,
  ) => Promise<AiProjectFileContext | undefined>;
}
let revision = 0;
const MAX_FILES = 4;
const MAX_TOTAL_BYTES = 32 * 1024;
const canonicalId =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
function release(selection: Selection | null) {
  selection?.files.forEach(({ snapshot }) => {
    void workspaceApi
      .releaseFileSnapshot(selection.rootId, snapshot.id)
      .catch(() => {});
  });
}
function samePath(left: string, right: string) {
  return left.toLowerCase() === right.toLowerCase();
}
function selectionFrom(
  id: number,
  rootId: string,
  rootName: string,
  files: Selection["files"],
): Selection {
  return {
    id,
    rootId,
    rootName,
    files,
    expiresAt: Math.min(...files.map((file) => file.expiresAt)),
    totalBytes: files.reduce((total, file) => total + file.snapshot.size, 0),
  };
}
function matches(
  approval: Approval | null,
  selection: Selection,
  sessionId: string,
  provider: Provider,
) {
  return (
    approval?.selectionId === selection.id &&
    approval.sessionId === sessionId &&
    approval.providerId === provider.id &&
    approval.providerVersion === provider.version
  );
}

// No persist middleware: native handles, raw bytes and disclosure consent must
// never enter localStorage, draft strings, another chat owner or history.
export const useAiProjectFiles = create<State>((set, get) => ({
  selection: null,
  approval: null,
  loading: false,
  error: null,
  stage: async (root, path) => {
    useAiProjectFileReview.getState().clear();
    const request = ++revision;
    const previous = get().selection;
    const usablePrevious =
      previous && previous.expiresAt > Date.now() ? previous : null;
    if (previous && !usablePrevious) release(previous);
    set({
      selection: usablePrevious,
      approval: null,
      loading: false,
      error: null,
    });
    if (usablePrevious && usablePrevious.rootId !== root.id) {
      set({
        loading: false,
        error: "一条消息中的文件必须来自同一个已选工作目录",
      });
      return;
    }
    const replacingIndex =
      usablePrevious?.files.findIndex(({ snapshot }) =>
        samePath(snapshot.path, path),
      ) ?? -1;
    if (
      usablePrevious &&
      replacingIndex < 0 &&
      usablePrevious.files.length >= MAX_FILES
    ) {
      set({ loading: false, error: "一条消息最多选择 4 个文件" });
      return;
    }
    set({ loading: true });
    let snapshot: WorkspaceFileSnapshot | undefined;
    try {
      snapshot = await workspaceApi.fileSnapshot(root.id, path);
      if (request !== revision) {
        await workspaceApi.releaseFileSnapshot(root.id, snapshot.id);
        return;
      }
      if (
        snapshot.path !== path ||
        !canonicalId.test(snapshot.id) ||
        !/^[0-9a-f]{64}$/.test(snapshot.sha256) ||
        snapshot.size !==
          new TextEncoder().encode(snapshot.content).byteLength ||
        snapshot.content.includes("\0") ||
        !Number.isInteger(snapshot.expiresInSeconds) ||
        snapshot.expiresInSeconds < 1 ||
        snapshot.expiresInSeconds > 600
      ) {
        throw new Error("精确文件基线无效，请重新读取");
      }
      if (snapshot.size > 32 * 1024)
        throw new Error(
          "本次完整文件外发上限为 32 KiB，请选择较小的完整文件；不会使用截断内容",
        );
      const current = get().selection;
      if (current && current.rootId !== root.id)
        throw new Error("所选工作目录已变化，请重新选择文件");
      const existing = current?.files ?? [];
      const index = existing.findIndex(({ snapshot: item }) =>
        samePath(item.path, path),
      );
      if (index < 0 && existing.length >= MAX_FILES)
        throw new Error("一条消息最多选择 4 个文件");
      const files = existing.slice();
      const replaced = index >= 0 ? files[index] : undefined;
      const nextFile = {
        snapshot,
        expiresAt: Date.now() + snapshot.expiresInSeconds * 1000,
      };
      if (index >= 0) files[index] = nextFile;
      else files.push(nextFile);
      const totalBytes = files.reduce(
        (total, file) => total + file.snapshot.size,
        0,
      );
      if (totalBytes > MAX_TOTAL_BYTES)
        throw new Error("所选文件合计不能超过 32 KiB；不会截断任何文件");
      const nextSelection = selectionFrom(request, root.id, root.name, files);
      set({
        selection: nextSelection,
        loading: false,
        error: null,
      });
      if (replaced)
        void workspaceApi
          .releaseFileSnapshot(root.id, replaced.snapshot.id)
          .catch(() => {});
    } catch (error) {
      if (snapshot)
        void workspaceApi
          .releaseFileSnapshot(root.id, snapshot.id)
          .catch(() => {});
      if (request === revision)
        set({
          loading: false,
          error: error instanceof Error ? error.message : String(error),
        });
    }
  },
  remove: (path) => {
    const selection = get().selection;
    if (!selection) return;
    const index = selection.files.findIndex(({ snapshot }) =>
      samePath(snapshot.path, path),
    );
    if (index < 0) return;
    useAiProjectFileReview.getState().clear();
    const request = ++revision;
    const removed = selection.files[index];
    const files = selection.files.filter((_, fileIndex) => fileIndex !== index);
    if (removed)
      void workspaceApi
        .releaseFileSnapshot(selection.rootId, removed.snapshot.id)
        .catch(() => {});
    set({
      selection: files.length
        ? selectionFrom(request, selection.rootId, selection.rootName, files)
        : null,
      approval: null,
      loading: false,
      error: null,
    });
  },
  approve: (sessionId, provider) => {
    const selection = get().selection;
    if (
      !selection ||
      selection.expiresAt <= Date.now() ||
      !canonicalId.test(sessionId) ||
      !canonicalId.test(provider.id) ||
      !Number.isSafeInteger(provider.version) ||
      provider.version < 1
    ) {
      set({ approval: null, error: "请选择对话、模型并重新读取文件后确认" });
      return;
    }
    set({
      approval: {
        selectionId: selection.id,
        sessionId,
        providerId: provider.id,
        providerVersion: provider.version,
      },
      error: null,
    });
  },
  revoke: () => {
    set({ approval: null });
  },
  clear: () => {
    useAiProjectFileReview.getState().clear();
    ++revision;
    release(get().selection);
    set({ selection: null, approval: null, loading: false, error: null });
  },
  consumeForReview: (provider) => {
    const { selection, approval } = get();
    if (
      !selection ||
      !approval ||
      !matches(approval, selection, approval.sessionId, provider) ||
      selection.expiresAt <= Date.now()
    )
      throw new Error("文件授权已变化，请重新确认");
    ++revision;
    // Transfer the local baseline, never the disclosure grant, to a single
    // review owner. The next message cannot inherit selection or consent.
    useAiProjectFileReview.getState().begin(selection, approval, provider);
    set({ selection: null, approval: null, loading: false, error: null });
  },
  prepare: async (sessionId, provider) => {
    const { selection, approval, loading, error } = get();
    if (loading) throw new Error("文件正在读取，请稍后发送");
    if (error) throw new Error(error);
    if (!selection) return undefined;
    if (
      !matches(approval, selection, sessionId, provider) ||
      selection.expiresAt <= Date.now()
    ) {
      set({ approval: null });
      throw new Error("请先确认本条消息的文件外发授权");
    }
    const request = revision;
    try {
      await Promise.all(
        selection.files.map(({ snapshot }) =>
          workspaceApi.validateFileSnapshot(selection.rootId, snapshot.id),
        ),
      );
      if (
        request !== revision ||
        get().selection !== selection ||
        get().approval !== approval ||
        selection.expiresAt <= Date.now()
      ) {
        throw new Error("文件选择或授权已变化，请重新确认后发送");
      }
      return {
        provider_id: provider.id,
        session_id: sessionId,
        provider_version: provider.version,
        confirmed: true,
        expires_at: new Date(selection.expiresAt).toISOString(),
        files: selection.files.map(({ snapshot }) => ({
          path: snapshot.path,
          content: snapshot.content,
          sha256: snapshot.sha256,
        })),
      };
    } catch (error) {
      if (get().selection === selection && get().approval === approval)
        set({ approval: null });
      throw error;
    }
  },
}));
