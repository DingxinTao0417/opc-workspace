import { useEffect } from "react";
import { useUiStore } from "../store/ui";

export function GlobalShortcuts() {
  const setCommandPaletteOpen = useUiStore(
    (state) => state.setCommandPaletteOpen,
  );
  const setNewTaskOpen = useUiStore((state) => state.setNewTaskOpen);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.isComposing || event.keyCode === 229) return;
      if (!(event.metaKey || event.ctrlKey) || event.altKey) return;
      if (event.key.toLowerCase() === "k") {
        event.preventDefault();
        setCommandPaletteOpen(true);
      } else if (event.key.toLowerCase() === "n") {
        event.preventDefault();
        setNewTaskOpen(true);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [setCommandPaletteOpen, setNewTaskOpen]);

  return null;
}
