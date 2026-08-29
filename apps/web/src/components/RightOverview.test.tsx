import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import { RightOverview } from "./RightOverview";

const mocks = vi.hoisted(() => ({
  recent: vi.fn(),
  refetch: vi.fn(),
}));

vi.mock("../api/hooks", () => ({
  useActiveFocusSessionQuery: () => ({
    data: { session: null },
    isError: false,
    isPending: false,
  }),
  usePauseFocusSession: () => ({ isPending: false, mutate: vi.fn() }),
  useResumeFocusSession: () => ({ isPending: false, mutate: vi.fn() }),
  useRecentClientActivitiesQuery: mocks.recent,
}));

vi.mock("../store/focus", () => ({
  formatFocusTime: (seconds: number) => `${seconds}s`,
  useBreakClock: () => ({ progress: 0, remainingSeconds: 0 }),
  useFocusClock: () => ({ progress: 0, remainingSeconds: 1500 }),
  useFocusCycleStore: (selector: (state: Record<string, unknown>) => unknown) =>
    selector({
      breakDurationSeconds: 300,
      breakEndsAtMs: null,
      pauseBreak: vi.fn(),
      phase: "idle",
      resumeBreak: vi.fn(),
      taskTitle: null,
    }),
}));

vi.mock("../store/settings", () => ({
  useSettingsStore: (selector: (state: Record<string, unknown>) => unknown) =>
    selector({ focusMinutes: 25, preview: null }),
}));

afterEach(() => cleanup());

beforeEach(() => {
  mocks.refetch.mockReset();
  mocks.recent.mockReset();
});

describe("RightOverview recent client activities", () => {
  it("shows real local activity facts and links to the owning client", () => {
    mocks.recent.mockReturnValue({
      data: {
        items: [
          {
            id: "activity-1",
            clientId: "client-1",
            clientName: "示例客户",
            clientStatus: "active",
            kind: "meeting",
            title: "复盘会议",
            body: "下一步",
            occurredAt: "2026-08-29T09:00:00Z",
            createdBy: {
              id: "owner-1",
              type: "owner",
              displayName: "Owner",
            },
            sourceType: null,
            sourceId: null,
            version: 1,
            deletedAt: null,
            deletedByActorId: null,
            deleteReason: null,
            createdAt: "2026-08-29T09:00:00Z",
            updatedAt: "2026-08-29T09:00:00Z",
            clientVersion: 2,
          },
        ],
        meta: { page: 1, pageSize: 3, total: 1 },
      },
      isError: false,
      isPending: false,
      refetch: mocks.refetch,
    });

    render(
      <MemoryRouter>
        <RightOverview />
      </MemoryRouter>,
    );

    const link = screen.getByRole("link", {
      name: "查看客户 示例客户：复盘会议",
    });
    expect(link).toHaveAttribute("href", "/clients/client-1");
    expect(screen.getByText("示例客户")).toBeInTheDocument();
    expect(screen.queryByText("暂无客户动态")).not.toBeInTheDocument();
  });

  it("keeps explicit empty and retry states", () => {
    mocks.recent.mockReturnValue({
      data: undefined,
      isError: true,
      isPending: false,
      refetch: mocks.refetch,
    });
    const { rerender } = render(
      <MemoryRouter>
        <RightOverview />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "读取失败，重试" }));
    expect(mocks.refetch).toHaveBeenCalledTimes(1);

    mocks.recent.mockReturnValue({
      data: { items: [], meta: { page: 1, pageSize: 3, total: 0 } },
      isError: false,
      isPending: false,
      refetch: mocks.refetch,
    });
    rerender(
      <MemoryRouter>
        <RightOverview />
      </MemoryRouter>,
    );
    expect(screen.getByText("暂无客户动态")).toBeInTheDocument();
  });
});
