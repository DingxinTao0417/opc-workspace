import { describe, expect, it } from "vitest";
import { aiWorkspaceHref, isAiWorkspaceRoute } from "./aiWorkspaceLinks";
import {
  clientRecordHref,
  parseClientRecordLocation,
} from "./clientRecordLocation";

const clientId = "018f0000-0000-7000-8000-000000005831";
const recordId = "018f0000-0000-7000-8000-000000005832";
const sessionId = "018f0000-0000-7000-8000-000000005833";

describe("client record locations", () => {
  it.each(["activity", "followup"] as const)(
    "opens exactly one %s and adds only UI-owned return context",
    (kind) => {
      const href = clientRecordHref(clientId, kind, recordId);
      expect(href).toBe(`/clients/${clientId}?${kind}=${recordId}`);
      expect(
        parseClientRecordLocation(new URLSearchParams(href.split("?")[1])),
      ).toEqual({ kind, id: recordId });
      expect(isAiWorkspaceRoute(href)).toBe(true);
      expect(aiWorkspaceHref(href, sessionId)).toBe(
        `${href}&return_session=${sessionId}`,
      );
      expect(isAiWorkspaceRoute(`${href}&return_session=${sessionId}`)).toBe(
        false,
      );
      expect(aiWorkspaceHref(href, "not-a-session")).toBe(href);
    },
  );

  it.each([
    "activity=invalid",
    `activity=${recordId}&activity=${recordId}`,
    `activity=${recordId}&followup=${recordId}`,
    `followup=${recordId}&redirect=/ai`,
    `activity=${recordId.toUpperCase()}`,
    "activity=",
    "",
    "view=activity",
  ])("rejects ambiguous or invalid selection %s", (search) => {
    expect(parseClientRecordLocation(new URLSearchParams(search))).toBeNull();
    expect(isAiWorkspaceRoute(`/clients/${clientId}?${search}`)).toBe(false);
  });

  it("rejects external addresses, fragments, invalid parent IDs and duplicate return context", () => {
    const href = clientRecordHref(clientId, "activity", recordId);
    for (const invalid of [
      `https://example.com${href}`,
      `${href}#other`,
      href.replace(clientId, "bad"),
    ]) {
      expect(isAiWorkspaceRoute(invalid)).toBe(false);
    }
    expect(
      parseClientRecordLocation(
        new URLSearchParams(
          `activity=${recordId}&return_session=${sessionId}&return_session=${sessionId}`,
        ),
      ),
    ).toBeNull();
  });
});
