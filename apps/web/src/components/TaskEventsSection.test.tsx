import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { TaskWorkflowEvent } from "../types/models";
import { TaskEventsSection } from "./TaskEventsSection";

const apiMocks = vi.hoisted(() => ({ getTaskEvents: vi.fn() }));

vi.mock("../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../api/client")>("../api/client");
  return { ...actual, ...apiMocks };
});

const event: TaskWorkflowEvent = {
  id: "event-1",
  action: "task_completed",
  actor: {
    id: "actor-owner",
    type: "owner",
    displayName: "我",
    status: "active",
    isBuiltin: true,
    version: 1,
  },
  assignmentId: null,
  submissionId: null,
  artifactId: null,
  requestId: "request-1",
  commandSeq: 2,
  previous: { status: "in_progress", version: 4 },
  current: { status: "done", version: 5 },
  reason: null,
  createdAt: "2026-08-27T10:00:00Z",
};

function renderSection() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <TaskEventsSection taskId="task-1" />
    </QueryClientProvider>,
  );
}

describe("TaskEventsSection", () => {
  beforeEach(() => {
    apiMocks.getTaskEvents.mockResolvedValue({
      items: [event],
      meta: { page: 1, pageSize: 20, total: 1, taskVersion: 5 },
    });
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("loads the timeline on demand and translates status transitions", async () => {
    renderSection();
    expect(apiMocks.getTaskEvents).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "查看记录" }));

    expect(await screen.findByText("完成任务")).toBeVisible();
    expect(screen.getByText("进行中 → 已完成")).toBeVisible();
    expect(screen.getByText(/我/)).toBeVisible();
  });

  it("labels a task created by automation instead of using the generic fallback", async () => {
    apiMocks.getTaskEvents.mockResolvedValue({
      items: [
        {
          ...event,
          action: "task_created_from_automation",
          actor: null,
          commandSeq: null,
          previous: null,
          current: { status: "todo", version: 1 },
        },
      ],
      meta: { page: 1, pageSize: 20, total: 1, taskVersion: 1 },
    });
    renderSection();

    fireEvent.click(screen.getByRole("button", { name: "查看记录" }));

    expect(await screen.findByText("自动化创建任务")).toBeVisible();
    expect(screen.queryByText("任务记录已更新")).toBeNull();
  });

  it("translates internal assignment reasons and migration events", async () => {
    apiMocks.getTaskEvents.mockResolvedValue({
      items: [
        {
          ...event,
          id: "event-ended",
          action: "assignment_ended",
          reason: "Task cancelled",
          previous: {},
          current: { reason: "Task cancelled" },
        },
        {
          ...event,
          id: "event-migration",
          action: "migration_assignment_backfill",
          commandSeq: null,
          reason: "schema_v7_migration_inferred_owner",
          previous: null,
          current: { inferred: true },
        },
        {
          ...event,
          id: "event-submission-migration",
          action: "migration_submission_backfill",
          submissionId: "submission-1",
          commandSeq: null,
          reason: "schema_v9_migration_inferred_submission",
          previous: null,
          current: { inferred: true },
        },
        {
          ...event,
          id: "event-output",
          action: "task_output_submitted",
          submissionId: "submission-2",
          previous: { status: "in_progress" },
          current: { status: "waiting_review" },
        },
      ],
      meta: { page: 1, pageSize: 20, total: 4, taskVersion: 5 },
    });
    renderSection();
    fireEvent.click(screen.getByRole("button", { name: "查看记录" }));

    expect(await screen.findByText("原因：任务取消后自动结束")).toBeVisible();
    expect(screen.getByText("迁移推定历史负责人")).toBeVisible();
    expect(screen.getByText("迁移推定历史提交")).toBeVisible();
    expect(screen.getByText("提交任务产出")).toBeVisible();
    expect(screen.queryByText(/Task cancelled/)).toBeNull();
    expect(screen.queryByText(/schema_v7/)).toBeNull();
  });

  it("presents child-rollup lifecycle events and internal reasons", async () => {
    const systemActor = {
      ...event.actor!,
      id: "actor-system",
      type: "system" as const,
      displayName: "系统",
    };
    apiMocks.getTaskEvents.mockResolvedValue({
      items: [
        {
          ...event,
          id: "event-parent-review",
          action: "task_parent_review_requested",
          actor: systemActor,
          previous: { status: "in_progress", version: 6 },
          current: { status: "waiting_review", version: 7 },
        },
        {
          ...event,
          id: "event-parent-withdrawn",
          action: "task_parent_review_withdrawn",
          actor: systemActor,
          previous: { status: "waiting_review", version: 7 },
          current: { status: "in_progress", version: 8 },
          reason: "child_rollup_gate_lost",
        },
        {
          ...event,
          id: "event-parent-reopened",
          action: "task_parent_reopened",
          actor: systemActor,
          previous: { status: "done", version: 8 },
          current: { status: "todo", version: 9 },
          reason: "child_rollup_invalidated",
        },
      ],
      meta: { page: 1, pageSize: 20, total: 3, taskVersion: 9 },
    });
    renderSection();
    fireEvent.click(screen.getByRole("button", { name: "查看记录" }));

    expect(
      await screen.findByText("子任务完成，系统发起父任务验收"),
    ).toBeVisible();
    expect(
      screen.getByText("子任务条件变化，系统撤回父任务验收"),
    ).toBeVisible();
    expect(
      screen.getByText("子任务条件变化，系统重新打开父任务"),
    ).toBeVisible();
    expect(
      screen.getByText("原因：父任务负责人或审核人门禁不再满足"),
    ).toBeVisible();
    expect(screen.getByText("原因：非取消子任务不再全部完成")).toBeVisible();
  });

  it("loads older pages without duplicating events", async () => {
    apiMocks.getTaskEvents.mockImplementation(
      async (_taskId: string, input: { page: number }) => ({
        items: [
          input.page === 1
            ? event
            : { ...event, id: "event-older", action: "task_started" },
        ],
        meta: { page: input.page, pageSize: 1, total: 2, taskVersion: 5 },
      }),
    );
    renderSection();
    fireEvent.click(screen.getByRole("button", { name: "查看记录" }));
    fireEvent.click(await screen.findByRole("button", { name: "加载更早" }));

    expect(await screen.findByText("开始执行任务")).toBeVisible();
    await waitFor(() =>
      expect(apiMocks.getTaskEvents).toHaveBeenLastCalledWith(
        "task-1",
        expect.objectContaining({ page: 2 }),
      ),
    );
  });

  it("shows an independent retryable error", async () => {
    apiMocks.getTaskEvents.mockRejectedValue(new Error("offline"));
    renderSection();
    fireEvent.click(screen.getByRole("button", { name: "查看记录" }));

    expect(
      await screen.findByText(
        "活动记录读取失败；任务详情仍可继续查看。",
        {},
        { timeout: 3_000 },
      ),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "重试" })).toBeVisible();
  });
});
