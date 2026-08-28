import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ProjectWorkflowEvent } from "../types/models";
import { ProjectEventsSection } from "./ProjectEventsSection";

const apiMocks = vi.hoisted(() => ({ getProjectEvents: vi.fn() }));

vi.mock("../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../api/client")>("../api/client");
  return { ...actual, ...apiMocks };
});

const event: ProjectWorkflowEvent = {
  id: "event-1",
  action: "project_paused",
  actor: {
    id: "owner-1",
    type: "owner",
    displayName: "我",
    status: "active",
    isBuiltin: true,
    version: 1,
  },
  requestId: "request-1",
  previous: { status: "in_progress", version: 3 },
  current: { status: "paused", version: 4 },
  createdAt: "2026-08-28T10:00:00Z",
};

function renderSection() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <ProjectEventsSection projectId="project-1" />
    </QueryClientProvider>,
  );
}

describe("ProjectEventsSection", () => {
  beforeEach(() => {
    apiMocks.getProjectEvents.mockResolvedValue({
      items: [event],
      meta: { page: 1, pageSize: 20, total: 1, projectVersion: 4 },
    });
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("loads the current project timeline and translates a status change", async () => {
    renderSection();

    expect(await screen.findByText("暂停项目")).toBeVisible();
    expect(screen.getByText("进行中 → 已暂停")).toBeVisible();
    expect(screen.getByText(/我/)).toBeVisible();
    expect(apiMocks.getProjectEvents).toHaveBeenCalledWith("project-1", {
      page: 1,
      pageSize: 20,
    });
  });

  it("summarizes edited fields without exposing raw snapshots", async () => {
    apiMocks.getProjectEvents.mockResolvedValue({
      items: [
        {
          ...event,
          action: "project_updated",
          previous: {
            name: "旧名称",
            description: "旧说明",
            status: "planning",
          },
          current: {
            name: "新名称",
            description: "新说明",
            status: "planning",
          },
        },
      ],
      meta: { page: 1, pageSize: 20, total: 1, projectVersion: 2 },
    });
    renderSection();

    expect(await screen.findByText("修改项目资料")).toBeVisible();
    expect(screen.getByText("变更：名称、说明")).toBeVisible();
    expect(screen.queryByText("旧名称")).toBeNull();
  });

  it("loads older pages without duplicating events", async () => {
    apiMocks.getProjectEvents.mockImplementation(
      async (_projectId: string, input: { page: number }) => ({
        items: [
          input.page === 1
            ? event
            : { ...event, id: "event-older", action: "project_created" },
        ],
        meta: {
          page: input.page,
          pageSize: 1,
          total: 2,
          projectVersion: 4,
        },
      }),
    );
    renderSection();
    fireEvent.click(await screen.findByRole("button", { name: "加载更早" }));

    expect(await screen.findByText("创建项目")).toBeVisible();
    await waitFor(() =>
      expect(apiMocks.getProjectEvents).toHaveBeenLastCalledWith(
        "project-1",
        expect.objectContaining({ page: 2 }),
      ),
    );
  });

  it("keeps a retryable timeline error isolated from the project page", async () => {
    apiMocks.getProjectEvents.mockRejectedValue(new Error("offline"));
    renderSection();

    expect(
      await screen.findByText(
        "活动记录读取失败；项目其他内容不受影响。",
        {},
        { timeout: 3_000 },
      ),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "重试" })).toBeVisible();
  });
});
