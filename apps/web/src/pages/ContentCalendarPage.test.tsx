import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ContentItem } from "../types/models";
import { ContentCalendarPage } from "./ContentCalendarPage";

const hooks = vi.hoisted(() => ({ items: vi.fn(), projects: vi.fn() }));
vi.mock("../api/hooks", () => ({
  useContentItemsQuery: hooks.items,
  useProjectsQuery: hooks.projects,
  useCreateContentItem: () => ({ isPending: false, isError: false, mutate: vi.fn() }),
}));

const item: ContentItem = { id: "content-1", title: "发布产品更新", platform: "微信公众号", status: "scheduled", scheduledAt: "2026-09-04T01:00:00Z", scheduledTimezone: "Asia/Shanghai", publishedAt: null, projectId: null, notes: null, externalLink: null, manualOrder: 1024, archivedFromStatus: null, version: 1, createdAt: "2026-08-29T00:00:00Z", updatedAt: "2026-08-29T00:00:00Z", tasks: [], requiredTaskTotal: 2, requiredTaskDone: 1 };

describe("ContentCalendarPage", () => {
  afterEach(cleanup);
  beforeEach(() => {
    hooks.items.mockReturnValue({ data: { items: [item], meta: { page: 1, pageSize: 50, total: 1 } }, isError: false, isPending: false, isSuccess: true, refetch: vi.fn() });
    hooks.projects.mockReturnValue({ data: { items: [] }, isPending: false });
  });
  it("renders content scheduling facts without external publishing controls", () => {
    render(<MemoryRouter><ContentCalendarPage /></MemoryRouter>);
    expect(screen.getByText("发布产品更新")).toBeTruthy();
    expect(screen.getByText("微信公众号")).toBeTruthy();
    expect(screen.getByText("1/2 项准备任务")).toBeTruthy();
    expect(screen.queryByText("发布到平台")).toBeNull();
  });
});
