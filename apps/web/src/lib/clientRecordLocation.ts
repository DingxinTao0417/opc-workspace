import { isWorkspaceIdentity } from "./focusReportLocation";

export type ClientRecordKind = "activity" | "followup";
export interface ClientRecordSelection {
  kind: ClientRecordKind;
  id: string;
}

export function clientRecordHref(
  clientId: string,
  kind: ClientRecordKind,
  id: string,
): string {
  return `/clients/${clientId}?${kind}=${id}`;
}

export function parseClientRecordLocation(
  params: URLSearchParams,
): ClientRecordSelection | null {
  for (const key of params.keys()) {
    if (
      !["activity", "followup", "return_session"].includes(key) ||
      params.getAll(key).length !== 1
    )
      return null;
  }
  if (params.has("activity") === params.has("followup")) return null;
  const kind = params.has("activity") ? "activity" : "followup";
  const id = params.get(kind);
  return isWorkspaceIdentity(id) ? { kind, id } : null;
}

export function isClientRecordRoute(value: string): boolean {
  const match = /^\/clients\/([^/?#]+)\?([^#]+)$/.exec(value);
  if (!match || !isWorkspaceIdentity(match[1])) return false;
  const params = new URLSearchParams(match[2]);
  return (
    !params.has("return_session") && parseClientRecordLocation(params) !== null
  );
}
