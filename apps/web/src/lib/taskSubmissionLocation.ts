import { isWorkspaceIdentity } from "./focusReportLocation";

export function taskSubmissionHref(taskId: string, submissionId: string) {
  return `/tasks/${taskId}/submissions/${submissionId}`;
}

export function taskArtifactHref(
  taskId: string,
  submissionId: string,
  artifactId: string,
) {
  return `${taskSubmissionHref(taskId, submissionId)}?artifact=${artifactId}`;
}

// Model routes carry only identities. Return context is supplied by the UI.
export function isTaskSubmissionRoute(value: string): boolean {
  const parts = /^\/tasks\/([^/?#]+)\/submissions\/([^/?#]+)$/.exec(value);
  return (
    !!parts && isWorkspaceIdentity(parts[1]) && isWorkspaceIdentity(parts[2])
  );
}

export function validTaskSubmissionLocation(
  taskId: string | undefined,
  submissionId: string | undefined,
  params: URLSearchParams,
) {
  return (
    isWorkspaceIdentity(taskId) &&
    isWorkspaceIdentity(submissionId) &&
    [...params.keys()].every(
      (key) => key === "return_session" || key === "artifact",
    ) &&
    params.getAll("return_session").length <= 1 &&
    params.getAll("artifact").length <= 1 &&
    (!params.has("return_session") ||
      isWorkspaceIdentity(params.get("return_session"))) &&
    (!params.has("artifact") || isWorkspaceIdentity(params.get("artifact")))
  );
}
