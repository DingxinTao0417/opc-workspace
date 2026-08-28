import { create } from "zustand";

interface UiState {
  commandPaletteOpen: boolean;
  newTaskOpen: boolean;
  newTaskProjectId: string | null;
  settingsOpen: boolean;
  taskDetailId: string | null;
  setCommandPaletteOpen: (open: boolean) => void;
  setNewTaskOpen: (open: boolean) => void;
  openNewTaskForProject: (projectId: string) => void;
  setSettingsOpen: (open: boolean) => void;
  setTaskDetailId: (id: string | null) => void;
}

export const useUiStore = create<UiState>((set) => ({
  commandPaletteOpen: false,
  newTaskOpen: false,
  newTaskProjectId: null,
  settingsOpen: false,
  taskDetailId: null,
  setCommandPaletteOpen: (commandPaletteOpen) => set({ commandPaletteOpen }),
  setNewTaskOpen: (newTaskOpen) =>
    set({
      newTaskOpen,
      ...(newTaskOpen ? {} : { newTaskProjectId: null }),
    }),
  openNewTaskForProject: (newTaskProjectId) =>
    set({ newTaskOpen: true, newTaskProjectId }),
  setSettingsOpen: (settingsOpen) => set({ settingsOpen }),
  setTaskDetailId: (taskDetailId) => set({ taskDetailId }),
}));
