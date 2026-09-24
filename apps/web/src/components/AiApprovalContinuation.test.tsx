import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { getAiGeneration } from "../api/aiActions";
import { AiApprovalContinuation } from "./AiApprovalContinuation";
import type { AiActionContinuation } from "../lib/aiActionContinuation";

vi.mock("../api/aiActions", () => ({ getAiGeneration: vi.fn() }));
vi.mock("./AiWorkspaceActions", () => ({
  AiWorkspaceActions: (props: {
    generationId: string;
    sessionId: string;
    proposalId: string;
    onContinue?: (request: AiActionContinuation) => void;
  }) => (
    <div data-testid="original-approval">
      {JSON.stringify(props)}
      {props.onContinue ? (
        <button
          type="button"
          onClick={() =>
            props.onContinue?.({
              generationId: props.generationId,
              prompt: "固定继续提示",
              scopes: ["work", "actions"],
            })
          }
        >
          模拟继续
        </button>
      ) : null}
    </div>
  ),
}));
const target = {
  kind: "approval" as const,
  sessionId: "018f0000-0000-7000-8000-000000000751",
  generationId: "018f0000-0000-7000-8000-000000000752",
  proposalId: "018f0000-0000-7000-8000-000000000753",
};
function setup(onContinue?: (request: AiActionContinuation) => void) {
  const onClose = vi.fn();
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <AiApprovalContinuation
        target={target}
        onClose={onClose}
        onContinue={onContinue}
      />
    </QueryClientProvider>,
  );
  return onClose;
}
beforeEach(() => vi.resetAllMocks());
afterEach(cleanup);

it("only exposes the source input continuation after ownership has been verified", async () => {
  vi.mocked(getAiGeneration).mockResolvedValue({
    id: target.generationId,
    session_id: target.sessionId,
    persist: true,
  } as never);
  const next = vi.fn();
  setup(next);
  fireEvent.click(await screen.findByRole("button", { name: "模拟继续" }));
  expect(next).toHaveBeenCalledExactlyOnceWith({
    generationId: target.generationId,
    prompt: "固定继续提示",
    scopes: ["work", "actions"],
  });
});

it("opens the exact original card without loading older chat pages", async () => {
  vi.mocked(getAiGeneration).mockResolvedValue({
    id: target.generationId,
    session_id: target.sessionId,
    persist: true,
  } as never);
  const close = setup();
  const card = await screen.findByTestId("original-approval");
  expect(JSON.parse(card.textContent!)).toEqual({
    sessionId: target.sessionId,
    generationId: target.generationId,
    proposalId: target.proposalId,
  });
  expect(getAiGeneration).toHaveBeenCalledExactlyOnceWith(target.generationId);
  fireEvent.click(screen.getByRole("button", { name: "关闭" }));
  expect(close).toHaveBeenCalledOnce();
});
it.each([
  {
    id: target.generationId,
    session_id: "018f0000-0000-7000-8000-000000000754",
    persist: true,
  },
  { id: target.proposalId, session_id: target.sessionId, persist: true },
  { id: target.generationId, session_id: target.sessionId, persist: false },
])(
  "refuses to show an approval from a mismatched or unsaved source",
  async (generation) => {
    vi.mocked(getAiGeneration).mockResolvedValue(generation as never);
    setup();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "不属于当前保存会话",
    );
    expect(screen.queryByTestId("original-approval")).toBeNull();
  },
);
it("offers a read retry after source lookup fails", async () => {
  vi.mocked(getAiGeneration).mockRejectedValueOnce(new Error("offline"));
  setup();
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "无法读取原操作建议",
  );
  vi.mocked(getAiGeneration).mockResolvedValue({
    id: target.generationId,
    session_id: target.sessionId,
    persist: true,
  } as never);
  fireEvent.click(screen.getByRole("button", { name: "重新定位" }));
  await waitFor(() =>
    expect(screen.getByTestId("original-approval")).toBeInTheDocument(),
  );
});
