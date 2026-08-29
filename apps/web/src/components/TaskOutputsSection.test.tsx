import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../api/client";
import type {
  ActorSummary,
  Task,
  TaskArtifactSummary,
  TaskAssignment,
  TaskSubmission,
} from "../types/models";
import { TaskOutputsSection } from "./TaskOutputsSection";

const apiMocks = vi.hoisted(() => ({
  getTaskSubmissions: vi.fn(),
  getTaskArtifacts: vi.fn(),
  getTaskArtifact: vi.fn(),
  getTaskAssignments: vi.fn(),
  submitTaskOutput: vi.fn(),
  reviewTaskSubmission: vi.fn(),
  deleteTaskArtifact: vi.fn(),
  downloadTaskArtifact: vi.fn(),
}));

vi.mock("../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../api/client")>("../api/client");
  return { ...actual, ...apiMocks };
});

const owner: ActorSummary = {
  id: "actor-owner",
  type: "owner",
  displayName: "我",
  status: "active",
  isBuiltin: true,
  version: 1,
};

const person: ActorSummary = {
  id: "actor-person",
  type: "person",
  displayName: "陈设计",
  status: "active",
  isBuiltin: false,
  version: 1,
};

const system: ActorSummary = {
  id: "actor-system",
  type: "system",
  displayName: "系统",
  status: "active",
  isBuiltin: true,
  version: 1,
};

const task: Task = {
  id: "task-1",
  title: "交付视觉稿",
  description: "",
  kind: "work",
  status: "todo",
  priority: "P1",
  projectId: null,
  parentTaskId: null,
  completionCriteria: "文件可打开",
  reviewPolicy: "manual",
  blockedReason: null,
  blockedAt: null,
  blockedFromStatus: null,
  dueDate: null,
  plannedDate: null,
  estimatedMinutes: 60,
  actualMinutes: 0,
  manualOrder: null,
  version: 4,
  subtaskTotal: 0,
  subtaskCompleted: 0,
  createdAt: "2026-08-27T00:00:00Z",
  updatedAt: "2026-08-27T00:00:00Z",
  completedAt: null,
  submittedAt: null,
  reviewedAt: null,
  currentSubmissionId: null,
  tags: [],
};

function assignment(
  role: "assignee" | "reviewer",
  actor: ActorSummary,
): TaskAssignment {
  return {
    id: `assignment-${role}`,
    taskId: task.id,
    role,
    actorId: actor.id,
    actor,
    assignedByActorId: owner.id,
    assignedByActor: owner,
    assignedAt: "2026-08-27T01:00:00Z",
    unassignedAt: null,
    reason: null,
    isActive: true,
    inferred: false,
  };
}

const textArtifact: TaskArtifactSummary = {
  id: "artifact-text",
  taskId: task.id,
  submissionId: "submission-1",
  submissionStatus: "pending_review",
  position: 1,
  storageKind: "text",
  name: "交付说明",
  mimeType: null,
  sizeBytes: null,
  sha256: null,
  requiresFollowup: false,
  producedByActorId: person.id,
  producedByActor: person,
  recordedByActorId: owner.id,
  recordedByActor: owner,
  integrityStatus: "unverified",
  integrityCheckedAt: null,
  deletedAt: null,
  deletedByActorId: null,
  deletedByActor: null,
  deleteReason: null,
  createdAt: "2026-08-27T02:00:00Z",
};

const fileArtifact: TaskArtifactSummary = {
  ...textArtifact,
  id: "artifact-file",
  position: 2,
  storageKind: "file",
  name: "visual-final.png",
  mimeType: "image/png",
  sizeBytes: 1024,
  sha256: "a".repeat(64),
  integrityStatus: "verified",
  integrityCheckedAt: "2026-08-27T02:00:00Z",
};

const submission: TaskSubmission = {
  id: "submission-1",
  taskId: task.id,
  sequence: 1,
  status: "pending_review",
  origin: "manual",
  summary: "请验收最终稿",
  submittedByActorId: owner.id,
  submittedByActor: owner,
  submittedAt: "2026-08-27T02:00:00Z",
  reviewedByActorId: null,
  reviewedByActor: null,
  reviewedAt: null,
  reviewReason: null,
  withdrawnByActorId: null,
  withdrawnByActor: null,
  withdrawnAt: null,
  isInferred: false,
  artifacts: [textArtifact, fileArtifact],
};

function renderSection(
  value: Task = task,
  options: {
    onBusyChange?: (busy: boolean) => void;
    onRefreshTask?: () => Promise<Task | null>;
  } = {},
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const onTaskUpdated = vi.fn();
  const onRefreshTask =
    options.onRefreshTask ?? vi.fn(async () => ({ ...value, version: 5 }));
  const view = render(
    <QueryClientProvider client={queryClient}>
      <TaskOutputsSection
        onBusyChange={options.onBusyChange}
        onRefreshTask={onRefreshTask}
        onTaskUpdated={onTaskUpdated}
        task={value}
      />
    </QueryClientProvider>,
  );
  return { ...view, onRefreshTask, onTaskUpdated, queryClient };
}

describe("TaskOutputsSection", () => {
  beforeEach(() => {
    apiMocks.getTaskSubmissions.mockResolvedValue({
      items: [],
      meta: { page: 1, pageSize: 10, total: 0, taskVersion: task.version },
    });
    apiMocks.getTaskArtifacts.mockResolvedValue({
      items: [],
      meta: { page: 1, pageSize: 20, total: 0, taskVersion: task.version },
    });
    apiMocks.getTaskAssignments.mockResolvedValue({
      active: {
        assignee: assignment("assignee", person),
        reviewer: assignment("reviewer", owner),
      },
      history: [],
      meta: { page: 1, pageSize: 20, total: 0, taskVersion: task.version },
    });
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("explains direct completion for tasks without review", async () => {
    renderSection({ ...task, reviewPolicy: "none" });

    expect(await screen.findByText("此任务无需验收")).toBeVisible();
    expect(screen.getByText(/可在“任务状态”中直接完成/)).toBeVisible();
    expect(screen.queryByRole("button", { name: "提交验收" })).toBeNull();
  });

  it("explains that completed non-cancelled children do not auto-complete a none-policy parent", async () => {
    renderSection({
      ...task,
      reviewPolicy: "none",
      subtaskTotal: 3,
      subtaskCompleted: 2,
      subtaskCancelled: 1,
    });

    expect(await screen.findByText("非取消子任务已全部完成")).toBeVisible();
    expect(screen.getByText(/当前任务不会自动完成/)).toBeVisible();
  });

  it("labels system-generated child-rollup submissions", async () => {
    const childRollupSubmission: TaskSubmission = {
      ...submission,
      origin: "child_rollup",
      submittedByActorId: system.id,
      submittedByActor: system,
      summary: "所有非取消子任务已完成，等待父任务验收。",
      artifacts: [],
    };
    const waitingParent: Task = {
      ...task,
      status: "waiting_review",
      currentSubmissionId: childRollupSubmission.id,
      subtaskTotal: 3,
      subtaskCompleted: 2,
      subtaskCancelled: 1,
      version: task.version + 1,
    };
    apiMocks.getTaskSubmissions.mockResolvedValue({
      items: [childRollupSubmission],
      meta: {
        page: 1,
        pageSize: 10,
        total: 1,
        taskVersion: waitingParent.version,
      },
    });

    renderSection(waitingParent);

    expect(await screen.findByText("子任务汇总")).toBeVisible();
    expect(
      screen.getByText(/系统根据非取消子任务完成情况发起验收/),
    ).toBeVisible();
    expect(
      screen.getByText("所有非取消子任务已完成，等待父任务验收。"),
    ).toBeVisible();
    expect(screen.getByText("本批次仅提交了摘要。")).toBeVisible();
  });

  it("blocks manual submission until both assignee and reviewer exist", async () => {
    apiMocks.getTaskAssignments.mockResolvedValue({
      active: { assignee: null, reviewer: null },
      history: [],
      meta: { page: 1, pageSize: 20, total: 0, taskVersion: task.version },
    });
    renderSection();

    expect(await screen.findByText("提交前还缺少责任角色")).toBeVisible();
    expect(screen.getByText(/请先在上方设置负责人/)).toBeVisible();
    expect(screen.getByRole("button", { name: "提交验收" })).toBeDisabled();
  });

  it("submits mixed text and browser File drafts", async () => {
    const waitingTask = {
      ...task,
      status: "waiting_review" as const,
      currentSubmissionId: submission.id,
      version: 5,
    };
    apiMocks.submitTaskOutput.mockResolvedValue({
      task: waitingTask,
      submission,
      artifacts: submission.artifacts,
      event: { id: "event-submit", action: "task_output_submitted" },
    });
    renderSection();
    await screen.findByText(/陈设计 负责产出/);

    fireEvent.change(screen.getByLabelText("提交摘要"), {
      target: { value: "最终视觉稿与说明" },
    });
    fireEvent.click(screen.getByRole("button", { name: "添加产出" }));
    const textNameInput = screen.getByLabelText("第 1 项产出名称");
    fireEvent.change(textNameInput, {
      target: { value: "交付说明" },
    });
    expect(
      fireEvent.keyDown(textNameInput, { key: "Enter", code: "Enter" }),
    ).toBe(false);
    expect(
      fireEvent.keyDown(textNameInput, {
        key: "Enter",
        code: "Enter",
        isComposing: true,
      }),
    ).toBe(true);
    fireEvent.change(screen.getByLabelText("第 1 项文本内容"), {
      target: { value: "已完成所有页面" },
    });
    fireEvent.change(screen.getByLabelText("新增产出类型"), {
      target: { value: "file" },
    });
    fireEvent.click(screen.getByRole("button", { name: "添加产出" }));
    const draftFile = new File(["draft"], "visual-draft.png", {
      type: "image/png",
      lastModified: 122,
    });
    const fileInput = screen.getByLabelText("第 2 项本地文件");
    fireEvent.change(fileInput, {
      target: { files: [draftFile] },
    });
    expect(screen.getByLabelText("第 2 项产出名称")).toHaveValue(
      "visual-draft.png",
    );
    const file = new File(["pixels"], "visual-final.png", {
      type: "image/png",
      lastModified: 123,
    });
    fireEvent.change(fileInput, {
      target: { files: [file] },
    });
    expect(screen.getByLabelText("第 2 项产出名称")).toHaveValue(
      "visual-final.png",
    );
    fireEvent.click(screen.getByRole("button", { name: "收起并保留" }));
    expect(screen.queryByLabelText("第 2 项本地文件")).toBeNull();
    const continueButton = screen.getByRole("button", { name: "继续编辑" });
    await waitFor(() => expect(continueButton).toHaveFocus());
    fireEvent.click(continueButton);
    expect(screen.getByText(/visual-final\.png/)).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "提交验收" }));

    await waitFor(() => expect(apiMocks.submitTaskOutput).toHaveBeenCalled());
    const input = apiMocks.submitTaskOutput.mock.calls[0][1];
    expect(input).toMatchObject({
      summary: "最终视觉稿与说明",
      expectedVersion: task.version,
      artifacts: [
        expect.objectContaining({
          storageKind: "text",
          contentText: "已完成所有页面",
        }),
        expect.objectContaining({
          storageKind: "file",
          name: "visual-final.png",
        }),
      ],
    });
    expect(input.artifacts[1].file).toBe(file);
  });

  it("validates text by Unicode characters and structured JSON by UTF-8 bytes", async () => {
    renderSection();
    await screen.findByText(/陈设计 负责产出/);

    fireEvent.click(screen.getByRole("button", { name: "添加产出" }));
    fireEvent.change(screen.getByLabelText("第 1 项产出名称"), {
      target: { value: "超长文本" },
    });
    fireEvent.change(screen.getByLabelText("第 1 项文本内容"), {
      target: { value: "😀".repeat(500_001) },
    });
    fireEvent.click(screen.getByRole("button", { name: "提交验收" }));
    expect(
      await screen.findByText(
        "第 1 项文本产出不能超过 500000 个 Unicode 字符。",
      ),
    ).toBeVisible();

    fireEvent.change(screen.getByLabelText("第 1 项文本内容"), {
      target: { value: "有效文本" },
    });
    fireEvent.change(screen.getByLabelText("新增产出类型"), {
      target: { value: "structured" },
    });
    fireEvent.click(screen.getByRole("button", { name: "添加产出" }));
    fireEvent.change(screen.getByLabelText("第 2 项产出名称"), {
      target: { value: "超大 JSON" },
    });
    fireEvent.change(screen.getByLabelText("第 2 项结构化内容"), {
      target: { value: JSON.stringify({ value: "文".repeat(350_000) }) },
    });
    fireEvent.click(screen.getByRole("button", { name: "提交验收" }));
    expect(
      await screen.findByText("第 2 项结构化内容编码后不能超过 1 MiB。"),
    ).toBeVisible();
    expect(apiMocks.submitTaskOutput).not.toHaveBeenCalled();
  });

  it("accepts output and requires a reason when requesting changes", async () => {
    const waitingTask = {
      ...task,
      status: "waiting_review" as const,
      currentSubmissionId: submission.id,
      submittedAt: submission.submittedAt,
    };
    apiMocks.getTaskSubmissions.mockResolvedValue({
      items: [submission],
      meta: { page: 1, pageSize: 10, total: 1, taskVersion: task.version },
    });
    apiMocks.reviewTaskSubmission.mockResolvedValue({
      task: { ...waitingTask, status: "done", version: 5 },
      submission: { ...submission, status: "accepted" },
      event: { id: "event-review", action: "task_review_accepted" },
    });
    renderSection(waitingTask);

    fireEvent.click(await screen.findByRole("button", { name: "接受并完成" }));
    expect(apiMocks.reviewTaskSubmission).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "取消并保留" }));
    const continueReviewButton = screen.getByRole("button", {
      name: "继续验收草稿",
    });
    await waitFor(() => expect(continueReviewButton).toHaveFocus());
    fireEvent.click(continueReviewButton);
    fireEvent.click(screen.getByRole("button", { name: "确认提交" }));
    await waitFor(() =>
      expect(apiMocks.reviewTaskSubmission).toHaveBeenCalledWith(
        task.id,
        { decision: "accept", expectedVersion: task.version },
        expect.any(String),
      ),
    );

    cleanup();
    vi.clearAllMocks();
    apiMocks.getTaskSubmissions.mockResolvedValue({
      items: [submission],
      meta: { page: 1, pageSize: 10, total: 1, taskVersion: task.version },
    });
    apiMocks.getTaskAssignments.mockResolvedValue({
      active: {
        assignee: assignment("assignee", person),
        reviewer: assignment("reviewer", owner),
      },
      history: [],
      meta: { page: 1, pageSize: 20, total: 0, taskVersion: task.version },
    });
    apiMocks.reviewTaskSubmission.mockResolvedValue({
      task: { ...waitingTask, status: "in_progress", version: 5 },
      submission: { ...submission, status: "changes_requested" },
      event: { id: "event-changes", action: "task_changes_requested" },
    });
    renderSection(waitingTask);
    fireEvent.click(await screen.findByRole("button", { name: "要求返工" }));
    fireEvent.click(screen.getByRole("button", { name: "确认提交" }));
    expect(await screen.findByText("要求返工时必须填写原因。")).toBeVisible();
    expect(apiMocks.reviewTaskSubmission).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText("返工原因"), {
      target: { value: "移动端间距仍需修正" },
    });
    fireEvent.click(screen.getByRole("button", { name: "确认提交" }));
    await waitFor(() =>
      expect(apiMocks.reviewTaskSubmission).toHaveBeenCalledWith(
        task.id,
        {
          decision: "request_changes",
          reason: "移动端间距仍需修正",
          expectedVersion: task.version,
        },
        expect.any(String),
      ),
    );
  });

  it("disables review when cached prerequisites fail to refresh", async () => {
    const waitingTask = {
      ...task,
      status: "waiting_review" as const,
      currentSubmissionId: submission.id,
      submittedAt: submission.submittedAt,
    };
    apiMocks.getTaskSubmissions.mockResolvedValue({
      items: [submission],
      meta: { page: 1, pageSize: 10, total: 1, taskVersion: task.version },
    });
    const { queryClient } = renderSection(waitingTask);

    const acceptButton = await screen.findByRole("button", {
      name: "接受并完成",
    });
    fireEvent.click(acceptButton);
    const confirmButton = screen.getByRole("button", { name: "确认提交" });
    expect(confirmButton).toBeEnabled();

    apiMocks.getTaskAssignments.mockRejectedValue(
      new Error("assignment refresh failed"),
    );
    apiMocks.getTaskSubmissions.mockRejectedValue(
      new Error("submission refresh failed"),
    );
    await act(async () => {
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: ["task-assignments", task.id],
        }),
        queryClient.invalidateQueries({
          queryKey: ["task-submissions", task.id],
        }),
      ]);
    });

    expect(
      await screen.findByText("无法确认负责人和审核人，提交与验收已暂停。"),
    ).toBeVisible();
    expect(
      screen.getByText("当前提交读取失败，验收操作已暂停。"),
    ).toBeVisible();
    expect(acceptButton).toBeDisabled();
    expect(screen.getByRole("button", { name: "要求返工" })).toBeDisabled();
    expect(confirmButton).toBeDisabled();
    fireEvent.click(confirmButton);
    expect(apiMocks.reviewTaskSubmission).not.toHaveBeenCalled();
  });

  it("keeps the exact File draft across a version conflict and explicit retry", async () => {
    const latestTask = { ...task, version: 5 };
    apiMocks.submitTaskOutput
      .mockRejectedValueOnce(
        new ApiError("任务已变化", { code: "VERSION_CONFLICT", status: 409 }),
      )
      .mockResolvedValueOnce({
        task: {
          ...latestTask,
          status: "waiting_review",
          currentSubmissionId: submission.id,
        },
        submission,
        artifacts: submission.artifacts,
        event: { id: "event-submit", action: "task_output_submitted" },
      });
    const onRefreshTask = vi.fn(async () => latestTask);
    renderSection(task, { onRefreshTask });
    await screen.findByText(/陈设计 负责产出/);
    fireEvent.change(screen.getByLabelText("提交摘要"), {
      target: { value: "冲突也要保留" },
    });
    fireEvent.change(screen.getByLabelText("新增产出类型"), {
      target: { value: "file" },
    });
    fireEvent.click(screen.getByRole("button", { name: "添加产出" }));
    const file = new File(["same-object"], "draft.psd", {
      type: "image/vnd.adobe.photoshop",
      lastModified: 456,
    });
    const input = screen.getByLabelText("第 1 项本地文件") as HTMLInputElement;
    fireEvent.change(input, { target: { files: [file] } });
    fireEvent.click(screen.getByRole("button", { name: "提交验收" }));

    expect(
      await screen.findByText(
        /已读取最新版 v5；摘要、文本、链接、结构化内容和已选择文件仍保留/,
      ),
    ).toBeVisible();
    expect(input.files?.[0]).toBe(file);
    fireEvent.click(screen.getByRole("button", { name: "保留草稿重试" }));
    fireEvent.click(screen.getByRole("button", { name: "提交验收" }));

    await waitFor(() =>
      expect(apiMocks.submitTaskOutput).toHaveBeenCalledTimes(2),
    );
    expect(apiMocks.submitTaskOutput.mock.calls[1][1].expectedVersion).toBe(5);
    expect(apiMocks.submitTaskOutput.mock.calls[1][1].artifacts[0].file).toBe(
      file,
    );
  });

  it("shows controlled download errors and requires soft-delete confirmation", async () => {
    const acceptedSubmission: TaskSubmission = {
      ...submission,
      status: "accepted",
      reviewedByActorId: owner.id,
      reviewedByActor: owner,
      reviewedAt: "2026-08-27T03:00:00Z",
      artifacts: submission.artifacts.map((artifact) => ({
        ...artifact,
        submissionStatus: "accepted",
      })),
    };
    const doneTask = {
      ...task,
      status: "done" as const,
      currentSubmissionId: submission.id,
      version: 5,
    };
    apiMocks.getTaskSubmissions.mockResolvedValue({
      items: [acceptedSubmission],
      meta: { page: 1, pageSize: 10, total: 1, taskVersion: doneTask.version },
    });
    apiMocks.downloadTaskArtifact
      .mockRejectedValueOnce(
        new ApiError("文件缺失", {
          code: "ARTIFACT_FILE_MISSING",
          status: 410,
        }),
      )
      .mockRejectedValueOnce(
        new ApiError("storage failed", {
          code: "ARTIFACT_STORAGE_ERROR",
          status: 500,
        }),
      );
    apiMocks.deleteTaskArtifact.mockResolvedValue({
      task: { ...doneTask, version: 6 },
      artifact: {
        ...textArtifact,
        submissionStatus: "accepted",
        deletedAt: "2026-08-27T04:00:00Z",
        deletedByActorId: owner.id,
        deletedByActor: owner,
        deleteReason: "重复内容",
      },
      event: { id: "event-delete", action: "task_artifact_deleted" },
    });
    renderSection(doneTask);
    const historyButton = await screen.findByRole("button", {
      name: "提交历史 1",
    });
    expect(historyButton).toHaveAttribute("aria-controls");
    fireEvent.click(historyButton);

    const downloadButton = await screen.findByRole("button", {
      name: "下载产出“visual-final.png”",
    });
    expect(
      screen.getByRole("button", { name: "查看产出“visual-final.png”" }),
    ).toHaveAttribute("aria-controls");
    fireEvent.click(downloadButton);
    expect(await screen.findByText("受控文件已缺失，无法下载。")).toBeVisible();
    await waitFor(() => expect(downloadButton).toBeEnabled());
    fireEvent.click(downloadButton);
    expect(
      await screen.findByText("本地产出存储操作失败，请稍后重试。"),
    ).toBeVisible();
    const deleteButtons = screen.getAllByRole("button", {
      name: /^删除产出/,
    });
    fireEvent.click(deleteButtons[0]);
    expect(apiMocks.deleteTaskArtifact).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "取消并保留" }));
    const continueDeleteButton = screen.getByRole("button", {
      name: "继续删除草稿",
    });
    await waitFor(() => expect(continueDeleteButton).toHaveFocus());
    fireEvent.click(continueDeleteButton);
    fireEvent.click(screen.getByRole("button", { name: "确认软删除" }));
    expect(await screen.findByText("删除产出时必须填写原因。")).toBeVisible();
    fireEvent.change(screen.getByLabelText("删除原因"), {
      target: { value: "重复内容" },
    });
    fireEvent.click(screen.getByRole("button", { name: "确认软删除" }));
    await waitFor(() =>
      expect(apiMocks.deleteTaskArtifact).toHaveBeenCalledWith(
        textArtifact.id,
        task.id,
        { reason: "重复内容", expectedVersion: doneTask.version },
        expect.any(String),
      ),
    );
  });

  it("uses an Artifact's authoritative submission status for deletion", async () => {
    const acceptedArtifact: TaskArtifactSummary = {
      ...textArtifact,
      submissionStatus: "accepted",
    };
    const doneTask = {
      ...task,
      status: "done" as const,
      currentSubmissionId: submission.id,
      version: 5,
    };
    apiMocks.getTaskSubmissions.mockResolvedValue({
      items: [],
      meta: { page: 1, pageSize: 10, total: 0, taskVersion: doneTask.version },
    });
    apiMocks.getTaskArtifacts.mockResolvedValue({
      items: [acceptedArtifact],
      meta: { page: 1, pageSize: 20, total: 1, taskVersion: doneTask.version },
    });
    renderSection(doneTask);

    fireEvent.click(
      await screen.findByRole("button", {
        name: "查看全部产出（含已删除）",
      }),
    );

    expect(
      await screen.findByRole("button", {
        name: "删除产出“交付说明”",
      }),
    ).toBeEnabled();
  });

  it("serializes downloads and reports download work as busy", async () => {
    const secondFileArtifact: TaskArtifactSummary = {
      ...fileArtifact,
      id: "artifact-file-2",
      position: 3,
      name: "visual-mobile.png",
      submissionStatus: "accepted",
    };
    const acceptedSubmission: TaskSubmission = {
      ...submission,
      status: "accepted",
      reviewedByActorId: owner.id,
      reviewedByActor: owner,
      reviewedAt: "2026-08-27T03:00:00Z",
      artifacts: [
        { ...fileArtifact, submissionStatus: "accepted" },
        secondFileArtifact,
      ],
    };
    const doneTask = {
      ...task,
      status: "done" as const,
      currentSubmissionId: submission.id,
      version: 5,
    };
    apiMocks.getTaskSubmissions.mockResolvedValue({
      items: [acceptedSubmission],
      meta: { page: 1, pageSize: 10, total: 1, taskVersion: doneTask.version },
    });
    let rejectDownload!: (reason: unknown) => void;
    apiMocks.downloadTaskArtifact.mockImplementation(
      () =>
        new Promise((_resolve, reject) => {
          rejectDownload = reject;
        }),
    );
    const onBusyChange = vi.fn();
    renderSection(doneTask, { onBusyChange });
    fireEvent.click(await screen.findByRole("button", { name: "提交历史 1" }));

    const downloadButtons = await screen.findAllByRole("button", {
      name: /^下载产出/,
    });
    fireEvent.click(downloadButtons[0]);
    await waitFor(() =>
      expect(apiMocks.downloadTaskArtifact).toHaveBeenCalledTimes(1),
    );
    await waitFor(() => {
      expect(onBusyChange).toHaveBeenCalledWith(true);
      downloadButtons.forEach((button) => expect(button).toBeDisabled());
      screen
        .getAllByRole("button", { name: /^删除产出/ })
        .forEach((button) => expect(button).toBeDisabled());
    });

    fireEvent.click(downloadButtons[1]);
    expect(apiMocks.downloadTaskArtifact).toHaveBeenCalledTimes(1);
    await act(async () => {
      rejectDownload(
        new ApiError("文件缺失", {
          code: "ARTIFACT_FILE_MISSING",
          status: 410,
        }),
      );
    });

    expect(await screen.findByText("受控文件已缺失，无法下载。")).toBeVisible();
    await waitFor(() => {
      downloadButtons.forEach((button) => expect(button).toBeEnabled());
      expect(onBusyChange).toHaveBeenLastCalledWith(false);
    });
  });
});
