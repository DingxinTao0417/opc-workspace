import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../api/client";
import { clientActivityQueryKey } from "../api/hooks";
import { useAiChatStore } from "../store/aiChat";
import { ClientRecordLocation, ReturnToAiChat } from "./ClientRecordLocation";

const clientId = "018f0000-0000-7000-8000-000000005831";
const recordId = "018f0000-0000-7000-8000-000000005832";
const sessionId = "018f0000-0000-7000-8000-000000005833";
const record = { id: recordId, clientId, body: "准确记录正文" };
const clients: QueryClient[] = [];
function Location() {
  return <output aria-label="当前位置">{useLocation().pathname}</output>;
}
function setup(
  load = vi.fn().mockResolvedValue(record),
  kind: "activity" | "followup" = "activity",
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  clients.push(client);
  const close = vi.fn();
  const view = render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[`/clients/${clientId}`]}>
        <ClientRecordLocation
          clientId={clientId}
          recordId={recordId}
          kind={kind}
          load={load}
          onClose={close}
          returnSession={sessionId}
        >
          {(value: typeof record) => <p>{value.body}</p>}
        </ClientRecordLocation>
        <Location />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return { client, load, close, ...view };
}
afterEach(() => {
  cleanup();
  clients.splice(0).forEach((client) => client.clear());
  useAiChatStore.setState({ activeSessionId: "" });
});

describe("direct client record location", () => {
  it.each([
    { ctrlKey: true },
    { metaKey: true },
    { shiftKey: true },
    { altKey: true },
    { button: 1 },
  ])(
    "does not switch or close the current conversation on modified return %j",
    (modifiers) => {
      useAiChatStore.setState({ activeSessionId: "current-session" });
      const onNavigate = vi.fn();
      render(
        <MemoryRouter initialEntries={["/tasks"]}>
          <ReturnToAiChat sessionId={sessionId} onNavigate={onNavigate} />
          <Location />
        </MemoryRouter>,
      );
      // Prevent only jsdom's unsupported native navigation, after React's handlers.
      document.addEventListener("click", (event) => event.preventDefault(), {
        once: true,
      });
      fireEvent.click(
        screen.getByRole("link", { name: "返回原对话" }),
        modifiers,
      );
      expect(onNavigate).not.toHaveBeenCalled();
      expect(useAiChatStore.getState().activeSessionId).toBe("current-session");
      expect(screen.getByLabelText("当前位置")).toHaveTextContent("/tasks");
    },
  );
  it("does not process a prevented return click or a native middle-button auxiliary click", () => {
    useAiChatStore.setState({ activeSessionId: "current-session" });
    const onNavigate = vi.fn();
    render(
      <MemoryRouter initialEntries={["/tasks"]}>
        <div onClickCapture={(event) => event.preventDefault()}>
          <ReturnToAiChat sessionId={sessionId} onNavigate={onNavigate} />
        </div>
        <Location />
      </MemoryRouter>,
    );
    const link = screen.getByRole("link", { name: "返回原对话" });
    fireEvent(link, new MouseEvent("auxclick", { bubbles: true, button: 1 }));
    fireEvent.click(link);
    expect(onNavigate).not.toHaveBeenCalled();
    expect(useAiChatStore.getState().activeSessionId).toBe("current-session");
    expect(screen.getByLabelText("当前位置")).toHaveTextContent("/tasks");
  });
  it.each(["loading", "failed"])(
    "keeps followup return and close available during %s reads without unsaved work",
    async (status) => {
      const load =
        status === "loading"
          ? vi.fn(() => new Promise<typeof record>(() => {}))
          : vi.fn().mockRejectedValue(new ApiError("offline", { status: 503 }));
      const view = setup(load, "followup");
      if (status === "failed") await screen.findByText("客户回访不可用");
      else expect(screen.getByText("正在读取客户回访…")).toBeVisible();
      expect(screen.getByRole("button", { name: "关闭定位" })).toBeEnabled();
      fireEvent.click(screen.getByRole("button", { name: "关闭定位" }));
      expect(view.close).toHaveBeenCalledOnce();
      fireEvent.click(screen.getByRole("link", { name: "返回原对话" }));
      expect(useAiChatStore.getState().activeSessionId).toBe(sessionId);
      expect(screen.getByLabelText("当前位置")).toHaveTextContent("/ai");
    },
  );
  it.each(["activity", "followup"] as const)(
    "loads the exact %s, focuses it and returns to the source session",
    async (kind) => {
      const { load, close } = setup(undefined, kind);
      expect(await screen.findByText(record.body)).toBeVisible();
      expect(load).toHaveBeenCalledWith(recordId, expect.any(AbortSignal));
      expect(
        screen.getByLabelText(
          `定位的客户${kind === "activity" ? "活动" : "回访"}`,
        ),
      ).toHaveFocus();
      fireEvent.click(screen.getByRole("button", { name: "关闭定位" }));
      expect(close).toHaveBeenCalledOnce();
      useAiChatStore.setState({ activeSessionId: "another-session" });
      fireEvent.click(screen.getByRole("link", { name: "返回原对话" }));
      expect(useAiChatStore.getState().activeSessionId).toBe(sessionId);
      expect(screen.getByLabelText("当前位置")).toHaveTextContent("/ai");
    },
  );

  it.each([
    { ...record, clientId: sessionId },
    { ...record, id: sessionId },
  ])("does not render mismatched identity", async (data) => {
    setup(vi.fn().mockResolvedValue(data));
    expect(
      await screen.findByText("链接中的记录不属于当前客户，未展示其内容。"),
    ).toBeVisible();
    expect(screen.queryByText(record.body)).toBeNull();
  });

  it("shows a missing record and supports an explicit fresh retry", async () => {
    const load = vi
      .fn()
      .mockRejectedValueOnce(new ApiError("missing", { status: 404 }))
      .mockResolvedValue(record);
    setup(load);
    expect(await screen.findByText("客户活动不可用")).toBeVisible();
    expect(load).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(await screen.findByText(record.body)).toBeVisible();
    expect(load).toHaveBeenCalledTimes(2);
  });

  it("shares aggregate invalidation and never shows old content after a failed refresh", async () => {
    const { client, load } = setup();
    expect(await screen.findByText(record.body)).toBeVisible();
    load.mockRejectedValue(new Error("offline"));
    await act(async () => {
      await client.invalidateQueries({
        queryKey: clientActivityQueryKey(clientId),
      });
    });
    expect(await screen.findByText("客户活动不可用")).toBeVisible();
    expect(screen.queryByText(record.body)).toBeNull();
  });

  it("aborts an in-flight read when the selected location is unmounted", async () => {
    let signal: AbortSignal | undefined;
    const load = vi.fn((_id: string, input: AbortSignal) => {
      signal = input;
      return new Promise<typeof record>(() => {});
    });
    const view = setup(load);
    await waitFor(() => expect(load).toHaveBeenCalledOnce());
    expect(screen.getByText("正在读取客户活动…")).toBeVisible();
    view.unmount();
    expect(signal?.aborted).toBe(true);
  });
});
