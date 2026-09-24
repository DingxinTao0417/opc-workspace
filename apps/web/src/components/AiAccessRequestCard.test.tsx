import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { ApiError } from "../api/client";
import type { AiPlanContinuation } from "../api/aiPlanContinuation";
import type { AiMessage } from "../types/models";
import {
  AiAccessRequestCard,
  AiAccessRequestPrompt,
} from "./AiAccessRequestCard";

const mocks = vi.hoisted(() => ({
  dismiss: vi.fn().mockResolvedValue(undefined),
  stop: vi.fn(),
}));
vi.mock("../api/aiActions", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/aiActions")>()),
  dismissAiAccessRequest: mocks.dismiss,
}));
vi.mock("../api/aiPlanContinuation", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/aiPlanContinuation")>()),
  stopAiPlanContinuation: mocks.stop,
}));
afterEach(() => {
  cleanup();
  mocks.dismiss.mockReset().mockResolvedValue(undefined);
  mocks.stop.mockReset();
});

const waitingContinuation = {
  id: "continuation-1",
  session_id: "session-1",
  status: "waiting",
  reason: "pending_approval",
  version: 2,
} as AiPlanContinuation;

const savedRequestMessage: AiMessage = {
  id: "message-1",
  session_id: "session-1",
  role: "assistant",
  status: "completed",
  content: "请核对权限",
  reasoning: null,
  task_id: null,
  task_title_snapshot: null,
  generation_id: "generation-1",
  context_provider: null,
  context_sources: [],
  context_knowledge: [],
  citation_status: "not_requested",
  citations: [],
  created_at: "2026-09-21T12:00:00Z",
  access_request: { scopes: ["work", "actions"] },
};

it("stops a waiting continuation before preparing a new permission review", async () => {
  let finishStop!: (value: AiPlanContinuation) => void;
  mocks.stop.mockReturnValueOnce(
    new Promise<AiPlanContinuation>((resolve) => {
      finishStop = resolve;
    }),
  );
  const onPrepare = vi.fn();
  const client = new QueryClient();
  render(
    <QueryClientProvider client={client}>
      <AiAccessRequestPrompt
        sessionId="session-1"
        messages={[savedRequestMessage]}
        continuation={waitingContinuation}
        disabled={false}
        onPrepare={onPrepare}
        onDismissLive={vi.fn()}
      />
    </QueryClientProvider>,
  );
  fireEvent.click(
    screen.getByRole("button", { name: "停止自动续办并核对权限" }),
  );
  expect(mocks.stop).toHaveBeenCalledWith("continuation-1");
  expect(onPrepare).not.toHaveBeenCalled();
  expect(
    screen.getByRole("button", { name: "正在停止自动续办…" }),
  ).toBeDisabled();
  finishStop({
    ...waitingContinuation,
    status: "stopped",
    reason: "user_stopped",
    version: 3,
  });
  await waitFor(() =>
    expect(onPrepare).toHaveBeenCalledWith(["work", "actions"]),
  );
  expect(
    client.getQueryData(["ai", "plan-continuation", "session-1"]),
  ).toMatchObject({ status: "stopped" });
  expect(mocks.dismiss).not.toHaveBeenCalled();
  expect(screen.getByRole("region", { name: "工作台权限请求" })).toBeVisible();
});

it("keeps the request and draft untouched when stopping fails", async () => {
  mocks.stop.mockRejectedValueOnce(new ApiError("offline", { status: 503 }));
  const onPrepare = vi.fn();
  render(
    <QueryClientProvider client={new QueryClient()}>
      <AiAccessRequestPrompt
        sessionId="session-1"
        messages={[savedRequestMessage]}
        continuation={waitingContinuation}
        disabled={false}
        onPrepare={onPrepare}
        onDismissLive={vi.fn()}
      />
    </QueryClientProvider>,
  );
  fireEvent.click(
    screen.getByRole("button", { name: "停止自动续办并核对权限" }),
  );
  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent(
      "未能确认自动续办已停止",
    ),
  );
  expect(onPrepare).not.toHaveBeenCalled();
  expect(screen.getByRole("region", { name: "工作台权限请求" })).toBeVisible();
});

it("does not prepare against a newer active continuation", async () => {
  let finishStop!: (value: AiPlanContinuation) => void;
  mocks.stop.mockReturnValueOnce(
    new Promise<AiPlanContinuation>((resolve) => {
      finishStop = resolve;
    }),
  );
  const onPrepare = vi.fn();
  const client = new QueryClient();
  render(
    <QueryClientProvider client={client}>
      <AiAccessRequestPrompt
        sessionId="session-1"
        messages={[savedRequestMessage]}
        continuation={waitingContinuation}
        disabled={false}
        onPrepare={onPrepare}
        onDismissLive={vi.fn()}
      />
    </QueryClientProvider>,
  );
  fireEvent.click(
    screen.getByRole("button", { name: "停止自动续办并核对权限" }),
  );
  client.setQueryData(["ai", "plan-continuation", "session-1"], {
    ...waitingContinuation,
    id: "continuation-2",
  });
  finishStop({
    ...waitingContinuation,
    status: "stopped",
    reason: "user_stopped",
    version: 3,
  });
  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent("状态已变化"),
  );
  expect(onPrepare).not.toHaveBeenCalled();
  expect(
    client.getQueryData(["ai", "plan-continuation", "session-1"]),
  ).toMatchObject({
    id: "continuation-2",
    status: "waiting",
  });
});

it("does not carry an old request across a session switch", async () => {
  let finishStop!: (value: AiPlanContinuation) => void;
  mocks.stop.mockReturnValueOnce(
    new Promise<AiPlanContinuation>((resolve) => {
      finishStop = resolve;
    }),
  );
  const onPrepare = vi.fn();
  const client = new QueryClient();
  const view = render(
    <QueryClientProvider client={client}>
      <AiAccessRequestPrompt
        sessionId="session-1"
        messages={[savedRequestMessage]}
        continuation={waitingContinuation}
        disabled={false}
        onPrepare={onPrepare}
        onDismissLive={vi.fn()}
      />
    </QueryClientProvider>,
  );
  fireEvent.click(
    screen.getByRole("button", { name: "停止自动续办并核对权限" }),
  );
  view.rerender(
    <QueryClientProvider client={client}>
      <AiAccessRequestPrompt
        sessionId="session-2"
        messages={[savedRequestMessage]}
        continuation={null}
        disabled={false}
        onPrepare={onPrepare}
        onDismissLive={vi.fn()}
      />
    </QueryClientProvider>,
  );
  finishStop({
    ...waitingContinuation,
    status: "stopped",
    reason: "user_stopped",
    version: 3,
  });
  await waitFor(() =>
    expect(
      client.getQueryData(["ai", "plan-continuation", "session-1"]),
    ).toMatchObject({
      status: "stopped",
    }),
  );
  expect(onPrepare).not.toHaveBeenCalled();
  expect(screen.queryByRole("region", { name: "工作台权限请求" })).toBeNull();
});

it.each(["disabled", "unmounted"] as const)(
  "does not prepare a permission draft after the chat becomes %s while stopping",
  async (change) => {
    let finishStop!: (value: AiPlanContinuation) => void;
    mocks.stop.mockReturnValueOnce(
      new Promise<AiPlanContinuation>((resolve) => {
        finishStop = resolve;
      }),
    );
    const client = new QueryClient();
    const onPrepare = vi.fn();
    const content = (disabled: boolean) => (
      <QueryClientProvider client={client}>
        <AiAccessRequestPrompt
          sessionId="session-1"
          messages={[savedRequestMessage]}
          continuation={waitingContinuation}
          disabled={disabled}
          onPrepare={onPrepare}
          onDismissLive={vi.fn()}
        />
      </QueryClientProvider>
    );
    const view = render(content(false));
    fireEvent.click(
      screen.getByRole("button", { name: "停止自动续办并核对权限" }),
    );
    if (change === "unmounted") view.unmount();
    else view.rerender(content(true));
    finishStop({
      ...waitingContinuation,
      status: "stopped",
      reason: "user_stopped",
      version: 3,
    });
    await waitFor(() =>
      expect(client.getQueryData(continuationKeyForTest)).toMatchObject({
        status: "stopped",
      }),
    );
    expect(onPrepare).not.toHaveBeenCalled();
  },
);

const continuationKeyForTest = ["ai", "plan-continuation", "session-1"];

it("offers a separate permission review without granting or sending", () => {
  const onPrepare = vi.fn();
  const onDismiss = vi.fn();
  const first = render(
    <AiAccessRequestCard
      request={{ generationId: "generation-1", scopes: ["work", "actions"] }}
      disabled={false}
      onPrepare={onPrepare}
      onDismiss={onDismiss}
    />,
  );
  expect(screen.getByText(/工作事项、操作建议/)).toBeVisible();
  expect(screen.getByText(/这不是授权/)).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "核对权限并准备继续" }));
  expect(onPrepare).toHaveBeenCalledWith(["work", "actions"]);
  expect(onDismiss).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "忽略" }));
  expect(onDismiss).toHaveBeenCalledOnce();
  first.unmount();
});

it("recovers the saved latest request and persists a deliberate dismissal", async () => {
  mocks.dismiss.mockClear();
  const message: AiMessage = {
    id: "message-1",
    session_id: "session-1",
    role: "assistant",
    status: "completed",
    content: "请核对权限",
    reasoning: null,
    task_id: null,
    task_title_snapshot: null,
    generation_id: "generation-1",
    context_provider: null,
    context_sources: [],
    context_knowledge: [],
    citation_status: "not_requested",
    citations: [],
    created_at: "2026-09-21T12:00:00Z",
    access_request: { scopes: ["work", "actions"] },
  };
  const onDismissLive = vi.fn();
  const onPrepare = vi.fn();
  const queryClient = new QueryClient();
  const invalidate = vi.spyOn(queryClient, "invalidateQueries");
  // An automatic turn can save its user trigger and assistant reply with the
  // same timestamp; UUID tie-breaking may put that user row last.
  const tiedUser: AiMessage = {
    ...message,
    id: "message-2",
    role: "user",
    content: "自动续办",
    access_request: undefined,
  };
  render(
    <QueryClientProvider client={queryClient}>
      <AiAccessRequestPrompt
        sessionId="session-1"
        messages={[message, tiedUser]}
        disabled={false}
        onPrepare={onPrepare}
        onDismissLive={onDismissLive}
      />
    </QueryClientProvider>,
  );
  expect(screen.getByRole("region", { name: "工作台权限请求" })).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "核对权限并准备继续" }));
  expect(onPrepare).toHaveBeenCalledWith(["work", "actions"]);
  expect(onDismissLive).not.toHaveBeenCalled();
  expect(mocks.dismiss).not.toHaveBeenCalled();
  expect(screen.getByRole("region", { name: "工作台权限请求" })).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "忽略" }));
  await waitFor(() =>
    expect(mocks.dismiss).toHaveBeenCalledWith("generation-1"),
  );
  await waitFor(() =>
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: ["ai", "agent-inbox"],
    }),
  );
  expect(onDismissLive).toHaveBeenCalledWith("generation-1");
  expect(screen.queryByRole("region", { name: "工作台权限请求" })).toBeNull();
});

it("dismisses a live request immediately without waiting for the assistant message", async () => {
  const onDismissLive = vi.fn();
  render(
    <QueryClientProvider client={new QueryClient()}>
      <AiAccessRequestPrompt
        sessionId="session-1"
        messages={[]}
        liveRequest={{ generationId: "generation-1", scopes: ["work"] }}
        streamingGenerationId="generation-1"
        disabled={false}
        onPrepare={vi.fn()}
        onDismissLive={onDismissLive}
      />
    </QueryClientProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "忽略" }));
  await waitFor(() =>
    expect(mocks.dismiss).toHaveBeenCalledWith("generation-1"),
  );
  await waitFor(() =>
    expect(onDismissLive).toHaveBeenCalledWith("generation-1"),
  );
  expect(screen.queryByRole("region", { name: "工作台权限请求" })).toBeNull();
});

it("restores a live request if the immediate dismissal fails", async () => {
  mocks.dismiss.mockRejectedValueOnce(new ApiError("offline", { status: 503 }));
  const onDismissLive = vi.fn();
  render(
    <QueryClientProvider client={new QueryClient()}>
      <AiAccessRequestPrompt
        sessionId="session-1"
        messages={[]}
        liveRequest={{ generationId: "generation-1", scopes: ["work"] }}
        streamingGenerationId="generation-1"
        disabled={false}
        onPrepare={vi.fn()}
        onDismissLive={onDismissLive}
      />
    </QueryClientProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "忽略" }));
  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent("忽略权限建议失败"),
  );
  expect(screen.getByRole("region", { name: "工作台权限请求" })).toBeVisible();
  expect(onDismissLive).not.toHaveBeenCalled();
});

it("retries after persistence when an older Sidecar cannot dismiss a live request", async () => {
  mocks.dismiss.mockRejectedValueOnce(
    new ApiError("not yet saved", { status: 404 }),
  );
  const onDismissLive = vi.fn();
  const queryClient = new QueryClient();
  const renderPrompt = (messages: AiMessage[], live: boolean) => (
    <QueryClientProvider client={queryClient}>
      <AiAccessRequestPrompt
        sessionId="session-1"
        messages={messages}
        liveRequest={
          live ? { generationId: "generation-1", scopes: ["work"] } : undefined
        }
        streamingGenerationId={live ? "generation-1" : undefined}
        disabled={false}
        onPrepare={vi.fn()}
        onDismissLive={onDismissLive}
      />
    </QueryClientProvider>
  );
  const view = render(renderPrompt([], true));
  fireEvent.click(screen.getByRole("button", { name: "忽略" }));
  await waitFor(() => expect(mocks.dismiss).toHaveBeenCalledTimes(1));
  await waitFor(() =>
    expect(onDismissLive).toHaveBeenCalledWith("generation-1"),
  );
  const saved: AiMessage = {
    id: "message-1",
    session_id: "session-1",
    role: "assistant",
    status: "completed",
    content: "请核对权限",
    reasoning: null,
    task_id: null,
    task_title_snapshot: null,
    generation_id: "generation-1",
    context_provider: null,
    context_sources: [],
    context_knowledge: [],
    citation_status: "not_requested",
    citations: [],
    created_at: "2026-09-21T12:00:00Z",
    access_request: { scopes: ["work"] },
  };
  view.rerender(renderPrompt([saved], false));
  await waitFor(() => expect(mocks.dismiss).toHaveBeenCalledTimes(2));
  expect(screen.queryByRole("region", { name: "工作台权限请求" })).toBeNull();
});
