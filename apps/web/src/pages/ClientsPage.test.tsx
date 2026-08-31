import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Client, ClientListParams } from "../types/models";
import { ClientsPage } from "./ClientsPage";

const client: Client = {
  id: "client-1",
  name: "星河工作室",
  contactName: "陶先生",
  email: "hello@example.com",
  phone: "13800000000",
  notes: null,
  status: "lead",
  version: 3,
  projectCount: 2,
  latestActivityAt: null,
  createdAt: "2026-08-20T00:00:00Z",
  updatedAt: "2026-08-27T00:00:00Z",
};

const hooks = vi.hoisted(() => ({
  clients: vi.fn(),
  mutation: { error: null, isPending: false, mutate: vi.fn(), reset: vi.fn() },
  responsePage: null as number | null,
  total: 1,
}));

vi.mock("../api/hooks", () => ({
  useClientsQuery: hooks.clients,
  useCreateClient: () => hooks.mutation,
  useUpdateClient: () => hooks.mutation,
}));

describe("ClientsPage", () => {
  beforeEach(() => {
    hooks.responsePage = null;
    hooks.total = 1;
    hooks.clients.mockImplementation((input: ClientListParams) => ({
      data: {
        items: [client],
        meta: {
          page: hooks.responsePage ?? input.page ?? 1,
          pageSize: 20,
          total: hooks.total,
        },
      },
      isError: false,
      isFetching: false,
      isPending: false,
      isPlaceholderData: false,
      isSuccess: true,
      refetch: vi.fn(),
    }));
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("renders the prototype table hierarchy without invented financial or activity facts", () => {
    render(
      <MemoryRouter>
        <ClientsPage />
      </MemoryRouter>,
    );

    expect(screen.getByRole("columnheader", { name: "客户" })).toBeTruthy();
    expect(screen.getByRole("columnheader", { name: "累计收入" })).toBeTruthy();
    expect(screen.getByText("待客户聚合")).toBeTruthy();
    expect(screen.getByText("暂无本地活动")).toBeTruthy();
    expect(screen.getAllByText("潜在客户")).toHaveLength(2);
    expect(screen.getByRole("link", { name: "星河工作室" })).toHaveAttribute(
      "href",
      "/clients/client-1",
    );
  });

  it("passes search and status filters to the server query", () => {
    render(
      <MemoryRouter>
        <ClientsPage />
      </MemoryRouter>,
    );

    fireEvent.change(screen.getByLabelText("搜索客户"), {
      target: { value: "星河" },
    });
    fireEvent.change(screen.getByLabelText("客户状态"), {
      target: { value: "inactive" },
    });

    expect(hooks.clients).toHaveBeenLastCalledWith(
      expect.objectContaining({
        page: 1,
        q: "星河",
        status: "inactive",
      }),
    );
  });

  it("settles directly on the last valid client page after totals shrink", async () => {
    hooks.total = 41;
    const view = render(
      <MemoryRouter>
        <ClientsPage />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect(hooks.clients).toHaveBeenLastCalledWith(
      expect.objectContaining({ page: 3 }),
    );

    hooks.total = 0;
    view.rerender(
      <MemoryRouter>
        <ClientsPage />
      </MemoryRouter>,
    );

    await waitFor(() =>
      expect(hooks.clients).toHaveBeenLastCalledWith(
        expect.objectContaining({ page: 1 }),
      ),
    );
  });
});
