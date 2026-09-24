import { describe, expect, it, vi } from "vitest";
import {
  parseAiActionProposal,
  decideAiTaskOutput,
  type AiActionProposal,
} from "./aiWorkspaceActions";
const mocks = vi.hoisted(() => ({ request: vi.fn() }));
vi.mock("./client", async () => ({
  ...(await vi.importActual("./client")),
  apiRequest: mocks.request,
}));
const uuid = (n: number) =>
  `00000000-0000-4000-8000-${String(n).padStart(12, "0")}`;
function fixture(review = false): AiActionProposal {
  const before = {
    status: review ? "waiting_review" : "todo",
    review_policy: "manual",
    completion_criteria: "必须人工核实",
    current_submission_id: review ? uuid(4) : null,
    subtask_total: 0,
    subtask_completed: 0,
    subtask_cancelled: 0,
  };
  return {
    id: uuid(1),
    generation_id: uuid(2),
    fingerprint: "a".repeat(64),
    action: {
      action: review ? "task.review" : "task.submit_output",
      task_id: uuid(3),
      expected_version: 3,
      changes: review
        ? { submission_id: uuid(4), decision: "accept", reason: "" }
        : {
            summary: "全部摘要",
            artifacts: [
              {
                client_ref: "r",
                storage_kind: "text",
                name: "结果",
                content_text: "全部正文",
                requires_followup: true,
              },
            ],
          },
    },
    preview: {
      label: "真实任务",
      before,
      after: {
        ...before,
        status: review ? "done" : "waiting_review",
        submission_status: review ? "accepted" : "pending_review",
        submission_origin: "manual",
        ...(review ? { reason: "" } : {}),
      },
      task_output: {
        summary: "全部摘要",
        artifacts: [
          {
            id: review ? uuid(5) : null,
            storage_kind: "text",
            name: "结果",
            content: "全部正文",
            size_bytes: null,
            sha256: null,
            requires_followup: true,
            deleted: false,
          },
        ],
        assignments: ["assignee", "reviewer"].map((role, i) => ({
          id: uuid(6 + i),
          role: role as "assignee" | "reviewer",
          actor_id: uuid(8 + i),
          actor_name: role,
          actor_type: "owner",
          actor_status: "active",
          actor_version: 1,
          assigned_at: "2026-09-18T12:00:00Z",
          unassigned_at: null,
        })),
      },
    },
    status: "pending",
    can_confirm: true,
    result_id: null,
    result_version: null,
    created_at: "2026-09-18T12:00:00Z",
    decided_at: null,
    route: "https://forged.invalid",
  };
}
describe("task output approval contract", () => {
  it.each([false, true])(
    "binds full evidence and real task route (review=%s)",
    (review) => {
      const p = fixture(review);
      expect(parseAiActionProposal(p).route).toBe(
        `/tasks/${uuid(3)}${review ? `/submissions/${uuid(4)}` : ""}`,
      );
      const result = {
        ...p,
        status: "confirmed",
        can_confirm: false,
        result_id: uuid(4),
        result_version: 4,
        decided_at: p.created_at,
      };
      expect(parseAiActionProposal(result).result_id).toBe(uuid(4));
      expect(parseAiActionProposal(result).route).toBe(
        `/tasks/${uuid(3)}/submissions/${uuid(4)}`,
      );
      expect(() =>
        parseAiActionProposal({ ...result, result_version: 3 }),
      ).toThrow();
    },
  );
  it.each([
    [
      "unknown actor override",
      (p: any) => (p.action.changes.produced_by_actor_id = uuid(8)),
    ],
    ["model consent", (p: any) => (p.action.confirm_task_output = true)],
    ["missing evidence", (p: any) => delete p.preview.task_output],
    [
      "partial preview",
      (p: any) => (p.preview.task_output.artifacts[0].content = "截断"),
    ],
    [
      "changed name",
      (p: any) => (p.preview.task_output.artifacts[0].name = "其他"),
    ],
    ["forged completion", (p: any) => (p.preview.after.status = "done")],
    ["missing reviewer", (p: any) => p.preview.task_output.assignments.pop()],
    [
      "disabled producer",
      (p: any) =>
        (p.preview.task_output.assignments[0].actor_status = "inactive"),
    ],
    [
      "file write",
      (p: any) => (p.action.changes.artifacts[0].storage_kind = "file"),
    ],
    [
      "unexpected file path",
      (p: any) =>
        (p.preview.task_output.artifacts[0].relative_path = "objects/x"),
    ],
    ["unknown preview", (p: any) => (p.preview.tasks = [])],
    [
      "null followup",
      (p: any) => (p.preview.task_output.artifacts[0].requires_followup = null),
    ],
  ])("rejects %s", (_, change) => {
    const p = fixture();
    (change as (p: AiActionProposal) => void)(p);
    expect(() => parseAiActionProposal(p)).toThrow();
  });
  it("validates exact pending batch, rework reason and deleted/file payload boundaries", () => {
    const p = fixture(true);
    p.action.changes.submission_id = uuid(99);
    expect(() => parseAiActionProposal(p)).toThrow();
    p.action.changes.submission_id = uuid(4);
    p.action.changes.decision = "request_changes";
    p.preview.after.status = "in_progress";
    p.preview.after.submission_status = "changes_requested";
    expect(() => parseAiActionProposal(p)).toThrow();
    p.action.changes.reason = p.preview.after.reason = "请补充证据";
    expect(parseAiActionProposal(p).action.changes.decision).toBe(
      "request_changes",
    );
    const art = p.preview.task_output!.artifacts[0];
    art.deleted = true;
    expect(() => parseAiActionProposal(p)).toThrow();
    art.content = null;
    expect(parseAiActionProposal(p)).toBeDefined();
    art.storage_kind = "file";
    art.deleted = false;
    art.size_bytes = 42;
    art.sha256 = "f".repeat(64);
    expect(parseAiActionProposal(p)).toBeDefined();
    art.content = "不可假装读取文件";
    expect(() => parseAiActionProposal(p)).toThrow();
  });
  it("allows non-file structured/link payloads and large complete Unicode evidence", () => {
    const p = fixture();
    const art = p.preview.task_output!.artifacts[0];
    const change = (p.action.changes.artifacts as Record<string, unknown>[])[0];
    p.action.changes.summary = p.preview.task_output!.summary = "完整🙂".repeat(
      2000,
    );
    change.content_text = art.content = "正文\n".repeat(8000);
    expect(parseAiActionProposal(p).preview.task_output!.summary).toBe(
      p.action.changes.summary,
    );
    delete change.content_text;
    change.storage_kind = art.storage_kind = "link";
    change.reference_url = art.content = "https://example.com/result";
    expect(parseAiActionProposal(p)).toBeDefined();
    change.reference_url = art.content = "javascript:alert(1)";
    expect(() => parseAiActionProposal(p)).toThrow();
    delete change.reference_url;
    change.storage_kind = art.storage_kind = "structured";
    change.structured_json = { answer: 42 };
    art.content = '{"answer":42}';
    expect(parseAiActionProposal(p)).toBeDefined();
    art.content = '{"answer":43}';
    expect(() => parseAiActionProposal(p)).toThrow();
  });
  it("sends human consent only when confirming and rejects mismatched readback", async () => {
    const p = fixture();
    mocks.request.mockResolvedValue({
      data: {
        ...p,
        status: "confirmed",
        can_confirm: false,
        result_id: uuid(4),
        result_version: 4,
        decided_at: p.created_at,
      },
    });
    await decideAiTaskOutput(p, "confirm", true);
    expect(JSON.parse(mocks.request.mock.calls.at(-1)![1].body)).toEqual({
      fingerprint: p.fingerprint,
      decision: "confirm",
      confirm_task_output: true,
    });
    mocks.request.mockResolvedValue({
      data: {
        ...p,
        status: "rejected",
        can_confirm: false,
        decided_at: p.created_at,
      },
    });
    await decideAiTaskOutput(p, "reject", true);
    expect(JSON.parse(mocks.request.mock.calls.at(-1)![1].body)).toEqual({
      fingerprint: p.fingerprint,
      decision: "reject",
    });
    mocks.request.mockResolvedValue({ data: { ...p, id: uuid(90) } });
    await expect(decideAiTaskOutput(p, "confirm", true)).rejects.toThrow();
  });
});
