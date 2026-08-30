import { afterEach, describe, expect, it, vi } from "vitest";
import {
  disableAutomationRule,
  enableAutomationRule,
  getAutomationRules,
  getAutomationRun,
  getAutomationRuns,
  normalizeAutomationRunDetail,
  previewAutomationRule,
  resetRuntimeConnection,
  retryAutomationRun,
  updateAutomationRule,
} from "./client";

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function rulePayload(overrides: Record<string, unknown> = {}) {
  return {
    id: "00000000-0000-5000-8000-000000000102",
    preset_key: "daily-today-reminder",
    name: "每日查看今日任务",
    description: "每天指定当地时间创建一条应用内提醒。",
    status: "disabled",
    available: true,
    trigger_type: "schedule",
    trigger_label: "每天指定当地时间",
    action_type: "reminder",
    action_label: "创建“查看今日任务”本地提醒",
    config: { local_time: "09:00", timezone: "Asia/Shanghai" },
    next_run_at: null,
    permissions: ["读取本地时间", "创建一条本地 Reminder"],
    version: 1,
    created_at: "2026-08-29T10:00:00Z",
    updated_at: "2026-08-29T10:00:00Z",
    ...overrides,
  };
}

function runPayload(overrides: Record<string, unknown> = {}) {
  return {
    id: "018f0000-0000-7000-8000-000000001601",
    rule_id: "00000000-0000-5000-8000-000000000102",
    preset_key: "daily-today-reminder",
    rule_name: "每日查看今日任务",
    rule_version: 2,
    trigger_type: "schedule",
    source_event_id: null,
    scheduled_for: "2026-08-29T01:00:00Z",
    status: "succeeded",
    attempt: 1,
    retry_of_run_id: null,
    retryable: false,
    retry_at: null,
    caused_by_run_id: null,
    causal_depth: 0,
    config_snapshot: { local_time: "09:00", timezone: "Asia/Shanghai" },
    action_snapshot: { action_type: "reminder", title: "查看今日任务" },
    error_code: null,
    result_type: "reminder",
    result_id: "018f0000-0000-7000-8000-000000001602",
    result_summary: "已创建本地提醒。",
    started_at: "2026-08-29T01:00:01Z",
    ended_at: "2026-08-29T01:00:01Z",
    ...overrides,
  };
}

function attemptSummaryPayload(run = runPayload()) {
  return {
    id: run.id,
    status: run.status,
    attempt: run.attempt,
    retry_of_run_id: run.retry_of_run_id,
    retryable: run.retryable,
    retry_at: run.retry_at,
    error_code: run.error_code,
    result_type: run.result_type,
    result_id: run.result_id,
    result_summary: run.result_summary,
    started_at: run.started_at,
    ended_at: run.ended_at,
  };
}

function runDetailPayload(
  runOverrides: Record<string, unknown> = {},
  detailOverrides: Record<string, unknown> = {},
) {
  const run = runPayload(runOverrides);
  const source =
    run.trigger_type === "event"
      ? {
          kind: "event",
          available: true,
          event_id: run.source_event_id,
          aggregate_type: "invoice",
          aggregate_id: "018f0000-0000-7000-8000-000000001603",
          action: "invoice_overdue",
          occurred_at: "2026-08-29T01:00:00Z",
          scheduled_for: null,
        }
      : {
          kind: "schedule",
          available: true,
          event_id: null,
          aggregate_type: null,
          aggregate_id: null,
          action: null,
          occurred_at: null,
          scheduled_for: run.scheduled_for,
        };
  return {
    ...run,
    source,
    retry_chain: [attemptSummaryPayload(run)],
    ...detailOverrides,
  };
}

afterEach(() => {
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

describe("Automation API contract", () => {
  it("normalizes rules and run history", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ data: [rulePayload()] }))
      .mockResolvedValueOnce(
        jsonResponse({
          data: [runPayload()],
          meta: { page: 1, page_size: 20, total: 1 },
        }),
      );
    vi.stubGlobal("fetch", fetchMock);

    const rules = await getAutomationRules();
    const runs = await getAutomationRuns({ status: "succeeded" });
    expect(rules[0]).toMatchObject({
      presetKey: "daily-today-reminder",
      triggerType: "schedule",
      config: { localTime: "09:00", timezone: "Asia/Shanghai" },
    });
    expect(runs.items[0]).toMatchObject({
      ruleName: "每日查看今日任务",
      status: "succeeded",
      resultType: "reminder",
    });
    const runUrl = new URL(String(fetchMock.mock.calls[1][0]), "http://local");
    expect(Object.fromEntries(runUrl.searchParams)).toEqual({
      page: "1",
      page_size: "20",
      status: "succeeded",
    });
  });

  it("encodes run filters and pagination and forwards cancellation", async () => {
    const controller = new AbortController();
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, init?: RequestInit) => {
        controller.abort();
        expect(init?.signal?.aborted).toBe(true);
        return jsonResponse({
          data: [runPayload({ status: "failed" })],
          meta: { page: 3, page_size: 7, total: 15 },
        });
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    await getAutomationRuns(
      {
        ruleId: "00000000-0000-5000-8000-000000000102",
        status: "failed",
        page: 3,
        pageSize: 7,
      },
      controller.signal,
    );

    const url = new URL(String(fetchMock.mock.calls[0][0]), "http://local");
    expect(Object.fromEntries(url.searchParams)).toEqual({
      page: "3",
      page_size: "7",
      rule_id: "00000000-0000-5000-8000-000000000102",
      status: "failed",
    });
  });

  it("normalizes an encoded run detail request and its immutable audit facts", async () => {
    const id = "run/id with space";
    const controller = new AbortController();
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, init?: RequestInit) => {
        controller.abort();
        expect(init?.signal?.aborted).toBe(true);
        return jsonResponse({ data: runDetailPayload({ id }) });
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    const detail = await getAutomationRun(id, controller.signal);

    expect(fetchMock.mock.calls[0][0]).toBe(
      "/api/v1/automations/runs/run%2Fid%20with%20space",
    );
    expect(detail.source).toEqual({
      kind: "schedule",
      available: true,
      eventId: null,
      aggregateType: null,
      aggregateId: null,
      action: null,
      occurredAt: null,
      scheduledFor: "2026-08-29T01:00:00Z",
    });
    expect(detail.retryChain).toEqual([
      expect.objectContaining({ id, attempt: 1, status: "succeeded" }),
    ]);
  });

  it("accepts an unavailable immutable event source without inventing facts", () => {
    const eventId = "018f0000-0000-7000-8000-000000001611";
    const detail = normalizeAutomationRunDetail(
      runDetailPayload(
        {
          trigger_type: "event",
          source_event_id: eventId,
          scheduled_for: null,
        },
        {
          source: {
            kind: "event",
            available: false,
            event_id: eventId,
            aggregate_type: null,
            aggregate_id: null,
            action: null,
            occurred_at: null,
            scheduled_for: null,
          },
        },
      ),
    );

    expect(detail.source).toMatchObject({
      kind: "event",
      available: false,
      eventId,
      aggregateType: null,
      occurredAt: null,
    });
  });

  it("preserves an explicitly empty execution summary", () => {
    const detail = normalizeAutomationRunDetail(
      runDetailPayload({ result_summary: "" }),
    );

    expect(detail.resultSummary).toBe("");
    expect(detail.retryChain[0].resultSummary).toBe("");
  });

  it.each([
    [
      "source has an unknown field",
      () => {
        const detail = runDetailPayload();
        return {
          ...detail,
          source: { ...(detail.source as object), unexpected: true },
        };
      },
    ],
    [
      "source contradicts the run",
      () => {
        const detail = runDetailPayload();
        return {
          ...detail,
          source: { ...(detail.source as object), scheduled_for: null },
        };
      },
    ],
    ["retry chain is empty", () => runDetailPayload({}, { retry_chain: [] })],
    [
      "retry attempts are duplicated",
      () => {
        const detail = runDetailPayload();
        const summary = attemptSummaryPayload(detail);
        return { ...detail, retry_chain: [summary, { ...summary }] };
      },
    ],
    [
      "different retry attempts reuse one run id",
      () => {
        const current = runPayload({ attempt: 2 });
        const first = attemptSummaryPayload({
          ...current,
          attempt: 1,
          retry_of_run_id: null,
        });
        const second = attemptSummaryPayload(current);
        return runDetailPayload(
          { attempt: 2 },
          { retry_chain: [first, second] },
        );
      },
    ],
    [
      "retry chain omits the selected run",
      () => {
        const detail = runDetailPayload();
        return {
          ...detail,
          retry_chain: [
            {
              ...attemptSummaryPayload(detail),
              id: "018f0000-0000-7000-8000-000000001699",
            },
          ],
        };
      },
    ],
  ])("rejects an invalid run detail when %s", (_label, payload) => {
    expect(() => normalizeAutomationRunDetail(payload())).toThrowError(
      expect.objectContaining({ code: "INVALID_RESPONSE" }),
    );
  });

  it("normalizes the available invoice overdue task preset and its task result", async () => {
    const invoiceRule = rulePayload({
      id: "00000000-0000-5000-8000-000000000104",
      preset_key: "invoice-overdue-task",
      name: "发票逾期跟进",
      description:
        "发票进入逾期状态后创建本地跟进任务；不会自动发送邮件或客户消息。",
      trigger_type: "event",
      trigger_label: "发票工作流事件：invoice_overdue",
      action_type: "task",
      action_label: "创建“跟进逾期发票”本地任务",
      config: { priority: "P1" },
      permissions: [
        "读取本地发票逾期事件",
        "创建一条本地跟进任务",
        "记录本地自动化运行",
      ],
      unavailable_reason: "",
    });
    const invoiceRun = runPayload({
      rule_id: invoiceRule.id,
      preset_key: invoiceRule.preset_key,
      rule_name: invoiceRule.name,
      trigger_type: "event",
      source_event_id: "018f0000-0000-7000-8000-000000001611",
      scheduled_for: null,
      config_snapshot: { priority: "P1" },
      action_snapshot: { action_type: "task", title: "跟进逾期发票" },
      result_type: "task",
      result_id: "018f0000-0000-7000-8000-000000001612",
      result_summary: "已创建本地发票跟进任务。",
    });
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ data: [invoiceRule] }))
      .mockResolvedValueOnce(
        jsonResponse({
          data: [invoiceRun],
          meta: { page: 1, page_size: 20, total: 1 },
        }),
      );
    vi.stubGlobal("fetch", fetchMock);

    const [rule] = await getAutomationRules();
    const runs = await getAutomationRuns();

    expect(rule).toMatchObject({
      presetKey: "invoice-overdue-task",
      status: "disabled",
      available: true,
      unavailableReason: "",
      triggerType: "event",
      actionType: "task",
      config: { priority: "P1" },
    });
    expect(runs.items[0]).toMatchObject({
      presetKey: "invoice-overdue-task",
      resultType: "task",
      resultId: "018f0000-0000-7000-8000-000000001612",
      resultSummary: "已创建本地发票跟进任务。",
    });
  });

  it("serializes preview, update, commands and retry with optimistic locking", async () => {
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        if (url.endsWith("/preview")) {
          return jsonResponse({
            data: {
              can_enable: true,
              trigger_summary: "每天指定当地时间",
              action_summary: "创建本地提醒",
              config: { local_time: "08:30", timezone: "Asia/Shanghai" },
              next_run_at: "2026-08-30T00:30:00Z",
              permissions: ["读取本地时间"],
            },
          });
        }
        if (url.includes("/runs/")) {
          return jsonResponse(
            { data: runPayload({ status: "succeeded", attempt: 2 }) },
            201,
          );
        }
        return jsonResponse({ data: rulePayload({ version: 2 }) });
      },
    );
    vi.stubGlobal("fetch", fetchMock);
    const id = String(rulePayload().id);
    const config = { localTime: "08:30", timezone: "Asia/Shanghai" };

    await previewAutomationRule(id, config);
    await updateAutomationRule(id, config, 1);
    await enableAutomationRule(id, 2);
    await disableAutomationRule(id, 3);
    await retryAutomationRun(String(runPayload().id));

    expect(JSON.parse(String(fetchMock.mock.calls[0][1]?.body))).toEqual({
      config: { local_time: "08:30", timezone: "Asia/Shanghai" },
    });
    expect(
      new Headers(fetchMock.mock.calls[1][1]?.headers).get("If-Match"),
    ).toBe('"1"');
    expect(
      fetchMock.mock.calls.slice(2, 4).map(([url]) => String(url)),
    ).toEqual([
      `/api/v1/automations/rules/${id}/enable`,
      `/api/v1/automations/rules/${id}/disable`,
    ]);
    expect(fetchMock.mock.calls[4][1]?.method).toBe("POST");
  });
});
