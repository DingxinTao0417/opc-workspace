import { create } from "zustand";
import type { WorkspaceRoot } from "../api/workspace";
import type { BrowserSnapshot } from "../api/browser";

export type WorkspacePanelKind =
  | "start"
  | "overview"
  | "agents"
  | "files"
  | "file"
  | "artifact"
  | "browser"
  | "review"
  | "terminal"
  | "chat"
  | "ai-file-edit"
  | "managed";
export interface WorkspacePanel {
  id: string;
  kind: WorkspacePanelKind;
  title: string;
  root?: WorkspaceRoot;
  path?: string;
  terminalId?: string;
  browserId?: string;
  chatSessionId?: string;
  draft?: string;
  providerId?: string;
  artifactId?: string;
  artifactTaskId?: string;
  artifactSubmissionId?: string;
  returnSession?: string;
  proposalId?: string;
}
interface PanelsState {
  tabs: WorkspacePanel[];
  activeId: string;
  splitId: string | null;
  splitRatio: number;
  handledLayoutRequest: number;
  maximized: boolean;
  root: WorkspaceRoot | null;
  browserRevision: number;
  handledRequest: string;
  open: (panel: Omit<WorkspacePanel, "id">, id?: string) => string;
  select: (id: string) => void;
  close: (id: string) => void;
  update: (id: string, patch: Partial<WorkspacePanel>) => void;
  syncBrowser: (snapshot: BrowserSnapshot) => void;
  setRoot: (root: WorkspaceRoot) => void;
  split: () => void;
  setSplitWith: (otherId: string) => boolean;
  setSplitRatio: (ratio: number) => void;
  toggleMaximized: () => void;
}
export const useWorkspacePanels = create<PanelsState>((set, get) => ({
  tabs: [],
  activeId: "",
  splitId: null,
  splitRatio: 0.5,
  handledLayoutRequest: 0,
  maximized: false,
  root: null,
  browserRevision: -1,
  handledRequest: "",
  open: (panel, id = crypto.randomUUID()) => {
    const current = get();
    if (
      !current.tabs.some((tab) => tab.id === id) &&
      current.tabs.filter((tab) => tab.kind !== "browser").length >= 24
    )
      throw new Error("最多打开 24 个工具标签，请先关闭一些标签");
    set((state) => ({
      tabs: state.tabs.some((t) => t.id === id)
        ? state.tabs
        : [...state.tabs, { ...panel, id }],
      activeId: id,
      splitId: state.splitId === id ? null : state.splitId,
    }));
    return id;
  },
  select: (activeId) =>
    set((state) => {
      let splitId = state.splitId === activeId ? state.activeId : state.splitId;
      if (
        state.tabs.find((t) => t.id === activeId)?.kind === "browser" &&
        state.tabs.find((t) => t.id === splitId)?.kind === "browser"
      )
        splitId = null;
      return { activeId, splitId };
    }),
  close: (id) =>
    set((state) => {
      const index = state.tabs.findIndex((t) => t.id === id);
      const tabs = state.tabs.filter((t) => t.id !== id);
      const activeId =
        state.activeId === id
          ? (tabs[Math.min(index, tabs.length - 1)]?.id ?? "")
          : state.activeId;
      return {
        tabs,
        activeId,
        splitId:
          state.splitId === id || state.splitId === activeId || tabs.length < 2
            ? null
            : state.splitId,
      };
    }),
  update: (id, patch) =>
    set((state) => ({
      tabs: state.tabs.map((t) => (t.id === id ? { ...t, ...patch, id } : t)),
    })),
  syncBrowser: (snapshot) =>
    set((state) => {
      if (snapshot.revision < state.browserRevision) return state;
      const browserIds = new Set(snapshot.tabs.map((t) => t.id));
      const tabs = state.tabs.filter(
        (t) => !t.browserId || browserIds.has(t.browserId),
      );
      for (const browser of snapshot.tabs) {
        const id = `browser:${browser.id}`;
        const title =
          browser.title ||
          (browser.url === "about:blank" ? "新标签页" : browser.url);
        const index = tabs.findIndex((t) => t.id === id);
        const panel: WorkspacePanel = {
          id,
          kind: "browser",
          title,
          browserId: browser.id,
        };
        if (index >= 0) tabs[index] = panel;
        else tabs.push(panel);
      }
      const current = state.tabs.find((t) => t.id === state.activeId);
      const activeId =
        current?.kind === "browser" && snapshot.activeTabId
          ? `browser:${snapshot.activeTabId}`
          : tabs.some((t) => t.id === state.activeId)
            ? state.activeId
            : (tabs[0]?.id ?? "");
      return {
        tabs,
        activeId,
        splitId:
          current?.kind !== "browser" &&
          state.tabs.find((t) => t.id === state.splitId)?.kind === "browser" &&
          snapshot.activeTabId
            ? `browser:${snapshot.activeTabId}`
            : tabs.some((t) => t.id === state.splitId) &&
                state.splitId !== activeId
              ? state.splitId
              : null,
        browserRevision: snapshot.revision,
      };
    }),
  setRoot: (root) => set({ root }),
  split: () => {
    const state = get();
    if (state.splitId) {
      set({ splitId: null });
      return;
    }
    // A single native Chromium surface can be visible. A browser can be paired
    // with a file/review/terminal/chat, never two native surfaces simultaneously.
    const active = state.tabs.find((t) => t.id === state.activeId);
    const other = state.tabs.find(
      (t) =>
        t.id !== state.activeId &&
        !(active?.kind === "browser" && t.kind === "browser"),
    );
    if (other) set({ splitId: other.id, splitRatio: 0.5 });
  },
  setSplitWith: (otherId) => {
    const state = get();
    const active = state.tabs.find((tab) => tab.id === state.activeId);
    const other = state.tabs.find((tab) => tab.id === otherId);
    if (
      !active ||
      !other ||
      active.id === other.id ||
      (active.kind === "browser" && other.kind === "browser")
    )
      return false;
    set({
      splitId: other.id,
      ...(state.splitId !== other.id ? { splitRatio: 0.5 } : {}),
    });
    return true;
  },
  setSplitRatio: (ratio) =>
    set({
      splitRatio: Number.isFinite(ratio)
        ? Math.min(0.75, Math.max(0.25, ratio))
        : 0.5,
    }),
  toggleMaximized: () => set((state) => ({ maximized: !state.maximized })),
}));
