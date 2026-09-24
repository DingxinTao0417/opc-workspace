import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useTaskArtifactQuery } from "../api/hooks";
import {
  useWorkspacePanels,
  type WorkspacePanel,
} from "../store/workspacePanels";
import { useAiWorkbenchHandoff } from "../store/aiWorkbenchHandoff";
import { useUiStore } from "../store/ui";
import type { TaskArtifact } from "../types/models";
import { AiIssueHandoffCard } from "./AiWorkbenchHandoff";
import { TaskArtifactWorkspacePreview } from "./TaskArtifactWorkspacePreview";

vi.mock("../api/hooks", () => ({ useTaskArtifactQuery: vi.fn() }));

const artifactId = "018f0000-0000-7000-8000-000000000041";
const taskId = "018f0000-0000-7000-8000-000000000042";
const submissionId = "018f0000-0000-7000-8000-000000000043";
const sessionId = "018f0000-0000-7000-8000-000000000044";
const panel: WorkspacePanel = {
  id: `task-artifact:${artifactId}`,
  kind: "artifact",
  title: "任务产出",
  artifactId,
  artifactTaskId: taskId,
  artifactSubmissionId: submissionId,
  returnSession: sessionId,
};
const artifact = {
  id: artifactId,
  taskId,
  submissionId,
  name: "核对说明",
  storageKind: "text",
  contentText: "受控产出正文",
  structuredJson: null,
  referenceUrl: null,
  deletedAt: null,
  sizeBytes: null,
  mimeType: null,
  sha256: null,
} as TaskArtifact;
const refetch = vi.fn();

function setQuery(
  value: Partial<ReturnType<typeof useTaskArtifactQuery>> = {},
) {
  vi.mocked(useTaskArtifactQuery).mockReturnValue({
    data: artifact,
    isPending: false,
    isFetching: false,
    isError: false,
    refetch,
    ...value,
  } as ReturnType<typeof useTaskArtifactQuery>);
}

function mount(target = panel) {
  return render(
    <MemoryRouter>
      <TaskArtifactWorkspacePreview panel={target} />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  refetch.mockReset();
  setQuery();
});
afterEach(() => {
  cleanup();
  useAiWorkbenchHandoff.setState({ pending: null, pendingIssue: null });
  useWorkspacePanels.setState({ maximized: false });
  useUiStore.setState({ rightOverviewCollapsed: false });
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

function LocationProbe() {
  const location = useLocation();
  return (
    <output data-testid="location">
      {location.pathname + location.search}
    </output>
  );
}

describe("right-side exact Task Artifact preview", () => {
  it("shows the exact text without leaving chat or downloading and offers a full-detail link", () => {
    mount();
    expect(screen.getByText("受控产出正文")).toBeVisible();
    expect(screen.getByRole("link", { name: "打开完整详情" })).toHaveAttribute(
      "href",
      `/tasks/${taskId}/submissions/${submissionId}?artifact=${artifactId}&return_session=${sessionId}`,
    );
    expect(screen.queryByRole("button", { name: /下载/ })).toBeNull();
    expect(useTaskArtifactQuery).toHaveBeenCalledWith(artifactId, true, true);
  });

  it("stages an identity-only follow-up in the current chat without navigating or sending", () => {
    const onPrepare = vi.fn();
    useWorkspacePanels.setState({ maximized: true });
    render(
      <MemoryRouter initialEntries={["/ai?session=keep"]}>
        <TaskArtifactWorkspacePreview panel={panel} />
        <AiIssueHandoffCard disabled={false} onPrepare={onPrepare} />
        <LocationProbe />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "继续交给智能体" }));
    expect(screen.getByTestId("location")).toHaveTextContent(
      "/ai?session=keep",
    );
    expect(useWorkspacePanels.getState().maximized).toBe(false);
    expect(screen.getByText("让智能体处理：任务产出")).toBeVisible();
    const pending = useAiWorkbenchHandoff.getState().pendingIssue;
    expect(pending?.scopes).toEqual(["work", "outputs"]);
    expect(pending?.route).toBe(
      `/tasks/${taskId}/submissions/${submissionId}?artifact=${artifactId}`,
    );
    expect(pending?.prompt).toContain(`artifact_id=${artifactId}`);
    expect(pending?.prompt).not.toContain("受控产出正文");
    expect(pending?.prompt).not.toContain("核对说明");
    expect(onPrepare).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "带入问题并选择权限" }));
    expect(onPrepare).toHaveBeenCalledWith(pending);
    expect(useAiWorkbenchHandoff.getState().pendingIssue).toBeNull();
  });

  it("reveals the chat handoff card from a narrow right-side workspace", () => {
    vi.stubGlobal("matchMedia", () => ({ matches: true }));
    mount();
    fireEvent.click(screen.getByRole("button", { name: "继续交给智能体" }));
    expect(useUiStore.getState().rightOverviewCollapsed).toBe(true);
    expect(useAiWorkbenchHandoff.getState().pendingIssue).not.toBeNull();
  });

  it("hides a cached body while a fresh read is in flight or fails", () => {
    setQuery({ isFetching: true });
    const view = mount();
    expect(screen.queryByText("受控产出正文")).toBeNull();
    expect(screen.queryByRole("button", { name: "继续交给智能体" })).toBeNull();
    expect(screen.getByRole("status")).toHaveTextContent("正在读取指定产出");
    setQuery({ isError: true });
    view.rerender(
      <MemoryRouter>
        <TaskArtifactWorkspacePreview panel={panel} />
      </MemoryRouter>,
    );
    expect(screen.queryByText("受控产出正文")).toBeNull();
    expect(screen.queryByRole("button", { name: "继续交给智能体" })).toBeNull();
    expect(screen.getByRole("alert")).toHaveTextContent("不会展示旧缓存");
    fireEvent.click(screen.getByRole("button", { name: "刷新事实" }));
    expect(refetch).toHaveBeenCalledTimes(1);
  });

  it.each([
    [{ taskId: "018f0000-0000-7000-8000-000000000099" }, "不一致"],
    [{ submissionId: "018f0000-0000-7000-8000-000000000099" }, "不一致"],
    [{ deletedAt: "2026-09-21T00:00:00Z" }, "已删除"],
  ])("never displays a mismatched or deleted body", (changes, message) => {
    setQuery({ data: { ...artifact, ...changes } });
    mount();
    expect(screen.getByRole("alert")).toHaveTextContent(message);
    expect(screen.queryByText("受控产出正文")).toBeNull();
    expect(screen.queryByRole("button", { name: "继续交给智能体" })).toBeNull();
    expect(screen.queryByRole("link", { name: "打开完整详情" })).toBeNull();
  });

  it("keeps controlled files metadata-only and requires full detail for manual download", () => {
    setQuery({
      data: {
        ...artifact,
        storageKind: "file",
        contentText: null,
        mimeType: "application/pdf",
        sizeBytes: 123,
        sha256: "a".repeat(64),
        integrityStatus: "verified",
      },
    });
    mount();
    expect(screen.getByText("application/pdf")).toBeVisible();
    expect(screen.getByText("123 字节", { exact: false })).toBeVisible();
    expect(screen.queryByText("受控产出正文")).toBeNull();
    expect(screen.queryByRole("button", { name: /下载/ })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "继续交给智能体" }));
    expect(useAiWorkbenchHandoff.getState().pendingIssue?.scopes).toEqual([
      "work",
      "outputs",
    ]);
    expect(useAiWorkbenchHandoff.getState().pendingIssue?.prompt).toContain(
      "另行手选“产出文件正文”（output_files）",
    );
  });

  it("does not request a detail for an invalid panel identity", () => {
    mount({ ...panel, artifactTaskId: "invalid" });
    expect(screen.getByRole("alert")).toHaveTextContent("身份无效");
    expect(useTaskArtifactQuery).toHaveBeenCalledWith(null, true, true);
  });
});
