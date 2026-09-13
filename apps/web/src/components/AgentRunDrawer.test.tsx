import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getTask, getTaskAgentRuns } from "../api/client";
import { useUiStore } from "../store/ui";
import type { AgentRun, Task } from "../types/models";
import { AgentRunDrawer } from "./AgentRunDrawer";

vi.mock("../api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/client")>()),
  getTask: vi.fn(),
  getTaskAgentRuns: vi.fn(),
}));

const task = { id: "task-1", title: "整理季度报告" } as Task;
const completedRun: AgentRun = {
  id: "run-2",
  taskId: task.id,
  assignmentId: "assignment-1",
  actorId: "agent-1",
  adapterId: "adapter-1",
  createdByActorId: "owner-1",
  parentRunId: "run-1",
  attempt: 2,
  status: "succeeded",
  providerId: "provider-1",
  model: "deepseek-test",
  resultText: "<script>alert('unsafe')</script>\n季度报告正文",
  resultBytes: 60,
  errorCode: null,
  startedAt: "2026-09-12T12:00:01Z",
  completedAt: "2026-09-12T12:01:00Z",
  createdAt: "2026-09-12T12:00:00Z",
};
const failedRun: AgentRun = {
  ...completedRun,
  id: "run-1",
  parentRunId: null,
  attempt: 1,
  status: "failed",
  resultText: null,
  resultBytes: null,
  errorCode: "MODEL_UNAVAILABLE",
};

function renderDrawer(runId: string | null = completedRun.id) {
  useUiStore.setState({ agentRunDrawer: { taskId: task.id, runId } });
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  const result = render(
    <QueryClientProvider client={queryClient}>
      <AgentRunDrawer />
    </QueryClientProvider>,
  );
  return { ...result, queryClient };
}

beforeEach(() => {
  vi.resetAllMocks();
  useUiStore.setState({ agentRunDrawer: null, taskDetailId: null });
  vi.mocked(getTask).mockResolvedValue(task);
  vi.mocked(getTaskAgentRuns).mockResolvedValue([completedRun, failedRun]);
});

afterEach(() => {
  cleanup();
  useUiStore.setState({ agentRunDrawer: null, taskDetailId: null });
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("AgentRunDrawer", () => {
  it("shows service facts and treats result HTML as plain text without execution controls", async () => {
    renderDrawer();
    const dialog = await screen.findByRole("dialog", {
      name: "执行过程",
    });
    expect(await within(dialog).findByText(task.title)).toBeVisible();
    expect(
      await within(dialog).findByText(completedRun.model, { selector: "dd" }),
    ).toBeVisible();
    expect(
      within(dialog).getByText("第 2 次", { selector: "dd" }),
    ).toBeVisible();
    expect(within(dialog).getByText("创建时间")).toBeVisible();
    expect(within(dialog).getByText("开始时间")).toBeVisible();
    expect(within(dialog).getByText("结束时间")).toBeVisible();
    expect(dialog.querySelector("pre")?.textContent).toBe(
      completedRun.resultText,
    );
    expect(dialog.querySelector("script, iframe")).toBeNull();
    expect(
      within(dialog).queryByRole("button", { name: /启动执行|重试|取消执行/ }),
    ).not.toBeInTheDocument();
  });

  it("selects a historical run and shows its error instead of another run's result", async () => {
    renderDrawer();
    fireEvent.change(
      await screen.findByRole("combobox", { name: "执行记录" }),
      { target: { value: failedRun.id } },
    );
    expect(await screen.findByText("MODEL_UNAVAILABLE")).toBeVisible();
    expect(screen.getByText("此次执行没有可用文本产出。")).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "下载文本" }),
    ).not.toBeInTheDocument();
    expect(useUiStore.getState().agentRunDrawer?.runId).toBe(failedRun.id);
  });

  it("closes the drawer with its accessible close control", async () => {
    renderDrawer();
    fireEvent.click(
      await screen.findByRole("button", { name: "收起执行过程" }),
    );
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
    expect(useUiStore.getState().agentRunDrawer).toBeNull();
  });

  it("opens the task and closes only the run drawer", async () => {
    renderDrawer();
    fireEvent.click(await screen.findByRole("button", { name: "打开任务" }));
    expect(useUiStore.getState().taskDetailId).toBe(task.id);
    expect(useUiStore.getState().agentRunDrawer).toBeNull();
  });

  it("shows a loading state until records arrive", async () => {
    let resolveRuns!: (runs: AgentRun[]) => void;
    vi.mocked(getTaskAgentRuns).mockReturnValue(
      new Promise((resolve) => {
        resolveRuns = resolve;
      }),
    );
    renderDrawer();
    expect(screen.getByText("正在读取执行记录…")).toBeVisible();
    await act(async () => resolveRuns([completedRun]));
    expect(
      await screen.findByRole("button", { name: "下载文本" }),
    ).toBeVisible();
  });

  it("shows an empty state and no invented run result", async () => {
    vi.mocked(getTaskAgentRuns).mockResolvedValue([]);
    renderDrawer(null);
    expect(await screen.findByText("此任务尚无执行记录。")).toBeVisible();
    expect(
      screen.queryByRole("combobox", { name: "执行记录" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "下载文本" }),
    ).not.toBeInTheDocument();
  });

  it("can refresh a failed record request", async () => {
    vi.mocked(getTaskAgentRuns).mockRejectedValue(new Error("offline"));
    renderDrawer();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "无法读取最新执行记录",
    );
    vi.mocked(getTaskAgentRuns).mockResolvedValue([completedRun]);
    fireEvent.click(screen.getByRole("button", { name: "刷新执行记录" }));
    expect(
      await screen.findByRole("button", { name: "下载文本" }),
    ).toBeVisible();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("does not silently substitute another run when the requested run is missing", async () => {
    renderDrawer("missing-run");
    expect(
      await screen.findByText("所选执行记录已不可用，请选择其他历史记录。"),
    ).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "下载文本" }),
    ).not.toBeInTheDocument();
  });

  it("downloads HTML-looking output as a plain text file", async () => {
    const createObjectURL = vi.fn((_blob: Blob) => "blob:run-result");
    const revokeObjectURL = vi.fn();
    vi.stubGlobal(
      "URL",
      class extends URL {
        static createObjectURL = createObjectURL;
        static revokeObjectURL = revokeObjectURL;
      },
    );
    let downloadedName = "";
    vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (
      this: HTMLAnchorElement,
    ) {
      downloadedName = this.download;
    });
    renderDrawer();
    fireEvent.click(await screen.findByRole("button", { name: "下载文本" }));
    expect(createObjectURL).toHaveBeenCalledOnce();
    expect(createObjectURL.mock.calls[0]?.[0]).toHaveProperty(
      "type",
      "text/plain;charset=utf-8",
    );
    expect(downloadedName).toBe("整理季度报告-第2次.txt");
    await waitFor(() =>
      expect(revokeObjectURL).toHaveBeenCalledWith("blob:run-result"),
    );
  });
});
