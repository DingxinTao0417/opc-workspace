import { afterEach, describe, expect, it, vi } from "vitest";
import {
  appendAiRunProgress,
  parseAiRunProgress,
  parseAiRunProgressList,
} from "./aiProgress";
import { streamAiChat } from "./ai";
import { getAiGeneration } from "./aiActions";
import { resetRuntimeConnection } from "./client";

const step = {
  sequence: 2,
  kind: "model_turn",
  status: "running",
  turn_index: 1,
  started_at: "2026-09-18T10:00:00.123456789Z",
  duration_ms: 0,
} as const;
const terminal = {
  ...step,
  status: "succeeded" as const,
  completed_at: "2026-09-18T10:00:01Z",
  duration_ms: 877,
};
afterEach(() => {
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

describe("content-free live progress", () => {
  it.each(["model_turn", "self_check"])(
    "accepts bounded read-result compaction for %s",
    (kind) => {
      const compacted = parseAiRunProgress({
        ...terminal,
        kind,
        compacted_tool_results: 2,
      });
      expect(compacted.compacted_tool_results).toBe(2);
      expect(
        appendAiRunProgress([parseAiRunProgress({ ...step, kind })], compacted),
      ).toEqual([compacted]);
    },
  );
  it.each([0, -1, 1.5, 33, "2", null, Number.NaN])(
    "rejects invalid read-result compaction %s",
    (compacted_tool_results) => {
      expect(() =>
        parseAiRunProgress({ ...terminal, compacted_tool_results }),
      ).toThrow("运行进度响应无效");
    },
  );
  it.each(["tool_call", "citation_validation", "persistence"])(
    "rejects read-result compaction on %s",
    (kind) => {
      expect(() =>
        parseAiRunProgress({
          ...terminal,
          kind,
          turn_index: undefined,
          ...(kind === "tool_call" ? { tool_name: "workspace_get" } : {}),
          compacted_tool_results: 1,
        }),
      ).toThrow("运行进度响应无效");
    },
  );
  it.each(["model_turn", "self_check"])(
    "accepts bounded history adjustment for %s",
    (kind) => {
      const adjusted = parseAiRunProgress({
        ...terminal,
        kind,
        trimmed_history_turns: 2,
      });
      expect(adjusted.trimmed_history_turns).toBe(2);
      expect(
        appendAiRunProgress([parseAiRunProgress({ ...step, kind })], adjusted),
      ).toEqual([adjusted]);
      expect(parseAiRunProgress({ ...terminal, kind })).not.toHaveProperty(
        "trimmed_history_turns",
      );
    },
  );
  it.each([
    0,
    -1,
    1.5,
    201,
    "2",
    null,
    Number.NaN,
    Number.MAX_SAFE_INTEGER + 1,
  ])("rejects invalid history adjustment %s", (trimmed_history_turns) => {
    expect(() =>
      parseAiRunProgress({ ...terminal, trimmed_history_turns }),
    ).toThrow("运行进度响应无效");
  });
  it.each(["tool_call", "citation_validation", "persistence"])(
    "rejects history adjustment on %s",
    (kind) => {
      expect(() =>
        parseAiRunProgress({
          ...terminal,
          kind,
          turn_index: undefined,
          ...(kind === "tool_call" ? { tool_name: "workspace_get" } : {}),
          trimmed_history_turns: 1,
        }),
      ).toThrow("运行进度响应无效");
    },
  );
  it.each([
    "workspace_guide",
    "workspace_plan",
    "workspace_agent_runs",
    "workspace_agent_execution",
    "workspace_agent_project_files",
    "workspace_task_views",
    "workspace_today",
    "workspace_tasks",
    "workspace_projects",
    "workspace_project_outputs",
    "workspace_read_artifact_file",
    "workspace_content_items",
    "workspace_roadmap_milestones",
    "workspace_inbox_source",
    "knowledge_library",
  ])("accepts %s progress without result payloads", (tool_name) => {
    const query = {
      ...step,
      kind: "tool_call",
      turn_index: undefined,
      tool_name,
    };
    expect(parseAiRunProgress(query).tool_name).toBe(tool_name);
    expect(() => parseAiRunProgress({ ...query, result: "private" })).toThrow();
  });
  it("accepts bounded model retry facts and rejects retry data on non-model steps", () => {
    expect(
      parseAiRunProgress({
        ...step,
        retry_count: 2,
        retry_reason: "upstream_transient_error",
      }),
    ).toMatchObject({
      retry_count: 2,
      retry_reason: "upstream_transient_error",
    });
    expect(() =>
      parseAiRunProgress({
        ...step,
        kind: "tool_call",
        turn_index: undefined,
        tool_name: "workspace_search",
        retry_count: 1,
      }),
    ).toThrow("运行进度响应无效");
    expect(() =>
      parseAiRunProgress({
        ...step,
        retry_reason: "upstream_transient_error",
      }),
    ).toThrow("运行进度响应无效");
  });
  it("accepts submission-query progress without accepting result text", () => {
    const query = {
      ...step,
      kind: "tool_call",
      turn_index: undefined,
      tool_name: "workspace_task_submissions",
    };
    expect(parseAiRunProgress(query).tool_name).toBe(
      "workspace_task_submissions",
    );
    expect(() =>
      parseAiRunProgress({ ...query, result: "private summary" }),
    ).toThrow();
  });

  it.each([
    { prompt: "secret" },
    { result: "secret" },
    { error_code: "untrusted" },
    { sequence: 1 },
    { sequence: 65 },
    { sequence: 2.5 },
    { sequence: Number.MAX_SAFE_INTEGER + 1 },
    { turn_index: 9 },
    { turn_index: undefined },
    { kind: ["model_turn"] },
    { status: ["running"] },
    { started_at: "yesterday" },
    { duration_ms: -1 },
    { duration_ms: 2 },
    { completed_at: step.started_at },
    { tool_name: "private-name" },
    { kind: "tool_call", turn_index: undefined, tool_name: "private-name" },
  ])("rejects invalid or private fields %j", (patch) => {
    expect(() => parseAiRunProgress({ ...step, ...patch })).toThrow(
      "运行进度响应无效",
    );
  });
  it("checks terminal identity and disallows regressions, gaps or concurrent steps", () => {
    const start = parseAiRunProgress(step);
    const done = parseAiRunProgress(terminal);
    expect(appendAiRunProgress([start], done)).toEqual([done]);
    expect(() => appendAiRunProgress([done], start)).toThrow();
    expect(() =>
      appendAiRunProgress([start], { ...done, turn_index: 2 }),
    ).toThrow();
    expect(() =>
      appendAiRunProgress([start], { ...start, sequence: 3 }),
    ).toThrow();
    expect(() => appendAiRunProgress([], { ...start, sequence: 3 })).toThrow();
    expect(() =>
      parseAiRunProgressList([step, { ...step, sequence: 3 }]),
    ).toThrow();
    expect(() => parseAiRunProgressList(Array(64).fill(terminal))).toThrow();
    expect(() =>
      parseAiRunProgress({ ...terminal, completed_at: "2025-01-01T00:00:00Z" }),
    ).toThrow();
    expect(parseAiRunProgressList(undefined)).toBeUndefined();
    expect(parseAiRunProgressList([])).toEqual([]);
  });
  it.each([
    "not-json",
    "null",
    JSON.stringify({ generation_id: "g", step: { ...step, result: "secret" } }),
    JSON.stringify({ generation_id: "g", step, prompt: "secret" }),
  ])("fails closed on malformed progress SSE %s", async (data) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            `event: progress\ndata: ${data}\n\nevent: done\ndata: {"generation_id":"g"}\n\n`,
          ),
      ),
    );
    const onEvent = vi.fn();
    await expect(
      streamAiChat({ providerId: "p", message: "test", onEvent }),
    ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
    expect(onEvent).not.toHaveBeenCalled();
  });
  it.each([
    { ...terminal, trimmed_history_turns: 3 },
    { ...terminal, compacted_tool_results: 2 },
    {
      ...terminal,
      kind: "tool_call",
      turn_index: undefined,
      tool_name: "workspace_tasks",
    },
    {
      ...terminal,
      kind: "tool_call",
      turn_index: undefined,
      tool_name: "workspace_project_outputs",
    },
    {
      ...terminal,
      kind: "tool_call",
      turn_index: undefined,
      tool_name: "workspace_content_items",
    },
    {
      ...terminal,
      kind: "tool_call",
      turn_index: undefined,
      tool_name: "workspace_inbox_source",
    },
  ])(
    "decodes the same $kind progress from SSE and generation recovery",
    async (adjusted) => {
      const event = { generation_id: "g", step: adjusted };
      vi.stubGlobal(
        "fetch",
        vi.fn(async (url) =>
          String(url).endsWith("/chat")
            ? new Response(
                `event: progress\ndata: ${JSON.stringify(event)}\n\nevent: done\ndata: {"generation_id":"g"}\n\n`,
              )
            : Response.json({
                data: {
                  id: "g",
                  session_id: "s",
                  provider_id: "p",
                  status: "completed",
                  progress: [adjusted],
                },
              }),
        ),
      );
      const onEvent = vi.fn();
      await streamAiChat({ providerId: "p", message: "test", onEvent });
      const recovered = await getAiGeneration("g");
      expect(onEvent.mock.calls[0][0].step).toEqual(recovered.progress?.[0]);
    },
  );
  it("rejects still-running terminal recovery", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        Response.json({
          data: {
            id: "g",
            session_id: "s",
            provider_id: "p",
            status: "cancelled",
            progress: [step],
          },
        }),
      ),
    );
    await expect(getAiGeneration("g")).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
  });
});
