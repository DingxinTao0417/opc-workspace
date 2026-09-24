import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { WorkspaceTerminal } from "./WorkspaceTerminal";
import { useWorkspacePanels } from "../store/workspacePanels";
import { useAiWorkbenchHandoff } from "../store/aiWorkbenchHandoff";

const api = vi.hoisted(() => ({
  terminalStart: vi.fn(),
  terminalClose: vi.fn(),
  terminalRead: vi.fn(),
  terminalResize: vi.fn(),
  terminalWrite: vi.fn(),
}));
vi.mock("../api/workspace", () => ({
  workspaceApi: api,
  workspaceError: (e: Error) => e.message,
}));
vi.mock("@xterm/xterm", () => ({
  Terminal: class {
    cols = 80;
    rows = 24;
    options = { disableStdin: false };
    loadAddon() {}
    open() {}
    onData() {
      return { dispose() {} };
    }
    write(_: string | Uint8Array, done?: () => void) {
      done?.();
    }
    dispose() {}
  },
}));
vi.mock("@xterm/addon-fit", () => ({
  FitAddon: class {
    fit() {}
  },
}));
const root = { id: "root-1", name: "demo", path: "D:/demo" };
beforeEach(() => {
  vi.clearAllMocks();
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      disconnect() {}
    },
  );
  api.terminalResize.mockResolvedValue(undefined);
  useWorkspacePanels.setState({ tabs: [], activeId: "", splitId: null, root });
  useAiWorkbenchHandoff.setState({ pending: null, pendingIssue: null });
  useWorkspacePanels
    .getState()
    .open({ kind: "terminal", title: "终端", root }, "term");
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

it("requires a manual start and reports native failure without inventing a session", async () => {
  api.terminalStart.mockRejectedValueOnce(new Error("无法托管终端进程树"));
  render(<WorkspaceTerminal panel={useWorkspacePanels.getState().tabs[0]} />);
  expect(api.terminalStart).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "启动终端 · demo" }));
  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent("无法托管终端进程树"),
  );
  expect(useWorkspacePanels.getState().tabs[0].terminalId).toBeUndefined();
  expect(screen.getByRole("button", { name: "启动终端 · demo" })).toBeEnabled();
});

it("terminates a shell whose tab was closed while native creation was pending", async () => {
  let finish!: (id: string) => void;
  api.terminalStart.mockReturnValueOnce(
    new Promise<string>((resolve) => {
      finish = resolve;
    }),
  );
  api.terminalClose.mockResolvedValueOnce(undefined);
  const view = render(
    <WorkspaceTerminal panel={useWorkspacePanels.getState().tabs[0]} />,
  );
  fireEvent.click(screen.getByRole("button", { name: "启动终端 · demo" }));
  expect(api.terminalStart).toHaveBeenCalledWith("root-1");
  act(() => useWorkspacePanels.getState().close("term"));
  view.unmount();
  await act(async () => {
    finish("pty-late");
  });
  expect(api.terminalClose).toHaveBeenCalledWith("pty-late");
  expect(useWorkspacePanels.getState().tabs).toHaveLength(0);
});

it("stages only captured terminal output after an explicit human click", async () => {
  api.terminalRead
    .mockResolvedValueOnce({
      data: "bnBtIHJ1biBjaGVjazpkb2NzDQrplJnor6/vvJrphY3nva7nvLrlpLENCg==",
      nextOffset: 40,
      dropped: false,
      ended: false,
    })
    .mockResolvedValueOnce({
      data: "",
      nextOffset: 40,
      dropped: false,
      ended: true,
    });
  render(
    <MemoryRouter>
      <WorkspaceTerminal
        panel={{
          id: "term",
          kind: "terminal",
          title: "终端 · demo",
          root,
          terminalId: "pty-secret-id",
        }}
      />
    </MemoryRouter>,
  );
  fireEvent.click(
    await screen.findByRole("button", { name: "交给智能体分析" }),
  );
  const pending = useAiWorkbenchHandoff.getState().pendingIssue;
  expect(pending).toMatchObject({
    label: "终端输出",
    route: "/ai?workspace=terminal",
    scopes: [],
    attachment: "terminal_output",
  });
  expect(pending?.prompt).toContain("错误：配置缺失");
  expect(pending?.prompt).not.toContain(root.path);
  expect(pending?.prompt).not.toContain("pty-secret-id");
  expect(api.terminalWrite).not.toHaveBeenCalled();
});
