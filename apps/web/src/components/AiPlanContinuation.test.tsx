import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  act,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  AiAutomaticReply,
  AiPlanContinuation,
  AiContinuationMonitor,
} from "./AiPlanContinuation";
import {
  createAiPlanContinuation,
  getAiPlanContinuation,
  getActiveAiPlanContinuations,
  getRecentAiPlanContinuations,
  stopAiPlanContinuation,
  continuationKey,
  type AiPlanContinuation as Lease,
} from "../api/aiPlanContinuation";
import { ApiError, getAiProviders, getAiSessions } from "../api/client";
import { getAiGeneration } from "../api/aiActions";
import { useAiChatStore } from "../store/aiChat";
vi.mock("../api/aiPlanContinuation", async () => ({
  ...(await vi.importActual("../api/aiPlanContinuation")),
  createAiPlanContinuation: vi.fn(),
  getAiPlanContinuation: vi.fn(),
  getActiveAiPlanContinuations: vi.fn(),
  getRecentAiPlanContinuations: vi.fn(),
  stopAiPlanContinuation: vi.fn(),
}));
vi.mock("../api/client", async () => ({
  ...(await vi.importActual("../api/client")),
  getAiProviders: vi.fn(),
  getAiSessions: vi.fn(),
}));
vi.mock("../api/aiActions", async () => ({
  ...(await vi.importActual("../api/aiActions")),
  getAiGeneration: vi.fn(),
}));
const id = "018f0000-0000-7000-8000-000000000011";
const providerId = "018f0000-0000-7000-8000-000000000012";
const leaseId = "018f0000-0000-7000-8000-000000000013";
const generationId = "018f0000-0000-7000-8000-000000000014";
const provider = {
  id: providerId,
  name: "测试模型",
  kind: "remote" as const,
  protocol: "openai_chat" as const,
  model: "model",
  version: 2,
  config_version: 4,
};
const fixture = (): Lease => ({
  id: leaseId,
  session_id: id,
  version: 1,
  status: "waiting",
  reason: "pending_approval",
  initial_plan_version: 2,
  current_plan_version: 2,
  provider,
  workspace: { provider_version: 2, scopes: ["work", "outputs", "actions"] },
  max_turns: 3,
  turns_started: 0,
  current_generation_id: null,
  last_generation_id: null,
  created_at: "2026-09-21T12:00:00Z",
  updated_at: "2026-09-21T12:00:00Z",
  expires_at: "2026-09-21T12:30:00Z",
});
function mount(node: React.ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const view = render(
    <QueryClientProvider client={client}>
      <MemoryRouter>{node}</MemoryRouter>
    </QueryClientProvider>,
  );
  return { ...view, client };
}
async function open() {
  const button = await screen.findByRole("button", { name: "设置自动续办" });
  await waitFor(() => expect(button).toBeEnabled());
  fireEvent.click(button);
  await screen.findByRole("option", { name: "测试模型 · model" });
  fireEvent.change(screen.getByRole("combobox", { name: "续办 Provider" }), {
    target: { value: providerId },
  });
}
function recommend() {
  fireEvent.click(screen.getByRole("button", { name: /采用推荐范围/ }));
}
function consent() {
  fireEvent.click(
    screen.getByRole("checkbox", { name: /我同意按以上 Provider/ }),
  );
}
beforeEach(() => {
  vi.resetAllMocks();
  vi.mocked(getAiPlanContinuation).mockResolvedValue(null);
  vi.mocked(getActiveAiPlanContinuations).mockResolvedValue([]);
  vi.mocked(getRecentAiPlanContinuations).mockResolvedValue([]);
  vi.mocked(getAiProviders).mockResolvedValue([
    {
      ...provider,
      configVersion: 4,
      status: "ready",
      base_url: "http://private",
      health_status: "unknown",
      health_error_code: null,
      has_key: true,
      last_health_at: null,
      created_at: "",
      updated_at: "",
    },
  ]);
  vi.mocked(getAiSessions).mockResolvedValue([
    {
      id,
      title: "会话",
      persist: true,
      version: 5,
      compacted_message_count: 0,
      created_at: "",
      updated_at: "",
    },
  ]);
  vi.mocked(createAiPlanContinuation).mockResolvedValue(fixture());
  useAiChatStore.setState({ input: "我的草稿", acceptedCommand: null });
});
afterEach(cleanup);
describe("explicit bounded plan continuation", () => {
  it("starts only after independent provider, scopes and consent; does not send or consume draft", async () => {
    mount(<AiPlanContinuation sessionId={id} planVersion={2} />);
    await open();
    expect(
      screen.getByRole("button", { name: "确认并开启自动续办" }),
    ).toBeDisabled();
    expect(
      screen
        .getAllByRole("checkbox")
        .every((n) => !(n as HTMLInputElement).checked),
    ).toBe(true);
    recommend();
    expect(
      screen.getByRole("checkbox", { name: /产出文件正文/ }),
    ).not.toBeChecked();
    expect(
      screen.getByRole("checkbox", { name: /同项目已验收文件/ }),
    ).not.toBeChecked();
    consent();
    fireEvent.click(screen.getByRole("button", { name: "确认并开启自动续办" }));
    await waitFor(() =>
      expect(createAiPlanContinuation).toHaveBeenCalledWith(id, {
        expected_session_version: 5,
        expected_plan_version: 2,
        provider_id: providerId,
        expected_provider_version: 2,
        expected_provider_config_version: 4,
        workspace: {
          provider_version: 2,
          scopes: ["work", "outputs", "actions"],
        },
        max_turns: 3,
        ttl_minutes: 30,
        confirm_automatic_continuation: true,
      }),
    );
    expect(useAiChatStore.getState().input).toBe("我的草稿");
    expect(useAiChatStore.getState().acceptedCommand).toBeNull();
    expect(screen.queryByText("http://private")).toBeNull();
  });
  it("keeps restart recovery off by default and clears consent before separately enabling it", async () => {
    mount(<AiPlanContinuation sessionId={id} planVersion={2} />);
    await open();
    recommend();
    consent();
    fireEvent.click(
      screen.getByRole("checkbox", { name: /我另行同意：本地服务重启后/ }),
    );
    expect(
      screen.getByRole("checkbox", { name: /我同意按以上 Provider/ }),
    ).not.toBeChecked();
    consent();
    fireEvent.click(screen.getByRole("button", { name: "确认并开启自动续办" }));
    await waitFor(() =>
      expect(createAiPlanContinuation).toHaveBeenCalledWith(
        id,
        expect.objectContaining({
          resume_after_restart: true,
          confirm_restart_continuation: true,
        }),
      ),
    );
  });
  it("explains when the running Sidecar is too old for the new restart option", async () => {
    vi.mocked(createAiPlanContinuation).mockRejectedValueOnce(
      new ApiError("unknown field", { code: "INVALID_JSON", status: 400 }),
    );
    mount(<AiPlanContinuation sessionId={id} planVersion={2} />);
    await open();
    recommend();
    fireEvent.click(
      screen.getByRole("checkbox", { name: /我另行同意：本地服务重启后/ }),
    );
    consent();
    fireEvent.click(screen.getByRole("button", { name: "确认并开启自动续办" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /Sidecar 尚未加载重启续办能力/,
    );
    expect(
      screen.getByRole("checkbox", { name: /我同意按以上 Provider/ }),
    ).not.toBeChecked();
  });
  it("changing limits or scopes clears consent and dependencies are not independent grants", async () => {
    mount(<AiPlanContinuation sessionId={id} planVersion={2} />);
    await open();
    recommend();
    consent();
    fireEvent.change(screen.getByRole("spinbutton", { name: "最多生成轮数" }), {
      target: { value: 4 },
    });
    expect(
      screen.getByRole("checkbox", { name: /我同意按以上/ }),
    ).not.toBeChecked();
    fireEvent.click(screen.getByRole("checkbox", { name: /同项目已验收文件/ }));
    expect(
      screen.getByRole("checkbox", { name: /Agent 受控文件/ }),
    ).toBeChecked();
    fireEvent.click(screen.getByRole("checkbox", { name: /^工作事项/ }));
    expect(
      screen.getByRole("checkbox", { name: /同项目已验收文件/ }),
    ).not.toBeChecked();
    expect(
      screen.getByRole("button", { name: "确认并开启自动续办" }),
    ).toBeDisabled();
  });
  it.each(["null", "previous lease"])(
    "keeps a newly accepted lease when an older GET returns %s",
    async (kind) => {
      const { client } = mount(
        <AiPlanContinuation sessionId={id} planVersion={2} />,
      );
      await open();
      recommend();
      consent();
      let resolve!: (value: Lease | null) => void;
      vi.mocked(getAiPlanContinuation).mockImplementationOnce(
        () =>
          new Promise((r) => {
            resolve = r;
          }),
      );
      void client.invalidateQueries({ queryKey: continuationKey(id) });
      await waitFor(() => expect(resolve).toBeTypeOf("function"));
      fireEvent.click(
        screen.getByRole("button", { name: "确认并开启自动续办" }),
      );
      await screen.findByRole("button", { name: "停止自动续办" });
      await act(async () => {
        resolve(
          kind === "null"
            ? null
            : {
                ...fixture(),
                id: "018f0000-0000-7000-8000-000000000091",
                status: "stopped",
                reason: "user_stopped",
                version: 4,
              },
        );
      });
      expect(client.getQueryData(continuationKey(id))).toMatchObject({
        id: leaseId,
        status: "waiting",
      });
      expect(
        screen.getByRole("button", { name: "停止自动续办" }),
      ).toBeVisible();
      expect(createAiPlanContinuation).toHaveBeenCalledTimes(1);
    },
  );
  it("provider config drift clears old consent", async () => {
    const { client } = mount(
      <AiPlanContinuation sessionId={id} planVersion={2} />,
    );
    await open();
    recommend();
    consent();
    await act(async () => {
      client.setQueryData(
        ["ai", "providers"],
        (old: Awaited<ReturnType<typeof getAiProviders>>) =>
          old.map((p) => ({ ...p, configVersion: 5 })),
      );
    });
    await waitFor(() =>
      expect(
        screen.getByRole("checkbox", { name: /我同意按以上/ }),
      ).not.toBeChecked(),
    );
  });
  it("temporary session cannot authorize", async () => {
    vi.mocked(getAiSessions).mockResolvedValue([
      {
        id,
        title: "临时",
        persist: false,
        version: 5,
        compacted_message_count: 0,
        created_at: "",
        updated_at: "",
      },
    ]);
    mount(<AiPlanContinuation sessionId={id} planVersion={2} />);
    await open();
    recommend();
    expect(
      screen.getByRole("checkbox", { name: /我同意按以上/ }),
    ).toBeDisabled();
    expect(createAiPlanContinuation).not.toHaveBeenCalled();
  });
  it("reads wait reason and stops permission, without approving or cancelling an Agent Run", async () => {
    vi.mocked(getAiPlanContinuation).mockResolvedValue(fixture());
    vi.mocked(stopAiPlanContinuation).mockResolvedValue({
      ...fixture(),
      status: "stopped",
      reason: "user_stopped",
      version: 2,
    });
    mount(<AiPlanContinuation sessionId={id} planVersion={2} />);
    fireEvent.click(
      await screen.findByRole("button", { name: "停止自动续办" }),
    );
    await screen.findByText("已请求停止自动续办");
    expect(stopAiPlanContinuation).toHaveBeenCalledWith(leaseId);
    expect(createAiPlanContinuation).not.toHaveBeenCalled();
  });
  it("does not treat failed stop as revoked permission", async () => {
    vi.mocked(getAiPlanContinuation).mockResolvedValue(fixture());
    vi.mocked(stopAiPlanContinuation).mockRejectedValue(new Error("offline"));
    mount(<AiPlanContinuation sessionId={id} planVersion={2} />);
    fireEvent.click(
      await screen.findByRole("button", { name: "停止自动续办" }),
    );
    await screen.findByRole("alert");
    expect(screen.getByText("等待人工核对或逐项审批")).toBeVisible();
  });
  it("ignores an older in-flight waiting snapshot after stop succeeds", async () => {
    vi.mocked(getAiPlanContinuation).mockResolvedValueOnce(fixture());
    const { client } = mount(
      <AiPlanContinuation sessionId={id} planVersion={2} />,
    );
    await screen.findByRole("button", { name: "停止自动续办" });
    let resolve!: (value: Lease) => void;
    vi.mocked(getAiPlanContinuation).mockImplementationOnce(
      () =>
        new Promise((r) => {
          resolve = r;
        }),
    );
    void client.invalidateQueries({ queryKey: continuationKey(id) });
    await waitFor(() => expect(resolve).toBeTypeOf("function"));
    vi.mocked(stopAiPlanContinuation).mockResolvedValue({
      ...fixture(),
      version: 2,
      status: "stopped",
      reason: "user_stopped",
    });
    fireEvent.click(screen.getByRole("button", { name: "停止自动续办" }));
    await screen.findByText("已请求停止自动续办");
    await act(async () => {
      resolve(fixture());
    });
    expect(client.getQueryData(continuationKey(id))).toMatchObject({
      version: 2,
      status: "stopped",
    });
    expect(screen.queryByRole("button", { name: "停止自动续办" })).toBeNull();
  });
  it("closing and reopening the form never restores prior selection or consent", async () => {
    mount(<AiPlanContinuation sessionId={id} planVersion={2} />);
    await open();
    recommend();
    consent();
    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    await open();
    expect(
      screen
        .getAllByRole("checkbox")
        .every((n) => !(n as HTMLInputElement).checked),
    ).toBe(true);
    expect(createAiPlanContinuation).not.toHaveBeenCalled();
  });
  it.each([
    [0, 30],
    [9, 30],
    [3, 0],
    [3, 121],
    [1.5, 30],
  ])("rejects invalid turn/time inputs %s/%s", async (turns, ttl) => {
    mount(<AiPlanContinuation sessionId={id} planVersion={2} />);
    await open();
    recommend();
    fireEvent.change(screen.getByRole("spinbutton", { name: "最多生成轮数" }), {
      target: { value: turns },
    });
    fireEvent.change(
      screen.getByRole("spinbutton", { name: "授权时限（分钟）" }),
      { target: { value: ttl } },
    );
    expect(
      screen.getByRole("checkbox", { name: /我同意按以上/ }),
    ).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "确认并开启自动续办" }),
    ).toBeDisabled();
    expect(createAiPlanContinuation).not.toHaveBeenCalled();
  });
  it("polls actual server reply with provenance and does not take manual stream ownership", async () => {
    const lease = {
      ...fixture(),
      status: "running" as const,
      reason: "generating" as const,
      turns_started: 1,
      current_generation_id: generationId,
      last_generation_id: generationId,
    };
    vi.mocked(getAiPlanContinuation).mockResolvedValue(lease);
    vi.mocked(getAiGeneration).mockResolvedValue({
      id: generationId,
      session_id: id,
      provider_id: providerId,
      status: "streaming",
      content: "后台真实回复",
      reasoning: "",
      error_code: null,
      persist: true,
      client_request_id: "",
      origin: {
        kind: "plan_continuation",
        continuation_id: leaseId,
        turn_index: 1,
        max_turns: 3,
      },
    });
    mount(<AiAutomaticReply sessionId={id} knownGenerationIds={[]} />);
    await screen.findByText("后台真实回复");
    expect(screen.getByText("计划自动续办 · 第 1/3 轮")).toBeVisible();
    expect(useAiChatStore.getState().input).toBe("我的草稿");
    expect(createAiPlanContinuation).not.toHaveBeenCalled();
  });
  it("does not duplicate an automatic answer already in saved history", async () => {
    vi.mocked(getAiPlanContinuation).mockResolvedValue({
      ...fixture(),
      turns_started: 1,
      last_generation_id: generationId,
    });
    const { container } = mount(
      <AiAutomaticReply sessionId={id} knownGenerationIds={[generationId]} />,
    );
    await waitFor(() => expect(getAiPlanContinuation).toHaveBeenCalled());
    expect(container.querySelector("article")).toBeNull();
  });
  it("keeps background lease discoverable outside its panel without starting a new turn", async () => {
    vi.mocked(getActiveAiPlanContinuations).mockResolvedValue([fixture()]);
    const { client, unmount } = mount(<AiContinuationMonitor />);
    expect(
      await screen.findByRole("link", { name: /自动续办 0\/3 轮/ }),
    ).toHaveAttribute("href", `/ai?session=${id}`);
    expect(client.getQueryData(continuationKey(id))).toMatchObject({
      id: leaseId,
    });
    unmount();
    expect(stopAiPlanContinuation).not.toHaveBeenCalled();
    expect(createAiPlanContinuation).not.toHaveBeenCalled();
  });
  it("keeps a bounded terminal result discoverable after reload without restarting it", async () => {
    vi.mocked(getRecentAiPlanContinuations).mockResolvedValue([
      {
        ...fixture(),
        session_title: "项目收尾",
        status: "completed",
        reason: "plan_complete",
      },
    ]);
    mount(<AiContinuationMonitor />);
    const summary = await screen.findByText(/最近 24 小时的自动续办结果/);
    fireEvent.click(summary);
    expect(
      screen.getByRole("link", { name: /项目收尾 · 已完成 · 已启动 0\/3 轮/ }),
    ).toHaveAttribute("href", `/ai?session=${id}`);
    expect(screen.getByText(/计划已满足/)).toBeVisible();
    expect(createAiPlanContinuation).not.toHaveBeenCalled();
  });
  it("does not hide an unreadable continuation state as an empty result", async () => {
    vi.mocked(getActiveAiPlanContinuations).mockRejectedValueOnce(
      new Error("offline"),
    );
    mount(<AiContinuationMonitor />);
    expect(await screen.findByRole("alert", { name: "" })).toHaveTextContent(
      "不能据此判断是否仍在运行或已经结束",
    );
  });
});
