import { describe, expect, it } from "vitest";
import {
  knowledgeIndexLocationHref,
  parseKnowledgeIndexLocation,
} from "./knowledgeIndexLocation";

const source = "018f0000-0000-7000-8000-000000000901";
const job = "018f0000-0000-7000-8000-000000000902";

describe("knowledge index location", () => {
  it("round-trips an exact source and job identity", () => {
    const href = knowledgeIndexLocationHref(source, job)!;
    expect(href).toBe(`/knowledge?source=${source}&job=${job}`);
    expect(
      parseKnowledgeIndexLocation(
        new URL(href, "http://localhost").searchParams,
      ),
    ).toEqual({ source, job });
  });

  it("rejects missing, duplicated, or unsafe identities", () => {
    for (const value of [
      `source=${source}`,
      `source=${source}&job=${job}&job=${job}`,
      `source=${source}&job=${job}&filter=ready`,
      `source=../../private&job=${job}`,
      `source=${source}&job=https://example.com`,
    ]) {
      expect(
        parseKnowledgeIndexLocation(new URLSearchParams(value)),
      ).toBeNull();
    }
    expect(knowledgeIndexLocationHref("../../private", job)).toBeNull();
  });
});
