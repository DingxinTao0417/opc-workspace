import { afterEach, expect, it, vi } from "vitest";
import { streamAiChat } from "./ai";
import { useAiChatStore } from "../store/aiChat";
import { useAiProjectFiles } from "../store/aiProjectFiles";
import { useAiProjectFileReview } from "../store/aiProjectFileReview";
import { workspaceApi } from "./workspace";
import type { AiProvider } from "../types/models";
vi.mock("./client", async (load) => ({
  ...(await load<typeof import("./client")>()),
  getRuntimeConnection: vi.fn(async () => ({
    baseUrl: "http://127.0.0.1:9876",
    token: "test",
  })),
}));
vi.mock("./workspace", () => ({
  workspaceApi: {
    fileSnapshot: vi.fn(),
    validateFileSnapshot: vi.fn(async () => {}),
    releaseFileSnapshot: vi.fn(async () => {}),
  },
}));
vi.mock("./aiActions", () => ({
  getActiveAiGenerations: vi.fn(async () => []),
  getAiGeneration: vi.fn(async () => {
    throw new Error("unavailable");
  }),
  getAiGenerationByRequest: vi.fn(async () => {
    throw new Error("unavailable");
  }),
  cancelAiGeneration: vi.fn(),
}));
const session = "018f0000-0000-7000-8000-000000000101";
const provider = {
  id: "018f0000-0000-7000-8000-000000000102",
  version: 2,
  name: "test",
  model: "test",
} as AiProvider;
const generation = "018f0000-0000-7000-8000-000000000104";
const payload = {
  generation_id: generation,
  proposal_id: "018f0000-0000-7000-8000-000000000105",
  path: "a.txt",
  base_sha256: "a".repeat(64),
  content: "\ufeffcandidate\r\n",
};
const block = (event: string, data: unknown) =>
  `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`;
afterEach(() => {
  useAiProjectFiles.getState().clear();
  useAiChatStore.setState({
    streaming: null,
    retainedTurns: [],
    interrupted: null,
    acceptedCommand: null,
    streamError: null,
    activeGenerations: [],
  });
  sessionStorage.clear();
  vi.clearAllMocks();
  vi.unstubAllGlobals();
});

it.each([
  { content: undefined },
  { content: null },
  { content: "\ud800" },
  { content: "\0" },
  { content: "汉".repeat(12000) },
  { base_sha256: "bad" },
  { proposal_id: "bad" },
  { path: 4 },
  { write: true },
])(
  "rejects malformed proposal %j before it reaches the store",
  async (change) => {
    const event = { ...payload, ...change };
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            block("project_file_proposal", event) +
              block("done", { generation_id: generation }),
          ),
      ),
    );
    const events: unknown[] = [];
    await expect(
      streamAiChat({
        providerId: provider.id,
        sessionId: session,
        message: "edit",
        onEvent: (e) => events.push(e),
      }),
    ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
    expect(events).toEqual([]);
  },
);

it.each([
  "done",
  "cancelled",
  "error",
  "disconnect",
  "wrong-generation",
  "unmount",
  "side-owner",
])(
  "real parser and chat store keep a review only after matching completion: %s",
  async (mode) => {
    vi.mocked(workspaceApi.fileSnapshot).mockResolvedValue({
      id: "018f0000-0000-7000-8000-000000000103",
      path: "a.txt",
      content: "original",
      size: 8,
      sha256: payload.base_sha256,
      expiresInSeconds: 600,
    });
    await useAiProjectFiles
      .getState()
      .stage({ id: "root", name: "project", path: "D:/private" }, "a.txt");
    useAiProjectFiles.getState().approve(session, provider);
    const files = await useAiProjectFiles.getState().prepare(session, provider);
    useAiProjectFiles.getState().consumeForReview(provider);
    const fetchMock = vi.fn(async () => {
      if (mode === "unmount") useAiProjectFiles.getState().clear();
      const data =
        block("meta", {
          generation_id: generation,
          session_id: session,
          provider_id: provider.id,
          model: "test",
          protocol: "openai_chat",
          sse_protocol: "opc-ai-sse-v1",
        }) +
        block("project_file_proposal", {
          ...payload,
          ...(mode === "wrong-generation" ? { generation_id: session } : {}),
        }) +
        (mode === "disconnect"
          ? ""
          : block(mode === "cancelled" || mode === "error" ? mode : "done", {
              generation_id: generation,
              error: "AI_STREAM_ERROR",
            }));
      return new Response(data);
    });
    vi.stubGlobal("fetch", fetchMock);
    await useAiChatStore.getState().send({
      providerId: provider.id,
      sessionId: session,
      message: "edit",
      projectFiles: files,
      ...(mode === "side-owner" ? { owner: "side" } : {}),
    });
    const review = useAiProjectFileReview.getState().review;
    if (mode === "done") {
      expect(review?.status).toBe("ready");
      expect(review?.proposal?.content).toBe(payload.content);
      expect(workspaceApi.releaseFileSnapshot).not.toHaveBeenCalled();
    } else expect(review).toBeNull();
    expect(workspaceApi.validateFileSnapshot).toHaveBeenCalledTimes(1); // Only pre-send; no native action from SSE.
    expect(
      JSON.stringify(useAiChatStore.getState().retainedTurns),
    ).not.toContain("candidate");
    expect(JSON.stringify(sessionStorage)).not.toContain("candidate");
    expect(fetchMock).toHaveBeenCalledTimes(1);
  },
);
