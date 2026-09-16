import { create } from "zustand";

export type SettingsModule =
  | "profile"
  | "general"
  | "appearance"
  | "focus"
  | "actors"
  | "agent"
  | "ai"
  | "automation"
  | "data"
  | "diagnostics"
  | "about";

export interface AgentRunDrawerState {
  taskId: string;
  runId: string | null;
}

export type RightPanelTab = "summary" | "agents" | "files" | "browser";

export const RIGHT_OVERVIEW_DEFAULT_WIDTH = 280;
export const RIGHT_OVERVIEW_MIN_WIDTH = 240;
export const RIGHT_OVERVIEW_MAX_WIDTH = 480;

function clampRightOverviewWidth(width: number) {
  if (!Number.isFinite(width)) return RIGHT_OVERVIEW_DEFAULT_WIDTH;
  return Math.min(
    RIGHT_OVERVIEW_MAX_WIDTH,
    Math.max(RIGHT_OVERVIEW_MIN_WIDTH, Math.round(width)),
  );
}

interface UiState {
  commandPaletteOpen: boolean;
  newTaskOpen: boolean;
  newTaskProjectId: string | null;
  sidebarCollapsed: boolean;
  agentRailCollapsed: boolean;
  rightOverviewCollapsed: boolean;
  rightOverviewWidth: number;
  lastWorkspacePath: string;
  rightPanelTab: RightPanelTab;
  settingsOpen: boolean;
  settingsModule: SettingsModule;
  taskDetailId: string | null;
  agentRunDrawer: AgentRunDrawerState | null;
  setCommandPaletteOpen: (open: boolean) => void;
  setNewTaskOpen: (open: boolean) => void;
  openNewTaskForProject: (projectId: string) => void;
  toggleSidebarCollapsed: () => void;
  toggleAgentRailCollapsed: () => void;
  toggleRightOverviewCollapsed: () => void;
  setRightOverviewWidth: (width: number) => void;
  setLastWorkspacePath: (path: string) => void;
  setRightPanelTab: (tab: RightPanelTab) => void;
  setSettingsOpen: (open: boolean, module?: SettingsModule) => void;
  setTaskDetailId: (id: string | null) => void;
  openAgentRunDrawer: (taskId: string, runId?: string | null) => void;
  closeAgentRunDrawer: () => void;
}

export const useUiStore = create<UiState>((set) => ({
  commandPaletteOpen: false,
  newTaskOpen: false,
  newTaskProjectId: null,
  sidebarCollapsed: false,
  agentRailCollapsed: false,
  rightOverviewCollapsed: false,
  rightOverviewWidth: RIGHT_OVERVIEW_DEFAULT_WIDTH,
  lastWorkspacePath: "/today",
  rightPanelTab: "summary",
  settingsOpen: false,
  settingsModule: "general",
  taskDetailId: null,
  agentRunDrawer: null,
  setCommandPaletteOpen: (commandPaletteOpen) => set({ commandPaletteOpen }),
  setNewTaskOpen: (newTaskOpen) =>
    set({
      newTaskOpen,
      ...(newTaskOpen ? {} : { newTaskProjectId: null }),
    }),
  openNewTaskForProject: (newTaskProjectId) =>
    set({ newTaskOpen: true, newTaskProjectId }),
  toggleSidebarCollapsed: () =>
    set((state) => ({ sidebarCollapsed: !state.sidebarCollapsed })),
  toggleAgentRailCollapsed: () =>
    set((state) => ({ agentRailCollapsed: !state.agentRailCollapsed })),
  toggleRightOverviewCollapsed: () =>
    set((state) => ({
      rightOverviewCollapsed: !state.rightOverviewCollapsed,
    })),
  setRightOverviewWidth: (rightOverviewWidth) =>
    set({ rightOverviewWidth: clampRightOverviewWidth(rightOverviewWidth) }),
  setLastWorkspacePath: (lastWorkspacePath) => set({ lastWorkspacePath }),
  setRightPanelTab: (rightPanelTab) => set({ rightPanelTab }),
  setSettingsOpen: (settingsOpen, settingsModule = "general") =>
    set({ settingsOpen, settingsModule }),
  setTaskDetailId: (taskDetailId) => set({ taskDetailId }),
  openAgentRunDrawer: (taskId, runId = null) =>
    set({ agentRunDrawer: { taskId, runId } }),
  closeAgentRunDrawer: () => set({ agentRunDrawer: null }),
}));
