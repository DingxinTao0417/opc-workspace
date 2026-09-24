import type { IncomeStatsParams } from "../types/models";
import { focusReportDays, isWorkspaceIdentity } from "./focusReportLocation";

export function parseFinanceReportLocation(
  params: URLSearchParams,
): IncomeStatsParams | null {
  for (const key of params.keys()) {
    if (
      !["currency", "date_from", "date_to", "return_session"].includes(key) ||
      params.getAll(key).length !== 1
    )
      return null;
  }
  const currency = params.get("currency") ?? "";
  const dateFrom = params.get("date_from") ?? "";
  const dateTo = params.get("date_to") ?? "";
  const days = focusReportDays(dateFrom, dateTo);
  return /^[A-Z]{3}$/.test(currency) &&
    days !== null &&
    days >= 1 &&
    days <= 366
    ? { currency, dateFrom, dateTo }
    : null;
}

// A model cannot supply return_session. The UI binds that to the source message.
export function isFinanceLocationRoute(value: string): boolean {
  if (value.includes("#")) return false;
  if (value.startsWith("/income?")) {
    const params = new URLSearchParams(value.slice(8));
    return (
      !params.has("return_session") &&
      parseFinanceReportLocation(params) !== null
    );
  }
  const match = /^\/(?:income|invoices)\/([^/?]+)$/.exec(value);
  return !!match && isWorkspaceIdentity(match[1]);
}
