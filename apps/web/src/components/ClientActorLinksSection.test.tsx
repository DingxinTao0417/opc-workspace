import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type {
  Actor,
  ClientActorLink,
  ClientActorLinkListResult,
} from "../types/models";
import { ApiError } from "../api/client";
import { ClientActorLinksSection } from "./ClientActorLinksSection";

const person: Actor = {
  id: "actor-1",
  type: "person",
  displayName: "陶先生",
  status: "active",
  isBuiltin: false,
  notes: "",
  metadata: {},
  version: 2,
  createdAt: "2026-08-28T08:00:00Z",
  updatedAt: "2026-08-28T08:00:00Z",
};

const activeLink: ClientActorLink = {
  id: "link-1",
  clientId: "client-1",
  role: "contact",
  actor: {
    id: person.id,
    type: person.type,
    displayName: person.displayName,
    status: person.status,
    version: person.version,
  },
  linkedBy: { id: "owner-1", type: "owner", displayName: "Owner" },
  linkedAt: "2026-08-28T08:00:00Z",
  unlinkedAt: null,
  unlinkedBy: null,
  unlinkReason: null,
  clientVersion: 4,
};

interface LinkQueryState {
  data?: ClientActorLinkListResult;
  isError: boolean;
  isFetching: boolean;
  isPending: boolean;
  isPlaceholderData: boolean;
  refetch: ReturnType<typeof vi.fn>;
}

interface LinkQueryInput {
  state: "active" | "unlinked";
  page: number;
  pageSize: number;
}

const state = vi.hoisted(() => ({
  linkQueries: new Map<string, unknown>(),
  queryCalls: [] as Array<{
    clientId: string;
    input: { state: "active" | "unlinked"; page: number; pageSize: number };
    enabled: boolean;
  }>,
  create: {
    error: null as unknown,
    isPending: false,
    mutateAsync: vi.fn(),
    variables: undefined as { clientId: string } | undefined,
  },
  remove: {
    error: null as unknown,
    isPending: false,
    mutateAsync: vi.fn(),
    variables: undefined as { clientId: string } | undefined,
  },
}));

function queryKey(clientId: string, queryState: string, page: number) {
  return `${clientId}:${queryState}:${page}`;
}

function linkQuery({
  items = [],
  page = 1,
  pageSize = 6,
  total = items.length,
  clientVersion = 4,
  isError = false,
  isFetching = false,
  isPending = false,
  isPlaceholderData = false,
  includeData = true,
}: {
  items?: ClientActorLink[];
  page?: number;
  pageSize?: number;
  total?: number;
  clientVersion?: number;
  isError?: boolean;
  isFetching?: boolean;
  isPending?: boolean;
  isPlaceholderData?: boolean;
  includeData?: boolean;
} = {}): LinkQueryState {
  return {
    data: includeData
      ? { items, meta: { page, pageSize, total, clientVersion } }
      : undefined,
    isError,
    isFetching,
    isPending,
    isPlaceholderData,
    refetch: vi.fn(),
  };
}

function setLinkQuery(
  clientId: string,
  queryState: "active" | "unlinked",
  page: number,
  query: LinkQueryState,
) {
  state.linkQueries.set(queryKey(clientId, queryState, page), query);
}

function historicalLink(
  id: string,
  displayName: string,
  clientId = "client-1",
): ClientActorLink {
  return {
    ...activeLink,
    id,
    clientId,
    actor: { ...activeLink.actor, id: `actor-${id}`, displayName },
    unlinkedAt: "2026-08-28T09:00:00Z",
    unlinkedBy: { id: "owner-1", type: "owner", displayName: "Owner" },
    unlinkReason: `解除 ${displayName}`,
  };
}

vi.mock("../api/hooks", () => ({
  useClientActorLinksQuery: (
    clientId: string,
    input: LinkQueryInput,
    enabled = true,
  ) => {
    state.queryCalls.push({ clientId, input: { ...input }, enabled });
    const configured = state.linkQueries.get(
      queryKey(clientId, input.state, input.page),
    ) as LinkQueryState | undefined;
    if (configured) return configured;
    return enabled
      ? linkQuery({ page: input.page, pageSize: input.pageSize })
      : linkQuery({
          page: input.page,
          pageSize: input.pageSize,
          includeData: false,
          isPending: true,
        });
  },
  useClientActorOptionsQuery: () => ({
    data: [person],
    isError: false,
    isPending: false,
    refetch: vi.fn(),
  }),
  useCreateClientActorLink: () => state.create,
  useDeleteClientActorLink: () => state.remove,
}));

describe("ClientActorLinksSection", () => {
  beforeEach(() => {
    state.linkQueries.clear();
    state.queryCalls = [];
    setLinkQuery(
      "client-1",
      "active",
      1,
      linkQuery({ pageSize: 1, clientVersion: 4 }),
    );
    state.create.mutateAsync.mockReset().mockResolvedValue(activeLink);
    state.create.error = null;
    state.create.variables = undefined;
    state.remove.mutateAsync.mockReset().mockResolvedValue({
      ...activeLink,
      unlinkedAt: "2026-08-28T09:00:00Z",
      unlinkedBy: { id: "owner-1", type: "owner", displayName: "Owner" },
      unlinkReason: "联系人已变更",
      clientVersion: 5,
    });
    state.remove.error = null;
    state.remove.variables = undefined;
  });

  afterEach(cleanup);

  it("queries one active link and defers the six-row history query until expanded", () => {
    render(
      <ClientActorLinksSection
        clientId="client-1"
        clientVersion={3}
        contactName="陶先生"
      />,
    );

    expect(state.queryCalls).toContainEqual({
      clientId: "client-1",
      input: { state: "active", page: 1, pageSize: 1 },
      enabled: true,
    });
    expect(state.queryCalls).toContainEqual({
      clientId: "client-1",
      input: { state: "unlinked", page: 1, pageSize: 6 },
      enabled: false,
    });

    fireEvent.click(screen.getByRole("button", { name: "关联历史" }));
    expect(state.queryCalls).toContainEqual({
      clientId: "client-1",
      input: { state: "unlinked", page: 1, pageSize: 6 },
      enabled: true,
    });
  });

  it("links an existing active person with the latest Client version", async () => {
    render(
      <ClientActorLinksSection
        clientId="client-1"
        clientVersion={3}
        contactName="陶先生"
      />,
    );
    fireEvent.change(screen.getByLabelText("本地人员"), {
      target: { value: "actor-1" },
    });
    fireEvent.click(screen.getByRole("button", { name: "确认关联" }));
    await waitFor(() =>
      expect(state.create.mutateAsync).toHaveBeenCalledWith({
        clientId: "client-1",
        input: { actorId: "actor-1", expectedVersion: 4 },
      }),
    );
  });

  it("prefills and atomically creates a new local person before linking", async () => {
    render(
      <ClientActorLinksSection
        clientId="client-1"
        clientVersion={4}
        contactName="陶先生"
      />,
    );
    fireEvent.click(screen.getByRole("tab", { name: /新建并关联/ }));
    expect(screen.getByDisplayValue("陶先生")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("本地备注"), {
      target: { value: "  客户联系人  " },
    });
    fireEvent.click(screen.getByRole("button", { name: "确认关联" }));
    await waitFor(() =>
      expect(state.create.mutateAsync).toHaveBeenCalledWith({
        clientId: "client-1",
        input: {
          createPerson: { displayName: "陶先生", notes: "客户联系人" },
          expectedVersion: 4,
        },
      }),
    );
  });

  it("requires an audit reason for unlink and exposes immutable history", async () => {
    setLinkQuery(
      "client-1",
      "active",
      1,
      linkQuery({ items: [activeLink], pageSize: 1, clientVersion: 4 }),
    );
    setLinkQuery(
      "client-1",
      "unlinked",
      1,
      linkQuery({ items: [historicalLink("link-old", "旧联系人")] }),
    );
    render(
      <ClientActorLinksSection
        clientId="client-1"
        clientVersion={3}
        contactName="陶先生"
      />,
    );
    const unlinkButton = screen.getByRole("button", { name: "解除关联" });
    expect(unlinkButton).toBeDisabled();
    fireEvent.change(screen.getByLabelText("解除原因"), {
      target: { value: "  联系人已变更  " },
    });
    fireEvent.click(unlinkButton);
    await waitFor(() =>
      expect(state.remove.mutateAsync).toHaveBeenCalledWith({
        id: "link-1",
        clientId: "client-1",
        input: { reason: "联系人已变更", expectedVersion: 4 },
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "关联历史" }));
    expect(screen.getByText("解除 旧联系人")).toBeTruthy();
  });

  it("paginates unlinked history with the server total", () => {
    const firstPage = Array.from({ length: 6 }, (_, index) =>
      historicalLink(`old-${index + 1}`, `历史 ${index + 1}`),
    );
    setLinkQuery(
      "client-1",
      "unlinked",
      1,
      linkQuery({ items: firstPage, total: 13 }),
    );
    setLinkQuery(
      "client-1",
      "unlinked",
      2,
      linkQuery({
        items: [historicalLink("old-7", "历史 7")],
        page: 2,
        total: 13,
      }),
    );
    render(
      <ClientActorLinksSection
        clientId="client-1"
        clientVersion={3}
        contactName="陶先生"
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "关联历史" }));
    expect(screen.getByText("第 1 / 3 页 · 共 13 条")).toBeTruthy();
    expect(screen.getByRole("button", { name: "上一页" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect(screen.getByText("历史 7")).toBeTruthy();
    expect(screen.queryByText("历史 1")).not.toBeInTheDocument();
    expect(screen.getByText("第 2 / 3 页 · 共 13 条")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "上一页" }));
    expect(screen.getByText("历史 1")).toBeTruthy();
  });

  it("does not render the previous page as the requested page while placeholder data is active", () => {
    const previous = historicalLink("old-page", "上一页旧联系人");
    const firstPageQuery = linkQuery({ items: [previous], total: 7 });
    setLinkQuery("client-1", "unlinked", 1, firstPageQuery);
    setLinkQuery("client-1", "unlinked", 2, {
      ...firstPageQuery,
      isFetching: true,
      isPlaceholderData: true,
    });
    render(
      <ClientActorLinksSection
        clientId="client-1"
        clientVersion={3}
        contactName="陶先生"
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "关联历史" }));
    expect(screen.getByText("上一页旧联系人")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect(screen.queryByText("上一页旧联系人")).not.toBeInTheDocument();
    expect(screen.getByText("正在读取第 2 页关联历史…")).toBeTruthy();
    expect(
      screen.queryByLabelText("联系人关联历史分页"),
    ).not.toBeInTheDocument();
  });

  it("retains the latest confirmed Client version while the next history page is placeholder data", async () => {
    const confirmedHistory = linkQuery({
      items: [historicalLink("confirmed", "已确认联系人")],
      total: 7,
      clientVersion: 9,
    });
    setLinkQuery("client-1", "unlinked", 1, confirmedHistory);
    setLinkQuery("client-1", "unlinked", 2, {
      ...confirmedHistory,
      isFetching: true,
      isPlaceholderData: true,
    });
    render(
      <ClientActorLinksSection
        clientId="client-1"
        clientVersion={3}
        contactName="陶先生"
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "关联历史" }));
    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    fireEvent.change(screen.getByLabelText("本地人员"), {
      target: { value: "actor-1" },
    });
    fireEvent.click(screen.getByRole("button", { name: "确认关联" }));

    await waitFor(() =>
      expect(state.create.mutateAsync).toHaveBeenCalledWith({
        clientId: "client-1",
        input: { actorId: "actor-1", expectedVersion: 9 },
      }),
    );
  });

  it("does not carry a previous client's placeholder version into a new relationship", async () => {
    const previousClientHistory = linkQuery({
      items: [historicalLink("confirmed", "客户一历史")],
      total: 1,
      clientVersion: 9,
    });
    setLinkQuery("client-1", "unlinked", 1, previousClientHistory);
    setLinkQuery(
      "client-2",
      "active",
      1,
      linkQuery({ pageSize: 1, clientVersion: 2 }),
    );
    setLinkQuery("client-2", "unlinked", 1, {
      ...previousClientHistory,
      isFetching: true,
      isPlaceholderData: true,
    });
    const view = render(
      <ClientActorLinksSection
        clientId="client-1"
        clientVersion={3}
        contactName="陶先生"
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "关联历史" }));
    expect(screen.getByText("客户一历史")).toBeTruthy();

    view.rerender(
      <ClientActorLinksSection
        clientId="client-2"
        clientVersion={2}
        contactName="新联系人"
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "关联历史" }));
    fireEvent.change(screen.getByLabelText("本地人员"), {
      target: { value: "actor-1" },
    });
    fireEvent.click(screen.getByRole("button", { name: "确认关联" }));

    await waitFor(() =>
      expect(state.create.mutateAsync).toHaveBeenCalledWith({
        clientId: "client-2",
        input: { actorId: "actor-1", expectedVersion: 2 },
      }),
    );
  });

  it("renders loading and empty history states without eager history data", () => {
    setLinkQuery(
      "client-1",
      "unlinked",
      1,
      linkQuery({ includeData: false, isPending: true }),
    );
    const view = render(
      <ClientActorLinksSection
        clientId="client-1"
        clientVersion={3}
        contactName="陶先生"
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "关联历史" }));
    expect(screen.getByText("正在读取第 1 页关联历史…")).toBeTruthy();

    setLinkQuery("client-1", "unlinked", 1, linkQuery({ total: 0 }));
    view.rerender(
      <ClientActorLinksSection
        clientId="client-1"
        clientVersion={3}
        contactName="陶先生"
      />,
    );
    expect(screen.getByText("暂无已解除的联系人关联。")).toBeTruthy();
    expect(screen.getByText("第 1 / 1 页 · 共 0 条")).toBeTruthy();
  });

  it("distinguishes an initial history error from a cached refresh error", () => {
    setLinkQuery(
      "client-1",
      "unlinked",
      1,
      linkQuery({ includeData: false, isError: true }),
    );
    const view = render(
      <ClientActorLinksSection
        clientId="client-1"
        clientVersion={3}
        contactName="陶先生"
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "关联历史" }));
    expect(screen.getByText("无法读取联系人关联历史。")).toBeTruthy();
    expect(
      screen.queryByText("联系人关联历史刷新失败，仍显示上次结果。"),
    ).not.toBeInTheDocument();

    setLinkQuery(
      "client-1",
      "unlinked",
      1,
      linkQuery({
        items: [historicalLink("cached", "缓存联系人")],
        total: 1,
        isError: true,
      }),
    );
    view.rerender(
      <ClientActorLinksSection
        clientId="client-1"
        clientVersion={3}
        contactName="陶先生"
      />,
    );
    expect(screen.getByText("缓存联系人")).toBeTruthy();
    expect(
      screen.getByText("联系人关联历史刷新失败，仍显示上次结果。"),
    ).toBeTruthy();
    expect(
      screen.queryByText("无法读取联系人关联历史。"),
    ).not.toBeInTheDocument();
  });

  it("clamps only after a fresh settled response reports fewer pages", async () => {
    setLinkQuery(
      "client-1",
      "unlinked",
      1,
      linkQuery({ items: [historicalLink("first", "第一页")], total: 12 }),
    );
    setLinkQuery(
      "client-1",
      "unlinked",
      2,
      linkQuery({
        items: [historicalLink("second", "第二页缓存")],
        page: 2,
        total: 12,
      }),
    );
    const view = render(
      <ClientActorLinksSection
        clientId="client-1"
        clientVersion={3}
        contactName="陶先生"
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "关联历史" }));
    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect(screen.getByText("第二页缓存")).toBeTruthy();

    setLinkQuery(
      "client-1",
      "unlinked",
      2,
      linkQuery({
        items: [historicalLink("second", "第二页缓存")],
        page: 2,
        total: 0,
        isError: true,
      }),
    );
    state.queryCalls = [];
    view.rerender(
      <ClientActorLinksSection
        clientId="client-1"
        clientVersion={3}
        contactName="陶先生"
      />,
    );
    expect(state.queryCalls.at(-1)).toMatchObject({
      input: { state: "unlinked", page: 2 },
      enabled: true,
    });

    setLinkQuery("client-1", "unlinked", 2, linkQuery({ page: 2, total: 0 }));
    state.queryCalls = [];
    view.rerender(
      <ClientActorLinksSection
        clientId="client-1"
        clientVersion={3}
        contactName="陶先生"
      />,
    );
    await waitFor(() =>
      expect(state.queryCalls).toContainEqual({
        clientId: "client-1",
        input: { state: "unlinked", page: 1, pageSize: 6 },
        enabled: true,
      }),
    );
  });

  it("collapses history and resets its page when the client changes", async () => {
    setLinkQuery(
      "client-1",
      "unlinked",
      1,
      linkQuery({ items: [historicalLink("first", "客户一第一页")], total: 8 }),
    );
    setLinkQuery(
      "client-1",
      "unlinked",
      2,
      linkQuery({
        items: [historicalLink("second", "客户一第二页")],
        page: 2,
        total: 8,
      }),
    );
    setLinkQuery(
      "client-2",
      "active",
      1,
      linkQuery({ pageSize: 1, clientVersion: 2 }),
    );
    setLinkQuery(
      "client-2",
      "unlinked",
      1,
      linkQuery({
        items: [historicalLink("client-2-old", "客户二历史", "client-2")],
        total: 1,
        clientVersion: 2,
      }),
    );
    const view = render(
      <ClientActorLinksSection
        clientId="client-1"
        clientVersion={3}
        contactName="陶先生"
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "关联历史" }));
    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect(screen.getByText("客户一第二页")).toBeTruthy();
    fireEvent.click(screen.getByRole("tab", { name: /新建并关联/ }));
    fireEvent.change(screen.getByLabelText("显示名称"), {
      target: { value: "客户一草稿" },
    });
    fireEvent.change(screen.getByLabelText("本地备注"), {
      target: { value: "不应带到下一个客户" },
    });

    state.queryCalls = [];
    view.rerender(
      <ClientActorLinksSection
        clientId="client-2"
        clientVersion={2}
        contactName="新联系人"
      />,
    );
    expect(screen.getByRole("button", { name: "关联历史" })).toBeTruthy();
    expect(screen.queryByText("客户一第二页")).not.toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /关联现有人员/ })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    await waitFor(() =>
      expect(state.queryCalls).toContainEqual({
        clientId: "client-2",
        input: { state: "unlinked", page: 1, pageSize: 6 },
        enabled: false,
      }),
    );

    fireEvent.click(screen.getByRole("button", { name: "关联历史" }));
    expect(screen.getByText("客户二历史")).toBeTruthy();
    expect(screen.getByText("第 1 / 1 页 · 共 1 条")).toBeTruthy();
    fireEvent.click(screen.getByRole("tab", { name: /新建并关联/ }));
    expect(screen.getByDisplayValue("新联系人")).toBeTruthy();
    expect(screen.getByLabelText("本地备注")).toHaveValue("");
  });

  it("does not expose a previous client's mutation error after navigation", () => {
    state.create.error = new ApiError("客户一关联失败", {
      code: "CLIENT_LINK_FAILED",
      requestId: "request-client-1",
    });
    state.create.variables = { clientId: "client-1" };
    setLinkQuery(
      "client-2",
      "active",
      1,
      linkQuery({ pageSize: 1, clientVersion: 2 }),
    );
    const view = render(
      <ClientActorLinksSection
        clientId="client-1"
        clientVersion={3}
        contactName="陶先生"
      />,
    );
    expect(screen.getByText(/客户一关联失败/)).toBeTruthy();

    view.rerender(
      <ClientActorLinksSection
        clientId="client-2"
        clientVersion={2}
        contactName="新联系人"
      />,
    );
    expect(screen.queryByText(/客户一关联失败/)).not.toBeInTheDocument();
    expect(screen.queryByText(/request-client-1/)).not.toBeInTheDocument();
  });
});
