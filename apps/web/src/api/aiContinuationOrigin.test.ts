import { afterEach, describe, expect, it, vi } from "vitest";
import { getAiGeneration } from "./aiActions";
import { getAiMessages, resetRuntimeConnection } from "./client";
import { useAiChatStore } from "../store/aiChat";

const id = "018f0000-0000-7000-8000-000000000001";
const origin = {
  kind: "plan_continuation",
  continuation_id: id,
  turn_index: 1,
  max_turns: 3,
};
const generation = {
  id,
  session_id: id,
  provider_id: id,
  status: "streaming",
  content: "自动分析",
  reasoning: "",
  persist: true,
  origin,
};
afterEach(() => {
  vi.unstubAllGlobals();
  resetRuntimeConnection();
  useAiChatStore.setState({
    activeGenerations: [],
    streaming: null,
    input: "",
    retainedTurns: [],
    acceptedCommand: null,
  });
  sessionStorage.clear();
});
describe("automatic continuation provenance", () => {
  it("keeps provenance on generation and saved user/assistant messages", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async (url) =>
          new Response(
            JSON.stringify(
              String(url).includes("/messages")
                ? {
                    data: ["user", "assistant"].map((role) => ({
                      ...generation,
                      role,
                      status: "completed",
                      citation_status: "not_requested",
                      citations: [],
                    })),
                    meta: { has_more: false },
                  }
                : { data: generation },
            ),
          ),
      ),
    );
    expect((await getAiGeneration(id)).origin).toEqual(origin);
    expect((await getAiMessages(id)).data.map((m) => m.origin)).toEqual([
      origin,
      origin,
    ]);
  });
  it.each([
    null,
    {},
    { ...origin, kind: "manual" },
    { ...origin, turn_index: 0 },
    { ...origin, turn_index: 4 },
    { ...origin, max_turns: 9 },
    { ...origin, continuation_id: "bad" },
    { ...origin, grant: { scopes: ["actions"] } },
  ])("fails closed on malformed present provenance %j", async (bad) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({ data: { ...generation, origin: bad } }),
          ),
      ),
    );
    await expect(getAiGeneration(id)).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
  });
  it("does not adopt an automatic generation into manual stream, draft, or acceptance", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response(JSON.stringify({ data: [generation] }))),
    );
    useAiChatStore.setState({
      streaming: null,
      activeGenerations: [],
      input: "手写草稿",
      lastSessionId: "manual-session",
      acceptedCommand: null,
    });
    await useAiChatStore.getState().recover();
    expect(useAiChatStore.getState().streaming).toBeNull();
    expect(useAiChatStore.getState().input).toBe("手写草稿");
    expect(useAiChatStore.getState().lastSessionId).toBe("manual-session");
    expect(useAiChatStore.getState().acceptedCommand).toBeNull();
  });
});
