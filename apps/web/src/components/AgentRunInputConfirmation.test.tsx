import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  confirmAgentRunStartGateInputs,
  getAgentRunStartGateInputs,
} from "../api/client";
import type {
  AgentRun,
  AgentRunInputSlotStatus,
  AgentRunStartGateInputs,
} from "../types/models";
import { AgentRunInputConfirmation } from "./AgentRunInputConfirmation";

vi.mock("../api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/client")>()),
  getAgentRunStartGateInputs: vi.fn(),
  confirmAgentRunStartGateInputs: vi.fn(),
}));

const id = {
  task: "018f0000-0000-7000-8000-000000007301",
  run: "018f0000-0000-7000-8000-000000007302",
  predecessor: "018f0000-0000-7000-8000-000000007303",
  sourceTask: "018f0000-0000-7000-8000-000000007304",
  project: "018f0000-0000-7000-8000-000000007305",
  submission: "018f0000-0000-7000-8000-000000007306",
  research: "018f0000-0000-7000-8000-000000007307",
  notes: "018f0000-0000-7000-8000-000000007308",
  provider: "018f0000-0000-7000-8000-000000007309",
};
const expectedFiles = [
  { name: "research.md", mime: "text/markdown" as const },
  { name: "notes.md", mime: "text/markdown" as const },
];
const sourceTask = {
  project_id: id.project,
  task_id: id.sourceTask,
  task_title: "完成调研",
  task_version: 4,
  submission_id: id.submission,
  submission_sequence: 1,
};
const files: AgentRunStartGateInputs["files"] = [
  {
    id: id.research,
    source_kind: "project_task_artifact",
    name: "research.md",
    mime: "text/markdown",
    size_bytes: 2048,
    sha256: "a".repeat(64),
    source_task: sourceTask,
  },
  {
    id: id.notes,
    source_kind: "project_task_artifact",
    name: "notes.md",
    mime: "text/markdown",
    size_bytes: 80,
    sha256: "b".repeat(64),
    source_task: sourceTask,
  },
];

function inputs(
  extra: Partial<AgentRunStartGateInputs> = {},
): AgentRunStartGateInputs {
  return {
    runId: id.run,
    taskId: id.task,
    status: "awaiting_confirmation",
    expiresAt: "2026-10-04T14:00:00Z",
    predecessor: {
      runId: id.predecessor,
      taskId: id.sourceTask,
      taskTitle: "完成调研",
      attempt: 1,
    },
    expectedFiles,
    provider: {
      id: id.provider,
      name: "远程模型",
      kind: "remote",
      model: "selected-model",
      version: 3,
      configVersion: 7,
      leavesDevice: true,
    },
    files,
    totalBytes: 2128,
    limits: { maxFiles: 4, maxFileBytes: 65_536, maxTotalBytes: 131_072 },
    fingerprint: "c".repeat(64),
    ...extra,
  };
}
const nothingYet = (extra: Partial<AgentRunStartGateInputs> = {}) =>
  inputs({ files: [], totalBytes: 0, fingerprint: undefined, ...extra });

function run(
  slot: AgentRunInputSlotStatus,
  extra: Partial<AgentRun> = {},
): AgentRun {
  const closed = slot === "closed";
  const bound = slot === "bound";
  return {
    id: id.run,
    taskId: id.task,
    assignmentId: "assignment-1",
    actorId: "agent-1",
    adapterId: "adapter-1",
    createdByActorId: "owner-1",
    parentRunId: null,
    attempt: 1,
    status: closed ? "cancelled" : bound ? "running" : "queued",
    providerId: id.provider,
    model: "selected-model",
    executionContractVersion: 7,
    resultText: null,
    resultBytes: null,
    errorCode: null,
    outputDeliveryStatus: "not_ready",
    outputDeliveryErrorCode: null,
    submissionId: null,
    artifactId: null,
    startedAt: bound ? "2026-10-04T13:00:00Z" : null,
    completedAt: closed ? "2026-10-04T13:00:00Z" : null,
    createdAt: "2026-10-04T12:00:00Z",
    startGate: {
      predecessorRunId: id.predecessor,
      require: "accepted",
      expiresAt: "2026-10-04T14:00:00Z",
      status: closed ? "closed" : bound ? "released" : "waiting",
      ...(closed
        ? { closeReason: "predecessor_output_unavailable" as const }
        : {}),
      inputSlot: { source: "predecessor_files", status: slot, expectedFiles },
    },
    ...extra,
  };
}

function renderPanel(value: AgentRun) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <AgentRunInputConfirmation run={value} />
    </QueryClientProvider>,
  );
}
const panel = () => screen.getByRole("region", { name: "前置产出输入" });
const confirmButton = () =>
  screen.getByRole("button", { name: "确认这些文件并启动执行" });
const tick = (name: RegExp) =>
  fireEvent.click(screen.getByRole("checkbox", { name }));
// The loading line is a status region too: wait for the settled text.
const settled = (text: string) =>
  waitFor(() =>
    expect(within(panel()).getByRole("status")).toHaveTextContent(text),
  );
const filesBox = /我已逐份核对上列文件/;
const providerBox = /我同意把这些文件的正文发送给/;

beforeEach(() => {
  vi.mocked(getAgentRunStartGateInputs).mockReset();
  vi.mocked(confirmAgentRunStartGateInputs).mockReset();
});
afterEach(cleanup);

describe("AgentRunInputConfirmation", () => {
  it("renders nothing for a Run without an input slot", () => {
    const { container } = renderPanel({
      ...run("awaiting_predecessor"),
      startGate: undefined,
    });
    expect(container).toBeEmptyDOMElement();
    expect(getAgentRunStartGateInputs).not.toHaveBeenCalled();
  });

  it("says it is reading and offers nothing to confirm meanwhile", () => {
    vi.mocked(getAgentRunStartGateInputs).mockReturnValue(
      new Promise(() => undefined),
    );
    renderPanel(run("awaiting_confirmation"));
    expect(within(panel()).getByRole("status")).toHaveTextContent(
      "正在读取前置产出",
    );
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("explains an empty slot and lists only what was promised", async () => {
    vi.mocked(getAgentRunStartGateInputs).mockResolvedValue(
      nothingYet({ status: "awaiting_predecessor" }),
    );
    renderPanel(run("awaiting_predecessor"));
    await settled(
      "前置任务「完成调研」的产出还没有被验收，现在没有可以确认的文件",
    );
    const promised = screen.getByRole("list", { name: "约定的产出文件" });
    expect(within(promised).getAllByRole("listitem")).toHaveLength(2);
    expect(promised).toHaveTextContent("research.md");
    expect(screen.queryByRole("list", { name: "前置产出文件" })).toBeNull();
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("shows every file, where it came from and who would receive it", async () => {
    vi.mocked(getAgentRunStartGateInputs).mockResolvedValue(inputs());
    renderPanel(run("awaiting_confirmation"));
    const list = await screen.findByRole("list", { name: "前置产出文件" });
    const rows = within(list).getAllByRole("listitem");
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveTextContent("research.md");
    expect(rows[0]).toHaveTextContent("text/markdown · 2.0 KiB");
    expect(rows[0]).toHaveTextContent("a".repeat(64));
    expect(rows[0]).toHaveTextContent("来源任务「完成调研」");
    expect(rows[0]).toHaveTextContent("第 1 批已验收");
    expect(rows[1]).toHaveTextContent("text/markdown · 80 B");
    expect(panel()).toHaveTextContent("下列 2 个文件（共 2.1 KiB）");
    expect(
      screen.getByRole("checkbox", { name: providerBox }),
    ).toHaveAccessibleName(
      /远程模型 · selected-model（远程 Provider：内容将离开本机，发送后无法撤回）/,
    );
  });

  it("says so when the Provider keeps the files on this device", async () => {
    vi.mocked(getAgentRunStartGateInputs).mockResolvedValue(
      inputs({
        provider: {
          ...inputs().provider,
          name: "本机模型",
          kind: "local",
          leavesDevice: false,
        },
      }),
    );
    renderPanel(run("awaiting_confirmation"));
    expect(
      await screen.findByRole("checkbox", { name: providerBox }),
    ).toHaveAccessibleName(
      /本机模型 · selected-model（本地 Provider：内容留在本机）/,
    );
  });

  it("needs both ticks, then confirms exactly what was shown", async () => {
    vi.mocked(getAgentRunStartGateInputs).mockResolvedValue(inputs());
    vi.mocked(confirmAgentRunStartGateInputs).mockResolvedValue(
      inputs({ status: "bound" }),
    );
    renderPanel(run("awaiting_confirmation"));
    await screen.findByRole("list", { name: "前置产出文件" });
    expect(confirmButton()).toBeDisabled();
    tick(filesBox);
    expect(confirmButton()).toBeDisabled();
    tick(providerBox);
    expect(confirmButton()).toBeEnabled();
    tick(filesBox);
    expect(confirmButton()).toBeDisabled();
    expect(confirmAgentRunStartGateInputs).not.toHaveBeenCalled();
    tick(filesBox);
    fireEvent.click(confirmButton());
    await waitFor(() =>
      expect(confirmAgentRunStartGateInputs).toHaveBeenCalledTimes(1),
    );
    expect(vi.mocked(confirmAgentRunStartGateInputs).mock.calls[0][0]).toEqual({
      runId: id.run,
      taskId: id.task,
      fingerprint: "c".repeat(64),
      provider: { version: 3, configVersion: 7, kind: "remote" },
    });
  });

  it("drops both ticks and reads again when a confirmation is refused", async () => {
    vi.mocked(getAgentRunStartGateInputs)
      .mockResolvedValueOnce(inputs())
      .mockResolvedValue(
        inputs({
          files: [{ ...files[0], sha256: "e".repeat(64) }, files[1]],
          fingerprint: "d".repeat(64),
        }),
      );
    vi.mocked(confirmAgentRunStartGateInputs).mockRejectedValue(
      new ApiError("changed", { code: "AGENT_RUN_INPUT_CHANGED", status: 409 }),
    );
    renderPanel(run("awaiting_confirmation"));
    await screen.findByRole("list", { name: "前置产出文件" });
    tick(filesBox);
    tick(providerBox);
    fireEvent.click(confirmButton());
    expect(await within(panel()).findByRole("alert")).toHaveTextContent(
      "刚才的确认没有生效，也没有发送任何文件",
    );
    await waitFor(() =>
      expect(
        screen.getByRole("list", { name: "前置产出文件" }),
      ).toHaveTextContent("e".repeat(64)),
    );
    expect(screen.getByRole("checkbox", { name: filesBox })).not.toBeChecked();
    expect(
      screen.getByRole("checkbox", { name: providerBox }),
    ).not.toBeChecked();
    expect(confirmButton()).toBeDisabled();
    expect(getAgentRunStartGateInputs).toHaveBeenCalledTimes(2);
  });

  it.each([
    [
      "AGENT_RUN_NOT_EXECUTABLE",
      "Agent 适配器当前不可执行，这次执行无法启动；没有发送任何文件",
    ],
    ["AGENT_RUN_IDENTITY_CHANGED", "文件、任务或模型在你核对之后发生了变化"],
    ["AGENT_RUN_INPUT_UNAVAILABLE", "前置任务已验收的文件已不可用"],
    ["AGENT_RUN_INPUT_NOT_READY", "启动条件已失效或等待时限已到"],
    ["INTERNAL_ERROR", "确认没有生效，没有发送任何文件"],
  ])("explains a refusal with %s", async (code, text) => {
    vi.mocked(getAgentRunStartGateInputs).mockResolvedValue(inputs());
    vi.mocked(confirmAgentRunStartGateInputs).mockRejectedValue(
      new ApiError("refused", { code, status: 409 }),
    );
    renderPanel(run("awaiting_confirmation"));
    await screen.findByRole("list", { name: "前置产出文件" });
    tick(filesBox);
    tick(providerBox);
    fireEvent.click(confirmButton());
    expect(await within(panel()).findByRole("alert")).toHaveTextContent(text);
  });

  it("offers a retry and no confirmation when the files cannot be read", async () => {
    vi.mocked(getAgentRunStartGateInputs)
      .mockRejectedValueOnce(new ApiError("offline", { code: "NETWORK_ERROR" }))
      .mockResolvedValue(inputs());
    renderPanel(run("awaiting_confirmation"));
    const alert = await within(panel()).findByRole("alert");
    expect(alert).toHaveTextContent(
      "无法读取前置产出，未展示任何文件，也不能确认",
    );
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "确认这些文件并启动执行" }),
    ).not.toBeInTheDocument();
    fireEvent.click(within(alert).getByRole("button", { name: "重新读取" }));
    expect(
      await screen.findByRole("list", { name: "前置产出文件" }),
    ).toBeInTheDocument();
    expect(getAgentRunStartGateInputs).toHaveBeenCalledTimes(2);
  });

  it.each([
    ["AGENT_RUN_INPUT_UNAVAILABLE", "前置任务已验收的文件已不可用"],
    ["AGENT_RUN_INPUT_NOT_READY", "启动条件已失效或等待时限已到"],
  ])("explains a read refused with %s", async (code, text) => {
    vi.mocked(getAgentRunStartGateInputs).mockRejectedValue(
      new ApiError("refused", { code, status: 409 }),
    );
    renderPanel(run("awaiting_confirmation"));
    expect(await within(panel()).findByRole("alert")).toHaveTextContent(text);
    expect(panel()).toHaveTextContent("没有读取或发送任何文件");
  });

  it("explains a closed slot from the Run itself, without reading anything", async () => {
    renderPanel(run("closed"));
    await settled("这次执行没有启动，也没有发送任何文件");
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    expect(getAgentRunStartGateInputs).not.toHaveBeenCalled();
  });

  it("explains a slot that closed while the files were being read", async () => {
    vi.mocked(getAgentRunStartGateInputs).mockResolvedValue(
      nothingYet({ status: "closed", closeReason: "expired" }),
    );
    renderPanel(run("awaiting_confirmation"));
    await settled("在确认的时限内前置条件没有满足，这次执行没有启动");
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("shows what was confirmed once the Run has its files", async () => {
    vi.mocked(getAgentRunStartGateInputs).mockResolvedValue(
      inputs({ status: "bound" }),
    );
    renderPanel(run("bound"));
    await settled(
      "已确认下列 2 个文件（共 2.1 KiB）作为这次执行的输入，发送对象为 远程模型 · selected-model",
    );
    expect(
      within(screen.getByRole("list", { name: "前置产出文件" })).getAllByRole(
        "listitem",
      ),
    ).toHaveLength(2);
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });
});
