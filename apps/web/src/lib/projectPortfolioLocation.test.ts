import { describe, expect, it } from "vitest";
import {
  projectPortfolioHref,
  readProjectPortfolioLocation,
} from "./projectPortfolioLocation";

const clientId = "018f0000-0000-7000-8000-000000001910";

describe("project portfolio location", () => {
  it("round-trips the exact visible filters and pagination", () => {
    const view = {
      query: "品牌 官网",
      status: "in_progress" as const,
      clientId,
      page: 3,
    };
    const href = projectPortfolioHref(view);
    expect(href).toBe(
      `/projects?q=%E5%93%81%E7%89%8C+%E5%AE%98%E7%BD%91&status=in_progress&client_id=${clientId}&page=3`,
    );
    expect(readProjectPortfolioLocation(href!.split("?")[1])).toEqual(view);
    expect(
      projectPortfolioHref({ query: "", status: "", clientId: "", page: 1 }),
    ).toBe("/projects");
  });

  it("drops malformed or duplicated metadata instead of applying another filter", () => {
    expect(
      readProjectPortfolioLocation(
        `?q=one&q=two&status=invalid&client_id=${clientId.toUpperCase()}&page=0&other=ignore`,
      ),
    ).toEqual({ query: "", status: "", clientId: "", page: 1 });
    expect(readProjectPortfolioLocation("?q=%0Aignore").query).toBe("");
    expect(
      projectPortfolioHref({
        query: "safe",
        status: "",
        clientId: "not-a-uuid",
        page: 1,
      }),
    ).toBeNull();
    expect(
      projectPortfolioHref({
        query: "x".repeat(201),
        status: "",
        clientId: "",
        page: 1,
      }),
    ).toBeNull();
  });
});
