import { describe, expect, it } from "vitest";
import {
  parseAiActionProposal,
  type AiActionProposal,
} from "./aiWorkspaceActions";
const task = "00000000-0000-4000-8000-000000000003";
const owner = "00000000-0000-5000-8000-000000000001";
function fixture(
  action: "task.assign" | "task.reassign" | "task.unassign" = "task.assign",
): AiActionProposal {
  const assigning = action === "task.assign",
    ending = action === "task.unassign";
  const person = "00000000-0000-4000-8000-000000000006";
  const assignment = "00000000-0000-4000-8000-000000000005";
  const common = {
    role: "assignee",
    previous_assignment_id: assigning ? null : assignment,
    task_status: "todo",
    review_policy: "manual",
    subtask_total: 1,
    subtask_completed: 1,
    subtask_cancelled: 0,
    current_submission_id: null,
  };
  return {
    id: "00000000-0000-4000-8000-000000000001",
    generation_id: "00000000-0000-4000-8000-000000000002",
    fingerprint: "a".repeat(64),
    action: {
      action,
      task_id: task,
      expected_version: 2,
      changes: {
        role: "assignee",
        ...(!ending ? { actor_id: owner } : {}),
        ...(!assigning ? { assignment_id: assignment, reason: "交接" } : {}),
      },
    },
    preview: {
      label: "责任任务",
      before: {
        ...common,
        actor_id: assigning ? null : person,
        actor_name: assigning ? null : "旧责任人",
        actor_type: assigning ? null : "person",
        actor_version: assigning ? null : 1,
      },
      after: {
        ...common,
        actor_id: ending ? null : owner,
        actor_name: ending ? null : "本人",
        actor_type: ending ? null : "owner",
        actor_version: ending ? null : 1,
        ...(!assigning ? { reason: "交接" } : {}),
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
describe("assignment proposal", () => {
  it.each(["task.assign", "task.reassign", "task.unassign"] as const)(
    "validates %s and code-owned task route",
    (action) => {
      const p = fixture(action);
      expect(parseAiActionProposal(p).route).toBe("/tasks/" + task);
      expect(
        parseAiActionProposal({
          ...p,
          status: "confirmed",
          can_confirm: false,
          result_id: task,
          result_version: 4,
          decided_at: p.created_at,
        }).status,
      ).toBe("confirmed");
    },
  );
  it.each([
    (p: AiActionProposal) => {
      p.action.changes.confirm = true;
    },
    (p: AiActionProposal) => {
      delete p.action.changes.role;
    },
    (p: AiActionProposal) => {
      p.action.changes.actor_id = null;
    },
    (p: AiActionProposal) => {
      p.preview.after.actor_id = task;
    },
    (p: AiActionProposal) => {
      p.preview.after.actor_version = 0;
    },
    (p: AiActionProposal) => {
      p.preview.after.actor_name = null;
    },
    (p: AiActionProposal) => {
      p.action.project_id = task;
    },
    (p: AiActionProposal) => {
      p.preview.before.actor_id = owner;
    },
    (p: AiActionProposal) => {
      p.preview.before.task_status = "done";
      p.preview.after.task_status = "done";
    },
    (p: AiActionProposal) => {
      p.preview.after.subtask_completed = 2;
    },
    (p: AiActionProposal) => {
      p.preview.after.current_submission_id = task;
    },
    (p: AiActionProposal) => {
      p.preview.after.hidden = "unexpected";
    },
    (p: AiActionProposal) => {
      p.result_id = task;
      p.result_version = 3;
    },
    (p: AiActionProposal) => {
      p.action.changes.role = "reviewer";
      p.preview.before.role = "reviewer";
      p.preview.after.role = "reviewer";
      p.preview.after.actor_type = "person";
    },
  ])("rejects incomplete or mismatched previews", (mutate) => {
    const p = fixture();
    mutate(p);
    expect(() => parseAiActionProposal(p)).toThrow();
  });
  it("binds replacement/ending to exact current assignment and reason", () => {
    for (const action of ["task.reassign", "task.unassign"] as const) {
      const p = fixture(action);
      p.action.changes.assignment_id = task;
      expect(() => parseAiActionProposal(p)).toThrow();
      const reason = fixture(action);
      reason.preview.after.reason = "changed";
      expect(() => parseAiActionProposal(reason)).toThrow();
    }
  });
  it("rejects another Task or an unchanged result version", () => {
    const p = fixture();
    for (const result of [
      { result_id: owner, result_version: 3 },
      { result_id: task, result_version: 2 },
    ]) {
      expect(() =>
        parseAiActionProposal({
          ...p,
          ...result,
          status: "confirmed",
          can_confirm: false,
          decided_at: p.created_at,
        }),
      ).toThrow();
    }
  });
});
