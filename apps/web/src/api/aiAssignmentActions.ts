import type { AiActionProposal } from "./aiWorkspaceActions";

export function isAssignmentAction(action: string) {
  return ["task.assign", "task.reassign", "task.unassign"].includes(action);
}
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const id = (v: unknown) => typeof v === "string" && uuid.test(v);
const version = (v: unknown) =>
  typeof v === "number" && Number.isSafeInteger(v) && v >= 1;
const actorKeys = ["actor_id", "actor_name", "actor_type", "actor_version"];
const fields = [
  ...actorKeys,
  "role",
  "previous_assignment_id",
  "task_status",
  "review_policy",
  "subtask_total",
  "subtask_completed",
  "subtask_cancelled",
  "current_submission_id",
];
const exact = (v: object, keys: string[]) =>
  Object.keys(v).length === keys.length &&
  Object.keys(v).every((k) => keys.includes(k));
export function validAssignmentProposal(p: AiActionProposal): boolean {
  const a = p.action,
    c = a.changes,
    b = p.preview.before,
    f = p.preview.after;
  const assign = a.action === "task.assign",
    unassign = a.action === "task.unassign";
  const keys = [
    "role",
    ...(!unassign ? ["actor_id"] : []),
    ...(!assign ? ["assignment_id", "reason"] : []),
  ];
  const actor = (v: typeof f, empty: boolean) => {
    if (empty) return actorKeys.every((k) => v[k] === null);
    return (
      id(v.actor_id) &&
      typeof v.actor_name === "string" &&
      v.actor_name.trim().length > 0 &&
      Array.from(v.actor_name).length <= 100 &&
      ["owner", "person", "agent"].includes(String(v.actor_type)) &&
      version(v.actor_version)
    );
  };
  if (
    !exact(a, ["action", "task_id", "expected_version", "changes"]) ||
    !id(a.task_id) ||
    !version(a.expected_version) ||
    !exact(c, keys) ||
    !["assignee", "reviewer"].includes(String(c.role)) ||
    (!unassign && !id(c.actor_id)) ||
    (!assign &&
      (!id(c.assignment_id) ||
        typeof c.reason !== "string" ||
        !c.reason.trim() ||
        Array.from(c.reason).length > 1000)) ||
    !exact(b, fields) ||
    !exact(f, [...fields, ...(!assign ? ["reason"] : [])]) ||
    !actor(b, assign) ||
    !actor(f, unassign) ||
    b.role !== c.role ||
    f.role !== c.role ||
    b.previous_assignment_id !== (assign ? null : c.assignment_id) ||
    f.previous_assignment_id !== b.previous_assignment_id ||
    (!unassign &&
      (f.actor_id !== c.actor_id ||
        (c.role === "reviewer" && f.actor_type !== "owner"))) ||
    (!assign && f.reason !== c.reason) ||
    (!assign && !unassign && b.actor_id === f.actor_id) ||
    ![
      "todo",
      "in_progress",
      "blocked",
      "waiting_review",
      "done",
      "cancelled",
    ].includes(String(b.task_status)) ||
    (!unassign && ["done", "cancelled"].includes(String(b.task_status))) ||
    ["subtask_total", "subtask_completed", "subtask_cancelled"].some(
      (k) =>
        typeof b[k] !== "number" ||
        !Number.isSafeInteger(b[k]) ||
        Number(b[k]) < 0 ||
        f[k] !== b[k],
    ) ||
    Number(b.subtask_completed) + Number(b.subtask_cancelled) >
      Number(b.subtask_total) ||
    (b.current_submission_id !== null && !id(b.current_submission_id)) ||
    f.current_submission_id !== b.current_submission_id ||
    f.task_status !== b.task_status ||
    !["none", "manual"].includes(String(b.review_policy)) ||
    f.review_policy !== b.review_policy ||
    !p.preview.label.trim() ||
    p.preview.tasks !== undefined ||
    p.preview.next_followup !== undefined ||
    p.preview.automation_retry !== undefined ||
    p.automation_run_result !== undefined ||
    p.invoice_pdf_result !== undefined
  )
    return false;
  if (p.status === "confirmed")
    return (
      p.result_id === a.task_id &&
      version(p.result_version) &&
      p.result_version! > a.expected_version! &&
      !!p.decided_at &&
      Number.isFinite(Date.parse(p.decided_at))
    );
  return (
    p.result_id === null &&
    p.result_version === null &&
    (p.status === "rejected"
      ? !!p.decided_at && Number.isFinite(Date.parse(p.decided_at))
      : p.decided_at === null)
  );
}
