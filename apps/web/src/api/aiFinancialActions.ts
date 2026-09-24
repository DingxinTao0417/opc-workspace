import {
  focusReportDays,
  isWorkspaceIdentity,
} from "../lib/focusReportLocation";
import type { AiActionProposal } from "./aiWorkspaceActions";

const base = [
  "type",
  "amount_minor",
  "currency",
  "occurred_on",
  "status",
  "category",
  "client_id",
  "project_id",
  "notes",
];
const all = [...base, "client_name", "project_name"];
const length = (value: string) => Array.from(value).length;
function field(key: string, value: unknown, voided = false): boolean {
  switch (key) {
    case "type":
      return value === "income" || value === "expense";
    case "amount_minor":
      return (
        typeof value === "number" &&
        Number.isSafeInteger(value) &&
        value > 0 &&
        value <= 9_000_000_000_000_000
      );
    case "currency":
      return typeof value === "string" && /^[A-Z]{3}$/.test(value);
    case "occurred_on":
      return typeof value === "string" && focusReportDays(value, value) === 1;
    case "status":
      return (
        value === "pending" ||
        value === "confirmed" ||
        (voided && value === "voided")
      );
    case "category":
      return (
        typeof value === "string" &&
        length(value.trim()) > 0 &&
        length(value) <= 80
      );
    case "notes":
      return typeof value === "string" && length(value) <= 10000;
    case "reason":
      return (
        typeof value === "string" &&
        length(value.trim()) > 0 &&
        length(value) <= 1000
      );
    case "client_id":
    case "project_id":
      return (
        value === null ||
        (typeof value === "string" && isWorkspaceIdentity(value))
      );
    case "client_name":
    case "project_name":
      return value === null || typeof value === "string";
    default:
      return false;
  }
}

// Reject incomplete or internally contradictory human previews before rendering.
export function validFinancialProposal(proposal: AiActionProposal): boolean {
  const { action, preview } = proposal;
  const creating = action.action === "financial_entry.create";
  const voiding = action.action === "financial_entry.void";
  const allowedActionKeys = creating
    ? ["action", "changes"]
    : ["action", "changes", "financial_entry_id", "expected_version"];
  if (Object.keys(action).some((key) => !allowedActionKeys.includes(key)))
    return false;
  const changes = action.changes;
  const keys = Object.keys(changes);
  if (
    !keys.length ||
    keys.some(
      (key) =>
        !(voiding ? ["reason"] : base).includes(key) ||
        !field(key, changes[key]),
    )
  )
    return false;
  if (creating && base.slice(0, 6).some((key) => !(key in changes)))
    return false;
  if (creating && Object.keys(preview.before).length !== 0) return false;
  for (const [snapshot, isAfter] of [
    [preview.before, false],
    [preview.after, true],
  ] as const) {
    if (creating && !isAfter) continue;
    const expected = isAfter && voiding ? [...all, "reason"] : all;
    if (
      Object.keys(snapshot).length !== expected.length ||
      expected.some(
        (key) =>
          !Object.hasOwn(snapshot, key) ||
          !field(key, snapshot[key], isAfter && voiding),
      )
    )
      return false;
    for (const relation of ["client", "project"]) {
      if (
        (snapshot[`${relation}_id`] === null) !==
        (snapshot[`${relation}_name`] === null)
      )
        return false;
    }
  }
  if (voiding && preview.after.status !== "voided") return false;
  for (const key of base) {
    // Native association rules may fill an omitted/null customer from a project.
    if (
      key === "client_id" &&
      (changes.client_id === null || changes.client_id === undefined) &&
      preview.after.project_id !== null
    )
      continue;
    const expected = Object.hasOwn(changes, key)
      ? changes[key]
      : creating
        ? key === "notes"
          ? ""
          : null
        : preview.before[key];
    if (voiding && key === "status") continue;
    if (preview.after[key] !== expected) return false;
  }
  return !voiding || preview.after.reason === changes.reason;
}

export function financialMinorDisplay(
  amount: number,
  currency: string,
): string {
  // Ledger minor units are hundredths, independent of Intl currency defaults.
  const integer = BigInt(amount);
  return `${currency} ${integer / 100n}.${String(integer % 100n).padStart(2, "0")}（${amount} 最小单位）`;
}
