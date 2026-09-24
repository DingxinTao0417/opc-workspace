import { describe, expect, it } from "vitest";
import { parseAiAccessRequest } from "./aiAccessRequest";

describe("saved workspace access request", () => {
  it("accepts only bounded known scopes", () => {
    expect(parseAiAccessRequest({ scopes: ["work", "actions"] })).toEqual({
      scopes: ["work", "actions"],
    });
    expect(parseAiAccessRequest(null)).toBeUndefined();
    expect(
      parseAiAccessRequest({
        scopes: ["work", "actions", "outputs", "clients"],
      })?.scopes,
    ).toHaveLength(4);
    for (const value of [
      { scopes: [] },
      { scopes: ["work", "work"] },
      { scopes: ["unknown"] },
      { scopes: ["work"], granted: true },
    ])
      expect(() => parseAiAccessRequest(value)).toThrow();
  });
});
