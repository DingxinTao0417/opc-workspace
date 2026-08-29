import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { InboxItemEventsSection } from "./InboxItemEventsSection";

const query = vi.hoisted(() => vi.fn());

vi.mock("../api/hooks", () => ({
  useInboxItemEventsQuery: query,
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("InboxItemEventsSection relation wording", () => {
  it("reads linked task facts from the nested relation snapshot", () => {
    query.mockReturnValue({
      data: {
        pages: [
          {
            items: [
              {
                id: "event-1",
                action: "task_requirement_changed",
                actorId: null,
                actor: null,
                requestId: null,
                previous: null,
                current: {
                  relation: {
                    task_title_snapshot: "整理发布清单",
                    is_required: false,
                  },
                },
                reason: null,
                createdAt: "2026-08-28T10:00:00Z",
              },
            ],
            meta: {
              page: 1,
              pageSize: 20,
              total: 1,
              inboxItemVersion: 3,
            },
          },
        ],
      },
      fetchNextPage: vi.fn(),
      hasNextPage: false,
      isError: false,
      isFetchingNextPage: false,
      isPending: false,
      refetch: vi.fn(),
    });

    render(<InboxItemEventsSection itemId="inbox-1" />);
    expect(screen.getByText("调整任务要求")).toBeInTheDocument();
    expect(screen.getByText("整理发布清单 · 改为可选任务")).toBeInTheDocument();
  });

  it("labels an archived source projection as handled", () => {
    query.mockReturnValue({
      data: {
        pages: [
          {
            items: [
              {
                id: "event-2",
                action: "source_resolved",
                actorId: null,
                actor: null,
                requestId: null,
                previous: null,
                current: { resolution_reason: "客户回访已完成" },
                reason: "客户回访已完成",
                createdAt: "2026-08-28T10:00:00Z",
              },
            ],
            meta: { page: 1, pageSize: 20, total: 1, inboxItemVersion: 3 },
          },
        ],
      },
      fetchNextPage: vi.fn(),
      hasNextPage: false,
      isError: false,
      isFetchingNextPage: false,
      isPending: false,
      refetch: vi.fn(),
    });

    render(<InboxItemEventsSection itemId="inbox-1" />);
    expect(screen.getByText("业务来源已处理")).toBeInTheDocument();
    expect(screen.getByText("原因：客户回访已完成")).toBeInTheDocument();
  });
});
