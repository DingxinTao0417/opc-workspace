import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import { RightFloatingCard } from "./RightFloatingCard";
import { useUiStore } from "../store/ui";

const mocks = vi.hoisted(() => ({
  knowledge: vi.fn(),
}));

vi.mock("../api/client", () => ({
  getKnowledgeSources: mocks.knowledge,
}));

vi.mock("../api/hooks", () => ({
  useAgentRunsQuery: () => ({
    data: {
      items: [
        {
          id: "run-1",
          status: "running",
          outputDeliveryStatus: "not_ready",
          taskTitle: "甲",
        },
        {
          id: "run-pending",
          status: "running",
          outputDeliveryStatus: "pending",
          taskTitle: "待登记",
        },
        {
          id: "run-2",
          status: "succeeded",
          outputDeliveryStatus: "submitted",
          taskTitle: "乙",
        },
        {
          id: "run-3",
          status: "succeeded",
          outputDeliveryStatus: "retained",
          taskTitle: "丙",
        },
      ],
      meta: {
        page: 1,
        pageSize: 50,
        total: 4,
        activeTotal: 87,
        pendingDeliveryTotal: 3,
        succeededTotal: 123,
      },
    },
    isError: false,
    isPending: false,
  }),
  useAiEvaluationsQuery: () => ({
    data: { items: [], meta: { page: 1, pageSize: 20, total: 4 } },
    isError: false,
  }),
  useAiProvidersQuery: () => ({ data: [{ id: "p1" }], isError: false }),
  useActiveFocusSessionQuery: () => ({
    data: { session: { id: "s1", status: "active" } },
    isError: false,
  }),
  useBackupsQuery: () => ({
    data: [{ id: "b1", createdAt: "2026-09-12T02:00:00Z" }],
    isError: false,
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
}));

function renderCard() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  mocks.knowledge.mockResolvedValue({
    items: [{ id: "k1", status: "indexing" }],
    meta: { page: 1, pageSize: 100, total: 9 },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <RightFloatingCard />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  useUiStore.setState({
    rightOverviewCollapsed: true,
    rightPanelTab: "summary",
    rightPanelRequestMode: "open",
  });
});

afterEach(() => {
  cleanup();
  useUiStore.setState({
    rightOverviewCollapsed: false,
    rightPanelTab: "summary",
    rightPanelRequestMode: "open",
  });
});

describe("RightFloatingCard", () => {
  it("groups environment, agents, background jobs and sources", async () => {
    renderCard();

    for (const title of ["环境信息", "Agent 执行", "后台进程", "来源"]) {
      expect(screen.getByText(title)).toBeInTheDocument();
    }
    expect(screen.getByText("v0.1.1 · 71")).toBeInTheDocument();
    expect(
      screen.getByText("87 运行 · 3 待登记 · 123 完成"),
    ).toBeInTheDocument();
    expect(await screen.findByText(/专注 · 索引 1/)).toBeInTheDocument();
    expect(await screen.findByText("9 来源")).toBeInTheDocument();
  });

  it("opens the docked panel on the agents tab when clicked", () => {
    renderCard();

    fireEvent.click(screen.getByRole("button", { name: /Agent Runs/ }));

    expect(useUiStore.getState().rightPanelTab).toBe("agents");
    expect(useUiStore.getState().rightOverviewCollapsed).toBe(false);
  });
});
