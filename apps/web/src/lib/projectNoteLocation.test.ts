import { describe, expect, it } from "vitest";
import { aiWorkspaceHref, isAiWorkspaceRoute } from "./aiWorkspaceLinks";
import {
  isProjectNoteRoute,
  parseProjectNoteLocation,
  projectNoteHref,
} from "./projectNoteLocation";

const projectId = "018f0000-0000-7000-8000-000000005831";
const noteId = "018f0000-0000-7000-8000-000000005832";
const sessionId = "018f0000-0000-7000-8000-000000005833";

describe("project note locations", () => {
  it("opens one exact note and adds only UI-owned return context", () => {
    const href = projectNoteHref(projectId, noteId);
    expect(href).toBe(`/projects/${projectId}?note=${noteId}`);
    expect(
      parseProjectNoteLocation(new URLSearchParams(href.split("?")[1])),
    ).toEqual({ id: noteId });
    expect(isProjectNoteRoute(href)).toBe(true);
    expect(isAiWorkspaceRoute(href)).toBe(true);
    expect(aiWorkspaceHref(href, sessionId)).toBe(
      `${href}&return_session=${sessionId}`,
    );
    expect(isAiWorkspaceRoute(`${href}&return_session=${sessionId}`)).toBe(
      false,
    );
  });

  it.each([
    "note=invalid",
    `note=${noteId}&note=${noteId}`,
    `note=${noteId}&redirect=/ai`,
    `note=${noteId.toUpperCase()}`,
    "note=",
    "",
  ])("rejects ambiguous or invalid selection %s", (search) => {
    expect(parseProjectNoteLocation(new URLSearchParams(search))).toBeNull();
    expect(isProjectNoteRoute(`/projects/${projectId}?${search}`)).toBe(false);
  });

  it("rejects external addresses, fragments and invalid project identities", () => {
    const href = projectNoteHref(projectId, noteId);
    for (const invalid of [
      `https://example.com${href}`,
      `${href}#other`,
      href.replace(projectId, "bad"),
    ]) {
      expect(isProjectNoteRoute(invalid)).toBe(false);
      expect(isAiWorkspaceRoute(invalid)).toBe(false);
    }
  });
});
