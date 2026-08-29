import { cleanup, fireEvent, render } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { useUiStore } from "../store/ui";
import { GlobalShortcuts, parseDesktopShortcutAction } from "./GlobalShortcuts";

describe("GlobalShortcuts", () => {
  afterEach(() => {
    cleanup();
    useUiStore.setState({ commandPaletteOpen: false, newTaskOpen: false });
  });

  it("ignores shortcuts while an IME composition is active", () => {
    render(<GlobalShortcuts />);

    fireEvent.keyDown(window, { ctrlKey: true, isComposing: true, key: "k" });
    fireEvent.keyDown(window, { ctrlKey: true, key: "n", keyCode: 229 });

    expect(useUiStore.getState()).toMatchObject({
      commandPaletteOpen: false,
      newTaskOpen: false,
    });
  });

  it("opens the command palette and new-task modal from WebView shortcuts", () => {
    render(<GlobalShortcuts />);

    fireEvent.keyDown(window, { ctrlKey: true, key: "k" });
    expect(useUiStore.getState().commandPaletteOpen).toBe(true);

    fireEvent.keyDown(window, { metaKey: true, key: "n" });
    expect(useUiStore.getState().newTaskOpen).toBe(true);
  });

  it("accepts only fixed actions from the desktop shortcut bridge", () => {
    expect(parseDesktopShortcutAction("command_palette")).toBe(
      "command_palette",
    );
    expect(parseDesktopShortcutAction("new_task")).toBe("new_task");
    expect(parseDesktopShortcutAction("C:\\private\\path")).toBeNull();
    expect(parseDesktopShortcutAction({ action: "new_task" })).toBeNull();
  });
});
