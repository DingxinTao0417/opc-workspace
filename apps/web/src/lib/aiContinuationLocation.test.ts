import { describe, expect, it } from "vitest";
import {
  aiContinuationHref,
  parseAiContinuationLocation,
} from "./aiContinuationLocation";

const sessionId = "018f0000-0000-7000-8000-000000000751";
const generationId = "018f0000-0000-7000-8000-000000000752";
const proposalId = "018f0000-0000-7000-8000-000000000753";

describe("AI continuation locations", () => {
  it("retains the exact approval identity independently of message pagination", () => {
    const target = {
      kind: "approval" as const,
      sessionId,
      generationId,
      proposalId,
    };
    const href = aiContinuationHref(target);
    expect(
      parseAiContinuationLocation(new URLSearchParams(href.split("?")[1])),
    ).toEqual(target);
  });
  it("distinguishes a plan request from an approval", () => {
    expect(
      parseAiContinuationLocation(
        new URLSearchParams(`session=${sessionId}&plan=1`),
      ),
    ).toEqual({ kind: "plan", sessionId });
  });
  it.each([
    `session=${sessionId}&generation=${generationId}`,
    `session=${sessionId}&generation=${generationId}&proposal=invalid`,
    `session=${sessionId}&generation=${generationId}&proposal=${proposalId}&proposal=${proposalId}`,
    `session=${sessionId}&plan=1&proposal=${proposalId}`,
    `session=${sessionId}&session=${sessionId}&plan=1`,
    `session=${sessionId}&plan=2`,
  ])("rejects ambiguous or incomplete targets: %s", (search) => {
    expect(parseAiContinuationLocation(new URLSearchParams(search))).toBeNull();
  });
});
