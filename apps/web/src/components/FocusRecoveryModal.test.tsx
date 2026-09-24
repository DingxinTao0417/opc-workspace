import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import type { FocusSessionSnapshot } from "../types/models";
import { useAiWorkbenchHandoff } from "../store/aiWorkbenchHandoff";
import { useFocusCycleStore } from "../store/focus";
import { FocusRecoveryModal } from "./FocusRecoveryModal";

const mocks = vi.hoisted(() => ({ recover: vi.fn() }));

const snapshot: FocusSessionSnapshot = {
  serverNow: "2026-08-28T10:10:00Z",
  receivedAtMs: Date.now(),
  session: {
    id: "018f0000-0000-7000-8000-000000000901",
    taskId: "task-1",
    taskTitle: "整理交付",
    status: "recovery_pending",
    plannedSeconds: 3000,
    accumulatedSeconds: 600,
    startedAt: "2026-08-28T09:50:00Z",
    endedAt: null,
    lastResumedAt: "2026-08-28T09:50:00Z",
    lastHeartbeatAt: "2026-08-28T10:05:00Z",
    endReason: null,
    version: 4,
    createdAt: "2026-08-28T09:50:00Z",
    updatedAt: "2026-08-28T10:10:00Z",
  },
};

vi.mock("../api/hooks", () => ({
  useActiveFocusSessionQuery: () => ({ data: snapshot }),
  useRecoverFocusSession: () => ({
    mutate: mocks.recover,
    isPending: false,
    isError: false,
    error: null,
  }),
}));

beforeEach(() => {
  vi.clearAllMocks();
  useFocusCycleStore.getState().resetCycle();
  const handoff = useAiWorkbenchHandoff.getState();
  if (handoff.pending) handoff.dismiss(handoff.pending.requestId);
  if (handoff.pendingIssue)
    handoff.dismissIssue(handoff.pendingIssue.requestId);
});
afterEach(cleanup);

function renderRecovery(path = "/today") {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <FocusRecoveryModal />
    </MemoryRouter>,
  );
}

describe("FocusRecoveryModal", () => {
  it("requires one of the three explicit recovery decisions", () => {
    renderRecovery();

    expect(screen.getByRole("dialog", { name: "恢复上次专注" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "关闭" })).toBeNull();
    expect(screen.queryByRole("button", { name: "关闭弹窗" })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: /按最后心跳恢复/ }));
    expect(mocks.recover).toHaveBeenLastCalledWith({
      id: snapshot.session!.id,
      action: "exclude_gap_resume",
      expectedVersion: 4,
    });

    fireEvent.click(screen.getByRole("button", { name: /计入中断间隔并恢复/ }));
    expect(mocks.recover).toHaveBeenLastCalledWith({
      id: snapshot.session!.id,
      action: "include_gap_resume",
      expectedVersion: 4,
    });

    fireEvent.click(screen.getByRole("button", { name: /结束为中断/ }));
    expect(mocks.recover).toHaveBeenLastCalledWith({
      id: snapshot.session!.id,
      action: "interrupt",
      expectedVersion: 4,
    });
  });
  it("stages a focus recovery handoff without recovering or resetting the cycle", () => {
    useFocusCycleStore
      .getState()
      .beginWork("task-1", 3, "整理交付", snapshot.session!.id);
    const before = useFocusCycleStore.getState();
    renderRecovery();
    fireEvent.click(screen.getByRole("button", { name: "在智能体中处理" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(mocks.recover).not.toHaveBeenCalled();
    expect(useFocusCycleStore.getState()).toBe(before);
    const pendingIssue = useAiWorkbenchHandoff.getState().pendingIssue;
    expect(pendingIssue).toEqual(
      expect.objectContaining({
        label: "专注恢复",
        route: "/focus",
        routeLabel: "打开专注页",
        scopes: ["work", "actions"],
      }),
    );
    expect(pendingIssue?.prompt).toContain(snapshot.session!.id);
    expect(pendingIssue?.prompt).toContain("workspace_focus");
    expect(pendingIssue?.prompt).toContain("focus.recover");
    expect(pendingIssue?.prompt).toContain(
      "必须明确让我在确认卡里额外勾选同意",
    );
    expect(pendingIssue?.prompt).toContain("不要自动恢复");
  });
  it("does not block AI chat while recovery remains pending", () => {
    renderRecovery("/ai");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(mocks.recover).not.toHaveBeenCalled();
  });
});
