import { describe, expect, it } from "vitest";
import { parseAgentRunSummaryRecord } from "./client";

const id = (suffix: number) =>
  `018f0000-0000-7000-8000-${String(suffix).padStart(12, "0")}`;

const runningSummary = {
  id: id(1),
  task_id: id(2),
  task_title: "执行阶段验证",
  assignment_id: id(3),
  actor_id: id(4),
  adapter_id: id(5),
  created_by_actor_id: id(6),
  parent_run_id: null,
  attempt: 1,
  status: "running",
  provider_id: id(7),
  model: "test-model",
  result_bytes: null,
  error_code: null,
  output_delivery_status: "not_ready",
  output_delivery_error_code: null,
  submission_id: null,
  artifact_id: null,
  started_at: "2026-09-23T12:00:01Z",
  completed_at: null,
  created_at: "2026-09-23T12:00:00Z",
};

describe("Agent Run progress metadata", () => {
  it("accepts known content-free phases and old responses without progress", () => {
    expect(
      parseAgentRunSummaryRecord({
        ...runningSummary,
        progress: { phase: "calling_model", elapsed_ms: 12_500 },
      }).progress,
    ).toEqual({ phase: "calling_model", elapsedMs: 12_500 });
    expect(parseAgentRunSummaryRecord(runningSummary).progress).toBeUndefined();
  });

  it.each([
    ["unknown phase", { phase: "thinking", elapsed_ms: 1 }],
    ["negative duration", { phase: "preparing", elapsed_ms: -1 }],
    ["fractional duration", { phase: "preparing", elapsed_ms: 0.5 }],
    [
      "unsafe duration",
      { phase: "preparing", elapsed_ms: Number.MAX_SAFE_INTEGER + 1 },
    ],
    [
      "extra field",
      { phase: "preparing", elapsed_ms: 1, text: "partial output" },
    ],
    ["camel-case alias", { phase: "preparing", elapsedMs: 1 }],
  ])("rejects %s", (_label, progress) => {
    expect(() =>
      parseAgentRunSummaryRecord({ ...runningSummary, progress }),
    ).toThrowError(/Agent 执行进度响应格式无效/);
  });

  it("rejects progress on a terminal run", () => {
    expect(() =>
      parseAgentRunSummaryRecord({
        ...runningSummary,
        status: "cancelled",
        completed_at: "2026-09-23T12:01:00Z",
        progress: { phase: "registering_result", elapsed_ms: 60_000 },
      }),
    ).toThrowError(/Agent 执行进度响应格式无效/);
  });
});
