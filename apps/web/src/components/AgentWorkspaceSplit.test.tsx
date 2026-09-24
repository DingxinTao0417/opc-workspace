import {
  cleanup,
  createEvent,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { useUiStore } from "../store/ui";
import { useWorkspacePanels } from "../store/workspacePanels";
import { AgentWorkspace } from "./AgentWorkspace";

beforeEach(() => {
  useWorkspacePanels.setState({
    tabs: [
      { id: "primary", kind: "start", title: "主面板" },
      { id: "secondary", kind: "start", title: "次面板" },
    ],
    activeId: "primary",
    splitId: "secondary",
    splitRatio: 0.5,
  });
  useUiStore.setState({
    rightPanelTab: "summary",
    rightPanelRequestMode: "open",
    rightPanelRequest: 0,
    workspacePanelsRequest: null,
    rightOverviewCollapsed: false,
  });
});

afterEach(() => {
  cleanup();
  useWorkspacePanels.setState({
    tabs: [],
    activeId: "",
    splitId: null,
    splitRatio: 0.5,
  });
});

it("supports bounded keyboard resizing and double-click reset", () => {
  render(<AgentWorkspace />);
  const separator = screen.getByRole("separator", {
    name: "调整工作区分栏比例",
  });

  expect(separator).toHaveAttribute("aria-valuenow", "50");
  fireEvent.keyDown(separator, { key: "ArrowRight" });
  expect(separator).toHaveAttribute("aria-valuenow", "53");
  fireEvent.keyDown(separator, { key: "ArrowLeft", shiftKey: true });
  expect(separator).toHaveAttribute("aria-valuenow", "43");
  fireEvent.keyDown(separator, { key: "Home" });
  expect(separator).toHaveAttribute("aria-valuenow", "25");
  fireEvent.keyDown(separator, { key: "End" });
  expect(separator).toHaveAttribute("aria-valuenow", "75");
  fireEvent.doubleClick(separator);
  expect(separator).toHaveAttribute("aria-valuenow", "50");
});

it("drags the divider and stops resizing when the pointer is released", () => {
  const { container } = render(<AgentWorkspace />);
  const panes = container.querySelector(".ws-panes");
  if (!panes) throw new Error("workspace panes were not rendered");
  vi.spyOn(panes, "getBoundingClientRect").mockReturnValue({
    width: 812,
  } as DOMRect);

  const separator = screen.getByRole("separator", {
    name: "调整工作区分栏比例",
  });
  const start = createEvent.pointerDown(separator);
  Object.assign(start, { pointerId: 1, clientX: 100, button: 0 });
  fireEvent(separator, start);
  expect(container.querySelector(".ws-panes")).toHaveClass("is-resizing");

  const move = createEvent.pointerMove(separator);
  Object.assign(move, { pointerId: 1, clientX: 300 });
  fireEvent(separator, move);
  expect(separator).toHaveAttribute("aria-valuenow", "75");

  const end = createEvent.pointerUp(separator);
  Object.assign(end, { pointerId: 1 });
  fireEvent(separator, end);
  expect(container.querySelector(".ws-panes")).not.toHaveClass("is-resizing");
});

it("omits the resize separator when the workspace is not split", () => {
  useWorkspacePanels.setState({ splitId: null });
  render(<AgentWorkspace />);

  expect(
    screen.queryByRole("separator", { name: "调整工作区分栏比例" }),
  ).toBeNull();
});
