import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../api/client";
import type { Project } from "../types/models";
import { ProjectFormModal } from "./ProjectFormModal";

const mutations = vi.hoisted(() => ({
  create: { error: null, isPending: false, mutate: vi.fn(), reset: vi.fn() },
  update: { error: null, isPending: false, mutate: vi.fn(), reset: vi.fn() },
  clientDetailError: false,
  clients: {
    data: {
      items: [
        { id: "client-active", name: "星河工作室", status: "active" },
        { id: "client-inactive", name: "旧客户", status: "inactive" },
      ],
      meta: { page: 1, pageSize: 20, total: 2 },
    },
    isError: false,
    isFetching: false,
    isPending: false,
    refetch: vi.fn(),
  },
}));

vi.mock("../api/hooks", () => ({
  useCreateProject: () => mutations.create,
  useUpdateProject: () => mutations.update,
  useClientOptionsQuery: () => mutations.clients,
  useClientQuery: (id: string | null) => ({
    data: mutations.clientDetailError
      ? undefined
      : mutations.clients.data.items.find((client) => client.id === id),
    isError: mutations.clientDetailError,
    isPending: false,
  }),
}));

const project: Project = {
  id: "project-1",
  name: "客户门户",
  description: "交付新版门户",
  clientId: null,
  clientName: null,
  status: "planning",
  startDate: "2026-08-28",
  dueDate: "2026-09-30",
  amountMinor: 128000,
  color: "#6E7BF2",
  version: 4,
  archivedFromStatus: null,
  createdAt: "2026-08-27T00:00:00Z",
  updatedAt: "2026-08-27T00:00:00Z",
  taskSummary: {
    total: 0,
    completed: 0,
    inProgress: 0,
    remaining: 0,
    progressPercent: 0,
    actualMinutes: 0,
  },
  invoiceCount: 0,
  availableActions: ["start", "archive"],
};

describe("ProjectFormModal", () => {
  beforeEach(() => {
    mutations.create.mutate.mockClear();
    mutations.update.mutate.mockClear();
    mutations.clients.isError = false;
    mutations.clients.isFetching = false;
    mutations.clients.isPending = false;
    mutations.clientDetailError = false;
    mutations.clients.data = {
      items: [
        { id: "client-active", name: "星河工作室", status: "active" },
        { id: "client-inactive", name: "旧客户", status: "inactive" },
      ],
      meta: { page: 1, pageSize: 20, total: 2 },
    };
  });

  afterEach(cleanup);

  it("converts the displayed yuan amount to integer minor units", () => {
    render(<ProjectFormModal onClose={vi.fn()} open />);

    fireEvent.change(screen.getByLabelText("项目名称"), {
      target: { value: "品牌官网改版" },
    });
    fireEvent.change(screen.getByLabelText("合同金额"), {
      target: { value: "123.45" },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建项目" }));

    expect(mutations.create.mutate).toHaveBeenCalledWith(
      expect.objectContaining({
        name: "品牌官网改版",
        amountMinor: 12345,
        clientId: null,
      }),
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
  });

  it("sends the current version when editing", () => {
    const onVersionConflict = vi.fn();
    render(
      <ProjectFormModal
        onClose={vi.fn()}
        onVersionConflict={onVersionConflict}
        open
        project={project}
      />,
    );

    fireEvent.change(screen.getByLabelText("项目名称"), {
      target: { value: "客户门户二期" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存修改" }));

    expect(mutations.update.mutate).toHaveBeenCalledWith(
      {
        id: project.id,
        input: expect.objectContaining({
          name: "客户门户二期",
          amountMinor: 128000,
          expectedVersion: project.version,
        }),
      },
      expect.objectContaining({
        onError: expect.any(Function),
        onSuccess: expect.any(Function),
      }),
    );

    const options = mutations.update.mutate.mock.calls[0][1];
    options.onError(
      new ApiError("Project has changed", { code: "VERSION_CONFLICT" }),
    );
    expect(onVersionConflict).toHaveBeenCalledOnce();
  });

  it("lists every client status and submits the selected client", () => {
    render(<ProjectFormModal onClose={vi.fn()} open />);

    fireEvent.focus(screen.getByRole("combobox", { name: "客户" }));
    expect(screen.getByRole("option", { name: /旧客户，已停用/ })).toBeTruthy();
    fireEvent.change(screen.getByLabelText("项目名称"), {
      target: { value: "客户官网" },
    });
    fireEvent.click(screen.getByRole("option", { name: /旧客户，已停用/ }));
    fireEvent.click(screen.getByRole("button", { name: "创建项目" }));

    expect(mutations.create.mutate).toHaveBeenCalledWith(
      expect.objectContaining({ clientId: "client-inactive" }),
      expect.any(Object),
    );
  });

  it("keeps an existing client id when options fail to load", () => {
    mutations.clients.isError = true;
    mutations.clientDetailError = true;
    mutations.clients.data = {
      items: [],
      meta: { page: 1, pageSize: 20, total: 0 },
    };
    const linkedProject = {
      ...project,
      clientId: "client-inactive",
      clientName: "旧客户",
    };
    render(<ProjectFormModal onClose={vi.fn()} open project={linkedProject} />);

    expect(screen.getByLabelText("客户")).toHaveValue("旧客户");
    fireEvent.click(screen.getByRole("button", { name: "保存修改" }));

    expect(mutations.update.mutate).toHaveBeenCalledWith(
      expect.objectContaining({
        input: expect.objectContaining({ clientId: "client-inactive" }),
      }),
      expect.any(Object),
    );
  });
});
