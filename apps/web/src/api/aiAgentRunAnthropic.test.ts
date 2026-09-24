import { describe, expect, it, vi } from "vitest";
import { decideAiAgentRun, parseAiActionProposal } from "./aiWorkspaceActions";
import { restartProposal } from "../test/agentRunRestartFixture";
const mocks = vi.hoisted(() => ({ request: vi.fn() }));
vi.mock("./client", async () => ({
  ...(await vi.importActual("./client")),
  apiRequest: mocks.request,
}));
const id = (n: number) =>
  `018f0000-0000-7000-8000-${String(n).padStart(12, "0")}`;
const fileLimits = {
  max_files: 4,
  max_file_bytes: 65536,
  max_total_bytes: 131072,
  max_result_bytes: 65536,
};

function anthropic() {
  const value = restartProposal();
  Object.assign(value.preview.agent_run_start, {
    execution_contract_version: 5,
    output_contract: { type: "text" },
    runtime_limits: {
      timeout_seconds: 600,
      max_result_bytes: 65536,
      max_output_tokens: 8192,
    },
  });
  value.preview.agent_run_start.provider.protocol = "anthropic_messages";
  return value;
}

describe("Anthropic Agent v5 preview", () => {
  it.each(["text", "file", "files"])(
    "accepts %s output with permissions based only on real inputs",
    (type) => {
      const value = anthropic();
      if (type !== "text") {
        Object.assign(value.action.changes, { output_kind: type });
        Object.assign(value.preview.agent_run_start, {
          output_contract:
            type === "file"
              ? { type, name: "result.md", mime: "text/markdown" }
              : {
                  type,
                  files: [
                    { name: "result.md", mime: "text/markdown" },
                    { name: "data.json", mime: "application/json" },
                  ],
                },
          file_limits: fileLimits,
          input_files_leave_device: false,
        });
      }
      expect(
        parseAiActionProposal(value).preview.agent_run_start?.output_contract
          ?.type,
      ).toBe(type);
    },
  );
  it.each([false, true])(
    "freezes optional rework without treating version 5 as file consent; inputs=%s",
    async (files) => {
      const value = anthropic();
      Object.assign(value.action.changes, {
        rework_submission_id: id(30),
        rework_artifact_ids: [],
      });
      Object.assign(value.preview.agent_run_start, {
        rework_context: {
          submission_id: id(30),
          sequence: 2,
          review_reason: "修正金额",
          reviewed_at: "2026-09-21T12:00:00Z",
          artifacts: [],
        },
      });
      if (files) {
        Object.assign(value.action.changes, {
          input_file_candidate_ids: [id(31)],
        });
        Object.assign(value.preview.agent_run_start, {
          input_files: [
            {
              source_kind: "task_artifact",
              id: id(32),
              name: "input.md",
              mime: "text/markdown",
              size_bytes: 12,
              sha256: "b".repeat(64),
            },
          ],
          input_file_total_bytes: 12,
          input_files_leave_device: true,
          file_limits: fileLimits,
        });
      }
      const parsed = parseAiActionProposal(value);
      mocks.request.mockReset().mockResolvedValue({ data: value });
      await decideAiAgentRun(parsed, "confirm", true, true, true, true);
      const body = JSON.parse(mocks.request.mock.calls[0][1].body);
      expect(body.confirm_agent_execution).toBe(true);
      expect(body.confirm_agent_restart).toBe(true);
      expect(body.confirm_agent_rework).toBe(true);
      expect(body.confirm_agent_files).toBe(files ? true : undefined);
    },
  );
  it("accepts plain-text exact retry and keeps the exact source instead of restart", () => {
    const value = anthropic();
    Reflect.deleteProperty(value.preview.agent_run_start, "restart");
    value.preview.agent_run_start.attempt = 2;
    Object.assign(value.action, {
      action: "agent_run.retry",
      agent_run_id: id(5),
      changes: {},
    });
    Object.assign(value.preview, {
      agent_run_retry: { run_id: id(5), status: "failed", attempt: 1 },
    });
    expect(parseAiActionProposal(value).action.action).toBe("agent_run.retry");
  });
  it("accepts remote text restart without inventing file or rework permissions", () => {
    const parsed = parseAiActionProposal(anthropic());
    expect(parsed.preview.agent_run_start).toMatchObject({
      execution_contract_version: 5,
      provider: { protocol: "anthropic_messages", leaves_device: true },
      output_contract: { type: "text" },
      runtime_limits: { max_output_tokens: 8192 },
    });
    expect(parsed.preview.agent_run_start?.file_limits).toBeUndefined();
    expect(parsed.preview.agent_run_start?.rework_context).toBeUndefined();
  });
  it.each([
    "local",
    "wrongProtocol",
    "missingTokens",
    "wrongTokens",
    "legacyVersion",
  ])("rejects incompatible %s", (kind) => {
    const value = anthropic();
    const snapshot = value.preview.agent_run_start;
    if (kind === "local") snapshot.provider.kind = "local";
    if (kind === "wrongProtocol") snapshot.provider.protocol = "openai_chat";
    if (kind === "legacyVersion") snapshot.execution_contract_version = 4;
    if (kind === "missingTokens")
      Reflect.deleteProperty(snapshot.runtime_limits, "max_output_tokens");
    if (kind === "wrongTokens")
      Object.assign(snapshot.runtime_limits, { max_output_tokens: 8193 });
    expect(() => parseAiActionProposal(value)).toThrow();
  });
  it.each([
    "extraLimits",
    "missingOutput",
    "unknownOutput",
    "phantomFileLimits",
    "leavesDevice",
    "badRework",
    "missingFileLimits",
  ])("rejects malformed %s", (kind) => {
    const value = anthropic();
    const snapshot = value.preview.agent_run_start;
    if (kind === "extraLimits")
      Object.assign(snapshot.runtime_limits, { max_turns: 2 });
    if (kind === "missingOutput")
      Reflect.deleteProperty(snapshot, "output_contract");
    if (kind === "unknownOutput")
      Object.assign(snapshot, { output_contract: { type: "unknown" } });
    if (kind === "phantomFileLimits")
      Object.assign(snapshot, {
        file_limits: fileLimits,
        input_files_leave_device: false,
      });
    if (kind === "leavesDevice") snapshot.provider.leaves_device = false;
    if (kind === "badRework") Object.assign(snapshot, { rework_context: null });
    if (kind === "missingFileLimits") {
      Object.assign(value.action.changes, { output_kind: "file" });
      Object.assign(snapshot, {
        output_contract: { type: "file", name: "a.md", mime: "text/markdown" },
      });
    }
    expect(() => parseAiActionProposal(value)).toThrow();
  });
  it.each([1, 2, 3, 4])(
    "rejects Anthropic masquerading as legacy version %s",
    (version) => {
      const value = anthropic();
      value.preview.agent_run_start.execution_contract_version = version;
      Reflect.deleteProperty(
        value.preview.agent_run_start.runtime_limits,
        "max_output_tokens",
      );
      expect(() => parseAiActionProposal(value)).toThrow();
    },
  );
  it("does not allow the v5 token budget field on a legacy OpenAI preview", () => {
    const value = restartProposal();
    Object.assign(value.preview.agent_run_start.runtime_limits, {
      max_output_tokens: 8192,
    });
    expect(() => parseAiActionProposal(value)).toThrow();
  });
});
