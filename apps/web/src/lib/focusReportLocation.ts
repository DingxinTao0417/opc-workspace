import type { FocusReportParams } from "../types/models";

export const focusReportViews = [
  "summary",
  "days",
  "projects",
  "tags",
  "hours",
  "heatmap",
] as const;
export type FocusReportView = (typeof focusReportViews)[number];
export interface FocusReportLocation extends FocusReportParams {
  view: FocusReportView;
}
export const focusReportLocationKeys = [
  "report",
  "date_from",
  "date_to",
  "timezone",
  "project_id",
] as const;
export const focusReportViewLabels: Record<FocusReportView, string> = {
  summary: "报告概况",
  days: "每日趋势",
  projects: "项目分布",
  tags: "标签分布",
  hours: "时段分布",
  heatmap: "专注热力图",
};

export function isWorkspaceIdentity(
  value: string | null | undefined,
): value is string {
  return (
    typeof value === "string" &&
    /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(value)
  );
}

export function focusReportDays(from: string, to: string): number | null {
  const date = (value: string) => {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return NaN;
    const time = Date.parse(`${value}T00:00:00Z`);
    return Number.isFinite(time) &&
      new Date(time).toISOString().slice(0, 10) === value
      ? time
      : NaN;
  };
  const days = (date(to) - date(from)) / 86_400_000 + 1;
  return Number.isFinite(days) ? days : null;
}

export function parseFocusReportLocation(
  params: URLSearchParams,
): FocusReportLocation | null {
  const allowed: readonly string[] = [
    ...focusReportLocationKeys,
    "return_session",
  ];
  for (const key of params.keys()) {
    if (!allowed.includes(key) || params.getAll(key).length !== 1) return null;
  }
  const dateFrom = params.get("date_from") ?? "";
  const dateTo = params.get("date_to") ?? "";
  const timezone = params.get("timezone") ?? "";
  const view = params.get("report") as FocusReportView;
  const projectId = params.get("project_id");
  const days = focusReportDays(dateFrom, dateTo);
  if (
    !focusReportViews.includes(view) ||
    days === null ||
    days < 1 ||
    days > 93 ||
    timezone.length > 100 ||
    !/^[A-Za-z][A-Za-z0-9_+\-/]*$/.test(timezone) ||
    timezone === "Local" ||
    (projectId !== null && !isWorkspaceIdentity(projectId))
  )
    return null;
  try {
    new Intl.DateTimeFormat("en", { timeZone: timezone }).format();
  } catch {
    return null;
  }
  return {
    dateFrom,
    dateTo,
    timezone,
    view,
    ...(projectId ? { projectId } : {}),
  };
}

export function focusReportLocationHref(location: FocusReportLocation): string {
  const params = new URLSearchParams({
    date_from: location.dateFrom,
    date_to: location.dateTo,
    report: location.view,
    timezone: location.timezone,
  });
  if (location.projectId) params.set("project_id", location.projectId);
  params.sort();
  return `/focus?${params}`;
}

export function focusReportReturnSession(
  params: URLSearchParams,
): string | null {
  const value = params.get("return_session");
  return params.getAll("return_session").length === 1 &&
    isWorkspaceIdentity(value)
    ? value
    : null;
}
