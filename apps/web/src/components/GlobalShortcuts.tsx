import { useEffect } from "react";
import { isDesktopRuntime } from "../api/desktop";
import { useUiStore } from "../store/ui";

const desktopShortcutEvent = "desktop-global-shortcut";

type DesktopShortcutAction = "command_palette" | "new_task";

export function parseDesktopShortcutAction(
  value: unknown,
): DesktopShortcutAction | null {
  return value === "command_palette" || value === "new_task" ? value : null;
}

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

  useEffect(() => {
    if (!isDesktopRuntime()) return;
    let disposed = false;
    let unlisten: (() => void) | undefined;
    void import("@tauri-apps/api/event")
      .then(({ listen }) =>
        listen<unknown>(desktopShortcutEvent, ({ payload }) => {
          const action = parseDesktopShortcutAction(payload);
          if (action === "command_palette") setCommandPaletteOpen(true);
          if (action === "new_task") setNewTaskOpen(true);
        }),
      )
      .then((stopListening) => {
        if (disposed) stopListening();
        else unlisten = stopListening;
      })
      .catch(() => {
        // WebView keyboard shortcuts remain available if the desktop event API fails.
      });
    return () => {
      disposed = true;
      unlisten?.();
    };
  }, [setCommandPaletteOpen, setNewTaskOpen]);

  return null;
}
