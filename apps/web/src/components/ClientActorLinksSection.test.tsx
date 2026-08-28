import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Actor, ClientActorLink } from "../types/models";
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

const state = vi.hoisted(() => ({
  activeItems: [] as ClientActorLink[],
  historyItems: [] as ClientActorLink[],
  create: { error: null, isPending: false, mutateAsync: vi.fn() },
  remove: { error: null, isPending: false, mutateAsync: vi.fn() },
}));

vi.mock("../api/hooks", () => ({
  useClientActorLinksQuery: (
    _id: string,
    input: { includeUnlinked?: boolean },
  ) => {
    const items = input.includeUnlinked
      ? state.historyItems
      : state.activeItems;
    return {
      data: {
        items,
        meta: { page: 1, pageSize: 20, total: items.length, clientVersion: 4 },
      },
      isError: false,
      isPending: false,
      isSuccess: true,
      refetch: vi.fn(),
    };
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
    state.activeItems = [];
    state.historyItems = [];
    state.create.mutateAsync.mockReset().mockResolvedValue(activeLink);
    state.remove.mutateAsync.mockReset().mockResolvedValue({
      ...activeLink,
      unlinkedAt: "2026-08-28T09:00:00Z",
      unlinkedBy: { id: "owner-1", type: "owner", displayName: "Owner" },
      unlinkReason: "联系人已变更",
      clientVersion: 5,
    });
  });

  afterEach(cleanup);

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
    state.activeItems = [activeLink];
    state.historyItems = [
      {
        ...activeLink,
        id: "link-old",
        unlinkedAt: "2026-08-27T09:00:00Z",
        unlinkedBy: { id: "owner-1", type: "owner", displayName: "Owner" },
        unlinkReason: "联系人已变更",
      },
    ];
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
    expect(screen.getByText("联系人已变更")).toBeTruthy();
  });
});
