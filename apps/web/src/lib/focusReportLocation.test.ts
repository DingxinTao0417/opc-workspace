import { describe, expect, it } from "vitest";
import {
  parseFocusReportLocation,
  focusReportLocationHref,
} from "./focusReportLocation";
import { aiWorkspaceHref, isAiWorkspaceRoute } from "./aiWorkspaceLinks";

const projectId = "018f0000-0000-7000-8000-000000005833";
const sessionId = "018f0000-0000-7000-8000-000000005834";
const report = {
  dateFrom: "2026-11-01",
  dateTo: "2026-11-02",
  timezone: "America/Los_Angeles",
  projectId,
  view: "heatmap" as const,
};
const href = () => focusReportLocationHref(report);

describe("focus report locations", () => {
  it("round trips the exact window, zone, project and dimension", () => {
    expect(
      parseFocusReportLocation(new URLSearchParams(href().split("?")[1])),
    ).toEqual(report);
    expect(isAiWorkspaceRoute(href())).toBe(true);
    expect(aiWorkspaceHref(href(), sessionId)).toBe(
      `${href()}&return_session=${sessionId}`,
    );
    // The model cannot choose a return session, even a valid one.
    expect(isAiWorkspaceRoute(`${href()}&return_session=${sessionId}`)).toBe(
      false,
    );
    expect(aiWorkspaceHref(href(), "javascript:alert(1)")).toBe(href());
  });
  it.each([
    ["date_from", "2026-02-30"],
    ["date_from", "2026-11-03"],
    ["date_from", "2026-01-01"],
    ["date_to", "2026-11-02T00:00:00Z"],
    ["timezone", "Local"],
    ["timezone", "+08:00"],
    ["timezone", "../UTC"],
    ["timezone", "Bad/Zone"],
    ["project_id", "bad"],
    ["project_id", ""],
    ["report", "unknown"],
  ])("rejects invalid %s=%s", (key, value) => {
    const params = new URLSearchParams(href().split("?")[1]);
    params.set(key, value);
    expect(parseFocusReportLocation(params)).toBeNull();
    expect(isAiWorkspaceRoute(`/focus?${params}`)).toBe(false);
  });
  it("rejects missing, duplicated, unknown parameters and fragments", () => {
    for (const key of ["report", "date_from", "date_to", "timezone"]) {
      const params = new URLSearchParams(href().split("?")[1]);
      params.delete(key);
      expect(parseFocusReportLocation(params)).toBeNull();
    }
    for (const extra of [
      "&timezone=UTC",
      "&report=days",
      "&redirect=/settings",
      "#outside",
    ]) {
      expect(isAiWorkspaceRoute(href() + extra)).toBe(false);
    }
    expect(isAiWorkspaceRoute("https://example.com" + href())).toBe(false);
    expect(isAiWorkspaceRoute("/focus/../focus" + href().slice(6))).toBe(false);
  });
  it("counts calendar days independently of DST and accepts 93 days, not 94", () => {
    const params = new URLSearchParams(
      "report=days&date_from=2026-01-01&date_to=2026-04-03&timezone=America%2FLos_Angeles",
    );
    expect(parseFocusReportLocation(params)).not.toBeNull();
    params.set("date_to", "2026-04-04");
    expect(parseFocusReportLocation(params)).toBeNull();
  });
});
