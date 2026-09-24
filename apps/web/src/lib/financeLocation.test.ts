import { describe, expect, it } from "vitest";
import { parseFinanceReportLocation } from "./financeLocation";
import { aiWorkspaceHref, isAiWorkspaceRoute } from "./aiWorkspaceLinks";

const id = "018f0000-0000-7000-8000-000000001710";
const session = "018f0000-0000-7000-8000-000000001711";
const report = "/income?currency=USD&date_from=2024-01-01&date_to=2024-12-31";
describe("finance navigation boundary", () => {
  it.each([report, `/income/${id}`, `/invoices/${id}`])(
    "binds only the UI source conversation to %s",
    (route) => {
      expect(isAiWorkspaceRoute(route)).toBe(true);
      expect(aiWorkspaceHref(route, session)).toBe(
        `${route}${route.includes("?") ? "&" : "?"}return_session=${session}`,
      );
    },
  );
  it("preserves a leap-year inclusive range without timezone conversion", () => {
    expect(
      parseFinanceReportLocation(new URLSearchParams(report.split("?")[1])),
    ).toEqual({
      currency: "USD",
      dateFrom: "2024-01-01",
      dateTo: "2024-12-31",
    });
  });
  it.each([
    `${report}&return_session=${session}`,
    `${report}&currency=CNY`,
    `${report}#other`,
    `${report}&timezone=UTC`,
    report.replace("2024-12-31", "2025-01-01"),
    report.replace("2024-01-01", "2024-02-30"),
    report.replace("USD", "usd"),
    `/income/${id}?return_session=${session}`,
    `/invoices/${id}?confirm=true`,
    `/income/${id}/../settings`,
    "/income/not-an-id",
    `https://evil.example/invoices/${id}`,
  ])("rejects %s", (route) => expect(isAiWorkspaceRoute(route)).toBe(false));
});
