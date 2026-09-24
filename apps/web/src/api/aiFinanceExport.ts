import type { AiActionProposal } from "./aiWorkspaceActions";

export const financialCSVColumns =
  "id,type,status,amount_minor,currency,occurred_on,category,client,project,invoice,notes,created_at,updated_at";
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const object = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === "object" && !Array.isArray(v);
const date = (v: unknown): v is string =>
  typeof v === "string" &&
  /^\d{4}-\d{2}-\d{2}$/.test(v) &&
  Number.isFinite(Date.parse(v)) &&
  new Date(v).toISOString().slice(0, 10) === v;

export function validFinanceExport(p: AiActionProposal): boolean {
  const a = p.action,
    f = a.changes.export_filters,
    after = p.preview.after;
  if (
    Object.keys(a).length !== 2 ||
    Object.keys(a.changes).length !== 1 ||
    !object(f)
  )
    return false;
  if (
    Object.keys(f).some(
      (k) =>
        ![
          "currency",
          "date_from",
          "date_to",
          "entry_type",
          "status",
          "category",
          "client_id",
          "project_id",
        ].includes(k),
    )
  )
    return false;
  if (
    typeof f.currency !== "string" ||
    !/^[A-Z]{3}$/.test(f.currency) ||
    !date(f.date_from) ||
    !date(f.date_to) ||
    f.date_from > f.date_to ||
    Date.parse(f.date_to) - Date.parse(f.date_from) > 365 * 86400000
  )
    return false;
  if (
    !["all", "income", "expense"].includes(String(f.entry_type)) ||
    !["active", "all", "pending", "confirmed", "voided"].includes(
      String(f.status),
    )
  )
    return false;
  if (
    "category" in f &&
    (typeof f.category !== "string" ||
      !f.category.trim() ||
      [...f.category].length > 80)
  )
    return false;
  for (const key of ["client_id", "project_id"])
    if (key in f && (typeof f[key] !== "string" || !uuid.test(f[key])))
      return false;
  if (
    Object.keys(p.preview).sort().join() !== "after,before,label" ||
    Object.keys(p.preview.before).length !== 0 ||
    Object.keys(after).sort().join() !==
      [
        "currency",
        "date_from",
        "date_to",
        "entry_type",
        "export_status",
        "category",
        "client_id",
        "project_id",
        "row_count",
        "size_bytes",
        "sha256",
        "csv_columns",
        "export_sort",
      ]
        .sort()
        .join()
  )
    return false;
  for (const key of ["currency", "date_from", "date_to", "entry_type"])
    if (after[key] !== f[key]) return false;
  if (
    after.export_status !== f.status ||
    after.category !== (f.category ?? "") ||
    after.client_id !== (f.client_id ?? null) ||
    after.project_id !== (f.project_id ?? null)
  )
    return false;
  if (
    after.csv_columns !== financialCSVColumns ||
    after.export_sort !== "occurred_on DESC, created_at DESC, id ASC" ||
    typeof after.sha256 !== "string" ||
    !/^[a-f0-9]{64}$/.test(after.sha256)
  )
    return false;
  if (
    typeof after.row_count !== "number" ||
    !Number.isSafeInteger(after.row_count) ||
    after.row_count < 0 ||
    after.row_count > 10000 ||
    typeof after.size_bytes !== "number" ||
    !Number.isSafeInteger(after.size_bytes) ||
    after.size_bytes < 1 ||
    after.size_bytes > 16 * 1024 * 1024
  )
    return false;
  if (
    p.invoice_pdf_result !== undefined ||
    p.automation_run_result !== undefined
  )
    return false;
  if (p.status === "confirmed")
    return (
      p.result_id === p.id &&
      p.result_version === 1 &&
      typeof p.decided_at === "string" &&
      Number.isFinite(Date.parse(p.decided_at))
    );
  return (
    p.result_id === null &&
    p.result_version === null &&
    (p.status === "rejected"
      ? typeof p.decided_at === "string" &&
        Number.isFinite(Date.parse(p.decided_at))
      : p.decided_at === null)
  );
}
