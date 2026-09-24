import { describe, expect, it } from "vitest";
import { aiWorkspaceHref, isAiWorkspaceRoute } from "./aiWorkspaceLinks";
import {
  automationLocationHref,
  parseAutomationLocation,
} from "./automationLocation";

const id = "00000000-0000-5000-8000-000000000102";
const session = "018f0000-0000-7000-8000-000000000001";

describe("automation record navigation", () => {
  it.each(["rule", "run"] as const)(
    "binds %s identity and UI-owned return context",
    (kind) => {
      const route = automationLocationHref(kind, id);
      expect(route).toBe(`/settings/automation?${kind}=${id}`);
      expect(isAiWorkspaceRoute(route)).toBe(true);
      const href = aiWorkspaceHref(route, session);
      expect(
        parseAutomationLocation(new URLSearchParams(href.split("?")[1])),
      ).toEqual({ kind, id });
      expect(href).toBe(`${route}&return_session=${session}`);
      expect(isAiWorkspaceRoute(href)).toBe(false);
    },
  );

  it.each([
    `rule=${id}&run=${id}`,
    `run=${id}&run=${id}`,
    `run=bad`,
    `run=${id}&execute=true`,
    `rule=`,
    `run=${id}&return_session=${session}&return_session=${session}`,
  ])("rejects ambiguous or unknown parameters: %s", (query) => {
    expect(parseAutomationLocation(new URLSearchParams(query))).toBeNull();
    expect(isAiWorkspaceRoute(`/settings/automation?${query}`)).toBe(false);
  });

  it("does not allow arbitrary settings, fragments or external URLs", () => {
    for (const route of [
      `/settings/general?rule=${id}`,
      `/settings/automation?rule=${id}#confirm`,
      `https://x.test/settings/automation?run=${id}`,
    ]) {
      expect(isAiWorkspaceRoute(route)).toBe(false);
    }
  });
});
