import { create } from "zustand";

interface UiState {
  commandPaletteOpen: boolean;
  newTaskOpen: boolean;
  settingsOpen: boolean;
  setCommandPaletteOpen: (open: boolean) => void;
  setNewTaskOpen: (open: boolean) => void;
  setSettingsOpen: (open: boolean) => void;
}

export const useUiStore = create<UiState>((set) => ({
  commandPaletteOpen: false,
  newTaskOpen: false,
  settingsOpen: false,
  setCommandPaletteOpen: (commandPaletteOpen) => set({ commandPaletteOpen }),
  setNewTaskOpen: (newTaskOpen) => set({ newTaskOpen }),
  setSettingsOpen: (settingsOpen) => set({ settingsOpen }),
}));
