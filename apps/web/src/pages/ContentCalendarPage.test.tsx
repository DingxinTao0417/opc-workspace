import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ContentItem } from "../types/models";
import { ContentCalendarPage } from "./ContentCalendarPage";

const hooks = vi.hoisted(() => ({ items: vi.fn(), projects: vi.fn(), tasks: vi.fn(), update: vi.fn(), schedule: vi.fn(), publish: vi.fn(), link: vi.fn(), unlink: vi.fn() }));
vi.mock("../api/hooks", () => ({
  useContentItemsQuery: hooks.items,
  useProjectsQuery: hooks.projects,
  useTasksQuery: hooks.tasks,
  useCreateContentItem: () => ({ isPending: false, isError: false, mutate: vi.fn() }),
  useUpdateContentItem: () => ({ isPending: false, error: null, mutate: hooks.update }),
  useScheduleContentItem: () => ({ isPending: false, error: null, mutate: hooks.schedule }),
  usePublishContentItem: () => ({ isPending: false, error: null, mutate: hooks.publish }),
  useLinkContentItemTask: () => ({ isPending: false, error: null, mutate: hooks.link }),
  useUnlinkContentItemTask: () => ({ isPending: false, error: null, mutate: hooks.unlink }),
}));

const item: ContentItem = { id: "content-1", title: "发布产品更新", platform: "微信公众号", status: "scheduled", scheduledAt: "2026-09-04T01:00:00Z", scheduledTimezone: "Asia/Shanghai", publishedAt: null, projectId: null, notes: null, externalLink: null, manualOrder: 1024, archivedFromStatus: null, version: 1, createdAt: "2026-08-29T00:00:00Z", updatedAt: "2026-08-29T00:00:00Z", tasks: [], requiredTaskTotal: 2, requiredTaskDone: 1 };

describe("ContentCalendarPage", () => {
  afterEach(cleanup);
  beforeEach(() => {
    hooks.update.mockReset(); hooks.schedule.mockReset(); hooks.publish.mockReset(); hooks.link.mockReset(); hooks.unlink.mockReset();
    hooks.items.mockReturnValue({ data: { items: [item], meta: { page: 1, pageSize: 50, total: 1 } }, isError: false, isPending: false, isSuccess: true, refetch: vi.fn() });
    hooks.projects.mockReturnValue({ data: { items: [] }, isPending: false });
    hooks.tasks.mockReturnValue({ data: [{ id: "task-2", title: "准备配图", status: "todo" }], isPending: false });
  });
  it("renders content scheduling facts without external publishing controls", () => {
    render(<MemoryRouter><ContentCalendarPage /></MemoryRouter>);
    expect(screen.getByText("发布产品更新")).toBeTruthy();
    expect(screen.getByText("微信公众号")).toBeTruthy();
    expect(screen.getByText("1/2 项准备任务")).toBeTruthy();
    expect(screen.queryByText("发布到平台")).toBeNull();
  });

  it("opens the local edit workflow and exposes explicit schedule and publish actions", () => {
    render(<MemoryRouter><ContentCalendarPage /></MemoryRouter>);
    fireEvent.click(screen.getByRole("button", { name: "编辑 发布产品更新" }));
    expect(screen.getByRole("heading", { name: "内容详情与排期" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "保存排期" }));
    expect(hooks.schedule).toHaveBeenCalledWith(expect.objectContaining({ id: "content-1", input: expect.objectContaining({ expectedVersion: 1 }) }), expect.anything());
    fireEvent.click(screen.getByRole("button", { name: "确认已发布" }));
    expect(hooks.publish).toHaveBeenCalledWith(expect.objectContaining({ id: "content-1", input: expect.objectContaining({ expectedVersion: 1 }) }), expect.anything());
    fireEvent.change(screen.getByRole("combobox", { name: "选择准备任务" }), { target: { value: "task-2" } });
    fireEvent.click(screen.getByRole("button", { name: "关联" }));
    expect(hooks.link).toHaveBeenCalledWith(expect.objectContaining({ id: "content-1", taskId: "task-2", isRequired: true, expectedVersion: 1 }), expect.anything());
  });
});
