import { beforeEach, describe, expect, it, vi } from "vitest";
import { workspaceApi } from "../api/workspace";
import { getAgentRun } from "../api/client";
import type { AgentRun } from "../types/models";
import {
  agentRunFileApplyCandidates,
  useAgentRunFileApply,
} from "./agentRunFileApply";

vi.mock("../api/workspace", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/workspace")>()),
  workspaceApi: {
    fileSnapshot: vi.fn(),
    validateFileSnapshot: vi.fn(),
    releaseFileSnapshot: vi.fn().mockResolvedValue(undefined),
    applyFileEdit: vi.fn(),
  },
}));

vi.mock("../api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/client")>()),
  getAgentRun: vi.fn(),
}));

const ids = {
  run: "018f0000-0000-7000-8000-000000000101",
  task: "018f0000-0000-7000-8000-000000000102",
  assignment: "018f0000-0000-7000-8000-000000000103",
  actor: "018f0000-0000-7000-8000-000000000104",
  adapter: "018f0000-0000-7000-8000-000000000105",
  provider: "018f0000-0000-7000-8000-000000000106",
  submission: "018f0000-0000-7000-8000-000000000107",
  artifact: "018f0000-0000-7000-8000-000000000108",
  snapshot: "018f0000-0000-7000-8000-000000000109",
};

function run(overrides: Partial<AgentRun> = {}): AgentRun {
  return {
    id: ids.run,
    taskId: ids.task,
    assignmentId: ids.assignment,
    actorId: ids.actor,
    adapterId: ids.adapter,
    createdByActorId: ids.actor,
    parentRunId: null,
    attempt: 2,
    status: "succeeded",
    providerId: ids.provider,
    model: "test-model",
    outputContract: {
      type: "files",
      files: [
        { name: "report.md", mime: "text/markdown" },
        { name: "data.json", mime: "application/json" },
      ],
    },
    resultText: JSON.stringify(["new report", '{"ok":true}']),
    resultBytes: 32,
    errorCode: null,
    outputDeliveryStatus: "submitted",
    outputDeliveryErrorCode: null,
    submissionId: ids.submission,
    artifactId: ids.artifact,
    startedAt: "2026-09-22T10:00:00Z",
    completedAt: "2026-09-22T10:01:00Z",
    createdAt: "2026-09-22T10:00:00Z",
    ...overrides,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(workspaceApi.releaseFileSnapshot).mockResolvedValue(undefined);
  vi.mocked(getAgentRun).mockResolvedValue(run());
  useAgentRunFileApply.setState({
    candidate: null,
    review: null,
    loading: false,
    error: null,
  });
});

describe("Agent Run file replacement bridge", () => {
  it("extracts only submitted bounded file outputs, never text or retained bodies", () => {
    expect(agentRunFileApplyCandidates(run(), "交付任务")).toMatchObject([
      { name: "report.md", content: "new report", index: 0 },
      { name: "data.json", content: '{"ok":true}', index: 1 },
    ]);
    expect(
      agentRunFileApplyCandidates(
        run({ outputDeliveryStatus: "retained" }),
        "交付任务",
      ),
    ).toEqual([]);
    expect(
      agentRunFileApplyCandidates(
        run({
          outputContract: { type: "text" },
          resultText: "plain text",
        }),
        "交付任务",
      ),
    ).toEqual([]);
    expect(
      agentRunFileApplyCandidates(
        run({
          outputContract: {
            type: "file",
            name: "too-large.md",
            mime: "text/markdown",
          },
          resultText: "x".repeat(32 * 1024 + 1),
        }),
        "交付任务",
      ),
    ).toEqual([]);
  });

  it("binds one exact target snapshot and applies only after a fresh operation id", async () => {
    const [candidate] = agentRunFileApplyCandidates(run(), "交付任务");
    expect(useAgentRunFileApply.getState().select(candidate)).toBe(true);
    vi.mocked(workspaceApi.fileSnapshot).mockResolvedValue({
      id: ids.snapshot,
      path: "src/report.md",
      content: "old report",
      size: 10,
      sha256: "a".repeat(64),
      expiresInSeconds: 600,
    });
    vi.mocked(workspaceApi.validateFileSnapshot).mockResolvedValue(undefined);
    vi.mocked(workspaceApi.applyFileEdit).mockImplementation(
      async (_root, _snapshot, operationId) => ({
        id: operationId,
        status: "applied",
        backupPath: "recovery/record.json",
      }),
    );

    await useAgentRunFileApply
      .getState()
      .stageTarget(
        { id: "root-1", name: "project", path: "D:/project" },
        "src/report.md",
      );
    const review = useAgentRunFileApply.getState().review;
    expect(review).toMatchObject({
      path: "src/report.md",
      status: "ready",
      candidate: { runId: ids.run, artifactId: ids.artifact },
    });
    expect(review?.operationId).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/,
    );

    await useAgentRunFileApply.getState().validate();
    expect(workspaceApi.validateFileSnapshot).toHaveBeenCalledWith(
      "root-1",
      ids.snapshot,
    );
    await useAgentRunFileApply.getState().apply();
    expect(getAgentRun).toHaveBeenCalledTimes(2);
    expect(workspaceApi.applyFileEdit).toHaveBeenCalledWith(
      "root-1",
      ids.snapshot,
      review?.operationId,
      "new report",
    );
    expect(useAgentRunFileApply.getState().review).toMatchObject({
      status: "applied",
      backupPath: "recovery/record.json",
    });
  });

  it("blocks a stale submitted source before the native write is requested", async () => {
    const [candidate] = agentRunFileApplyCandidates(run(), "交付任务");
    useAgentRunFileApply.getState().select(candidate);
    vi.mocked(workspaceApi.fileSnapshot).mockResolvedValue({
      id: ids.snapshot,
      path: "src/report.md",
      content: "old report",
      size: 10,
      sha256: "c".repeat(64),
      expiresInSeconds: 600,
    });
    await useAgentRunFileApply
      .getState()
      .stageTarget(
        { id: "root-1", name: "project", path: "D:/project" },
        "src/report.md",
      );
    vi.mocked(getAgentRun).mockResolvedValue(
      run({ outputDeliveryStatus: "retained" }),
    );

    await useAgentRunFileApply.getState().apply();

    expect(workspaceApi.applyFileEdit).not.toHaveBeenCalled();
    expect(workspaceApi.releaseFileSnapshot).toHaveBeenCalledWith(
      "root-1",
      ids.snapshot,
    );
    expect(useAgentRunFileApply.getState().review).toMatchObject({
      status: "conflict",
      error: expect.stringContaining("提交身份、契约或正文已经变化"),
    });
  });

  it("drops a late target read after the selected Agent output changes", async () => {
    const [first, second] = agentRunFileApplyCandidates(run(), "交付任务");
    let resolveSnapshot!: (
      value: Awaited<ReturnType<typeof workspaceApi.fileSnapshot>>,
    ) => void;
    vi.mocked(workspaceApi.fileSnapshot).mockReturnValue(
      new Promise((resolve) => {
        resolveSnapshot = resolve;
      }),
    );
    useAgentRunFileApply.getState().select(first);
    const pending = useAgentRunFileApply
      .getState()
      .stageTarget(
        { id: "root-1", name: "project", path: "D:/project" },
        "src/report.md",
      );
    await vi.waitFor(() =>
      expect(workspaceApi.fileSnapshot).toHaveBeenCalledOnce(),
    );
    useAgentRunFileApply.getState().select(second);
    resolveSnapshot({
      id: ids.snapshot,
      path: "src/report.md",
      content: "old report",
      size: 10,
      sha256: "b".repeat(64),
      expiresInSeconds: 600,
    });
    await pending;
    expect(workspaceApi.releaseFileSnapshot).toHaveBeenCalledWith(
      "root-1",
      ids.snapshot,
    );
    expect(useAgentRunFileApply.getState()).toMatchObject({
      candidate: { name: "data.json" },
      review: null,
    });
  });
});
