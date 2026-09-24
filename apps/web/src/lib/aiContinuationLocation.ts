import { isWorkspaceIdentity } from "./focusReportLocation";

export type AiContinuationLocation =
  | { kind: "plan"; sessionId: string }
  | {
      kind: "approval";
      sessionId: string;
      generationId: string;
      proposalId: string;
    };

export function aiContinuationHref(target: AiContinuationLocation): string {
  if (!isWorkspaceIdentity(target.sessionId)) return "";
  if (target.kind === "plan") return `/ai?session=${target.sessionId}&plan=1`;
  if (
    !isWorkspaceIdentity(target.generationId) ||
    !isWorkspaceIdentity(target.proposalId)
  )
    return "";
  return `/ai?session=${target.sessionId}&generation=${target.generationId}&proposal=${target.proposalId}`;
}

export function parseAiContinuationLocation(
  params: URLSearchParams,
): AiContinuationLocation | null {
  const sessionId = params.get("session");
  if (params.getAll("session").length !== 1 || !isWorkspaceIdentity(sessionId))
    return null;
  if (params.has("plan")) {
    return params.getAll("plan").length === 1 &&
      params.get("plan") === "1" &&
      !params.has("generation") &&
      !params.has("proposal")
      ? { kind: "plan", sessionId: sessionId! }
      : null;
  }
  const generationId = params.get("generation");
  const proposalId = params.get("proposal");
  return params.getAll("generation").length === 1 &&
    params.getAll("proposal").length === 1 &&
    isWorkspaceIdentity(generationId) &&
    isWorkspaceIdentity(proposalId)
    ? {
        kind: "approval",
        sessionId: sessionId!,
        generationId: generationId!,
        proposalId: proposalId!,
      }
    : null;
}
