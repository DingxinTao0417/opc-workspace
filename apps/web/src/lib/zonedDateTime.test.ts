import { describe, expect, it } from "vitest";
import {
  formatDateTimeLocalInTimeZone,
  localDateTimeToZonedISOString,
} from "./zonedDateTime";

describe("zonedDateTime", () => {
  it("formats and saves wall-clock plan times in the selected IANA zone", () => {
    expect(
      formatDateTimeLocalInTimeZone(
        "2026-01-15T00:00:00.000Z",
        "Asia/Shanghai",
      ),
    ).toBe("2026-01-15T08:00");
    expect(
      localDateTimeToZonedISOString("2026-01-15T08:00", "Asia/Shanghai"),
    ).toEqual({
      kind: "valid",
      iso: "2026-01-15T00:00:00.000Z",
      ambiguous: false,
    });
  });

  it("rejects spring-forward gaps and deterministically uses the earlier fall-back instant", () => {
    expect(
      localDateTimeToZonedISOString("2026-03-08T02:30", "America/Los_Angeles"),
    ).toEqual({ kind: "nonexistent" });
    expect(
      localDateTimeToZonedISOString("2026-11-01T01:30", "America/Los_Angeles"),
    ).toEqual({
      kind: "valid",
      iso: "2026-11-01T08:30:00.000Z",
      ambiguous: true,
    });
  });
});
