import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../api/client";
import type { Client } from "../types/models";
import { ClientFormModal } from "./ClientFormModal";

const mutations = vi.hoisted(() => ({
  create: { error: null, isPending: false, mutate: vi.fn(), reset: vi.fn() },
  update: { error: null, isPending: false, mutate: vi.fn(), reset: vi.fn() },
}));

vi.mock("../api/hooks", () => ({
  useCreateClient: () => mutations.create,
  useUpdateClient: () => mutations.update,
}));

const client: Client = {
  id: "client-1",
  name: "星河工作室",
  contactName: "陶先生",
  email: "hello@example.com",
  phone: "13800000000",
  notes: "品牌设计客户",
  status: "active",
  version: 4,
  projectCount: 2,
  latestActivityAt: null,
  createdAt: "2026-08-20T00:00:00Z",
  updatedAt: "2026-08-27T00:00:00Z",
};

describe("ClientFormModal", () => {
  beforeEach(() => {
    mutations.create.mutate.mockClear();
    mutations.update.mutate.mockClear();
  });

  afterEach(cleanup);

  it("creates a normalized client draft", () => {
    render(<ClientFormModal onClose={vi.fn()} open />);

    fireEvent.change(screen.getByLabelText("客户名称"), {
      target: { value: "  新客户  " },
    });
    fireEvent.change(screen.getByLabelText("状态"), {
      target: { value: "lead" },
    });
    fireEvent.change(screen.getByLabelText("邮箱"), {
      target: { value: "  lead@example.com  " },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建客户" }));

    expect(mutations.create.mutate).toHaveBeenCalledWith(
      {
        name: "新客户",
        contactName: null,
        email: "lead@example.com",
        phone: null,
        notes: null,
        status: "lead",
      },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
  });

  it("keeps the edited draft and adopts the refreshed version after conflict", async () => {
    const latest = { ...client, name: "其他窗口名称", version: 6 };
    const onVersionConflict = vi.fn().mockResolvedValue(latest);
    render(
      <ClientFormModal
        client={client}
        onClose={vi.fn()}
        onVersionConflict={onVersionConflict}
        open
      />,
    );

    fireEvent.change(screen.getByLabelText("客户名称"), {
      target: { value: "我的草稿名称" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存修改" }));

    const firstOptions = mutations.update.mutate.mock.calls[0][1];
    await act(async () => {
      firstOptions.onError(
        new ApiError("版本冲突", { code: "VERSION_CONFLICT", status: 409 }),
      );
    });
    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent("版本 6"),
    );
    expect(screen.getByLabelText("客户名称")).toHaveValue("我的草稿名称");

    fireEvent.click(screen.getByRole("button", { name: "保存修改" }));
    expect(mutations.update.mutate).toHaveBeenLastCalledWith(
      {
        id: client.id,
        input: expect.objectContaining({
          name: "我的草稿名称",
          expectedVersion: 6,
        }),
      },
      expect.any(Object),
    );
  });

  it("rejects phone values beyond the server limit", () => {
    render(<ClientFormModal onClose={vi.fn()} open />);
    fireEvent.change(screen.getByLabelText("客户名称"), {
      target: { value: "新客户" },
    });
    fireEvent.change(screen.getByLabelText("电话"), {
      target: { value: "1".repeat(51) },
    });
    fireEvent.submit(document.getElementById("client-form")!);

    expect(screen.getByRole("alert")).toHaveTextContent("50 个字符");
    expect(mutations.create.mutate).not.toHaveBeenCalled();
  });

  it("keeps phone as trimmed text without inventing a digit rule", () => {
    render(<ClientFormModal onClose={vi.fn()} open />);
    fireEvent.change(screen.getByLabelText("客户名称"), {
      target: { value: "新客户" },
    });
    fireEvent.change(screen.getByLabelText("电话"), {
      target: { value: "  总机转前台  " },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建客户" }));

    expect(mutations.create.mutate).toHaveBeenCalledWith(
      expect.objectContaining({ phone: "总机转前台" }),
      expect.any(Object),
    );
  });

  it("counts Unicode code points consistently with the Sidecar", () => {
    const name = "😀".repeat(200);
    render(<ClientFormModal onClose={vi.fn()} open />);
    fireEvent.change(screen.getByLabelText("客户名称"), {
      target: { value: name },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建客户" }));

    expect(mutations.create.mutate).toHaveBeenCalledWith(
      expect.objectContaining({ name }),
      expect.any(Object),
    );
  });
});
