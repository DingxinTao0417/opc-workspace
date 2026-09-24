import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
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
} from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../api/client";
import { getKnowledgeChunk } from "../api/knowledgeChunk";
import { knowledgeLocationHref } from "../lib/knowledgeLocation";
import { useAiChatStore } from "../store/aiChat";
import { KnowledgeCitationLocation } from "./KnowledgeCitationLocation";

vi.mock("../api/knowledgeChunk", () => ({ getKnowledgeChunk: vi.fn() }));
const evidence = {
  source_id: "018f0000-0000-7000-8000-000000006101",
  document_id: "018f0000-0000-7000-8000-000000006102",
  chunk_id: "018f0000-0000-7000-8000-000000006103",
  source_version: 2,
  document_version: 1,
  source_name: "evidence.pdf",
  document_title: "Evidence",
  source_type: "pdf" as const,
  content: "本地证据 <script>alert(1)</script>",
  chunk_index: 0,
  start_char: 0,
  end_char: 29,
  start_line: 2,
  end_line: 4,
  start_page: 1,
  end_page: 3,
};
const sessionId = "018f0000-0000-7000-8000-000000006104";
const href = knowledgeLocationHref(evidence, sessionId)!;
function CurrentRoute() {
  const location = useLocation();
  const navigate = useNavigate();
  return (
    <>
      <output aria-label="current route">
        {location.pathname + location.search}
      </output>
      <button onClick={() => navigate(href)} type="button">
        打开引用
      </button>
    </>
  );
}
function mount(route = href) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const view = render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[route]}>
        <CurrentRoute />
        <Routes>
          <Route path="/knowledge" element={<KnowledgeCitationLocation />} />
          <Route path="/ai" element={<div>原对话</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return { ...view, client };
}
beforeEach(() => {
  vi.mocked(getKnowledgeChunk).mockReset();
  vi.mocked(getKnowledgeChunk).mockResolvedValue(evidence);
  useAiChatStore.setState({ activeSessionId: "" });
});
afterEach(cleanup);
describe("knowledge citation preview", () => {
  it("cancels a pending read when the preview is closed", async () => {
    let pendingSignal: AbortSignal | undefined;
    vi.mocked(getKnowledgeChunk).mockImplementation((_location, signal) => {
      pendingSignal = signal;
      return new Promise((_resolve, reject) =>
        signal!.addEventListener(
          "abort",
          () => reject(new Error("cancelled")),
          { once: true },
        ),
      );
    });
    mount();
    await waitFor(() => expect(pendingSignal).toBeDefined());
    expect(screen.getByText("正在核验来源版本…")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "关闭预览" }));
    await waitFor(() => expect(pendingSignal!.aborted).toBe(true));
    expect(screen.queryByRole("dialog")).toBeNull();
  });
  it("loads only the addressed chunk, renders plain text/pages and returns to the exact conversation", async () => {
    mount();
    expect(await screen.findByLabelText("知识引用原片段")).toHaveTextContent(
      evidence.content,
    );
    expect(document.querySelector("script")).toBeNull();
    expect(screen.getByText(/第 1–3 页 · 第 2–4 行/)).toBeVisible();
    expect(screen.getByText(/不是原始版面/)).toBeVisible();
    expect(getKnowledgeChunk).toHaveBeenCalledWith(
      {
        source_id: evidence.source_id,
        document_id: evidence.document_id,
        chunk_id: evidence.chunk_id,
        source_version: 2,
        document_version: 1,
      },
      expect.any(AbortSignal),
    );
    fireEvent.click(screen.getByRole("link", { name: "返回对话" }));
    expect(screen.getByText("原对话")).toBeVisible();
    expect(useAiChatStore.getState().activeSessionId).toBe(sessionId);
  });
  it("blocks invalid links without reading and preserves unrelated URL parameters when closing", async () => {
    mount(
      "/knowledge?chunk_id=../../private&filter=ready&return_session=https://example.com",
    );
    expect(screen.getByText("引用位置无效")).toBeVisible();
    expect(getKnowledgeChunk).not.toHaveBeenCalled();
    expect(screen.queryByRole("link", { name: "返回对话" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "关闭预览" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.getByLabelText("current route")).toHaveTextContent(
      "/knowledge?filter=ready",
    );
  });
  it.each(["KNOWLEDGE_CHUNK_NOT_FOUND", "KNOWLEDGE_LOCATION_CHANGED"])(
    "does not replace expired evidence or display cached text: %s",
    async (code) => {
      const { client } = mount();
      await screen.findByLabelText("知识引用原片段");
      vi.mocked(getKnowledgeChunk).mockRejectedValue(
        new ApiError("private server detail", { code }),
      );
      await client.invalidateQueries({ queryKey: ["knowledge", "chunk"] });
      expect(await screen.findByText("原引用片段已不可用")).toBeVisible();
      expect(screen.queryByLabelText("知识引用原片段")).toBeNull();
      expect(screen.queryByText("private server detail")).toBeNull();
      expect(screen.getByText(/不会用新版片段替代/)).toBeVisible();
    },
  );
  it("offers a retry for service failure and rereads after closing and reopening", async () => {
    vi.mocked(getKnowledgeChunk).mockRejectedValueOnce(new Error("offline"));
    mount();
    expect(await screen.findByText("暂时无法读取引用")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    await screen.findByLabelText("知识引用原片段");
    fireEvent.click(screen.getByRole("button", { name: "关闭预览" }));
    fireEvent.click(screen.getByRole("button", { name: "打开引用" }));
    await waitFor(() => expect(getKnowledgeChunk).toHaveBeenCalledTimes(3));
  });
});
