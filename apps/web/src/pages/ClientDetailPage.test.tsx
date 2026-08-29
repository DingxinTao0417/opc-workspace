import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Client, Project } from "../types/models";
import { ClientDetailPage } from "./ClientDetailPage";

const activeClient: Client = {
  id: "client-1",
  name: "星河工作室",
  contactName: "陶先生",
  email: "hello@example.com",
  phone: "13800000000",
  notes: "品牌设计客户",
  status: "active",
  version: 3,
  projectCount: 1,
  latestActivityAt: null,
  createdAt: "2026-08-20T00:00:00Z",
  updatedAt: "2026-08-27T00:00:00Z",
};

const project: Project = {
  id: "project-1",
  name: "品牌官网改版",
  description: "完成网站交付",
  clientId: activeClient.id,
  clientName: activeClient.name,
  status: "in_progress",
  startDate: null,
  dueDate: null,
  amountMinor: null,
  color: "#6E7BF2",
  version: 2,
  archivedFromStatus: null,
  createdAt: "2026-08-20T00:00:00Z",
  updatedAt: "2026-08-27T00:00:00Z",
  taskSummary: {
    total: 4,
    completed: 2,
    inProgress: 1,
    remaining: 2,
    progressPercent: 50,
    actualMinutes: 120,
  },
  invoiceCount: 0,
  availableActions: ["pause", "complete", "archive"],
};

const state = vi.hoisted(() => ({
  client: null as Client | null,
  projectQueryInput: null as unknown,
  update: { error: null, isPending: false, mutate: vi.fn(), reset: vi.fn() },
  remove: { error: null, isPending: false, mutate: vi.fn() },
  activityMutation: {
    error: null,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
  },
}));

vi.mock("../api/hooks", () => ({
  useClientQuery: () => ({
    data: state.client,
    isError: false,
    isPending: false,
    refetch: vi.fn().mockResolvedValue({ data: state.client }),
  }),
  useProjectsQuery: (input: unknown) => {
    state.projectQueryInput = input;
    return {
      data: { items: [project], meta: { page: 1, pageSize: 8, total: 1 } },
      isError: false,
      isFetching: false,
      isPending: false,
      isSuccess: true,
      refetch: vi.fn(),
    };
  },
  useCreateClient: () => state.update,
  useUpdateClient: () => state.update,
  useDeleteClient: () => state.remove,
  useClientActivitiesQuery: () => ({
    data: {
      items: [],
      meta: { page: 1, pageSize: 6, total: 0, clientVersion: 3 },
    },
    isError: false,
    isFetching: false,
    isPending: false,
    isSuccess: true,
    refetch: vi.fn(),
  }),
  useClientFollowupsQuery: () => ({
    data: {
      items: [],
      meta: { page: 1, pageSize: 6, total: 0 },
    },
    isError: false,
    isFetching: false,
    isPending: false,
    isSuccess: true,
    refetch: vi.fn(),
  }),
  useClientFollowupActorOptionsQuery: () => ({
    data: [],
    isError: false,
    isPending: false,
    refetch: vi.fn(),
  }),
  useCreateClientFollowup: () => state.activityMutation,
  useUpdateClientFollowup: () => state.activityMutation,
  useCompleteClientFollowup: () => state.activityMutation,
  useSkipClientFollowup: () => state.activityMutation,
  useCancelClientFollowup: () => state.activityMutation,
  useRescheduleClientFollowup: () => state.activityMutation,
  useCreateClientActivity: () => state.activityMutation,
  useUpdateClientActivity: () => state.activityMutation,
  useDeleteClientActivity: () => state.activityMutation,
  useClientAttachmentsQuery: () => ({
    data: {
      items: [],
      meta: { page: 1, pageSize: 10, total: 0, clientVersion: 3 },
    },
    isError: false,
    isFetching: false,
    isPending: false,
    isSuccess: true,
    refetch: vi.fn(),
  }),
  useCreateClientAttachment: () => state.activityMutation,
  useDeleteClientAttachment: () => state.activityMutation,
  useDownloadClientAttachment: () => state.activityMutation,
  useClientActorLinksQuery: () => ({
    data: {
      items: [],
      meta: { page: 1, pageSize: 20, total: 0, clientVersion: 3 },
    },
    isError: false,
    isPending: false,
    isSuccess: true,
    refetch: vi.fn(),
  }),
  useClientActorOptionsQuery: () => ({
    data: [],
    isError: false,
    isPending: false,
    refetch: vi.fn(),
  }),
  useCreateClientActorLink: () => ({
    error: null,
    isPending: false,
    mutateAsync: vi.fn(),
  }),
  useDeleteClientActorLink: () => ({
    error: null,
    isPending: false,
    mutateAsync: vi.fn(),
  }),
}));

function renderDetail() {
  return render(
    <MemoryRouter initialEntries={["/clients/client-1"]}>
      <Routes>
        <Route element={<ClientDetailPage />} path="/clients/:clientId" />
      </Routes>
    </MemoryRouter>,
  );
}

describe("ClientDetailPage", () => {
  beforeEach(() => {
    state.client = activeClient;
    state.update.mutate.mockClear();
    state.remove.mutate.mockClear();
  });

  afterEach(cleanup);

  it("shows real related projects, local activity, followup history and attachment entry", () => {
    renderDetail();

    expect(screen.getByRole("link", { name: /品牌官网改版/ })).toHaveAttribute(
      "href",
      "/projects/project-1",
    );
    expect(screen.getByText(/v0.4 交付财务事实后可用/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "记录活动" })).toBeEnabled();
    expect(screen.getByText(/不代表客户回访或其他外部通信/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "添加附件" })).toBeEnabled();
    expect(screen.getByRole("heading", { name: "本地联系人" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "确认关联" })).toBeDisabled();
    expect(screen.getByRole("heading", { name: "客户回访" })).toBeTruthy();
    expect(screen.getByText(/不会发送邮件、短信或其他外部消息/)).toBeTruthy();
    expect(state.projectQueryInput).toEqual(
      expect.objectContaining({
        clientId: activeClient.id,
        includeArchived: true,
      }),
    );
  });

  it("requires the client to be inactive before permanent deletion", () => {
    const view = renderDetail();

    expect(screen.getByRole("button", { name: "先停用客户" })).toBeDisabled();
    expect(screen.getByText(/永久删除前必须先停用客户/)).toBeTruthy();

    state.client = { ...activeClient, status: "inactive", version: 4 };
    view.rerender(
      <MemoryRouter initialEntries={["/clients/client-1"]}>
        <Routes>
          <Route element={<ClientDetailPage />} path="/clients/:clientId" />
        </Routes>
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "永久删除客户" }));
    fireEvent.click(screen.getByRole("button", { name: "确认永久删除" }));

    expect(state.remove.mutate).toHaveBeenCalledWith(
      { id: activeClient.id, expectedVersion: 4 },
      expect.objectContaining({
        onError: expect.any(Function),
        onSuccess: expect.any(Function),
      }),
    );
  });
});
