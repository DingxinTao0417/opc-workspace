import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import { TodayPage } from "./TodayPage";

const mocks = vi.hoisted(() => ({
  inbox: vi.fn(),
  stats: vi.fn(),
  tasks: vi.fn(),
  setNewTaskOpen: vi.fn(),
}));

vi.mock("../api/hooks", () => ({
  useInboxStatsQuery: mocks.inbox,
  useTodayStatsQuery: mocks.stats,
  useTasksQuery: mocks.tasks,
}));

vi.mock("../store/ui", () => ({
  useUiStore: (selector: (state: unknown) => unknown) =>
    selector({ setNewTaskOpen: mocks.setNewTaskOpen }),
}));

describe("TodayPage Inbox overview", () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("shows derived Inbox counts with risk deep links", () => {
    mocks.tasks.mockReturnValue({
      data: [],
      isError: false,
      isPending: false,
      isSuccess: true,
      refetch: vi.fn(),
    });
    mocks.stats.mockReturnValue({
      data: {
        date: "2026-08-28",
        tasks: {
          total: 0,
          completed: 0,
          remaining: 0,
          overdue: 0,
          dueSoon: 0,
          estimatedMinutes: 0,
          actualMinutes: 0,
        },
        focus: { sessions: 0, seconds: 0, minutes: 0 },
      },
      isError: false,
      refetch: vi.fn(),
    });
    mocks.inbox.mockReturnValue({
      data: {
        serverNow: "2026-08-28T10:00:00Z",
        pending: 8,
        unread: 5,
        tracking: 4,
        waitingReview: 2,
        blocked: 1,
      },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });

    render(
      <MemoryRouter>
        <TodayPage />
      </MemoryRouter>,
    );

    expect(screen.getByText("5 未读")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /待处理 8/ })).toHaveAttribute(
      "href",
      "/inbox",
    );
    expect(screen.getByRole("link", { name: /跟进中 4/ })).toHaveAttribute(
      "href",
      "/inbox?risk=tracking",
    );
    expect(screen.getByRole("link", { name: /待验收 2/ })).toHaveAttribute(
      "href",
      "/inbox?risk=waiting_review",
    );
    expect(screen.getByRole("link", { name: /有阻塞 1/ })).toHaveAttribute(
      "href",
      "/inbox?risk=blocked",
    );
  });
});
