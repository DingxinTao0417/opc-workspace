import { describe, expect, it } from "vitest";
import {
  knowledgeLocationHref,
  parseKnowledgeLocation,
} from "./knowledgeLocation";

const location = {
  source_id: "018f0000-0000-7000-8000-000000006101",
  document_id: "018f0000-0000-7000-8000-000000006102",
  chunk_id: "018f0000-0000-7000-8000-000000006103",
  source_version: 2,
  document_version: 1,
};
describe("knowledge citation locations", () => {
  it("round trips server metadata without accepting arbitrary return URLs", () => {
    const href = knowledgeLocationHref(location, location.source_id)!;
    const params = new URL(href, "http://localhost").searchParams;
    expect(parseKnowledgeLocation(params)).toEqual(location);
    expect(params.get("return_session")).toBe(location.source_id);
    expect(
      knowledgeLocationHref(location, "https://example.com"),
    ).not.toContain("return_session");
    expect(
      knowledgeLocationHref({ ...location, chunk_id: "../../private" }),
    ).toBeNull();
  });
  it.each(["", "0", "-1", "1.1", "1e2", "01", "9007199254740992"])(
    "rejects malformed versions %j",
    (value) => {
      const params = new URL(
        knowledgeLocationHref(location)!,
        "http://localhost",
      ).searchParams;
      params.set("source_version", value);
      expect(parseKnowledgeLocation(params)).toBeNull();
    },
  );
  it("rejects missing and ambiguous identities", () => {
    const params = new URL(knowledgeLocationHref(location)!, "http://localhost")
      .searchParams;
    params.append("source_id", location.source_id);
    expect(parseKnowledgeLocation(params)).toBeNull();
    params.delete("source_id");
    expect(parseKnowledgeLocation(params)).toBeNull();
  });
});
