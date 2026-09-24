import {
  focusReportDays,
  isWorkspaceIdentity,
} from "../lib/focusReportLocation";
import type { AiActionProposal } from "./aiWorkspaceActions";

export interface AiInvoicePdfResult {
  asset_id: string;
  invoice_id: string;
  file_name: string;
  mime_type: "application/pdf";
  size_bytes: number;
  sha256: string;
  generated_from_version: number;
  generated_at: string;
  integrity_status: "verified";
  integrity_checked_at: string;
}

export function validInvoicePdfResult(p: AiActionProposal): boolean {
  const v = p.invoice_pdf_result;
  if (p.action.action !== "invoice.generate_pdf" || p.status !== "confirmed")
    return v === undefined;
  if (!v || typeof v !== "object" || Array.isArray(v)) return false;
  const keys = [
    "asset_id",
    "invoice_id",
    "file_name",
    "mime_type",
    "size_bytes",
    "sha256",
    "generated_from_version",
    "generated_at",
    "integrity_status",
    "integrity_checked_at",
  ];
  return (
    Object.keys(v).length === keys.length &&
    keys.every((k) => Object.hasOwn(v, k)) &&
    v.asset_id === p.result_id &&
    v.invoice_id === p.action.invoice_id &&
    v.generated_from_version === p.action.expected_version &&
    v.generated_from_version === p.result_version &&
    v.mime_type === "application/pdf" &&
    v.integrity_status === "verified" &&
    v.integrity_checked_at === v.generated_at &&
    validDeletionPdf({
      pdf_asset_id: v.asset_id,
      pdf_file_name: v.file_name,
      pdf_size_bytes: v.size_bytes,
      pdf_sha256: v.sha256,
      pdf_generated_from_version: v.generated_from_version,
      pdf_generated_at: v.generated_at,
    })
  );
}

const editable = [
  "client_id",
  "project_id",
  "amount_minor",
  "currency",
  "issue_date",
  "due_date",
  "notes",
];
const snapshots = [
  ...editable,
  "client_name",
  "project_name",
  "invoice_number",
  "status",
  "paid_date",
];
const pdfSnapshot = [
  "pdf_asset_id",
  "pdf_file_name",
  "pdf_size_bytes",
  "pdf_sha256",
  "pdf_generated_from_version",
  "pdf_generated_at",
];
function validDeletionPdf(
  before: AiActionProposal["preview"]["before"],
): boolean {
  if (before.pdf_asset_id === null)
    return pdfSnapshot.every((k) => before[k] === null);
  return (
    typeof before.pdf_asset_id === "string" &&
    isWorkspaceIdentity(before.pdf_asset_id) &&
    typeof before.pdf_file_name === "string" &&
    before.pdf_file_name.length >= 5 &&
    before.pdf_file_name.length <= 180 &&
    !/[\\/\u0000-\u001f\u007f]/.test(before.pdf_file_name) &&
    before.pdf_file_name.endsWith(".pdf") &&
    typeof before.pdf_sha256 === "string" &&
    /^[a-f0-9]{64}$/.test(before.pdf_sha256) &&
    typeof before.pdf_size_bytes === "number" &&
    Number.isSafeInteger(before.pdf_size_bytes) &&
    before.pdf_size_bytes > 0 &&
    before.pdf_size_bytes <= 50 * 1024 * 1024 &&
    typeof before.pdf_generated_from_version === "number" &&
    Number.isSafeInteger(before.pdf_generated_from_version) &&
    before.pdf_generated_from_version > 0 &&
    typeof before.pdf_generated_at === "string" &&
    Number.isFinite(Date.parse(before.pdf_generated_at))
  );
}
const date = (v: unknown): v is string =>
  typeof v === "string" && focusReportDays(v, v) === 1;
function field(k: string, v: unknown): boolean {
  switch (k) {
    case "client_id":
      return typeof v === "string" && isWorkspaceIdentity(v);
    case "project_id":
      return v === null || (typeof v === "string" && isWorkspaceIdentity(v));
    case "amount_minor":
      return (
        typeof v === "number" &&
        Number.isSafeInteger(v) &&
        v > 0 &&
        v <= 9_000_000_000_000_000
      );
    case "currency":
      return typeof v === "string" && /^[A-Z]{3}$/.test(v);
    case "issue_date":
    case "due_date":
    case "paid_date":
      return date(v);
    case "notes":
      return typeof v === "string" && Array.from(v).length <= 10000;
    default:
      return false;
  }
}
export function validInvoiceProposal({
  action,
  preview,
}: AiActionProposal): boolean {
  const creating = action.action === "invoice.create",
    deleting = action.action === "invoice.delete",
    generating = action.action === "invoice.generate_pdf",
    updating = action.action === "invoice.update",
    paid = action.action === "invoice.mark_paid";
  const keys = creating
    ? ["action", "changes"]
    : ["action", "changes", "invoice_id", "expected_version"];
  if (Object.keys(action).some((k) => !keys.includes(k))) return false;
  const c = action.changes,
    allowed = creating || updating ? editable : paid ? ["paid_date"] : [];
  if (Object.keys(c).some((k) => !allowed.includes(k) || !field(k, c[k])))
    return false;
  if (
    creating &&
    ["client_id", "amount_minor", "currency", "issue_date", "due_date"].some(
      (k) => !Object.hasOwn(c, k),
    )
  )
    return false;
  if ((updating && !Object.keys(c).length) || (paid && !date(c.paid_date)))
    return false;
  if (creating && Object.keys(preview.before).length) return false;
  for (const [s, after] of [
    [preview.before, false],
    [preview.after, true],
  ] as const) {
    if ((creating && !after) || ((deleting || generating) && after)) continue;
    const required =
      deleting || generating ? [...snapshots, ...pdfSnapshot] : snapshots;
    if (
      Object.keys(s).length !== required.length ||
      required.some((k) => !Object.hasOwn(s, k)) ||
      editable.some((k) => !field(k, s[k]))
    )
      return false;
    if (
      String(s.due_date) < String(s.issue_date) ||
      typeof s.client_name !== "string"
    )
      return false;
    if (
      (s.project_id === null) !== (s.project_name === null) ||
      (s.project_id !== null && typeof s.project_name !== "string")
    )
      return false;
    if (
      creating
        ? s.invoice_number !== null
        : typeof s.invoice_number !== "string" || !s.invoice_number
    )
      return false;
    if (
      !["draft", "sent", "viewed", "paid", "overdue"].includes(String(s.status))
    )
      return false;
    if (
      s.status === "paid"
        ? !date(s.paid_date) || s.paid_date < String(s.issue_date)
        : s.paid_date !== null
    )
      return false;
  }
  const a = preview.after,
    b = preview.before;
  if (deleting || generating)
    return (
      (generating || b.status === "draft") &&
      validDeletionPdf(b) &&
      Object.keys(a).length === 2 &&
      a[generating ? "pdf_generated" : "invoice_deleted"] === true &&
      a[generating ? "pdf_replaced" : "pdf_removed"] ===
        (b.pdf_asset_id !== null) &&
      (b.pdf_asset_id === null ||
        Number(b.pdf_generated_from_version) <= Number(action.expected_version))
    );
  if (creating && String(a.issue_date) < "2000-01-01") return false;
  const transitions: Record<string, [string[], string]> = {
    "invoice.update": [["draft"], "draft"],
    "invoice.mark_sent": [["draft"], "sent"],
    "invoice.mark_viewed": [["sent"], "viewed"],
    "invoice.mark_paid": [["viewed", "overdue"], "paid"],
    "invoice.mark_overdue": [["sent", "viewed"], "overdue"],
  };
  const transition = transitions[action.action];
  if (
    creating
      ? a.status !== "draft"
      : !transition ||
        !transition[0].includes(String(b.status)) ||
        a.status !== transition[1]
  )
    return false;
  for (const k of editable) {
    const expected = Object.hasOwn(c, k)
      ? c[k]
      : creating
        ? k === "notes"
          ? ""
          : null
        : b[k];
    if (a[k] !== expected) return false;
  }
  if (a.paid_date !== (paid ? c.paid_date : creating ? null : b.paid_date))
    return false;
  if (!creating && a.invoice_number !== b.invoice_number) return false;
  for (const r of ["client", "project"]) {
    if (
      !creating &&
      a[`${r}_id`] === b[`${r}_id`] &&
      a[`${r}_name`] !== b[`${r}_name`]
    )
      return false;
  }
  return true;
}
