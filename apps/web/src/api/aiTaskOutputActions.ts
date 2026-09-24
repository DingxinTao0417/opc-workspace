import type { AiActionProposal } from "./aiWorkspaceActions";

export interface AiTaskOutputPreview {
  summary: string;
  artifacts: {
    id: string | null;
    storage_kind: "text" | "link" | "structured" | "file";
    name: string;
    content: string | null;
    size_bytes: number | null;
    sha256: string | null;
    requires_followup: boolean;
    deleted: boolean;
  }[];
  assignments: {
    id: string;
    role: "assignee" | "reviewer";
    actor_id: string;
    actor_name: string;
    actor_type: "owner" | "person" | "agent";
    actor_status: string;
    actor_version: number;
    assigned_at: string;
    unassigned_at: null;
  }[];
}
export const isTaskOutputAction = (a: string) =>
  a === "task.submit_output" || a === "task.review";
const object = (v: unknown): v is Record<string, unknown> =>
  v !== null && typeof v === "object" && !Array.isArray(v);
const exact = (v: object, keys: string[]) =>
  Object.keys(v).length === keys.length &&
  Object.keys(v).every((k) => keys.includes(k));
const id = (v: unknown) =>
  typeof v === "string" &&
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v);
const natural = (v: unknown, min = 0) =>
  typeof v === "number" && Number.isSafeInteger(v) && v >= min;
const text = (v: unknown, max: number, nonempty = false) =>
  typeof v === "string" &&
  Array.from(v).length <= max &&
  (!nonempty || !!v.trim());
const validDate = (v: unknown) =>
  typeof v === "string" && Number.isFinite(Date.parse(v));
const structured = (v: unknown) => {
  try {
    return typeof v === "string" && object(JSON.parse(v));
  } catch {
    return false;
  }
};
const link = (v: unknown) => {
  try {
    const u = new URL(String(v));
    return (
      typeof v === "string" &&
      ["https:", "http:"].includes(u.protocol) &&
      !!u.hostname &&
      !u.username &&
      !u.password &&
      new TextEncoder().encode(v).length <= 4096
    );
  } catch {
    return false;
  }
};

export function validTaskOutputProposal(p: AiActionProposal): boolean {
  const a = p.action,
    c = a.changes,
    b = p.preview.before,
    f = p.preview.after,
    o = p.preview.task_output;
  const submitting = a.action === "task.submit_output";
  const before = [
    "status",
    "review_policy",
    "completion_criteria",
    "current_submission_id",
    "subtask_total",
    "subtask_completed",
    "subtask_cancelled",
  ];
  if (
    !isTaskOutputAction(a.action) ||
    !exact(a, ["action", "task_id", "expected_version", "changes"]) ||
    !id(a.task_id) ||
    !natural(a.expected_version, 1) ||
    !exact(
      c,
      submitting
        ? ["summary", "artifacts"]
        : ["submission_id", "decision", "reason"],
    ) ||
    !exact(p.preview, ["label", "before", "after", "task_output"]) ||
    !p.preview.label.trim() ||
    !exact(b, before) ||
    !exact(f, [
      ...before,
      "submission_status",
      "submission_origin",
      ...(!submitting ? ["reason"] : []),
    ]) ||
    b.review_policy !== "manual" ||
    !text(b.completion_criteria, 10000) ||
    (b.current_submission_id !== null && !id(b.current_submission_id)) ||
    [
      "review_policy",
      "completion_criteria",
      "current_submission_id",
      "subtask_total",
      "subtask_completed",
      "subtask_cancelled",
    ].some((k) => f[k] !== b[k]) ||
    ["subtask_total", "subtask_completed", "subtask_cancelled"].some(
      (k) => !natural(b[k]),
    ) ||
    Number(b.subtask_completed) + Number(b.subtask_cancelled) >
      Number(b.subtask_total) ||
    !object(o) ||
    !exact(o, ["summary", "artifacts", "assignments"]) ||
    !text(o.summary, 10000) ||
    !Array.isArray(o.artifacts) ||
    o.artifacts.length > 20 ||
    !Array.isArray(o.assignments) ||
    o.assignments.length > 2 ||
    p.invoice_pdf_result !== undefined ||
    p.automation_run_result !== undefined ||
    !validDate(p.created_at)
  )
    return false;
  const roles = new Set<string>();
  for (const row of o.assignments) {
    if (
      !object(row) ||
      !exact(row, [
        "id",
        "role",
        "actor_id",
        "actor_name",
        "actor_type",
        "actor_status",
        "actor_version",
        "assigned_at",
        "unassigned_at",
      ]) ||
      !id(row.id) ||
      !id(row.actor_id) ||
      !text(row.actor_name, 100, true) ||
      !natural(row.actor_version, 1) ||
      !["owner", "person", "agent"].includes(String(row.actor_type)) ||
      !["active", "inactive"].includes(String(row.actor_status)) ||
      !["assignee", "reviewer"].includes(String(row.role)) ||
      roles.has(String(row.role)) ||
      !validDate(row.assigned_at) ||
      row.unassigned_at !== null
    )
      return false;
    roles.add(String(row.role));
  }
  const reviewer = o.assignments.find((r) => r.role === "reviewer");
  if (
    !reviewer ||
    reviewer.actor_type !== "owner" ||
    reviewer.actor_status !== "active"
  )
    return false;
  if (
    submitting &&
    !o.assignments.some(
      (r) => r.role === "assignee" && r.actor_status === "active",
    )
  )
    return false;
  const ids = new Set<string>();
  for (const art of o.artifacts) {
    if (
      !object(art) ||
      !exact(art, [
        "id",
        "storage_kind",
        "name",
        "content",
        "size_bytes",
        "sha256",
        "requires_followup",
        "deleted",
      ]) ||
      !text(art.name, 255, true) ||
      typeof art.deleted !== "boolean" ||
      typeof art.requires_followup !== "boolean" ||
      !["text", "link", "structured", "file"].includes(
        String(art.storage_kind),
      ) ||
      (submitting
        ? art.id !== null || art.deleted || art.storage_kind === "file"
        : !id(art.id) || ids.has(String(art.id)))
    )
      return false;
    if (art.id) ids.add(String(art.id));
    if (art.storage_kind === "file") {
      if (
        art.content !== null ||
        !natural(art.size_bytes, 1) ||
        typeof art.sha256 !== "string" ||
        !/^[a-f0-9]{64}$/.test(art.sha256)
      )
        return false;
    } else {
      if (art.size_bytes !== null || art.sha256 !== null) return false;
      if (
        art.deleted
          ? art.content !== null
          : !text(art.content, 500000, true) ||
            (art.storage_kind === "link" && !link(art.content)) ||
            (art.storage_kind === "structured" && !structured(art.content))
      )
        return false;
    }
  }
  if (submitting) {
    if (
      !["todo", "in_progress"].includes(String(b.status)) ||
      f.status !== "waiting_review" ||
      f.submission_status !== "pending_review" ||
      f.submission_origin !== "manual" ||
      c.summary !== o.summary ||
      !Array.isArray(c.artifacts) ||
      c.artifacts.length !== o.artifacts.length ||
      (!o.summary.trim() && !o.artifacts.length)
    )
      return false;
    const refs = new Set<string>();
    for (let i = 0; i < c.artifacts.length; i++) {
      const item: unknown = c.artifacts[i],
        art = o.artifacts[i];
      const payload = {
        text: "content_text",
        link: "reference_url",
        structured: "structured_json",
      }[art.storage_kind as "text" | "link" | "structured"];
      if (
        !object(item) ||
        !exact(item, [
          "client_ref",
          "storage_kind",
          "name",
          "requires_followup",
          payload,
        ]) ||
        !text(item.client_ref, 100, true) ||
        refs.has(String(item.client_ref)) ||
        item.storage_kind !== art.storage_kind ||
        item.name !== art.name ||
        item.requires_followup !== art.requires_followup
      )
        return false;
      refs.add(String(item.client_ref));
      if (
        payload === "structured_json"
          ? !object(item[payload]) ||
            JSON.stringify(item[payload]) !==
              JSON.stringify(JSON.parse(art.content!))
          : item[payload] !== art.content
      )
        return false;
    }
  } else if (
    b.status !== "waiting_review" ||
    !id(c.submission_id) ||
    c.submission_id !== b.current_submission_id ||
    !["accept", "request_changes"].includes(String(c.decision)) ||
    !text(c.reason, 1000, c.decision === "request_changes") ||
    c.reason !== f.reason ||
    f.status !== (c.decision === "accept" ? "done" : "in_progress") ||
    f.submission_status !==
      (c.decision === "accept" ? "accepted" : "changes_requested") ||
    !["manual", "child_rollup"].includes(String(f.submission_origin))
  )
    return false;
  if (p.status === "confirmed")
    return (
      id(p.result_id) &&
      (!submitting ? p.result_id === c.submission_id : true) &&
      p.result_version === a.expected_version! + 1 &&
      validDate(p.decided_at)
    );
  return (
    p.result_id === null &&
    p.result_version === null &&
    (p.status === "rejected" ? validDate(p.decided_at) : p.decided_at === null)
  );
}
