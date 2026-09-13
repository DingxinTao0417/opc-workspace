import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import { RightOverview } from "./RightOverview";
import { useUiStore } from "../store/ui";

const mocks = vi.hoisted(() => ({
  knowledge: vi.fn(),
  openExternal: vi.fn(),
}));

vi.mock("../api/client", () => ({
  cancelAgentRun: vi.fn(),
  getKnowledgeSources: mocks.knowledge,
  retryAgentRun: vi.fn(),
}));

vi.mock("../api/desktop", () => ({
  openExternalBrowserWindow: mocks.openExternal,
}));

vi.mock("../api/hooks", () => ({
  useAgentRunsQuery: () => ({
    data: {
      items: [
        {
          id: "run-1",
          taskId: "task-1",
          taskTitle: "落地页任务",
          status: "succeeded",
          attempt: 1,
          model: "local-model",
          resultText: "交付正文",
          errorCode: null,
          startedAt: "2026-09-13T01:00:00Z",
          completedAt: "2026-09-13T01:05:00Z",
          createdAt: "2026-09-13T01:00:00Z",
        },
      ],
      meta: { page: 1, pageSize: 50, total: 1 },
    },
    isError: false,
    isPending: false,
    refetch: vi.fn(),
  }),
  useAiProvidersQuery: () => ({ data: [{ id: "provider-1" }], isError: false }),
  useBackupsQuery: () => ({
    data: [{ id: "backup-1", createdAt: "2026-09-12T02:00:00Z" }],
    isError: false,
  }),
  useControlledFilesQuery: () => ({
    data: {
      items: [
        {
          id: "artifact-1",
          scope: "artifact",
          name: "报告.md",
          mimeType: "text/markdown",
          sizeBytes: 10,
          sha256: "ab",
          ownerLabel: "落地页任务",
          contentRoute: "/api/v1/artifacts/artifact-1",
          updatedAt: "2026-09-12T02:00:00Z",
        },
      ],
      meta: { page: 1, pageSize: 50, total: 1 },
    },
    isError: false,
    isPending: false,
    refetch: vi.fn(),
  }),
  useHealthQuery: () => ({
    data: {
      status: "ok",
      app: { name: "opc-workspace", version: "0.1.1", commit: "abc" },
      api: { version: "v1" },
      schema: { version: 71 },
    },
    isError: false,
  }),
  useInboxStatsQuery: () => ({
    data: { pending: 4, unread: 2 },
    isError: false,
  }),
  useRemindersQuery: () => ({
    data: { items: [{ id: "reminder-1", triggerAt: "2026-09-14T01:30:00Z" }] },
    isError: false,
  }),
  useTaskArtifactQuery: () => ({
    data: { contentText: "预览正文" },
    isError: false,
    isPending: false,
  }),
  useTodayStatsQuery: () => ({
    data: {
      tasks: { total: 5, completed: 2, overdue: 1 },
      focus: { sessions: 3, minutes: 45 },
    },
    isError: false,
  }),
}));

function renderOverview() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  mocks.knowledge.mockResolvedValue({
    items: [],
    meta: { page: 1, pageSize: 1, total: 7 },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <RightOverview />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  cleanup();
  useUiStore.setState({ rightPanelTab: "summary" });
});

describe("RightOverview summary tab", () => {
  it("exposes a stable landmark and the tab bar", () => {
    renderOverview();

    expect(screen.getByLabelText("今日概览")).toHaveAttribute(
      "id",
      "right-overview",
    );
    for (const label of ["概要", "子智能体", "文件", "浏览器"]) {
      expect(screen.getByRole("tab", { name: label })).toBeInTheDocument();
    }
    expect(screen.getByRole("tab", { name: "概要" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
  });

  it("shows version, backup, task, focus and inbox facts as compact rows", async () => {
    renderOverview();

    expect(screen.getByText("v0.1.1 · API v1 · schema 71")).toBeInTheDocument();
    expect(screen.getByText("2/5 · 逾期1")).toBeInTheDocument();
    expect(screen.getByText("4 / 2")).toBeInTheDocument();
    expect(await screen.findByText("7 个来源")).toBeInTheDocument();
  });
});

describe("RightOverview agents tab", () => {
  it("lists runs and opens a run detail with its deliverable", () => {
    renderOverview();

    fireEvent.click(screen.getByRole("tab", { name: "子智能体" }));
    fireEvent.click(screen.getByRole("button", { name: /落地页任务/ }));

    expect(screen.getByText("交付正文")).toBeInTheDocument();
    expect(screen.getByText(/状态 succeeded/)).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "打开任务" }),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /返回列表/ }));
    expect(screen.queryByText("交付正文")).toBeNull();
  });
});

describe("RightOverview files tab", () => {
  it("lists scoped files and previews artifact text", () => {
    renderOverview();

    fireEvent.click(screen.getByRole("tab", { name: "文件" }));
    expect(screen.getByRole("button", { name: /报告.md/ })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /报告.md/ }));
    expect(screen.getByText("预览正文")).toBeInTheDocument();
    expect(screen.getByText(/SHA-256 ab/)).toBeInTheDocument();
  });
});

describe("RightOverview browser tab", () => {
  it("embeds loopback addresses in-panel", () => {
    renderOverview();

    fireEvent.click(screen.getByRole("tab", { name: "浏览器" }));
    fireEvent.click(screen.getByRole("button", { name: "打开地址" }));

    expect(screen.getByTitle("内置浏览器预览")).toHaveAttribute(
      "src",
      "http://127.0.0.1:5173",
    );
    expect(mocks.openExternal).not.toHaveBeenCalled();
  });

  it("delegates external addresses to the native window", () => {
    mocks.openExternal.mockResolvedValue(true);
    renderOverview();

    fireEvent.click(screen.getByRole("tab", { name: "浏览器" }));
    fireEvent.change(screen.getByPlaceholderText("http://127.0.0.1:5173"), {
      target: { value: "https://example.com/docs" },
    });
    fireEvent.click(screen.getByRole("button", { name: "打开地址" }));

    expect(mocks.openExternal).toHaveBeenCalledWith("https://example.com/docs");
    expect(screen.queryByTitle("内置浏览器预览")).toBeNull();
  });
});
