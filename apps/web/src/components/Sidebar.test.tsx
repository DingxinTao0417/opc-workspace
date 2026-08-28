import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import { Sidebar } from "./Sidebar";

vi.mock("../api/hooks", () => ({
  useInboxStatsQuery: () => ({
    data: {
      serverNow: "2026-08-28T10:00:00Z",
      pending: 12,
      unread: 7,
      tracking: 4,
      blocked: 1,
      waitingReview: 2,
    },
  }),
  useTasksQuery: () => ({ data: [] }),
}));

vi.mock("../store/settings", () => ({
  useSettingsStore: (selector: (state: unknown) => unknown) =>
    selector({ displayName: "OPC", avatarDataUrl: "", preview: null }),
}));

vi.mock("../store/ui", () => ({
  useUiStore: (selector: (state: unknown) => unknown) =>
    selector({ setCommandPaletteOpen: vi.fn(), setSettingsOpen: vi.fn() }),
}));

describe("Sidebar", () => {
  afterEach(cleanup);

  it("shows the current actionable Inbox count", () => {
    render(
      <MemoryRouter>
        <Sidebar />
      </MemoryRouter>,
    );

    expect(screen.getByLabelText("12 项待处理")).toHaveTextContent("12");
  });
});
