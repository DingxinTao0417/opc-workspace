import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AiAssistantPage } from "./AiAssistantPage";
import { useAiChatStore } from "../store/aiChat";
import { resetRuntimeConnection } from "../api/client";

const provider = {
  id: "provider-1",
  name: "测试模型",
  kind: "local",
  protocol: "openai_chat",
  status: "ready",
  health_status: "healthy",
  model: "test",
  version: 1,
};
const session = {
  id: "session-1",
  title: "会话",
  persist: true,
  version: 1,
  created_at: "2026-09-10T12:00:00Z",
  updated_at: "2026-09-10T12:00:00Z",
};
const proposalId = "018f0000-0000-7000-8000-000000005741";
const message = {
  id: "message-1",
  session_id: session.id,
  role: "assistant",
  status: "completed",
  content: "",
  created_at: session.created_at,
};
const task = {
  id: "task-once",
  title: "修改后的任务",
  description: "",
  kind: "work",
  status: "todo",
  priority: "P2",
  review_policy: "none",
  blocked_from_status: null,
  estimated_minutes: null,
  manual_order: null,
};
const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
function page() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <AiAssistantPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
function baseResponse(url: string, row: unknown) {
  if (url.endsWith("/compaction"))
    return json({
      data: {
        session_id: session.id,
        status: "idle",
        partial_message: false,
        compacted_message_count: 0,
        error_code: null,
      },
    });
  if (url.endsWith("/ai/providers")) return json({ data: [provider] });
  if (url.endsWith("/ai/sessions")) return json({ data: [session] });
  if (url.includes("/messages?"))
    return json({ data: [row], meta: { has_more: false } });
  return null;
}

afterEach(() => {
  cleanup();
  useAiChatStore.setState({
    input: "",
    lastSessionId: "",
    streaming: null,
    interrupted: null,
    retainedTurns: [],
    streamError: null,
    activeGenerations: [],
  });
  sessionStorage.clear();
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

describe("AI page through real query hooks and HTTP adapters", () => {
  it("keeps a private SSE reply across page remounts without durable messages or confirmation requests", async () => {
    const reply = '[opc:task]{"title":"临时任务"}[/opc:task]';
    const fetcher = vi.fn(async (url: unknown) => {
      if (String(url).endsWith("/ai/chat")) {
        const body = [
          [
            "meta",
            { generation_id: "private-generation", session_id: session.id },
          ],
          ["delta", { generation_id: "private-generation", text: reply }],
          ["done", { generation_id: "private-generation" }],
        ]
          .map(
            ([event, data]) =>
              `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`,
          )
          .join("");
        return new Response(body);
      }
      if (String(url).endsWith("/ai/sessions"))
        return json({ data: [{ ...session, persist: false }] });
      if (String(url).includes("/messages?"))
        return json({ data: [], meta: { has_more: false } });
      const common = baseResponse(String(url), null);
      if (common) return common;
      throw new Error(`Unexpected endpoint ${url}`);
    });
    vi.stubGlobal("fetch", fetcher);
    const first = page();
    const input = await screen.findByPlaceholderText(/向 AI 助手提问/);
    fireEvent.change(input, { target: { value: "帮我创建临时任务" } });
    fireEvent.click(screen.getByRole("button", { name: "发送" }));
    await screen.findByText(/已整理任务建议「临时任务」，尚未创建/);
    await waitFor(() => expect(input).toHaveValue(""));
    first.unmount();
    page();
    await screen.findByText(/已整理任务建议「临时任务」，尚未创建/);
    expect(screen.getByText("帮我创建临时任务")).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: /点击确认创建|记住/ }),
    ).toBeNull();
    expect(
      fetcher.mock.calls.some(([url]) =>
        /task-confirmation|memory-decision|memory-proposals/.test(String(url)),
      ),
    ).toBe(false);
    expect(JSON.stringify(sessionStorage)).not.toContain("临时任务");
  });

  it("reports partial compaction and failure without claiming all history was summarized", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: unknown) => {
        if (String(url).endsWith("/compaction"))
          return json({
            data: {
              session_id: session.id,
              status: "failed",
              partial_message: true,
              compacted_message_count: 0,
              error_code: "AI_STREAM_INCOMPLETE",
            },
          });
        return (
          baseResponse(String(url), { ...message, content: "普通历史" }) ??
          json({ data: [] })
        );
      }),
    );
    page();
    await screen.findByText("较长消息正在分段整理，尚未全部压缩");
    expect(
      screen.getByText("早期对话整理失败，当前使用最近对话；后续会再次尝试"),
    ).toBeTruthy();
    expect(screen.queryByText(/已压缩前/)).toBeNull();
  });
  it.each([true, false])(
    "persists ignoring a %s modern proposal across a fresh page/query client",
    async (modern) => {
      let status = "pending";
      const row = {
        ...message,
        content: `[opc:memory]${JSON.stringify({ content: "回答保持简洁", ...(modern ? { proposal_id: proposalId } : {}) })}[/opc:memory]`,
      };
      const path = modern
        ? `/ai/memory-proposals/${proposalId}`
        : `/ai/messages/${message.id}/memory-decision`;
      const deletes: string[] = [];
      vi.stubGlobal(
        "fetch",
        vi.fn(async (url: unknown, init?: RequestInit) => {
          const common = baseResponse(String(url), row);
          if (common) return common;
          if (String(url).endsWith(path)) {
            if (init?.method === "DELETE") {
              status = "rejected";
              deletes.push(String(url));
            }
            return json({
              data: {
                id: proposalId,
                session_id: session.id,
                content: "回答保持简洁",
                status,
                memory_id: null,
              },
            });
          }
          throw new Error(`Unexpected endpoint ${url}`);
        }),
      );
      const first = page();
      await waitFor(() =>
        expect(screen.getByRole("button", { name: "忽略" })).not.toBeDisabled(),
      );
      fireEvent.click(screen.getByRole("button", { name: "忽略" }));
      await screen.findByText("已忽略这条记忆建议");
      first.unmount();
      page();
      await screen.findByText("已忽略这条记忆建议");
      expect(deletes).toHaveLength(1);
      expect(screen.queryByRole("button", { name: "记住" })).toBeNull();
    },
  );

  it("re-reads confirmed memory state and rejects a foreign proposal identity", async () => {
    let status = "pending";
    let decisionSession = session.id;
    let saves = 0;
    const row = {
      ...message,
      content: `[opc:memory]{"content":"回答保持简洁","proposal_id":"${proposalId}"}[/opc:memory]`,
    };
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: unknown, init?: RequestInit) => {
        const common = baseResponse(String(url), row);
        if (common) return common;
        if (String(url).endsWith(`/memory-proposals/${proposalId}`))
          return json({
            data: {
              id: proposalId,
              session_id: decisionSession,
              content: "回答保持简洁",
              status,
              memory_id: status === "confirmed" ? "memory-1" : null,
            },
          });
        if (String(url).endsWith("/ai/memories") && init?.method === "POST") {
          const body = JSON.parse(String(init.body));
          expect(body).toMatchObject({
            source_message_id: message.id,
            proposal_id: proposalId,
            content: "回答保持简洁",
          });
          saves++;
          status = "confirmed";
          return json({ data: { id: "memory-1", content: "回答保持简洁" } });
        }
        throw new Error(`Unexpected endpoint ${url}`);
      }),
    );
    const first = page();
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "记住" })).not.toBeDisabled(),
    );
    fireEvent.click(screen.getByRole("button", { name: "记住" }));
    await screen.findByText("已记住：回答保持简洁");
    first.unmount();
    const second = page();
    await screen.findByText("已记住：回答保持简洁");
    expect(saves).toBe(1);
    second.unmount();
    decisionSession = "different-session";
    status = "pending";
    page();
    await screen.findByText("记忆建议与当前会话不一致，未执行任何操作。");
    expect(screen.queryByRole("button", { name: "记住" })).toBeNull();
  });

  it("reports the actual task result after the confirmation response is lost", async () => {
    let created = false;
    let creations = 0;
    const row = {
      ...message,
      content: '已创建任务。[opc:task]{"title":"原建议"}[/opc:task]',
    };
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: unknown, init?: RequestInit) => {
        const common = baseResponse(String(url), {
          ...row,
          task_id: created ? task.id : null,
          task_title_snapshot: created ? task.title : null,
        });
        if (common) return common;
        if (String(url).endsWith(`/messages/${message.id}/task-confirmation`)) {
          expect(JSON.parse(String(init?.body)).title).toBe("修改后的任务");
          if (!created) {
            created = true;
            creations++;
          }
          throw new TypeError("response lost after commit");
        }
        if (String(url).endsWith(`/tasks/${task.id}`))
          return json({ data: task });
        throw new Error(`Unexpected endpoint ${url}`);
      }),
    );
    const first = page();
    await screen.findByText(/尚未创建/);
    fireEvent.click(screen.getByRole("button", { name: /建议任务：原建议/ }));
    fireEvent.change(screen.getByDisplayValue("原建议"), {
      target: { value: "修改后的任务" },
    });
    fireEvent.click(screen.getByRole("button", { name: "确认创建" }));
    await screen.findByText("已创建任务「修改后的任务」，可以从下方打开查看。");
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "确认创建" })).toBeNull(),
    );
    first.unmount();
    page();
    await screen.findByText("已创建任务「修改后的任务」，可以从下方打开查看。");
    expect(creations).toBe(1);
  });

  it("keeps the draft on a real pre-acceptance provider rejection", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: unknown) => {
        const common = baseResponse(String(url), {
          ...message,
          content: "普通历史",
        });
        if (common) return common;
        if (String(url).endsWith("/ai/chat"))
          return json({ code: "AI_PROVIDER_BUSY", message: "供应商忙" }, 409);
        throw new Error(`Unexpected endpoint ${url}`);
      }),
    );
    page();
    const input = await screen.findByPlaceholderText(/向 AI 助手提问/);
    fireEvent.change(input, { target: { value: "不能丢失的草稿" } });
    fireEvent.click(screen.getByRole("button", { name: "发送" }));
    await screen.findByText("供应商忙");
    expect(input).toHaveValue("不能丢失的草稿");
  });
});
