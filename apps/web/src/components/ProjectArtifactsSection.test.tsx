import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { useUiStore } from "../store/ui";
import type { ProjectArtifactItem } from "../types/models";
import { ProjectArtifactsSection } from "./ProjectArtifactsSection";

const item: ProjectArtifactItem = {
  artifact: {
    id: "artifact-1",
    taskId: "task-1",
    submissionId: "submission-1",
    submissionStatus: "accepted",
    position: 1,
    storageKind: "text",
    name: "交付说明",
    mimeType: null,
    sizeBytes: null,
    sha256: null,
    requiresFollowup: true,
    producedByActorId: "owner-1",
    producedByActor: {
      id: "owner-1",
      type: "owner",
      displayName: "我",
      status: "active",
      isBuiltin: true,
      version: 1,
    },
    recordedByActorId: "owner-1",
    recordedByActor: {
      id: "owner-1",
      type: "owner",
      displayName: "我",
      status: "active",
      isBuiltin: true,
      version: 1,
    },
    integrityStatus: "unverified",
    integrityCheckedAt: null,
    deletedAt: null,
    deletedByActorId: null,
    deletedByActor: null,
    deleteReason: null,
    createdAt: "2026-08-28T08:00:00Z",
  },
  task: { id: "task-1", title: "准备交付", status: "done" },
  submissionSequence: 2,
  followup: {
    inboxItemId: "inbox-1",
    inboxItemVersion: 5,
    status: "tracking",
    resolutionPolicy: "all_required_tasks_done",
    sourceDeletedAt: null,
    progress: {
      activeTotal: 4,
      requiredTotal: 4,
      requiredDone: 1,
      requiredRemaining: 3,
      requiredBlocked: 1,
      requiredWaitingReview: 1,
      requiredCancelled: 1,
      percent: 25,
      allRequiredDone: false,
    },
  },
};

const state = vi.hoisted(() => ({
  input: {} as Record<string, unknown>,
  items: [] as ProjectArtifactItem[],
  responsePage: null as number | null,
  total: null as number | null,
  refetch: vi.fn(),
}));

vi.mock("../api/hooks", () => ({
  useProjectArtifactsQuery: (
    _projectId: string,
    input: Record<string, unknown>,
  ) => {
    state.input = input;
    return {
      data: {
        items: state.items,
        meta: {
          page: state.responsePage ?? Number(input.page ?? 1),
          pageSize: 6,
          total: state.total ?? state.items.length,
          projectVersion: 9,
        },
      },
      isError: false,
      isFetching: false,
      isPending: false,
      isPlaceholderData: false,
      isSuccess: true,
      refetch: state.refetch,
    };
  },
}));

describe("ProjectArtifactsSection", () => {
  beforeEach(() => {
    state.refetch.mockReset();
    state.items = [item];
    state.responsePage = null;
    state.total = null;
    useUiStore.setState({ taskDetailId: null });
  });

  afterEach(cleanup);

  it("shows Task-owned outputs, opens the source Task, and switches history", () => {
    render(
      <MemoryRouter initialEntries={["/projects/project-1"]}>
        <Routes>
          <Route
            element={<ProjectArtifactsSection projectId="project-1" />}
            path="/projects/:projectId"
          />
          <Route
            element={<div>已打开产出跟进详情</div>}
            path="/inbox/:inboxItemId"
          />
        </Routes>
      </MemoryRouter>,
    );

    expect(screen.getByText("交付说明")).toBeVisible();
    expect(screen.getByText(/准备交付 · 第 2 次提交/)).toBeVisible();
    expect(screen.getByText("需要跟进")).toBeVisible();
    expect(screen.getByText("跟进中")).toBeVisible();
    expect(screen.getByText("必需任务 1/4")).toBeVisible();
    expect(screen.getByText("1 个阻塞")).toBeVisible();
    expect(screen.getByText("1 个待验收")).toBeVisible();
    expect(screen.getByText("1 个已取消（仍未满足）")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "打开任务“准备交付”" }));
    expect(useUiStore.getState().taskDetailId).toBe("task-1");

    fireEvent.click(screen.getByRole("checkbox", { name: /删除历史/ }));
    expect(state.input).toEqual(
      expect.objectContaining({ page: 1, includeDeleted: true }),
    );

    fireEvent.click(screen.getByRole("link", { name: /打开跟进/ }));
    expect(screen.getByText("已打开产出跟进详情")).toBeVisible();
  });

  it("settles on the last valid artifact page after outputs shrink", async () => {
    state.total = 7;
    const view = render(
      <MemoryRouter>
        <ProjectArtifactsSection projectId="project-1" />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect(state.input).toEqual(expect.objectContaining({ page: 2 }));

    state.total = 0;
    view.rerender(
      <MemoryRouter>
        <ProjectArtifactsSection projectId="project-1" />
      </MemoryRouter>,
    );

    await waitFor(() =>
      expect(state.input).toEqual(expect.objectContaining({ page: 1 })),
    );
  });

  it.each([
    ["open", "待拆分", "打开跟进"],
    ["tracking", "跟进中", "打开跟进"],
    ["resolved", "已解决", "查看记录"],
    ["dismissed", "已忽略", "查看记录"],
  ] as const)("renders the %s follow-up state", (status, label, linkLabel) => {
    state.items = [
      {
        ...item,
        followup: item.followup ? { ...item.followup, status } : null,
      },
    ];
    render(
      <MemoryRouter>
        <ProjectArtifactsSection projectId="project-1" />
      </MemoryRouter>,
    );

    expect(screen.getByText(label)).toBeVisible();
    expect(
      screen.getByRole("link", { name: new RegExp(linkLabel) }),
    ).toHaveAttribute("href", "/inbox/inbox-1");
  });

  it("explains an unstarted or unavailable follow-up without inventing progress", () => {
    state.items = [
      {
        ...item,
        followup: item.followup
          ? {
              ...item.followup,
              status: "open",
              progress: {
                activeTotal: 0,
                requiredTotal: 0,
                requiredDone: 0,
                requiredRemaining: 0,
                requiredBlocked: 0,
                requiredWaitingReview: 0,
                requiredCancelled: 0,
                percent: null,
                allRequiredDone: false,
              },
            }
          : null,
      },
    ];
    const { rerender } = render(
      <MemoryRouter>
        <ProjectArtifactsSection projectId="project-1" />
      </MemoryRouter>,
    );
    expect(screen.getByText("尚未拆分后续任务")).toBeVisible();

    state.items = [{ ...item, followup: null }];
    rerender(
      <MemoryRouter>
        <ProjectArtifactsSection projectId="project-1" />
      </MemoryRouter>,
    );
    expect(screen.getByText(/跟进事项尚不可用/)).toBeVisible();
    expect(screen.queryByRole("link", { name: /跟进|记录/ })).toBeNull();
  });
});
