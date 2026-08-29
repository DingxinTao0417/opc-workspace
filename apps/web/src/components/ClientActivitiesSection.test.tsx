import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ClientActivity } from "../types/models";
import { ClientActivitiesSection } from "./ClientActivitiesSection";

const activity: ClientActivity = {
  id: "activity-1",
  clientId: "client-1",
  kind: "note",
  title: "项目沟通",
  body: "确认下一步",
  occurredAt: "2026-08-28T08:00:00Z",
  createdBy: { id: "owner-1", type: "owner", displayName: "Owner" },
  sourceType: null,
  sourceId: null,
  version: 2,
  deletedAt: null,
  deletedByActorId: null,
  deleteReason: null,
  createdAt: "2026-08-28T08:01:00Z",
  updatedAt: "2026-08-28T08:02:00Z",
  clientVersion: 4,
};

const state = vi.hoisted(() => ({
  items: [] as ClientActivity[],
  queryInput: null as unknown,
  create: { error: null, isPending: false, mutate: vi.fn(), reset: vi.fn() },
  update: { error: null, isPending: false, mutate: vi.fn(), reset: vi.fn() },
  remove: { error: null, isPending: false, mutate: vi.fn(), reset: vi.fn() },
  refetch: vi.fn(),
}));

vi.mock("../api/hooks", () => ({
  useClientActivitiesQuery: (_id: string, input: unknown) => {
    state.queryInput = input;
    return {
      data: {
        items: state.items,
        meta: {
          page: 1,
          pageSize: 6,
          total: state.items.length,
          clientVersion: 4,
        },
      },
      isError: false,
      isFetching: false,
      isPending: false,
      isSuccess: true,
      refetch: state.refetch,
    };
  },
  useCreateClientActivity: () => state.create,
  useUpdateClientActivity: () => state.update,
  useDeleteClientActivity: () => state.remove,
}));

describe("ClientActivitiesSection", () => {
  beforeEach(() => {
    state.items = [];
    state.create.mutate.mockClear();
    state.update.mutate.mockClear();
    state.remove.mutate.mockClear();
    state.refetch.mockClear();
  });

  afterEach(cleanup);

  it("records a real local meeting with an explicit occurrence time", () => {
    render(<ClientActivitiesSection clientId="client-1" />);

    fireEvent.click(screen.getByRole("button", { name: "记录活动" }));
    fireEvent.change(screen.getByLabelText("类型"), {
      target: { value: "meeting" },
    });
    fireEvent.change(screen.getByLabelText("标题"), {
      target: { value: "  需求复盘  " },
    });
    fireEvent.change(screen.getByLabelText("正文"), {
      target: { value: "  确认第二阶段  " },
    });
    fireEvent.change(screen.getByLabelText("发生时间"), {
      target: { value: "2026-08-27T08:00" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存活动" }));

    expect(state.create.mutate).toHaveBeenCalledWith(
      {
        clientId: "client-1",
        input: {
          kind: "meeting",
          title: "需求复盘",
          body: "确认第二阶段",
          occurredAt: expect.stringMatching(/^2026-08-27T/),
        },
      },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
  });

  it("edits with the activity version and requires a deletion reason", () => {
    state.items = [activity];
    render(<ClientActivitiesSection clientId="client-1" />);

    fireEvent.click(screen.getByRole("button", { name: "编辑活动 项目沟通" }));
    fireEvent.change(screen.getByLabelText("标题"), {
      target: { value: "更新沟通" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存修改" }));
    expect(state.update.mutate).toHaveBeenCalledWith(
      {
        id: activity.id,
        input: expect.objectContaining({
          title: "更新沟通",
          expectedVersion: 2,
        }),
      },
      expect.objectContaining({ onError: expect.any(Function) }),
    );

    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    fireEvent.click(screen.getByRole("button", { name: "删除活动 项目沟通" }));
    fireEvent.click(screen.getByRole("button", { name: "确认删除活动" }));
    expect(screen.getByText("删除原因需填写 1–1,000 个字符。")).toBeVisible();
    fireEvent.change(screen.getByLabelText("删除原因"), {
      target: { value: "重复记录" },
    });
    fireEvent.click(screen.getByRole("button", { name: "确认删除活动" }));
    expect(state.remove.mutate).toHaveBeenCalledWith(
      {
        id: activity.id,
        input: { reason: "重复记录", expectedVersion: 2 },
      },
      expect.objectContaining({ onError: expect.any(Function) }),
    );
  });

  it("switches to the auditable deleted-history query", () => {
    render(<ClientActivitiesSection clientId="client-1" />);
    fireEvent.click(screen.getByRole("checkbox", { name: "显示已删除记录" }));
    expect(state.queryInput).toEqual(
      expect.objectContaining({ includeDeleted: true, page: 1 }),
    );
  });

  it("renders project workflow facts as human-readable system-only activity", () => {
    const workflowEventId = "8b04aa98-f190-4cc4-b91f-5b883289f98a";
    state.items = [
      {
        ...activity,
        id: "activity-project-completed",
        kind: "system_reference",
        title: "项目「网站重构」已完成",
        body: null,
        sourceType: "project_workflow_event",
        sourceId: workflowEventId,
      },
    ];

    render(<ClientActivitiesSection clientId="client-1" />);

    expect(
      screen.getByText(
        "汇总手工记录的笔记与会议，以及系统记录的项目状态事实；这些内容不代表客户回访或其他外部通信。",
      ),
    ).toBeVisible();
    expect(screen.getByText("项目「网站重构」已完成")).toBeVisible();
    expect(screen.getByText("项目生命周期")).toBeVisible();
    expect(screen.getByText("来源：项目状态变更 · 系统只读")).toBeVisible();
    expect(screen.queryByText(workflowEventId)).not.toBeInTheDocument();
    expect(
      screen.queryByText("project_workflow_event"),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", {
        name: "编辑活动 项目「网站重构」已完成",
      }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", {
        name: "删除活动 项目「网站重构」已完成",
      }),
    ).not.toBeInTheDocument();
  });
});
