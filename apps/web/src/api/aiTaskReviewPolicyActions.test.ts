import { describe, expect, it } from "vitest";
import { parseAiActionProposal } from "./aiWorkspaceActions";

function fixture(before = "none", after = "manual") {
  return {
    id: "00000000-0000-4000-8000-000000000001",
    generation_id: "00000000-0000-4000-8000-000000000002",
    fingerprint: "a".repeat(64),
    action: {
      action: "task.update",
      task_id: "00000000-0000-4000-8000-000000000003",
      expected_version: 2,
      changes: { review_policy: after },
    },
    preview: {
      label: "交付报告",
      before: { review_policy: before, status: "todo" },
      after: {
        review_policy: after,
        status: "todo",
        subtask_total: 0,
        subtask_completed: 0,
        subtask_cancelled: 0,
        parent_rollup_gates_ready: false,
        will_request_parent_review: false,
      },
    },
    status: "pending",
    can_confirm: true,
    result_id: null,
    result_version: null,
    route: "/tasks/00000000-0000-4000-8000-000000000003",
    created_at: "2026-09-18T12:00:00Z",
    decided_at: null,
  };
}

describe("Task review policy proposal contract", () => {
  it.each([
    ["none", "manual"],
    ["manual", "none"],
    ["manual", "manual"],
  ])("accepts the exact %s to %s policy preview", (before, after) => {
    const value = fixture(before, after);
    expect(parseAiActionProposal(value)).toEqual(value);
  });

  it("preserves existing fields when the policy is changed alongside them", () => {
    const value = fixture();
    const mixed = {
      ...value,
      action: {
        ...value.action,
        changes: { ...value.action.changes, title: "新报告" },
      },
      preview: {
        ...value.preview,
        before: { ...value.preview.before, title: "旧报告" },
        after: { ...value.preview.after, title: "新报告" },
      },
    };
    expect(parseAiActionProposal(mixed)).toEqual(mixed);
  });

  it.each(
    [null, "automatic", "", "Manual", 1, true, [], {}].map((policy) => [
      policy,
    ]),
  )(
    "rejects non-enum policy %j in the command and each preview side",
    (policy) => {
      const value = fixture();
      for (const invalid of [
        {
          ...value,
          action: { ...value.action, changes: { review_policy: policy } },
        },
        {
          ...value,
          preview: {
            ...value.preview,
            before: { ...value.preview.before, review_policy: policy },
          },
        },
        {
          ...value,
          preview: {
            ...value.preview,
            after: { ...value.preview.after, review_policy: policy },
          },
        },
      ]) {
        expect(() => parseAiActionProposal(invalid)).toThrow(
          /操作确认信息不完整/,
        );
      }
    },
  );

  it("rejects missing, mismatched or unrequested policy previews", () => {
    const value = fixture();
    for (const invalid of [
      { ...value, preview: { ...value.preview, before: {} } },
      { ...value, preview: { ...value.preview, after: {} } },
      {
        ...value,
        preview: {
          ...value.preview,
          after: { ...value.preview.after, review_policy: "none" },
        },
      },
      {
        ...value,
        action: { ...value.action, changes: { title: "无策略修改" } },
      },
    ]) {
      expect(() => parseAiActionProposal(invalid)).toThrow(
        /操作确认信息不完整/,
      );
    }
  });

  it.each(["confirm_review_policy", "confirm", "force", "status"])(
    "rejects the extra model-supplied %s field",
    (field) => {
      const value = fixture();
      for (const invalid of [
        { ...value, action: { ...value.action, [field]: true } },
        {
          ...value,
          action: {
            ...value.action,
            changes: { ...value.action.changes, [field]: true },
          },
        },
        { ...value, preview: { ...value.preview, [field]: true } },
      ]) {
        expect(() => parseAiActionProposal(invalid)).toThrow(
          /操作确认信息不完整/,
        );
      }
    },
  );

  it("keeps existing task updates without a review policy compatible", () => {
    const value = fixture();
    const existing = {
      ...value,
      action: { ...value.action, changes: { planned_date: "2026-09-22" } },
      preview: {
        label: value.preview.label,
        before: { planned_date: null },
        after: { planned_date: "2026-09-22" },
      },
    };
    expect(parseAiActionProposal(existing)).toEqual(existing);
  });

  it("accepts a truthful parent rollup preview without confusing it with acceptance", () => {
    const value = fixture();
    Object.assign(value.preview.after, {
      subtask_total: 3,
      subtask_completed: 2,
      subtask_cancelled: 1,
      parent_rollup_gates_ready: true,
      will_request_parent_review: true,
      status: "waiting_review",
    });
    expect(parseAiActionProposal(value)).toEqual(value);
  });

  it.each([
    { subtask_total: -1 },
    { subtask_total: 0.5 },
    { subtask_completed: 1 },
    { subtask_cancelled: 1 },
    { subtask_total: 2, subtask_completed: 2, subtask_cancelled: 1 },
    { parent_rollup_gates_ready: "true" },
    { will_request_parent_review: null },
    { status: "done" },
    { will_request_parent_review: true, status: "waiting_review" },
    { subtask_total: 1, subtask_completed: 1, parent_rollup_gates_ready: true },
  ])("rejects inconsistent policy side effects %j", (patch) => {
    const value = fixture();
    Object.assign(value.preview.after, patch);
    expect(() => parseAiActionProposal(value)).toThrow(/操作确认信息不完整/);
  });

  it.each([
    "subtask_total",
    "subtask_completed",
    "subtask_cancelled",
    "parent_rollup_gates_ready",
    "will_request_parent_review",
    "status",
  ])("rejects a missing frozen parent-review field %s", (field) => {
    const value = fixture();
    delete (value.preview.after as Record<string, unknown>)[field];
    expect(() => parseAiActionProposal(value)).toThrow(/操作确认信息不完整/);
  });

  it("preserves a same-policy no-op but rejects a real change outside todo", () => {
    const same = fixture("manual", "manual");
    same.preview.before.status = "in_progress";
    Object.assign(same.preview.after, {
      status: "in_progress",
      subtask_total: 1,
      subtask_completed: 1,
      parent_rollup_gates_ready: true,
    });
    expect(parseAiActionProposal(same)).toEqual(same);
    const changed = fixture();
    changed.preview.before.status = "in_progress";
    changed.preview.after.status = "in_progress";
    expect(() => parseAiActionProposal(changed)).toThrow(/操作确认信息不完整/);
  });

  it("does not request review when all children are cancelled or owner gates are missing", () => {
    for (const patch of [
      {
        subtask_total: 2,
        subtask_completed: 0,
        subtask_cancelled: 2,
        parent_rollup_gates_ready: true,
      },
      {
        subtask_total: 2,
        subtask_completed: 2,
        subtask_cancelled: 0,
        parent_rollup_gates_ready: false,
      },
    ]) {
      const value = fixture();
      Object.assign(value.preview.after, patch);
      expect(parseAiActionProposal(value)).toEqual(value);
    }
  });

  it.each(["title", "description", "kind", "priority", "completion_criteria"])(
    "rejects policy updates with explicit null for non-nullable %s",
    (field) => {
      const value = fixture();
      const invalid = {
        ...value,
        action: {
          ...value.action,
          changes: { ...value.action.changes, [field]: null },
        },
        preview: {
          ...value.preview,
          before: { ...value.preview.before, [field]: "旧值" },
          after: { ...value.preview.after, [field]: null },
        },
      };
      expect(() => parseAiActionProposal(invalid)).toThrow(
        /操作确认信息不完整/,
      );
    },
  );
});
