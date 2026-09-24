import { isWorkspaceIdentity } from "./focusReportLocation";

export type AutomationLocation = { kind: "rule" | "run"; id: string };

export function automationLocationHref(
  kind: AutomationLocation["kind"],
  id: string,
) {
  return `/settings/automation?${kind}=${id}`;
}

export function parseAutomationLocation(
  params: URLSearchParams,
): AutomationLocation | null {
  for (const key of params.keys()) {
    if (
      !["rule", "run", "return_session"].includes(key) ||
      params.getAll(key).length !== 1
    )
      return null;
  }
  if (params.has("rule") === params.has("run")) return null;
  const kind = params.has("rule") ? "rule" : "run";
  const id = params.get(kind);
  return isWorkspaceIdentity(id) ? { kind, id } : null;
}

export function isAutomationLocationRoute(value: string): boolean {
  const prefix = "/settings/automation?";
  if (!value.startsWith(prefix) || value.includes("#")) return false;
  const params = new URLSearchParams(value.slice(prefix.length));
  return (
    !params.has("return_session") && parseAutomationLocation(params) !== null
  );
}
