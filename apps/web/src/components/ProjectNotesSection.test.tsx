import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ProjectNote } from "../types/models";
import { useAiWorkbenchHandoff } from "../store/aiWorkbenchHandoff";
import { ProjectNotesSection } from "./ProjectNotesSection";

const note: ProjectNote = {
  id: "note-1",
  projectId: "project-1",
  title: "交付范围",
  body: "确认第一阶段交付范围",
  occurredAt: "2026-08-28T08:00:00Z",
  createdBy: { id: "owner-1", type: "owner", displayName: "我" },
  version: 2,
  deletedAt: null,
  deletedByActorId: null,
  deleteReason: null,
  createdAt: "2026-08-28T08:01:00Z",
  updatedAt: "2026-08-28T08:02:00Z",
  projectVersion: 4,
};
const exactProjectId = "018f0000-0000-7000-8000-000000005851";
const exactNoteId = "018f0000-0000-7000-8000-000000005852";
const exactNote: ProjectNote = {
  ...note,
  id: exactNoteId,
  projectId: exactProjectId,
  title: "页外精确笔记",
  body: "不依赖当前分页读取的正文",
  version: 9,
};
const getProjectNote = vi.hoisted(() => vi.fn());

vi.mock("../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../api/client")>("../api/client");
  return { ...actual, getProjectNote };
});

const state = vi.hoisted(() => ({
  queryInput: {} as Record<string, unknown>,
  responsePage: null as number | null,
  total: 1,
  create: { mutate: vi.fn(), reset: vi.fn() },
  update: { mutate: vi.fn(), reset: vi.fn() },
  remove: { mutate: vi.fn(), reset: vi.fn() },
  refetch: vi.fn(),
}));

vi.mock("../api/hooks", () => ({
  projectNoteQueryKey: (projectId: string) => [
    "projects",
    "detail",
    projectId,
    "notes",
  ],
  useProjectNotesQuery: (
    _projectId: string,
    input: Record<string, unknown>,
  ) => {
    state.queryInput = input;
    return {
      data: {
        items: [note],
        meta: {
          page: state.responsePage ?? Number(input.page ?? 1),
          pageSize: 6,
          total: state.total,
          projectVersion: 4,
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
  useCreateProjectNote: () => ({
    ...state.create,
    error: null,
    isPending: false,
  }),
  useUpdateProjectNote: () => ({
    ...state.update,
    error: null,
    isPending: false,
  }),
  useDeleteProjectNote: () => ({
    ...state.remove,
    error: null,
    isPending: false,
  }),
}));

describe("ProjectNotesSection", () => {
  beforeEach(() => {
    state.responsePage = null;
    state.total = 1;
    state.create.mutate.mockReset();
    state.update.mutate.mockReset();
    state.remove.mutate.mockReset();
    state.refetch.mockReset();
    getProjectNote.mockReset();
    useAiWorkbenchHandoff.setState({
      pending: null,
      pendingIssue: null,
      revision: 0,
    });
  });

  afterEach(cleanup);

  it("creates, edits, deletes, and switches deleted-history queries", () => {
    render(<ProjectNotesSection archived={false} projectId="project-1" />);

    expect(screen.getByText("确认第一阶段交付范围")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "添加笔记" }));
    fireEvent.change(screen.getByLabelText("标题"), {
      target: { value: "新的决策" },
    });
    fireEvent.change(screen.getByLabelText("正文"), {
      target: { value: "记录已经确认的事实" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存笔记" }));
    expect(state.create.mutate).toHaveBeenCalledWith(
      {
        projectId: "project-1",
        input: expect.objectContaining({
          title: "新的决策",
          body: "记录已经确认的事实",
        }),
      },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );

    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    fireEvent.click(screen.getByRole("button", { name: "编辑笔记 交付范围" }));
    fireEvent.change(screen.getByLabelText("标题"), {
      target: { value: "更新后的范围" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存修改" }));
    expect(state.update.mutate).toHaveBeenCalledWith(
      {
        projectId: "project-1",
        id: "note-1",
        input: expect.objectContaining({
          title: "更新后的范围",
          expectedVersion: 2,
        }),
      },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );

    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    fireEvent.click(screen.getByRole("button", { name: "删除笔记 交付范围" }));
    fireEvent.click(screen.getByRole("button", { name: "确认删除笔记" }));
    expect(screen.getByText("删除原因需填写 1–1,000 个字符。")).toBeVisible();
    fireEvent.change(screen.getByLabelText("删除原因"), {
      target: { value: "重复记录" },
    });
    fireEvent.click(screen.getByRole("button", { name: "确认删除笔记" }));
    expect(state.remove.mutate).toHaveBeenCalledWith(
      {
        projectId: "project-1",
        id: "note-1",
        input: { reason: "重复记录", expectedVersion: 2 },
      },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );

    fireEvent.click(screen.getByRole("checkbox", { name: "显示已删除记录" }));
    expect(state.queryInput).toEqual(
      expect.objectContaining({ includeDeleted: true, page: 1 }),
    );
  });

  it("keeps archived projects readable but disables note writes", () => {
    render(<ProjectNotesSection archived projectId="project-1" />);

    expect(screen.getByText(/归档项目只读/)).toBeVisible();
    expect(screen.getByRole("button", { name: "添加笔记" })).toBeDisabled();
    expect(
      screen.queryByRole("button", { name: "编辑笔记 交付范围" }),
    ).not.toBeInTheDocument();
  });

  it("settles on the last valid note page after the history shrinks", async () => {
    state.total = 7;
    const view = render(
      <ProjectNotesSection archived={false} projectId="project-1" />,
    );

    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect(state.queryInput).toEqual(expect.objectContaining({ page: 2 }));

    state.total = 0;
    view.rerender(
      <ProjectNotesSection archived={false} projectId="project-1" />,
    );

    await waitFor(() =>
      expect(state.queryInput).toEqual(expect.objectContaining({ page: 1 })),
    );
  });

  it("renders and edits an exact page-independent note using its fresh version", async () => {
    getProjectNote.mockResolvedValue(exactNote);
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const clear = vi.fn();
    render(
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <ProjectNotesSection
            archived={false}
            onClearSelection={clear}
            projectId={exactProjectId}
            selectedId={exactNoteId}
          />
        </MemoryRouter>
      </QueryClientProvider>,
    );

    expect(await screen.findByText(exactNote.body!)).toBeVisible();
    expect(getProjectNote).toHaveBeenCalledWith(
      exactNoteId,
      expect.any(AbortSignal),
    );
    fireEvent.click(
      screen.getByRole("button", { name: `编辑笔记 ${exactNote.title}` }),
    );
    fireEvent.click(screen.getByRole("button", { name: "保存修改" }));
    expect(state.update.mutate).toHaveBeenCalledWith(
      expect.objectContaining({
        id: exactNoteId,
        input: expect.objectContaining({ expectedVersion: 9 }),
      }),
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
    fireEvent.click(screen.getByRole("button", { name: "关闭定位" }));
    expect(clear).toHaveBeenCalledOnce();
    client.clear();
  });

  it("hands an exact project note to the agent without copying note body", async () => {
    getProjectNote.mockResolvedValue(exactNote);
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <ProjectNotesSection
            archived={false}
            projectId={exactProjectId}
            selectedId={exactNoteId}
          />
        </MemoryRouter>
      </QueryClientProvider>,
    );

    expect(await screen.findByText(exactNote.body!)).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "交给智能体" }));
    const pending = useAiWorkbenchHandoff.getState().pendingIssue;
    expect(pending).toEqual(
      expect.objectContaining({
        label: "项目笔记",
        route: `/projects/${exactProjectId}?note=${exactNoteId}`,
        scopes: ["work", "actions"],
      }),
    );
    expect(pending?.prompt).toContain("workspace_project_notes");
    expect(pending?.prompt).toContain(`project_id=${exactProjectId}`);
    expect(pending?.prompt).toContain(`note_id=${exactNoteId}`);
    expect(pending?.prompt).toContain("不要按标题猜测目标");
    expect(pending?.prompt).not.toContain(exactNote.body!);
    client.clear();
  });

  it("shows deletion metadata for an exact deleted note without its old body or writes", async () => {
    getProjectNote.mockResolvedValue({
      ...exactNote,
      body: null,
      deletedAt: "2026-09-18T09:00:00Z",
      deleteReason: "信息已被后续决策取代",
    });
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <ProjectNotesSection
            archived={false}
            projectId={exactProjectId}
            selectedId={exactNoteId}
          />
        </MemoryRouter>
      </QueryClientProvider>,
    );

    expect(await screen.findByText(exactNote.title)).toBeVisible();
    expect(screen.getByText(/信息已被后续决策取代/)).toBeVisible();
    expect(screen.queryByText(exactNote.body!)).toBeNull();
    expect(
      screen.queryByRole("button", { name: `编辑笔记 ${exactNote.title}` }),
    ).toBeNull();
    expect(
      screen.queryByRole("button", { name: `删除笔记 ${exactNote.title}` }),
    ).toBeNull();
    client.clear();
  });
});
