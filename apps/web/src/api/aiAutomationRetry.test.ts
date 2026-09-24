import { describe, expect, it, vi } from "vitest";
import {
  parseAiActionProposal,
  decideAiWorkspaceAction,
} from "./aiWorkspaceActions";
import { automationRetryResultRoute } from "./aiAutomationRetry";

const mocks = vi.hoisted(() => ({ request: vi.fn() }));
vi.mock("./client", async () => ({
  ...(await vi.importActual("./client")),
  apiRequest: mocks.request,
}));
const id = (n: string) => `00000000-0000-4000-8000-${n.padStart(12, "0")}`;
const retry = {
  run_id: id("3"),
  rule_id: "00000000-0000-5000-8000-000000000101",
  rule_version: 2,
  current_rule_version: 4,
  rule_enabled: false,
  trigger_type: "event",
  action_type: "inbox_item",
  permissions: ["创建本地收件箱事项"],
  attempt: 1,
  next_attempt: 2,
  scheduled_for: null,
  error_code: "ACTION_WRITE_FAILED",
  config: { priority: "P1" },
  action: {
    action_type: "inbox_item",
    title: "核对发票",
    project_id: id("4"),
    project_name: "客户项目",
    priority: "P1",
  },
};
const pending = {
  id: id("1"),
  generation_id: id("2"),
  fingerprint: "a".repeat(64),
  action: {
    action: "automation.retry",
    automation_run_id: id("3"),
    expected_version: 2,
    changes: {},
  },
  preview: {
    label: "项目完成后核对发票",
    before: {},
    after: {},
    automation_retry: retry,
  },
  status: "pending",
  can_confirm: true,
  result_id: null,
  result_version: null,
  route: "https://evil.example",
  created_at: "2026-09-18T12:00:00Z",
  decided_at: null,
};
const confirmed = (success: boolean) => ({
  ...pending,
  status: "confirmed",
  can_confirm: false,
  result_id: id("5"),
  result_version: 2,
  automation_run_result: {
    id: id("5"),
    rule_id: retry.rule_id,
    rule_version: 2,
    status: success ? "succeeded" : "failed",
    attempt: 2,
    retryable: !success,
    retry_at: success ? null : "2026-09-18T12:05:00Z",
    error_code: success ? null : "ACTION_WRITE_FAILED",
    result_type: success ? "inbox_item" : null,
    result_id: success ? id("6") : null,
  },
});
describe("automation retry approval contract", () => {
  it("accepts preset 105 as notification-only with strict numeric Agent attempt", () => {
    const action = {
      action_type: "inbox_item",
      agent_run_id: id("8"),
      task_id: id("9"),
      attempt: 4,
      error_code: "AGENT_MODEL_FAILED",
      failed_at: "2026-09-21T12:00:00.000000000Z",
      priority: "P1",
    };
    const make = (snapshot: Record<string, unknown>) => ({
      ...pending,
      preview: {
        ...pending.preview,
        automation_retry: {
          ...retry,
          rule_id: "00000000-0000-5000-8000-000000000105",
          action: snapshot,
        },
      },
    });
    expect(
      parseAiActionProposal(make(action)).preview.automation_retry?.action,
    ).toEqual(action);
    for (const error_code of [
      "AGENT_MODEL_TRUNCATED",
      "AGENT_MODEL_FILTERED",
      "AGENT_MODEL_RESPONSE_INVALID",
    ]) {
      expect(
        parseAiActionProposal(make({ ...action, error_code })).preview
          .automation_retry?.action,
      ).toEqual({ ...action, error_code });
    }
    const missingSourceResult = {
      ...make(action),
      status: "confirmed",
      can_confirm: false,
      result_id: id("5"),
      result_version: 2,
      automation_run_result: {
        ...confirmed(false).automation_run_result,
        rule_id: "00000000-0000-5000-8000-000000000105",
        error_code: "SOURCE_UNAVAILABLE",
        retryable: false,
        retry_at: null,
      },
    };
    expect(
      parseAiActionProposal(missingSourceResult).automation_run_result
        ?.error_code,
    ).toBe("SOURCE_UNAVAILABLE");
    expect(() =>
      parseAiActionProposal({
        ...missingSourceResult,
        automation_run_result: {
          ...missingSourceResult.automation_run_result,
          retryable: true,
          retry_at: "2026-09-21T12:05:00Z",
        },
      }),
    ).toThrow();
    expect(() =>
      parseAiActionProposal({
        ...confirmed(false),
        automation_run_result: {
          ...confirmed(false).automation_run_result,
          error_code: "SOURCE_UNAVAILABLE",
          retryable: false,
          retry_at: null,
        },
      }),
    ).toThrow();
    for (const patch of [
      { attempt: "4" },
      { attempt: 0 },
      { attempt: 1.2 },
      { attempt: Number.MAX_SAFE_INTEGER + 1 },
      { error_code: "raw provider error" },
      { title: "private title" },
      { agent_run_id: "../run" },
      { failed_at: "2026-09-21T12:00:00Z" },
    ]) {
      expect(() =>
        parseAiActionProposal(make({ ...action, ...patch })),
      ).toThrow();
    }
    expect(() =>
      parseAiActionProposal({
        ...pending,
        preview: {
          ...pending.preview,
          automation_retry: {
            ...retry,
            action: { ...retry.action, attempt: 4 },
          },
        },
      }),
    ).toThrow();
  });
  it("accepts complete captures and distinguishes confirmed failure from success", () => {
    expect(parseAiActionProposal(pending).route).toBe(
      `/settings/automation?run=${id("3")}`,
    );
    for (const success of [true, false]) {
      const p = parseAiActionProposal(confirmed(success));
      expect(p.route).toBe(`/settings/automation?run=${id("5")}`);
      expect(automationRetryResultRoute(p.automation_run_result)).toBe(
        success ? `/inbox/${id("6")}` : "",
      );
      expect(p.result_version).toBe(2);
    }
    expect(() =>
      parseAiActionProposal({
        ...confirmed(true),
        automation_run_result: undefined,
      }),
    ).toThrow();
    expect(() =>
      parseAiActionProposal({
        ...pending,
        action: { ...pending.action, automation_rule_id: retry.rule_id },
      }),
    ).toThrow();
    expect(() =>
      parseAiActionProposal({
        ...pending,
        action: { ...pending.action, changes: { priority: "P0" } },
      }),
    ).toThrow();
    expect(() =>
      parseAiActionProposal({
        ...pending,
        preview: { ...pending.preview, automation_retry: undefined },
      }),
    ).toThrow();
  });
  it("rejects missing or substituted snapshots, unsupported fields and false outcomes", () => {
    for (const patch of [
      { rule_version: 4 },
      { run_id: id("4") },
      { current_rule_version: 1 },
      { rule_enabled: null },
      { attempt: 3 },
      { next_attempt: 3 },
      { permissions: [] },
      { scheduled_for: "wrong" },
      { action_type: "task" },
      { config: {} },
      { config: { priority: "P0" } },
      { action: { ...retry.action, command: "whoami" } },
      { action: { ...retry.action, project_name: undefined } },
    ])
      expect(() =>
        parseAiActionProposal({
          ...pending,
          preview: {
            ...pending.preview,
            automation_retry: { ...retry, ...patch },
          },
        }),
      ).toThrow();
    const done = confirmed(true);
    for (const patch of [
      { id: id("3") },
      { rule_version: 4 },
      { attempt: 1 },
      { retryable: true },
      { result_type: "task" },
      { result_id: "../../bad" },
      { status: "running" },
      { result_summary: "private" },
    ]) {
      expect(() =>
        parseAiActionProposal({
          ...done,
          automation_run_result: { ...done.automation_run_result, ...patch },
        }),
      ).toThrow();
    }
    const failed = confirmed(false);
    expect(() =>
      parseAiActionProposal({
        ...failed,
        automation_run_result: {
          ...failed.automation_run_result,
          result_id: id("6"),
        },
      }),
    ).toThrow();
  });
  it("accepts schedule and invoice captures without sending their data in decisions", async () => {
    const schedule = {
      ...retry,
      rule_id: "00000000-0000-5000-8000-000000000102",
      rule_enabled: true,
      trigger_type: "schedule",
      action_type: "reminder",
      scheduled_for: "2026-09-18T09:00:00Z",
      config: { local_time: "09:00", timezone: "UTC" },
      action: {
        action_type: "reminder",
        title: "查看今日",
        summary: "今天",
        priority: "P2",
      },
    };
    const invoice = {
      ...retry,
      rule_id: "00000000-0000-5000-8000-000000000104",
      action_type: "task",
      action: {
        action_type: "task",
        title: "跟进发票",
        invoice_id: id("7"),
        invoice_number: "INV-100",
        project_id: null,
        description: "私有金额 ¥1234",
        kind: "followup",
        status: "todo",
        review_policy: "none",
        priority: "P1",
        due_date: null,
        planned_date: null,
      },
    };
    for (const p of [schedule, invoice]) {
      const raw = {
        ...pending,
        preview: { ...pending.preview, automation_retry: p },
      };
      const proposal = parseAiActionProposal(raw);
      mocks.request.mockResolvedValue({
        data: { ...raw, status: "rejected", can_confirm: false },
      });
      await decideAiWorkspaceAction(
        proposal,
        "reject",
        undefined,
        undefined,
        undefined,
        undefined,
        undefined,
        undefined,
        true,
      );
      expect(JSON.parse(mocks.request.mock.lastCall![1].body)).toEqual({
        fingerprint: pending.fingerprint,
        decision: "reject",
      });
    }
    mocks.request.mockResolvedValue({ data: confirmed(false) });
    await decideAiWorkspaceAction(
      parseAiActionProposal(pending),
      "confirm",
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      true,
    );
    expect(JSON.parse(mocks.request.mock.lastCall![1].body)).toEqual({
      fingerprint: pending.fingerprint,
      decision: "confirm",
      confirm_automation_retry: true,
    });
    expect(() =>
      parseAiActionProposal({
        ...pending,
        preview: {
          ...pending.preview,
          automation_retry: { ...schedule, rule_enabled: false },
        },
      }),
    ).toThrow();
  });
});
