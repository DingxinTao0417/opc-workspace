import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
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
};

const state = vi.hoisted(() => ({
  input: {} as Record<string, unknown>,
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
        items: [item],
        meta: { page: 1, pageSize: 6, total: 1, projectVersion: 9 },
      },
      isError: false,
      isFetching: false,
      isPending: false,
      isSuccess: true,
      refetch: state.refetch,
    };
  },
}));

describe("ProjectArtifactsSection", () => {
  beforeEach(() => {
    state.refetch.mockReset();
    useUiStore.setState({ taskDetailId: null });
  });

  afterEach(cleanup);

  it("shows Task-owned outputs, opens the source Task, and switches history", () => {
    render(<ProjectArtifactsSection projectId="project-1" />);

    expect(screen.getByText("交付说明")).toBeVisible();
    expect(screen.getByText(/准备交付 · 第 2 次提交/)).toBeVisible();
    expect(screen.getByText("需要跟进")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "打开任务" }));
    expect(useUiStore.getState().taskDetailId).toBe("task-1");

    fireEvent.click(screen.getByRole("checkbox", { name: /删除历史/ }));
    expect(state.input).toEqual(
      expect.objectContaining({ page: 1, includeDeleted: true }),
    );
  });
});
