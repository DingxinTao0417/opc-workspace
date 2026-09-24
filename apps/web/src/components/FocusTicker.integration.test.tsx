import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { focusSessionQueryKey } from "../api/hooks";
import { useFocusCycleStore } from "../store/focus";
import { DEFAULT_FOCUS_SETTINGS, useSettingsStore } from "../store/settings";
import type { FocusSessionSnapshot } from "../types/models";
import { FocusTicker } from "./FocusTicker";
import { AiWorkspaceActions } from "./AiWorkspaceActions";
import type { AiActionProposal } from "../api/aiWorkspaceActions";

const calls = vi.hoisted(() => ({
  active: vi.fn(),
  stop: vi.fn(),
  create: vi.fn(),
  actions: vi.fn(),
  decide: vi.fn(),
}));
vi.mock("../api/aiWorkspaceActions", async () => ({
  ...(await vi.importActual("../api/aiWorkspaceActions")),
  getAiWorkspaceActions: calls.actions,
  decideAiWorkspaceAction: calls.decide,
}));
vi.mock("../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../api/client")>("../api/client");
  return {
    ...actual,
    getActiveFocusSession: calls.active,
    stopFocusSession: calls.stop,
    createFocusSession: calls.create,
  };
});

function snapshot(
  id: string,
  status: "active" | "paused" | "completed",
  elapsed = 0,
): FocusSessionSnapshot {
  return {
    serverNow: "2026-09-18T12:00:00Z",
    receivedAtMs: Date.now(),
    session: {
      id,
      status,
      taskId: "same-task",
      taskTitle: "同一任务",
      plannedSeconds: 300,
      accumulatedSeconds: elapsed,
      version: status === "active" ? 1 : 2,
      startedAt: "2026-09-18T11:55:00Z",
      endedAt: status === "completed" ? "2026-09-18T12:00:00Z" : null,
      lastResumedAt: status === "active" ? "2026-09-18T12:00:00Z" : null,
      lastHeartbeatAt: "2026-09-18T12:00:00Z",
      endReason: status === "completed" ? "completed" : null,
      createdAt: "2026-09-18T11:55:00Z",
      updatedAt: "2026-09-18T12:00:00Z",
    },
  };
}

let client: QueryClient;
beforeEach(() => {
  vi.resetAllMocks();
  useFocusCycleStore.getState().resetCycle();
  useSettingsStore.getState().resetSettings();
  useSettingsStore.setState({ soundEnabled: false });
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
});
afterEach(() => {
  cleanup();
  client.clear();
});

function mount(initial: FocusSessionSnapshot, withApproval = false) {
  client.setQueryData(focusSessionQueryKey, initial);
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <FocusTicker />
        {withApproval ? <AiWorkspaceActions generationId="generation" /> : null}
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("Focus Session / query / cycle integration", () => {
  const focusApproval: AiActionProposal = {
    id: "00000000-0000-4000-8000-000000000001",
    generation_id: "generation",
    fingerprint: "a".repeat(64),
    action: {
      action: "focus.start",
      changes: { task_id: null, planned_seconds: 300 },
    },
    preview: {
      label: "专注",
      before: { status: "idle" },
      after: {
        status: "active",
        task_id: null,
        task_title: null,
        task_version: null,
        planned_seconds: 300,
      },
    },
    status: "pending",
    can_confirm: true,
    result_id: null,
    result_version: null,
    route: "",
    created_at: "2026-09-18T12:00:00Z",
    decided_at: null,
  };
  it("adopts a confirmed AI start as a new cycle even when the HTTP decision response is lost", async () => {
    const cycle = useFocusCycleStore.getState();
    cycle.beginWork("same-task", 3, "同一任务", "old");
    cycle.completeWork(
      "same-task",
      { ...DEFAULT_FOCUS_SETTINGS, autoStartBreak: false },
      "同一任务",
      Date.now(),
      "old",
    );
    const current = snapshot("from-ai", "active");
    current.session!.taskId = null;
    current.session!.taskTitle = null;
    calls.actions.mockResolvedValue([focusApproval]);
    calls.active.mockResolvedValue({ ...current, session: null });
    calls.decide.mockImplementation(async () => {
      calls.actions.mockResolvedValue([
        {
          ...focusApproval,
          status: "confirmed",
          can_confirm: false,
          result_id: "from-ai",
          result_version: 1,
          route: "/focus",
        },
      ]);
      calls.active.mockResolvedValue(current);
      throw new Error("response lost after commit");
    });
    mount({ ...current, session: null }, true);
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /我确认不绑定任务/ }),
    );
    fireEvent.click(screen.getByRole("button", { name: "确认执行" }));
    await waitFor(() =>
      expect(useFocusCycleStore.getState()).toMatchObject({
        phase: "work",
        sessionId: "from-ai",
        completedCycles: 0,
        taskId: null,
      }),
    );
    expect(await screen.findByText("已执行")).toBeInTheDocument();
    expect(calls.decide).toHaveBeenCalledTimes(1);
    expect(calls.create).not.toHaveBeenCalled();
    expect(calls.stop).not.toHaveBeenCalled();
  });
  it.each(["focus.stop", "focus.cancel"] as const)(
    "clears only the active work cycle after AI %s without creating a rest",
    async (action) => {
      const current = snapshot("from-ai", "active");
      const proposal = {
        ...focusApproval,
        action: {
          action,
          focus_session_id: "from-ai",
          expected_version: 1,
          changes: {},
        },
        preview: {
          label: "专注",
          before: { status: "active" },
          after: {
            status: action === "focus.stop" ? "completed" : "cancelled",
            task_id: "same-task",
            task_title: "同一任务",
            planned_seconds: 300,
          },
        },
        route: "/focus",
      };
      calls.actions.mockResolvedValue([proposal]);
      calls.active.mockResolvedValue(current);
      calls.decide.mockImplementation(async () => {
        const confirmed = {
          ...proposal,
          status: "confirmed",
          can_confirm: false,
          result_id: "from-ai",
          result_version: 2,
        };
        calls.actions.mockResolvedValue([confirmed]);
        calls.active.mockResolvedValue({ ...current, session: null });
        return confirmed;
      });
      mount(current, true);
      await waitFor(() =>
        expect(useFocusCycleStore.getState().sessionId).toBe("from-ai"),
      );
      fireEvent.click(await screen.findByRole("button", { name: "确认执行" }));
      await waitFor(() =>
        expect(useFocusCycleStore.getState()).toMatchObject({
          phase: "idle",
          sessionId: null,
          completedCycles: 0,
        }),
      );
      expect(calls.create).not.toHaveBeenCalled();
      expect(calls.stop).not.toHaveBeenCalled();
    },
  );
  it("does not replay an old confirmed AI start over an unrelated current rest", async () => {
    const cycle = useFocusCycleStore.getState();
    cycle.beginWork("same-task", 3, "同一任务", "newer");
    cycle.completeWork(
      "same-task",
      { ...DEFAULT_FOCUS_SETTINGS, autoStartBreak: false },
      "同一任务",
      Date.now(),
      "newer",
    );
    const revision = useFocusCycleStore.getState().revision;
    calls.actions.mockResolvedValue([
      {
        ...focusApproval,
        status: "confirmed",
        can_confirm: false,
        result_id: "old",
        result_version: 1,
        route: "/focus",
      },
    ]);
    const empty = { ...snapshot("old", "completed"), session: null };
    calls.active.mockResolvedValue(empty);
    mount(empty, true);
    expect(await screen.findByText("已执行")).toBeInTheDocument();
    expect(useFocusCycleStore.getState()).toMatchObject({
      phase: "break",
      sessionId: "newer",
      completedCycles: 1,
      revision,
    });
    expect(calls.decide).not.toHaveBeenCalled();
    expect(calls.create).not.toHaveBeenCalled();
  });
  it("keeps auto-stop intent while GET already reports empty, then counts exactly one round", async () => {
    let finish!: (value: FocusSessionSnapshot) => void;
    calls.stop.mockImplementation(
      () =>
        new Promise<FocusSessionSnapshot>((resolve) => {
          finish = resolve;
        }),
    );
    const empty = { ...snapshot("a", "completed", 300), session: null };
    calls.active.mockResolvedValue(empty);
    mount(snapshot("a", "active", 300));
    await waitFor(() => expect(calls.stop).toHaveBeenCalledTimes(1));
    await act(async () => {
      await client.fetchQuery({
        queryKey: focusSessionQueryKey,
        queryFn: async () => empty,
      });
    });
    expect(useFocusCycleStore.getState()).toMatchObject({
      phase: "work",
      sessionId: "a",
    });
    await act(async () => finish(snapshot("a", "completed", 300)));
    await waitFor(() =>
      expect(useFocusCycleStore.getState()).toMatchObject({
        phase: "break",
        sessionId: "a",
        completedCycles: 1,
      }),
    );
    expect(calls.stop).toHaveBeenCalledTimes(1);
    expect(
      client.getQueryData<FocusSessionSnapshot>(focusSessionQueryKey)?.session,
    ).toBeNull();
  });

  it("does not complete B when A's automatic stop response arrives late", async () => {
    let finish!: (value: FocusSessionSnapshot) => void;
    calls.stop.mockImplementation(
      () =>
        new Promise<FocusSessionSnapshot>((resolve) => {
          finish = resolve;
        }),
    );
    mount(snapshot("a", "active", 300));
    await waitFor(() => expect(calls.stop).toHaveBeenCalledTimes(1));
    const newer = snapshot("b", "paused");
    calls.active.mockResolvedValue(newer);
    await act(async () => {
      client.setQueryData(focusSessionQueryKey, newer);
    });
    await waitFor(() =>
      expect(useFocusCycleStore.getState().sessionId).toBe("b"),
    );
    await act(async () => finish(snapshot("a", "completed", 300)));
    await waitFor(() => expect(client.isMutating()).toBe(0));
    expect(useFocusCycleStore.getState()).toMatchObject({
      phase: "work",
      sessionId: "b",
      completedCycles: 0,
    });
    expect(
      client.getQueryData<FocusSessionSnapshot>(focusSessionQueryKey)?.session
        ?.id,
    ).toBe("b");
  });

  it("continues a local break through the actual create hook without losing completed rounds", async () => {
    useSettingsStore.setState({ autoStartFocus: true });
    const cycle = useFocusCycleStore.getState();
    cycle.beginWork("same-task", 2, "同一任务", "a");
    cycle.completeWork("same-task", DEFAULT_FOCUS_SETTINGS, "同一任务", 1, "a");
    const newer = snapshot("b", "active");
    calls.create.mockResolvedValue(newer);
    calls.active.mockResolvedValue(newer);
    mount({ ...newer, session: null });
    await waitFor(() =>
      expect(useFocusCycleStore.getState()).toMatchObject({
        phase: "work",
        sessionId: "b",
        completedCycles: 1,
        targetCycles: 2,
      }),
    );
    expect(calls.create).toHaveBeenCalledTimes(1);
    expect(calls.stop).not.toHaveBeenCalled();
  });

  it("adopts an external session and ignores the older auto-create callback", async () => {
    useSettingsStore.setState({ autoStartFocus: true });
    const cycle = useFocusCycleStore.getState();
    cycle.beginWork("same-task", 2, "同一任务", "previous");
    cycle.completeWork(
      "same-task",
      DEFAULT_FOCUS_SETTINGS,
      "同一任务",
      1,
      "previous",
    );
    let finish!: (value: FocusSessionSnapshot) => void;
    calls.create.mockImplementation(
      () =>
        new Promise<FocusSessionSnapshot>((resolve) => {
          finish = resolve;
        }),
    );
    mount({ ...snapshot("a", "active"), session: null });
    await waitFor(() => expect(calls.create).toHaveBeenCalledTimes(1));
    const newer = snapshot("b", "paused");
    calls.active.mockResolvedValue(newer);
    await act(async () => {
      client.setQueryData(focusSessionQueryKey, newer);
    });
    await waitFor(() =>
      expect(useFocusCycleStore.getState().sessionId).toBe("b"),
    );
    await act(async () => finish(snapshot("a", "active")));
    await waitFor(() => expect(client.isMutating()).toBe(0));
    expect(useFocusCycleStore.getState()).toMatchObject({
      phase: "work",
      sessionId: "b",
      completedCycles: 0,
    });
    expect(
      client.getQueryData<FocusSessionSnapshot>(focusSessionQueryKey)?.session
        ?.id,
    ).toBe("b");
  });
});
