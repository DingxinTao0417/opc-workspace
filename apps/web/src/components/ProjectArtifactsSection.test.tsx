import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { useAiWorkbenchHandoff } from "../store/aiWorkbenchHandoff";
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
  status: "success" as "success" | "pending" | "error",
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
      isError: state.status === "error",
      isFetching: false,
      isPending: state.status === "pending",
      isPlaceholderData: false,
      isSuccess: state.status === "success",
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
    state.status = "success";
    useAiWorkbenchHandoff.setState({ pending: null, pendingIssue: null });
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

  it("hands off a precise project artifact to the agent without opening files", () => {
    const projectId = "018f0000-0000-7000-8000-000000003104";
    const artifactId = "018f0000-0000-7000-8000-000000003101";
    const taskId = "018f0000-0000-7000-8000-000000003102";
    const submissionId = "018f0000-0000-7000-8000-000000003103";
    state.items = [
      {
        ...item,
        artifact: {
          ...item.artifact,
          id: artifactId,
          taskId,
          submissionId,
          storageKind: "file",
          name: "交付包.zip",
        },
        task: { ...item.task, id: taskId, title: "准备交付" },
      },
    ];
    render(
      <MemoryRouter>
        <ProjectArtifactsSection projectId={projectId} />
      </MemoryRouter>,
    );

    expect(screen.getByRole("button", { name: "梳理项目跟进" })).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "交给智能体" }));
    expect(useAiWorkbenchHandoff.getState().pending).toBeNull();
    const pending = useAiWorkbenchHandoff.getState().pendingIssue;
    expect(pending).toMatchObject({
      label: "任务产出",
      route: `/tasks/${taskId}/submissions/${submissionId}`,
      scopes: ["work", "outputs"],
    });
    expect(pending?.prompt).toContain(`artifact_id=${artifactId}`);
    expect(pending?.prompt).toContain("workspace_get");
    expect(pending?.prompt).toContain("type=artifact");
    expect(pending?.prompt).toContain("workspace_task_submissions");
    expect(pending?.prompt).toContain("不能声称已经下载或检查文件正文");
    expect(pending?.prompt).toContain("不要验收、删除或修改产出");
  });

  it("hands off project follow-up planning using only the project identity", () => {
    const projectId = "018f0000-0000-7000-8000-000000003104";
    const fetch = vi.fn();
    vi.stubGlobal("fetch", fetch);
    try {
      render(
        <MemoryRouter initialEntries={[`/projects/${projectId}`]}>
          <Routes>
            <Route
              element={<ProjectArtifactsSection projectId={projectId} />}
              path="/projects/:projectId"
            />
            <Route element={<div>已进入智能体</div>} path="/ai" />
          </Routes>
        </MemoryRouter>,
      );

      expect(useAiWorkbenchHandoff.getState().pendingIssue).toBeNull();
      fireEvent.click(screen.getByRole("button", { name: "梳理项目跟进" }));
      expect(screen.getByText("已进入智能体")).toBeVisible();
      expect(fetch).not.toHaveBeenCalled();
      expect(useAiWorkbenchHandoff.getState().pending).toBeNull();
      const pending = useAiWorkbenchHandoff.getState().pendingIssue;
      expect(pending).toMatchObject({
        label: "项目产出与跟进",
        route: `/projects/${projectId}`,
        scopes: ["work", "outputs", "actions"],
      });
      expect(Object.keys(pending ?? {}).sort()).toEqual(
        ["label", "route", "scopes", "prompt", "requestId"].sort(),
      );
      expect(pending?.prompt).toContain(`project_id=${projectId}`);
      expect(pending?.prompt).toContain("workspace_project_outputs");
      expect(pending?.prompt).toContain("workspace_inbox_tasks");
      expect(pending?.prompt).toContain("逐页");
      expect(pending?.prompt).toContain("待我逐项确认");
      expect(pending?.prompt).toContain("不要把文件元数据当作已检查正文");
      for (const privateValue of [
        item.artifact.name,
        item.artifact.id,
        item.artifact.submissionId,
        item.task.title,
        item.task.id,
        item.followup!.inboxItemId,
        "必需任务 1/4",
      ]) {
        expect(pending?.prompt).not.toContain(privateValue);
      }
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("offers project follow-up planning on an empty list without inventing outputs", () => {
    state.items = [];
    const projectId = "018f0000-0000-7000-8000-000000003104";
    render(
      <MemoryRouter>
        <ProjectArtifactsSection projectId={projectId} />
      </MemoryRouter>,
    );
    expect(screen.getByText("项目产出为空")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "梳理项目跟进" }));
    expect(useAiWorkbenchHandoff.getState().pendingIssue?.prompt).toContain(
      `project_id=${projectId}`,
    );
  });

  it.each(["pending", "error"] as const)(
    "can prepare a fresh project query while the local list is %s",
    (status) => {
      state.items = [];
      state.status = status;
      const projectId = "018f0000-0000-7000-8000-000000003104";
      render(
        <MemoryRouter>
          <ProjectArtifactsSection projectId={projectId} />
        </MemoryRouter>,
      );
      fireEvent.click(screen.getByRole("button", { name: "梳理项目跟进" }));
      expect(useAiWorkbenchHandoff.getState().pendingIssue).toMatchObject({
        route: `/projects/${projectId}`,
        scopes: ["work", "outputs", "actions"],
      });
      expect(state.refetch).not.toHaveBeenCalled();
    },
  );

  it("does not expose a project handoff for a malformed project identity", () => {
    render(
      <MemoryRouter>
        <ProjectArtifactsSection projectId="project-1" />
      </MemoryRouter>,
    );
    expect(screen.queryByRole("button", { name: "梳理项目跟进" })).toBeNull();
  });
});
