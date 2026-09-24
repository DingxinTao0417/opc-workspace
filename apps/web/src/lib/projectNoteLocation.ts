import { isWorkspaceIdentity } from "./focusReportLocation";

export interface ProjectNoteSelection {
  id: string;
}

export function projectNoteHref(projectId: string, noteId: string): string {
  return `/projects/${projectId}?note=${noteId}`;
}

export function parseProjectNoteLocation(
  params: URLSearchParams,
): ProjectNoteSelection | null {
  for (const key of params.keys()) {
    if (
      !["note", "return_session"].includes(key) ||
      params.getAll(key).length !== 1
    ) {
      return null;
    }
  }
  const id = params.get("note");
  return isWorkspaceIdentity(id) ? { id } : null;
}

export function isProjectNoteRoute(value: string): boolean {
  const match = /^\/projects\/([^/?#]+)\?([^#]+)$/.exec(value);
  if (!match || !isWorkspaceIdentity(match[1])) return false;
  const params = new URLSearchParams(match[2]);
  return (
    !params.has("return_session") && parseProjectNoteLocation(params) !== null
  );
}
