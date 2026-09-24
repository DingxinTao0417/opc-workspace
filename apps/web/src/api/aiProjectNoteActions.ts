import type { AiActionProposal } from "./aiWorkspaceActions";

const id = (v: unknown): v is string =>
  typeof v === "string" &&
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v);
const positive = (v: unknown) => Number.isSafeInteger(v) && Number(v) > 0;
const text = (v: unknown, max: number) =>
  typeof v === "string" &&
  v.trim() === v &&
  v.length > 0 &&
  Array.from(v).length <= max;
const stamp = (v: unknown) =>
  typeof v === "string" &&
  /^\d{4}-\d{2}-\d{2}T.*(?:Z|[+-]\d{2}:\d{2})$/.test(v) &&
  Number.isFinite(Date.parse(v));
const only = (v: object, keys: string[]) =>
  Object.keys(v).every((k) => keys.includes(k));

export function validProjectNoteProposal(p: AiActionProposal) {
  const a = p.action,
    c = a.changes,
    b = p.preview.before,
    f = p.preview.after;
  const create = a.action === "project_note.create",
    remove = a.action === "project_note.delete";
  if (!create && !remove && a.action !== "project_note.update") return false;
  if (
    !only(
      a,
      create
        ? ["action", "changes"]
        : ["action", "changes", "project_note_id", "expected_version"],
    ) ||
    (!create && (!id(a.project_note_id) || !positive(a.expected_version))) ||
    !only(p.preview, ["label", "before", "after"]) ||
    p.automation_run_result !== undefined ||
    p.invoice_pdf_result !== undefined
  )
    return false;
  const base = [
    "project_id",
    "project_name",
    "project_status",
    "project_version",
    "title",
    "body",
    "occurred_at",
    "note_state",
  ];
  const validFields = (fields: typeof f) =>
    only(fields, base.concat(remove ? ["reason"] : [])) &&
    base.every((key) => Object.hasOwn(fields, key)) &&
    id(fields.project_id) &&
    text(fields.project_name, 100) &&
    positive(fields.project_version) &&
    ["planning", "in_progress", "paused", "completed"].includes(
      String(fields.project_status),
    ) &&
    text(fields.title, 200) &&
    !/[\r\n]/.test(String(fields.title)) &&
    text(fields.body, 10000) &&
    stamp(fields.occurred_at);
  if (
    !validFields(f) ||
    f.note_state !== (remove ? "deleted" : "visible") ||
    p.preview.label !== f.title ||
    !stamp(p.created_at) ||
    (create
      ? Object.keys(b).length !== 0
      : !validFields(b) ||
        b.note_state !== "visible" ||
        Object.hasOwn(b, "reason"))
  )
    return false;
  const editable = create
    ? ["project_id", "title", "body", "occurred_at"]
    : remove
      ? ["reason"]
      : ["title", "body", "occurred_at"];
  if (
    !only(c, editable) ||
    Object.keys(c).length === 0 ||
    (create && editable.some((k) => !Object.hasOwn(c, k))) ||
    Object.entries(c).some(([k, v]) => f[k] !== v) ||
    (remove && !text(c.reason, 1000))
  )
    return false;
  if (
    !create &&
    base.some(
      (k) => k !== "note_state" && !Object.hasOwn(c, k) && b[k] !== f[k],
    )
  )
    return false;
  if (p.status === "confirmed")
    return (
      id(p.result_id) &&
      (create
        ? p.result_version === 1
        : p.result_id === a.project_note_id &&
          p.result_version === a.expected_version! + 1) &&
      stamp(p.decided_at)
    );
  return (
    p.result_id === null &&
    p.result_version === null &&
    (p.status === "rejected" ? stamp(p.decided_at) : p.decided_at === null)
  );
}
