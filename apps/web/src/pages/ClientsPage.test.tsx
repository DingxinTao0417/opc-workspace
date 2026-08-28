import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Client } from "../types/models";
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
  createdAt: "2026-08-20T00:00:00Z",
  updatedAt: "2026-08-27T00:00:00Z",
};

const hooks = vi.hoisted(() => ({
  clients: vi.fn(),
  mutation: { error: null, isPending: false, mutate: vi.fn(), reset: vi.fn() },
}));

vi.mock("../api/hooks", () => ({
  useClientsQuery: hooks.clients,
  useCreateClient: () => hooks.mutation,
  useUpdateClient: () => hooks.mutation,
}));

describe("ClientsPage", () => {
  beforeEach(() => {
    hooks.clients.mockReturnValue({
      data: { items: [client], meta: { page: 1, pageSize: 20, total: 1 } },
      isError: false,
      isFetching: false,
      isPending: false,
      isSuccess: true,
      refetch: vi.fn(),
    });
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
    expect(screen.getByText("v0.4 后可用")).toBeTruthy();
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
});
