import { create } from "zustand";
// Local cache invalidation only. Never added to AI messages or consent.
export const useWorkspaceFileChanges = create<{
  revision: number;
  changed: () => void;
}>((set) => ({
  revision: 0,
  changed: () => set((s) => ({ revision: s.revision + 1 })),
}));
