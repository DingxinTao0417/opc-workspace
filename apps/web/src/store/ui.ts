import { create } from "zustand";
import { isWorkspaceIdentity } from "../lib/focusReportLocation";
import type {
  AiWorkspacePanel,
  AiWorkspaceRecordNavigationType,
} from "../types/models";
import type { BrowserAction } from "../api/browser";

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
  returnSession?: string;
}

export type RightPanelTab =
  | "summary"
  | "overview"
  | "agents"
  | "files"
  | "review"
  | "terminal"
  | "browser"
  | "managed";

export type RightPanelRequestMode = "open" | "activate";

export interface BrowserNavigationRequest {
  id: number;
  url: string;
}

export interface BrowserActionRequest {
  id: number;
  action: BrowserAction;
}

export interface WorkspaceRecordNavigationRequest {
  id: number;
  recordType: AiWorkspaceRecordNavigationType;
  recordId: string;
  taskId?: string;
  parentId?: string;
  submissionId?: string;
}

export interface WorkspacePanelsRequest {
  id: number;
  panels: [AiWorkspacePanel, AiWorkspacePanel];
  splitRatio?: number;
}

export const RIGHT_OVERVIEW_DEFAULT_WIDTH = 280;
export const RIGHT_OVERVIEW_MIN_WIDTH = 240;
export const RIGHT_OVERVIEW_MAX_WIDTH = 480;
export const BROWSER_PANEL_DEFAULT_WIDTH = 680;
export const BROWSER_PANEL_MIN_WIDTH = 360;
export const BROWSER_PANEL_MAX_WIDTH = 1200;

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
  aiInboxOpen: boolean;
  rightOverviewCollapsed: boolean;
  rightOverviewWidth: number;
  browserPanelWidth: number;
  lastWorkspacePath: string;
  rightPanelTab: RightPanelTab;
  rightPanelRequestMode: RightPanelRequestMode;
  rightPanelRequest: number;
  workspacePanelsRequest: WorkspacePanelsRequest | null;
  workspacePanelsRequestId: number;
  browserNavigationRequest: BrowserNavigationRequest | null;
  browserActionRequest: BrowserActionRequest | null;
  workspaceRecordNavigationRequest: WorkspaceRecordNavigationRequest | null;
  localNavigationRequestRevision: number;
  settingsOpen: boolean;
  settingsModule: SettingsModule;
  taskDetailId: string | null;
  agentRunDrawer: AgentRunDrawerState | null;
  setCommandPaletteOpen: (open: boolean) => void;
  setNewTaskOpen: (open: boolean) => void;
  openNewTaskForProject: (projectId: string) => void;
  toggleSidebarCollapsed: () => void;
  toggleAgentRailCollapsed: () => void;
  setAiInboxOpen: (open: boolean) => void;
  openAiInbox: () => void;
  toggleRightOverviewCollapsed: () => void;
  collapseAgentWorkspace: () => void;
  setRightOverviewWidth: (width: number) => void;
  setBrowserPanelWidth: (width: number) => void;
  setLastWorkspacePath: (path: string) => void;
  setRightPanelTab: (tab: RightPanelTab, mode?: RightPanelRequestMode) => void;
  requestWorkspacePanels: (
    panels: [AiWorkspacePanel, AiWorkspacePanel],
    splitRatio?: number,
  ) => void;
  requestBrowserNavigation: (url: string) => void;
  dismissBrowserNavigation: (id: number) => void;
  requestBrowserAction: (action: BrowserAction) => void;
  dismissBrowserAction: (id: number) => void;
  requestWorkspaceRecordNavigation: (
    recordType: AiWorkspaceRecordNavigationType,
    recordId: string,
    taskId?: string,
    parentId?: string,
    submissionId?: string,
  ) => void;
  dismissWorkspaceRecordNavigation: (id: number) => void;
  setSettingsOpen: (open: boolean, module?: SettingsModule) => void;
  setTaskDetailId: (id: string | null) => void;
  openAgentRunDrawer: (
    taskId: string,
    runId?: string | null,
    returnSession?: string | null,
  ) => void;
  closeAgentRunDrawer: () => void;
}

export const useUiStore = create<UiState>((set) => ({
  commandPaletteOpen: false,
  newTaskOpen: false,
  newTaskProjectId: null,
  sidebarCollapsed: false,
  agentRailCollapsed: false,
  aiInboxOpen: false,
  rightOverviewCollapsed: true,
  rightOverviewWidth: RIGHT_OVERVIEW_DEFAULT_WIDTH,
  browserPanelWidth: BROWSER_PANEL_DEFAULT_WIDTH,
  lastWorkspacePath: "/today",
  rightPanelTab: "summary",
  rightPanelRequestMode: "open",
  rightPanelRequest: 0,
  workspacePanelsRequest: null,
  workspacePanelsRequestId: 0,
  browserNavigationRequest: null,
  browserActionRequest: null,
  workspaceRecordNavigationRequest: null,
  localNavigationRequestRevision: 0,
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
  setAiInboxOpen: (aiInboxOpen) => set({ aiInboxOpen }),
  openAiInbox: () => set({ aiInboxOpen: true, agentRailCollapsed: false }),
  toggleRightOverviewCollapsed: () =>
    set((state) => ({
      rightOverviewCollapsed: !state.rightOverviewCollapsed,
    })),
  collapseAgentWorkspace: () => set({ rightOverviewCollapsed: true }),
  setRightOverviewWidth: (rightOverviewWidth) =>
    set({ rightOverviewWidth: clampRightOverviewWidth(rightOverviewWidth) }),
  setBrowserPanelWidth: (width) =>
    set({
      browserPanelWidth: Number.isFinite(width)
        ? Math.min(
            BROWSER_PANEL_MAX_WIDTH,
            Math.max(BROWSER_PANEL_MIN_WIDTH, Math.round(width)),
          )
        : BROWSER_PANEL_DEFAULT_WIDTH,
    }),
  setLastWorkspacePath: (lastWorkspacePath) => set({ lastWorkspacePath }),
  setRightPanelTab: (rightPanelTab, rightPanelRequestMode = "open") =>
    set((state) => ({
      rightPanelTab,
      rightPanelRequestMode,
      rightPanelRequest: state.rightPanelRequest + 1,
    })),
  requestWorkspacePanels: (panels, splitRatio) =>
    set((state) => {
      if (
        panels.length !== 2 ||
        panels[0] === panels[1] ||
        (panels[0] === "browser" && panels[1] === "browser") ||
        (splitRatio !== undefined &&
          (!Number.isFinite(splitRatio) ||
            splitRatio < 0.25 ||
            splitRatio > 0.75))
      )
        return state;
      const id = state.workspacePanelsRequestId + 1;
      return {
        workspacePanelsRequestId: id,
        workspacePanelsRequest: {
          id,
          panels,
          ...(splitRatio !== undefined ? { splitRatio } : {}),
        },
      };
    }),
  requestBrowserNavigation: (url) =>
    set((state) => {
      if (state.browserNavigationRequest || state.browserActionRequest)
        return state;
      return {
        localNavigationRequestRevision:
          state.localNavigationRequestRevision + 1,
        browserNavigationRequest: {
          id: state.localNavigationRequestRevision + 1,
          url,
        },
      };
    }),
  dismissBrowserNavigation: (id) =>
    set((state) =>
      state.browserNavigationRequest?.id === id
        ? { browserNavigationRequest: null }
        : state,
    ),
  requestBrowserAction: (action) =>
    set((state) => {
      if (state.browserNavigationRequest || state.browserActionRequest)
        return state;
      return {
        localNavigationRequestRevision:
          state.localNavigationRequestRevision + 1,
        browserActionRequest: {
          id: state.localNavigationRequestRevision + 1,
          action,
        },
      };
    }),
  dismissBrowserAction: (id) =>
    set((state) =>
      state.browserActionRequest?.id === id
        ? { browserActionRequest: null }
        : state,
    ),
  requestWorkspaceRecordNavigation: (
    recordType,
    recordId,
    taskId,
    parentId,
    submissionId,
  ) =>
    set((state) => {
      if (state.workspaceRecordNavigationRequest) return state;
      return {
        localNavigationRequestRevision:
          state.localNavigationRequestRevision + 1,
        workspaceRecordNavigationRequest: {
          id: state.localNavigationRequestRevision + 1,
          recordType,
          recordId,
          ...(taskId ? { taskId } : {}),
          ...(parentId ? { parentId } : {}),
          ...(submissionId ? { submissionId } : {}),
        },
      };
    }),
  dismissWorkspaceRecordNavigation: (id) =>
    set((state) =>
      state.workspaceRecordNavigationRequest?.id === id
        ? { workspaceRecordNavigationRequest: null }
        : state,
    ),
  setSettingsOpen: (settingsOpen, settingsModule = "general") =>
    set({ settingsOpen, settingsModule }),
  setTaskDetailId: (taskDetailId) => set({ taskDetailId }),
  openAgentRunDrawer: (taskId, runId = null, returnSession) =>
    set({
      agentRunDrawer: {
        taskId,
        runId,
        ...(isWorkspaceIdentity(returnSession) ? { returnSession } : {}),
      },
    }),
  closeAgentRunDrawer: () => set({ agentRunDrawer: null }),
}));
