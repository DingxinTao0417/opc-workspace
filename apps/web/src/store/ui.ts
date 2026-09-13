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

interface UiState {
  commandPaletteOpen: boolean;
  newTaskOpen: boolean;
  newTaskProjectId: string | null;
  sidebarCollapsed: boolean;
  settingsOpen: boolean;
  settingsModule: SettingsModule;
  taskDetailId: string | null;
  agentRunDrawer: AgentRunDrawerState | null;
  setCommandPaletteOpen: (open: boolean) => void;
  setNewTaskOpen: (open: boolean) => void;
  openNewTaskForProject: (projectId: string) => void;
  toggleSidebarCollapsed: () => void;
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
  setSettingsOpen: (settingsOpen, settingsModule = "general") =>
    set({ settingsOpen, settingsModule }),
  setTaskDetailId: (taskDetailId) => set({ taskDetailId }),
  openAgentRunDrawer: (taskId, runId = null) =>
    set({ agentRunDrawer: { taskId, runId } }),
  closeAgentRunDrawer: () => set({ agentRunDrawer: null }),
}));
