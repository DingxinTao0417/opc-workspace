import { describe, expect, it } from "vitest";
import { normalizeInboxItem } from "./client";

const id = (n: number) =>
  `00000000-0000-4000-8000-${String(n).padStart(12, "0")}`;
const payload = {
  agent_run_id: id(2),
  task_id: id(3),
  attempt: 2,
  error_code: "AGENT_MODEL_FAILED",
  failed_at: "2026-09-21T12:00:00.123456789Z",
  automation_rule_id: "00000000-0000-5000-8000-000000000105",
  automation_run_id: id(4),
  source_event_id: id(5),
};
const item = {
  id: id(1),
  kind: "event",
  title: "Agent 执行失败诊断",
  summary: "请检查执行记录",
  source_entity_type: "agent_run_failed",
  source_entity_id: id(2),
  source_event_key: `agent-run:${id(2)}:failed`,
  source_deleted_at: null,
  payload_json: payload,
  due_at: null,
  resolution_policy: "manual",
  priority: "P1",
  status: "open",
  version: 1,
  created_at: payload.failed_at,
  updated_at: payload.failed_at,
  available_actions: ["edit", "resolve", "dismiss"],
};

describe("Agent failure Inbox source", () => {
  it("accepts only bounded failure metadata, including imported tombstones", () => {
    expect(normalizeInboxItem(item).payloadJson).toEqual(payload);
    expect(
      normalizeInboxItem({ ...item, source_deleted_at: payload.failed_at })
        .sourceDeletedAt,
    ).toBe(payload.failed_at);
    expect(
      normalizeInboxItem({ ...item, due_at: "2026-09-22T12:00:00Z" }).dueAt,
    ).toBe("2026-09-22T12:00:00Z");
  });
  it.each([
    "AGENT_EXECUTOR_UNAVAILABLE",
    "AGENT_EXECUTOR_FAILED",
    "AGENT_PROTOCOL_INVALID",
    "AGENT_RESULT_TOO_LARGE",
    "AGENT_RUN_TIMED_OUT",
    "AGENT_EXECUTOR_INVALID_INPUT",
    "AGENT_MODEL_ENDPOINT_INVALID",
    "AGENT_MODEL_UNAVAILABLE",
    "AGENT_MODEL_FAILED",
    "AGENT_MODEL_TRUNCATED",
    "AGENT_MODEL_FILTERED",
    "AGENT_MODEL_RESPONSE_INVALID",
    "AGENT_RESULT_EMPTY",
    "AGENT_RESULT_INVALID",
    "AGENT_RUN_IDENTITY_CHANGED",
    "AGENT_MODEL_KEY_UNAVAILABLE",
    "AGENT_RUN_FAILED",
  ])("accepts safe Agent diagnostic code %s", (error_code) => {
    expect(
      normalizeInboxItem({ ...item, payload_json: { ...payload, error_code } })
        .payloadJson.error_code,
    ).toBe(error_code);
  });
  it.each([
    { agent_run_id: id(6) },
    { task_id: "../tasks" },
    { attempt: "2" },
    { attempt: 0 },
    { attempt: 1.1 },
    { attempt: Number.MAX_SAFE_INTEGER + 1 },
    { error_code: "private provider error" },
    { error_code: null },
    { failed_at: "2026-02-30T12:00:00.000000000Z" },
    { failed_at: "0000-01-01T12:00:00.000000000Z" },
    { failed_at: "2026-09-21T12:00:00Z" },
    { failed_at: "2026-09-21T12:00:00.000000000+00:00" },
    { automation_rule_id: "00000000-0000-5000-8000-000000000101" },
    { source_event_id: null },
    { result_text: "private result" },
    { taskId: id(3) },
  ])("rejects malformed or unbounded metadata %j", (patch) => {
    expect(() =>
      normalizeInboxItem({ ...item, payload_json: { ...payload, ...patch } }),
    ).toThrow("收件箱条目响应格式无效");
  });
  it.each([
    { kind: "manual" },
    { source_entity_id: id(3) },
    { source_event_key: `agent-run:${id(3)}:failed` },
    { source_deleted_at: {} },
    { source_deleted_at: "" },
    { due_at: "" },
  ])("requires self-consistent immutable source identity %j", (patch) => {
    expect(() => normalizeInboxItem({ ...item, ...patch })).toThrow();
  });
});
