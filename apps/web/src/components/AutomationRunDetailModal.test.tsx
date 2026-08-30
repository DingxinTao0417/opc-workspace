import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AutomationRunDetail } from "../types/models";
import { AutomationRunDetailModal } from "./AutomationRunDetailModal";

const hookMocks = vi.hoisted(() => ({
  details: new Map<string, unknown>(),
  query: vi.fn(),
  retry: {
    error: null as unknown,
    isPending: false,
    mutateAsync: vi.fn(),
    reset: vi.fn(),
  },
}));

vi.mock("../api/hooks", () => ({
  useAutomationRunQuery: (id: string | null) => hookMocks.query(id),
  useRetryAutomationRun: () => hookMocks.retry,
}));

function detail(
  overrides: Partial<AutomationRunDetail> = {},
): AutomationRunDetail {
  const value: AutomationRunDetail = {
    id: "run-1",
    ruleId: "rule-1",
    presetKey: "invoice-overdue-task",
    ruleName: "发票逾期跟进",
    ruleVersion: 3,
    triggerType: "event",
    sourceEventId: "event-1",
    scheduledFor: null,
    status: "succeeded",
    attempt: 1,
    retryOfRunId: null,
    retryable: false,
    retryAt: null,
    causedByRunId: null,
    causalDepth: 0,
    configSnapshot: { priority: "P0" },
    actionSnapshot: { action_type: "task", title: "跟进逾期发票" },
    errorCode: null,
    resultType: "task",
    resultId: "task/id ?#1",
    resultSummary: "已创建任务",
    startedAt: "2026-08-30T10:00:01Z",
    endedAt: "2026-08-30T10:00:01Z",
    source: {
      kind: "event",
      available: true,
      eventId: "event-1",
      aggregateType: "invoice",
      aggregateId: "invoice-1",
      action: "invoice_overdue",
      occurredAt: "2026-08-30T10:00:00Z",
      scheduledFor: null,
    },
    retryChain: [],
    ...overrides,
  };
  if (!overrides.retryChain) {
    value.retryChain = [
      {
        id: value.id,
        status: value.status,
        attempt: value.attempt,
        retryOfRunId: value.retryOfRunId,
        retryable: value.retryable,
        retryAt: value.retryAt,
        errorCode: value.errorCode,
        resultType: value.resultType,
        resultId: value.resultId,
        resultSummary: value.resultSummary,
        startedAt: value.startedAt,
        endedAt: value.endedAt,
      },
    ];
  }
  return value;
}

function renderModal(
  runId = "run-1",
  onOpenTask = vi.fn(),
  onOpenInboxItem = vi.fn(),
) {
  return render(
    <AutomationRunDetailModal
      onClose={vi.fn()}
      onOpenInboxItem={onOpenInboxItem}
      onOpenTask={onOpenTask}
      runId={runId}
    />,
  );
}

beforeEach(() => {
  hookMocks.details.clear();
  hookMocks.retry.error = null;
  hookMocks.retry.isPending = false;
  hookMocks.retry.mutateAsync.mockReset();
  hookMocks.retry.reset.mockReset();
  hookMocks.query.mockReset();
  hookMocks.query.mockImplementation((id: string | null) => ({
    data: id ? hookMocks.details.get(id) : undefined,
    error: null,
    isError: false,
    isPending: Boolean(id && !hookMocks.details.has(id)),
    refetch: vi.fn(),
  }));
});

afterEach(cleanup);

describe("AutomationRunDetailModal", () => {
  it("shows immutable event facts, snapshots, results and navigates attempts", async () => {
    const first = detail({
      id: "run-1",
      status: "failed",
      retryable: true,
      errorCode: "ACTION_WRITE_FAILED",
      resultType: null,
      resultId: null,
      resultSummary: "写入失败",
    });
    const second = detail({ id: "run-2", attempt: 2 });
    const chain = [first.retryChain[0], second.retryChain[0]];
    first.retryChain = chain;
    second.retryChain = chain;
    hookMocks.details.set(first.id, first);
    hookMocks.details.set(second.id, second);
    const onOpenTask = vi.fn();

    renderModal(first.id, onOpenTask);

    expect(
      screen.getByRole("dialog", { name: "自动化运行详情" }),
    ).toBeVisible();
    expect(
      screen.getByText((_, element) =>
        Boolean(element?.textContent === "invoice · invoice_overdue"),
      ),
    ).toBeVisible();
    expect(screen.getByText("event-1")).toBeVisible();
    expect(screen.getByText(/"priority": "P0"/)).toBeVisible();
    expect(screen.getByText(/"action_type": "task"/)).toBeVisible();
    expect(screen.getAllByText("ACTION_WRITE_FAILED")).toHaveLength(2);

    fireEvent.click(screen.getByRole("button", { name: /第 2 次/ }));
    await waitFor(() => expect(hookMocks.query).toHaveBeenCalledWith("run-2"));
    fireEvent.click(await screen.findByRole("button", { name: "打开任务" }));
    expect(onOpenTask).toHaveBeenCalledWith("task/id ?#1");
  });

  it("shows schedule and reminder facts without inventing reminder navigation", () => {
    hookMocks.details.set(
      "schedule-run",
      detail({
        id: "schedule-run",
        presetKey: "daily-today-reminder",
        ruleName: "每日查看今日任务",
        triggerType: "schedule",
        sourceEventId: null,
        scheduledFor: "2026-08-30T09:00:00Z",
        resultType: "reminder",
        resultId: "reminder-1",
        resultSummary: "已创建提醒",
        source: {
          kind: "schedule",
          available: true,
          eventId: null,
          aggregateType: null,
          aggregateId: null,
          action: null,
          occurredAt: null,
          scheduledFor: "2026-08-30T09:00:00Z",
        },
      }),
    );

    renderModal("schedule-run");

    expect(screen.getByText("计划触发")).toBeVisible();
    expect(screen.getAllByText("已创建提醒")).toHaveLength(2);
    expect(screen.queryByRole("button", { name: /打开提醒/ })).toBeNull();
  });

  it("opens inbox results and selects the new attempt after retry succeeds", async () => {
    const failed = detail({
      id: "failed-run",
      status: "failed",
      retryable: true,
      retryAt: "2026-08-30T10:05:00Z",
      errorCode: "ACTION_WRITE_FAILED",
      resultType: null,
      resultId: null,
      resultSummary: "写入失败",
    });
    const retried = detail({
      id: "retried-run",
      attempt: 2,
      retryOfRunId: failed.id,
      resultType: "inbox_item",
      resultId: "inbox/item ?#2",
      resultSummary: "已创建事项",
    });
    const chain = [failed.retryChain[0], retried.retryChain[0]];
    failed.retryChain = chain;
    retried.retryChain = chain;
    hookMocks.details.set(failed.id, failed);
    hookMocks.details.set(retried.id, retried);
    hookMocks.retry.mutateAsync.mockResolvedValue(retried);
    const onOpenInboxItem = vi.fn();

    renderModal(failed.id, vi.fn(), onOpenInboxItem);
    fireEvent.click(screen.getByRole("button", { name: "重试本次运行" }));

    await waitFor(() => {
      expect(hookMocks.retry.mutateAsync).toHaveBeenCalledWith(failed.id);
      expect(hookMocks.query).toHaveBeenCalledWith(retried.id);
    });
    fireEvent.click(
      await screen.findByRole("button", { name: "打开收件箱事项" }),
    );
    expect(onOpenInboxItem).toHaveBeenCalledWith("inbox/item ?#2");
  });

  it("clears a retry error before switching to another attempt", async () => {
    const first = detail({
      id: "failed-run",
      status: "failed",
      retryable: true,
      errorCode: "ACTION_WRITE_FAILED",
      resultType: null,
      resultId: null,
      resultSummary: "写入失败",
    });
    const second = detail({ id: "successful-run", attempt: 2 });
    const chain = [first.retryChain[0], second.retryChain[0]];
    first.retryChain = chain;
    second.retryChain = chain;
    hookMocks.details.set(first.id, first);
    hookMocks.details.set(second.id, second);
    const view = renderModal(first.id);

    hookMocks.retry.error = new Error("retry failed");
    view.rerender(
      <AutomationRunDetailModal
        onClose={vi.fn()}
        onOpenInboxItem={vi.fn()}
        onOpenTask={vi.fn()}
        runId={first.id}
      />,
    );
    expect(screen.getByRole("alert")).toHaveTextContent("自动化运行详情");
    hookMocks.retry.reset.mockClear();
    hookMocks.retry.reset.mockImplementation(() => {
      hookMocks.retry.error = null;
    });

    fireEvent.click(screen.getByRole("button", { name: /第 2 次/ }));

    await waitFor(() => {
      expect(hookMocks.retry.reset).toHaveBeenCalledTimes(1);
      expect(hookMocks.query).toHaveBeenCalledWith(second.id);
      expect(screen.queryByRole("alert")).toBeNull();
    });
  });

  it("retains cached immutable facts when a background refresh fails", () => {
    const cached = detail();
    const refetch = vi.fn();
    hookMocks.query.mockReturnValue({
      data: cached,
      error: new Error("offline"),
      isError: true,
      isPending: false,
      refetch,
    });

    renderModal(cached.id);

    expect(screen.getByRole("alert")).toHaveTextContent(
      "运行详情刷新失败，当前显示上次成功读取的不可变记录",
    );
    expect(screen.getByText("发票逾期跟进")).toBeVisible();
    expect(screen.getByText(/"priority": "P0"/)).toBeVisible();
    expect(screen.queryByText("无法读取运行详情")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "重试刷新" }));
    expect(refetch).toHaveBeenCalledTimes(1);
  });

  it("keeps loading and errors explicit", () => {
    hookMocks.query.mockReturnValue({
      data: undefined,
      error: new Error("offline"),
      isError: true,
      isPending: false,
      refetch: vi.fn(),
    });

    renderModal("missing-run");
    expect(screen.getByRole("alert")).toHaveTextContent(
      "无法读取自动化运行详情",
    );
    expect(screen.getByRole("button", { name: "重试" })).toBeVisible();
  });

  it("announces loading without rendering stale detail facts", () => {
    hookMocks.query.mockReturnValue({
      data: undefined,
      error: null,
      isError: false,
      isPending: true,
      refetch: vi.fn(),
    });

    renderModal("loading-run");
    expect(screen.getByRole("status")).toHaveTextContent(
      "正在读取不可变运行记录…",
    );
    expect(screen.queryByText("不可变执行快照")).toBeNull();
  });
});
