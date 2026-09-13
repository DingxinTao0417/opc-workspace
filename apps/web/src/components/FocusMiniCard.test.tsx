import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import { FocusMiniCard } from "./FocusMiniCard";

const mocks = vi.hoisted(() => ({
  focus: vi.fn(),
  pause: vi.fn(),
  resume: vi.fn(),
}));

vi.mock("../api/hooks", () => ({
  useActiveFocusSessionQuery: mocks.focus,
  usePauseFocusSession: () => ({ isPending: false, mutate: mocks.pause }),
  useResumeFocusSession: () => ({ isPending: false, mutate: mocks.resume }),
}));

vi.mock("../store/focus", () => ({
  formatFocusTime: (seconds: number) => `${seconds}s`,
  useBreakClock: () => ({ progress: 0, remainingSeconds: 240 }),
  useFocusClock: () => ({ progress: 0.4, remainingSeconds: 900 }),
  useFocusCycleStore: (selector: (state: Record<string, unknown>) => unknown) =>
    selector({
      breakDurationSeconds: 300,
      breakEndsAtMs: 1,
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

afterEach(() => {
  cleanup();
});

beforeEach(() => {
  mocks.pause.mockReset();
  mocks.resume.mockReset();
  mocks.focus.mockReturnValue({
    data: { session: null },
    isError: false,
    isPending: false,
    refetch: vi.fn(),
  });
});

describe("FocusMiniCard", () => {
  it("shows the planned duration and a start link when idle", () => {
    render(
      <MemoryRouter>
        <FocusMiniCard />
      </MemoryRouter>,
    );

    expect(screen.getByText("1500s")).toBeInTheDocument();
    expect(screen.getByText("待开始")).toBeInTheDocument();
    const start = screen.getByRole("link", { name: "选择任务并开始" });
    expect(start).toHaveAttribute("href", "/focus");
  });

  it("shows remaining time and pauses a running session", () => {
    mocks.focus.mockReturnValue({
      data: {
        session: {
          id: "session-1",
          version: 2,
          status: "active",
          plannedSeconds: 1500,
          taskTitle: "写文档",
        },
      },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });

    render(
      <MemoryRouter>
        <FocusMiniCard />
      </MemoryRouter>,
    );

    expect(screen.getByText("900s")).toBeInTheDocument();
    expect(screen.getByText("进行中")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "暂停计时" }));
    expect(mocks.pause).toHaveBeenCalledWith({
      id: "session-1",
      expectedVersion: 2,
    });
  });

  it("resumes a paused session", () => {
    mocks.focus.mockReturnValue({
      data: {
        session: {
          id: "session-1",
          version: 3,
          status: "paused",
          plannedSeconds: 1500,
          taskTitle: "写文档",
        },
      },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });

    render(
      <MemoryRouter>
        <FocusMiniCard />
      </MemoryRouter>,
    );

    expect(screen.getByText("已暂停")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "继续专注" }));
    expect(mocks.resume).toHaveBeenCalledWith({
      id: "session-1",
      expectedVersion: 3,
    });
  });

  it("offers a retry action when the session read fails", () => {
    const refetch = vi.fn();
    mocks.focus.mockReturnValue({
      data: undefined,
      isError: true,
      isPending: false,
      refetch,
    });

    render(
      <MemoryRouter>
        <FocusMiniCard />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "重试读取专注会话" }));
    expect(refetch).toHaveBeenCalledTimes(1);
  });
});
