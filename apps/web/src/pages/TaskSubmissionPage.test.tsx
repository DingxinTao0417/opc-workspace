import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import {
  MemoryRouter,
  Route,
  Routes,
  useLocation,
  useNavigate,
  type NavigateFunction,
} from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { normalizeTaskArtifact, resetRuntimeConnection } from "../api/client";
import {
  taskArtifactDetailQueryKey,
  taskSubmissionQueryKey,
} from "../api/hooks";
import { getTaskSubmissionLocation } from "../api/taskSubmissionLocation";
import { renderAiRichText } from "../components/AiRichText";
import { aiWorkspaceHref, isAiWorkspaceRoute } from "../lib/aiWorkspaceLinks";
import { useAiChatStore } from "../store/aiChat";
import { useAiWorkbenchHandoff } from "../store/aiWorkbenchHandoff";
import { TaskSubmissionPage } from "./TaskSubmissionPage";

const id = (n: number) =>
  `018f0000-0000-7000-8000-${String(n).padStart(12, "0")}`;
const task = id(1),
  batch = id(2),
  session = id(3),
  other = id(4),
  artifactId = id(5);
const path = `/tasks/${task}/submissions/${batch}`;
const date = "2026-09-18T10:00:00Z";
const owner = {
  id: id(6),
  type: "owner",
  display_name: "我",
  status: "active",
  is_builtin: true,
  version: 1,
};
function artifact(file = false) {
  return {
    id: artifactId,
    task_id: task,
    submission_id: batch,
    submission_status: "changes_requested",
    position: 1,
    storage_kind: file ? "file" : "text",
    name: "具体产出",
    mime_type: file ? "text/plain" : null,
    size_bytes: file ? 5 : null,
    sha256: file ? "a".repeat(64) : null,
    requires_followup: false,
    produced_by_actor_id: owner.id,
    produced_by_actor: owner,
    recorded_by_actor_id: owner.id,
    recorded_by_actor: owner,
    integrity_status: "unverified",
    integrity_checked_at: null,
    deleted_at: null,
    deleted_by_actor_id: null,
    deleted_by_actor: null,
    delete_reason: null,
    created_at: date,
  };
}
function fixture(file = false) {
  return {
    data: {
      task: {
        id: task,
        title: "明确的任务",
        status: "waiting_review",
        version: 7,
        current_submission_id: other,
      },
      submission: {
        id: batch,
        task_id: task,
        sequence: 1,
        status: "changes_requested",
        origin: "manual",
        summary: "历史批次完整说明",
        submitted_by_actor_id: owner.id,
        submitted_by_actor: owner,
        submitted_at: date,
        reviewed_by_actor_id: owner.id,
        reviewed_by_actor: owner,
        reviewed_at: date,
        review_reason: "请修正证据",
        withdrawn_by_actor_id: null,
        withdrawn_by_actor: null,
        withdrawn_at: null,
        is_inferred: false,
        artifact_count: 1,
        artifacts: [artifact(file)],
      },
    },
  };
}
const response = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  });
const fetcher =
  vi.fn<(input: RequestInfo | URL, init?: RequestInit) => Promise<Response>>();
function downloadSpies() {
  const create = vi.fn(() => "blob:submission-file");
  const revoke = vi.fn();
  const anchors: HTMLAnchorElement[] = [];
  vi.stubGlobal(
    "URL",
    class extends URL {
      static createObjectURL = create;
      static revokeObjectURL = revoke;
    },
  );
  const click = vi
    .spyOn(HTMLAnchorElement.prototype, "click")
    .mockImplementation(function (this: HTMLAnchorElement) {
      anchors.push(this);
    });
  return { create, revoke, click, anchors };
}
function mount(
  initial = "/ai",
  source = session,
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, retryDelay: 0 } },
  }),
) {
  let navigate!: NavigateFunction;
  function Probe() {
    navigate = useNavigate();
    const location = useLocation();
    return (
      <output data-testid="location">
        {location.pathname}
        {location.search}
      </output>
    );
  }
  const view = render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[initial]}>
        <Probe />
        <Routes>
          <Route
            path="/ai"
            element={
              <div>{renderAiRichText(`[查看批次](${path})`, source)}</div>
            }
          />
          <Route
            path="/tasks/:taskId/submissions/:submissionId"
            element={<TaskSubmissionPage />}
          />
          <Route path="/tasks/:taskId" element={<div>任务详情目标</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return {
    ...view,
    client,
    navigate: (url: string) => act(() => navigate(url)),
  };
}
beforeEach(() => {
  resetRuntimeConnection();
  fetcher.mockReset();
  fetcher.mockImplementation(async (input) =>
    String(input).includes("/artifacts/")
      ? response({
          data: {
            ...artifact(),
            content_text: "按需读取完整正文",
            reference_url: null,
            structured_json: null,
          },
        })
      : response(fixture()),
  );
  vi.stubGlobal("fetch", fetcher);
  useAiChatStore.setState({ activeSessionId: undefined });
  useAiWorkbenchHandoff.setState({
    pending: null,
    pendingIssue: null,
    revision: 0,
  });
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  resetRuntimeConnection();
});

describe("exact submission navigation", () => {
  it.each([undefined, session])(
    "preserves only a valid return session on the next task hop (%s)",
    async (source) => {
      useAiChatStore.setState({ activeSessionId: other });
      mount(`${path}${source ? `?return_session=${source}` : ""}`);
      const next = await screen.findByRole("link", {
        name: "打开任务详情处理",
      });
      const target = `/tasks/${task}${source ? `?return_session=${source}` : ""}`;
      expect(next).toHaveAttribute("href", target);
      fireEvent.click(next);
      expect(screen.getByTestId("location")).toHaveTextContent(target);
      expect(useAiChatStore.getState().activeSessionId).toBe(other);
      expect(useAiWorkbenchHandoff.getState().pendingIssue).toBeNull();
      expect(fetcher).toHaveBeenCalledTimes(1);
      expect(
        fetcher.mock.calls.every(
          ([, init]) => !init?.method || init.method === "GET",
        ),
      ).toBe(true);
    },
  );

  it.each(["invalid", `${session}&return_session=${other}`])(
    "never exposes a next-hop link for invalid return identity %s",
    (source) => {
      mount(`${path}?return_session=${source}`);
      expect(screen.getByRole("alert")).toHaveTextContent("链接无效");
      expect(
        screen.queryByRole("link", { name: "打开任务详情处理" }),
      ).toBeNull();
      expect(fetcher).not.toHaveBeenCalled();
      expect(useAiChatStore.getState().activeSessionId).toBeUndefined();
    },
  );

  it("opens only the exact artifact in a read-only preview after navigation", async () => {
    mount(`${path}?artifact=${artifactId}&return_session=${session}`);
    expect(await screen.findByText("按需读取完整正文")).toBeVisible();
    expect(screen.getByRole("link", { name: "返回原对话" })).toBeVisible();
    expect(fetcher).toHaveBeenCalledTimes(2);
    expect(
      fetcher.mock.calls.every(
        ([, init]) => !init?.method || init.method === "GET",
      ),
    ).toBe(true);
    expect(screen.queryByRole("button", { name: /删除产出/ })).toBeNull();
  });

  it("does not substitute another or deleted artifact for an exact link", async () => {
    const view = mount(`${path}?artifact=${other}`);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "指定产出已不存在",
    );
    expect(screen.queryByText("按需读取完整正文")).toBeNull();
    expect(fetcher).toHaveBeenCalledTimes(1);
    view.navigate(`${path}?artifact=${artifactId}`);
    expect(await screen.findByText("按需读取完整正文")).toBeVisible();
  });

  it.each([session, id(99)])(
    "returns to the displayed main/side conversation %s and only reads",
    async (source) => {
      mount("/ai", source);
      fireEvent.click(screen.getByRole("link", { name: "查看批次" }));
      expect(await screen.findByText("历史批次完整说明")).toBeVisible();
      expect(screen.getByText(/这是历史提交批次/)).toBeVisible();
      expect(screen.getByText("已要求返工")).toBeVisible();
      expect(screen.queryByText("按需读取完整正文")).not.toBeInTheDocument();
      expect(
        screen.queryByRole("button", { name: /删除产出/ }),
      ).not.toBeInTheDocument();
      expect(fetcher).toHaveBeenCalledTimes(1);
      fireEvent.click(screen.getByRole("button", { name: /查看产出/ }));
      expect(await screen.findByText("按需读取完整正文")).toBeVisible();
      expect(
        fetcher.mock.calls.every(
          ([, init]) => !init?.method || init.method === "GET",
        ),
      ).toBe(true);
      fireEvent.click(screen.getByRole("link", { name: "返回原对话" }));
      expect(useAiChatStore.getState().activeSessionId).toBe(source);
    },
  );
  it("does not trust model-supplied return paths or URL parameters", () => {
    expect(isAiWorkspaceRoute(path)).toBe(true);
    expect(aiWorkspaceHref(path, session)).toBe(
      `${path}?return_session=${session}`,
    );
    for (const route of [
      `${path}?return_session=${other}`,
      `${path}#secret`,
      `${path}/..`,
      `${path}?submission_id=${other}`,
    ])
      expect(isAiWorkspaceRoute(route)).toBe(false);
  });
  it.each([
    `/tasks/bad/submissions/${batch}`,
    `${path}?return_session=evil`,
    `${path}?return_session=${session}&return_session=${other}`,
    `${path}?redirect=evil`,
    `${path}?artifact=evil`,
    `${path}?artifact=${artifactId}&artifact=${other}`,
  ])("rejects invalid location %s before fetching", (route) => {
    mount(route);
    expect(screen.getByRole("alert")).toHaveTextContent("链接无效");
    expect(fetcher).not.toHaveBeenCalled();
  });
  it.each(["missing", "wrong_task", "wrong_batch", "wrong_artifact"])(
    "does not substitute cached or mismatched facts after %s",
    async (kind) => {
      const cached = await getTaskSubmissionLocation(task, batch);
      const client = new QueryClient({
        defaultOptions: { queries: { retry: false } },
      });
      client.setQueryData(
        [...taskSubmissionQueryKey(task), "detail", batch],
        cached,
      );
      fetcher.mockImplementation(async () => {
        const value = fixture();
        if (kind === "missing")
          return response(
            { code: "TASK_SUBMISSION_NOT_FOUND", message: "没有这次提交" },
            404,
          );
        if (kind === "wrong_task") value.data.task.id = other;
        if (kind === "wrong_batch") value.data.submission.id = other;
        if (kind === "wrong_artifact")
          value.data.submission.artifacts[0].submission_id = other;
        return response(value);
      });
      mount(path, session, client);
      expect(screen.queryByText("历史批次完整说明")).not.toBeInTheDocument();
      await screen.findByText("提交批次不可用");
      expect(screen.queryByText("历史批次完整说明")).not.toBeInTheDocument();
    },
  );
  it("keeps refreshed artifact evidence hidden while fetching and on cross-batch mismatch", async () => {
    const { client } = mount(path);
    await screen.findByText("历史批次完整说明");
    client.setQueryData(
      taskArtifactDetailQueryKey(artifactId),
      normalizeTaskArtifact({
        ...artifact(),
        content_text: "缓存中的旧正文",
        reference_url: null,
        structured_json: null,
      }),
    );
    let release!: (r: Response) => void;
    fetcher.mockImplementation(
      () =>
        new Promise((resolve) => {
          release = resolve;
        }),
    );
    fireEvent.click(screen.getByRole("button", { name: /查看产出/ }));
    expect(screen.queryByText("缓存中的旧正文")).not.toBeInTheDocument();
    await waitFor(() => expect(release).toBeTypeOf("function"));
    await act(async () =>
      release(
        response({
          data: {
            ...artifact(),
            submission_id: other,
            content_text: "另一个批次的正文",
            reference_url: null,
            structured_json: null,
          },
        }),
      ),
    );
    expect(await screen.findByText(/产出与提交批次不一致/)).toBeVisible();
    expect(screen.queryByText("另一个批次的正文")).not.toBeInTheDocument();
  });
  it("ignores a late response after switching exact batch identities", async () => {
    let release!: (r: Response) => void;
    fetcher.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          release = resolve;
        }),
    );
    const view = mount(path);
    const next = fixture();
    next.data.submission.id = other;
    next.data.submission.summary = "另一批次";
    next.data.submission.artifacts = [];
    fetcher.mockImplementation(async () => response(next));
    view.navigate(`/tasks/${task}/submissions/${other}`);
    await screen.findByText("另一批次");
    await act(async () => release(response(fixture())));
    expect(screen.queryByText("历史批次完整说明")).not.toBeInTheDocument();
  });
  it("reports a missing file only after an explicit download click", async () => {
    fetcher.mockImplementation(async (input) =>
      String(input).endsWith("/content")
        ? response(
            { code: "ARTIFACT_FILE_MISSING", message: "文件缺失，不能下载" },
            410,
          )
        : response(fixture(true)),
    );
    mount(path);
    await screen.findByText("历史批次完整说明");
    expect(fetcher).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("button", { name: /下载产出/ }));
    expect(await screen.findByText("文件缺失，不能下载")).toBeVisible();
    expect(
      fetcher.mock.calls.some(([input]) =>
        String(input).endsWith(`/artifacts/${artifactId}/content`),
      ),
    ).toBe(true);
    expect(
      fetcher.mock.calls.every(
        ([, init]) => !init?.method || init.method === "GET",
      ),
    ).toBe(true);
  });
  it("hands the exact submission batch to the agent without copying evidence", async () => {
    mount(path);
    expect(await screen.findByText("历史批次完整说明")).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "交给智能体" }));

    const pending = useAiWorkbenchHandoff.getState().pendingIssue;
    expect(pending).toEqual(
      expect.objectContaining({
        label: "任务提交批次",
        route: path,
        scopes: ["work", "outputs", "actions"],
      }),
    );
    expect(pending?.prompt).toContain("workspace_task_submissions");
    expect(pending?.prompt).toContain(`submissions/${batch}`);
    expect(pending?.prompt).toContain("明确的任务");
    expect(pending?.prompt).toContain("不要把未读取的文件正文当作已检查");
    expect(pending?.prompt).not.toContain("历史批次完整说明");
    expect(screen.getByRole("link", { name: "查看批次" })).toBeVisible();
  });
  it("hands the exact file to the browser only on demand, without claiming acceptance or saving", async () => {
    const { create, revoke, click, anchors } = downloadSpies();
    fetcher.mockImplementation(async (input) =>
      String(input).endsWith("/content")
        ? new Response("hello", {
            headers: {
              "Content-Type": "text/plain",
              "Content-Disposition": 'attachment; filename="submission.txt"',
            },
          })
        : response(fixture(true)),
    );
    mount(`${path}?return_session=${session}`);
    await screen.findByText("历史批次完整说明");
    expect(create).not.toHaveBeenCalled();
    expect(fetcher).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("button", { name: /下载产出/ }));
    expect(await screen.findByText(/文件已交给浏览器下载/)).toHaveTextContent(
      "下载不等于验收通过",
    );
    expect(create).toHaveBeenCalledOnce();
    expect(click).toHaveBeenCalledOnce();
    const anchor = anchors[0];
    expect(anchor.download).toBe("submission.txt");
    expect(anchor.href).toBe("blob:submission-file");
    await waitFor(() =>
      expect(revoke).toHaveBeenCalledWith("blob:submission-file"),
    );
    expect(
      fetcher.mock.calls.every(
        ([, init]) => !init?.method || init.method === "GET",
      ),
    ).toBe(true);
    expect(screen.getByText("已要求返工")).toBeVisible();
  });
  it("aborts a download on leaving and ignores a late response even when the transport does not abort", async () => {
    const { create, click } = downloadSpies();
    let release!: (r: Response) => void;
    let signal: AbortSignal | null | undefined;
    fetcher.mockImplementation(async (input, init) => {
      if (!String(input).endsWith("/content")) return response(fixture(true));
      signal = init?.signal;
      return new Promise<Response>((resolve) => {
        release = resolve;
      });
    });
    const view = mount(path);
    await screen.findByText("历史批次完整说明");
    fireEvent.click(screen.getByRole("button", { name: /下载产出/ }));
    await waitFor(() => expect(release).toBeTypeOf("function"));
    expect(screen.getByRole("button", { name: /下载产出/ })).toBeDisabled();
    view.unmount();
    expect(signal?.aborted).toBe(true);
    await act(async () => {
      release(new Response("late bytes"));
    });
    expect(create).not.toHaveBeenCalled();
    expect(click).not.toHaveBeenCalled();
    expect(fetcher).toHaveBeenCalledTimes(2);
  });
  it("keeps deleted historical output metadata without opening or downloading its content", async () => {
    const value = fixture(true);
    Object.assign(value.data.submission.artifacts[0], {
      deleted_at: date,
      deleted_by_actor_id: owner.id,
      deleted_by_actor: owner,
      delete_reason: "已人工删除",
    });
    fetcher.mockResolvedValue(response(value));
    mount(path);
    await screen.findByText("历史批次完整说明");
    expect(screen.getByText("已删除")).toBeVisible();
    expect(screen.getByRole("button", { name: /查看产出/ })).toBeDisabled();
    expect(
      screen.queryByRole("button", { name: /下载产出/ }),
    ).not.toBeInTheDocument();
    expect(fetcher).toHaveBeenCalledTimes(1);
  });
});
