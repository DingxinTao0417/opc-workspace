import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { FocusSessionSnapshot } from "../types/models";
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
});

describe("FocusRecoveryModal", () => {
  it("requires one of the three explicit recovery decisions", () => {
    render(<FocusRecoveryModal />);

    expect(screen.getByRole("dialog", { name: "恢复上次专注" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "关闭" })).toBeNull();
    expect(screen.queryByRole("button", { name: "关闭弹窗" })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: /按最后心跳恢复/ }));
    expect(mocks.recover).toHaveBeenLastCalledWith(
      {
        id: snapshot.session!.id,
        action: "exclude_gap_resume",
        expectedVersion: 4,
      },
      expect.any(Object),
    );

    fireEvent.click(screen.getByRole("button", { name: /计入中断间隔并恢复/ }));
    expect(mocks.recover).toHaveBeenLastCalledWith(
      {
        id: snapshot.session!.id,
        action: "include_gap_resume",
        expectedVersion: 4,
      },
      expect.any(Object),
    );

    fireEvent.click(screen.getByRole("button", { name: /结束为中断/ }));
    expect(mocks.recover).toHaveBeenLastCalledWith(
      {
        id: snapshot.session!.id,
        action: "interrupt",
        expectedVersion: 4,
      },
      expect.any(Object),
    );
  });
});
