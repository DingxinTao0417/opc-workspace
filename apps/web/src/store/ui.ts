import { create } from "zustand";

interface UiState {
  commandPaletteOpen: boolean;
  newTaskOpen: boolean;
  settingsOpen: boolean;
  taskDetailId: string | null;
  setCommandPaletteOpen: (open: boolean) => void;
  setNewTaskOpen: (open: boolean) => void;
  setSettingsOpen: (open: boolean) => void;
  setTaskDetailId: (id: string | null) => void;
}

export const useUiStore = create<UiState>((set) => ({
  commandPaletteOpen: false,
  newTaskOpen: false,
  settingsOpen: false,
  taskDetailId: null,
  setCommandPaletteOpen: (commandPaletteOpen) => set({ commandPaletteOpen }),
  setNewTaskOpen: (newTaskOpen) => set({ newTaskOpen }),
  setSettingsOpen: (settingsOpen) => set({ settingsOpen }),
  setTaskDetailId: (taskDetailId) => set({ taskDetailId }),
}));
