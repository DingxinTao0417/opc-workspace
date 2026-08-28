import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { resetRuntimeConnection } from "../api/client";
import { ActorSettings } from "./ActorSettings";

const owner = {
  id: "00000000-0000-5000-8000-000000000001",
  type: "owner",
  display_name: "我",
  status: "active",
  is_builtin: true,
  notes: "",
  metadata: {},
  version: 1,
  created_at: "2026-08-27T00:00:00Z",
  updated_at: "2026-08-27T00:00:00Z",
};

const systemActor = {
  ...owner,
  id: "00000000-0000-5000-8000-000000000002",
  type: "system",
  display_name: "系统",
};

const person = {
  ...owner,
  id: "00000000-0000-5000-8000-000000000003",
  type: "person",
  display_name: "陈设计",
  is_builtin: false,
  notes: "负责视觉",
  metadata: { role: "design" },
  version: 3,
};

function response(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function renderActors() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <ActorSettings />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

describe("ActorSettings", () => {
  it("shows a retryable API error without inventing local actors", async () => {
    let attempts = 0;
    const fetchMock = vi.fn(async () => {
      attempts += 1;
      if (attempts <= 3) {
        return response(
          {
            code: "DATABASE_ERROR",
            message: "本地数据库暂时不可用",
            request_id: "request-actor-list",
          },
          503,
        );
      }
      return response({
        data: [owner, systemActor],
        meta: { page: 1, page_size: 100, total: 2 },
      });
    });
    vi.stubGlobal("fetch", fetchMock);

    renderActors();

    expect(
      await screen.findByText("无法读取人员列表", undefined, {
        timeout: 2_500,
      }),
    ).toBeTruthy();
    expect(screen.getByRole("alert")).toHaveTextContent(
      "本地数据库暂时不可用 · 请求 request-actor-list",
    );
    fireEvent.click(screen.getByRole("button", { name: "重试" }));

    expect(await screen.findByRole("button", { name: "编辑我" })).toBeTruthy();
    expect(fetchMock).toHaveBeenCalledTimes(4);
  });

  it("shows built-ins, people, and the local-only responsibility boundary", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        response({
          data: [owner, person, systemActor],
          meta: { page: 1, page_size: 100, total: 3 },
        }),
      ),
    );

    renderActors();

    expect(await screen.findByText("陈设计")).toBeTruthy();
    expect(screen.getByRole("button", { name: "编辑我" })).toBeTruthy();
    expect(screen.getByText("内置系统主体，不可编辑或删除。")).toBeTruthy();
    expect(
      screen.getByText(/不会获得账号、收到任务或访问数据，也不会同步/),
    ).toBeTruthy();
    expect(screen.queryByLabelText("编辑系统")).toBeNull();
  });

  it("creates a person with explicit local metadata", async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, init?: RequestInit) => {
        if (init?.method === "POST") {
          return response(
            {
              data: {
                ...person,
                display_name: "林顾问",
                notes: "线下沟通",
                metadata: { specialty: "税务" },
                version: 1,
              },
            },
            201,
          );
        }
        return response({
          data: [owner, systemActor],
          meta: { page: 1, page_size: 100, total: 2 },
        });
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    renderActors();
    await screen.findByText("还没有本地人员");
    fireEvent.click(screen.getByRole("button", { name: "新建人员" }));
    fireEvent.change(screen.getByLabelText("新人员名称"), {
      target: { value: " 林顾问 " },
    });
    fireEvent.change(screen.getByLabelText("新人员备注"), {
      target: { value: "线下沟通" },
    });
    fireEvent.change(screen.getByLabelText("新人员扩展信息"), {
      target: { value: '{"specialty":"税务"}' },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建人员" }));

    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(([, init]) => init?.method === "POST"),
      ).toBe(true),
    );
    const postCall = fetchMock.mock.calls.find(
      ([, init]) => init?.method === "POST",
    );
    expect(JSON.parse(String(postCall?.[1]?.body))).toEqual({
      type: "person",
      display_name: "林顾问",
      notes: "线下沟通",
      metadata: { specialty: "税务" },
      status: "active",
    });
    expect(screen.queryByLabelText("新人员名称")).toBeNull();
  });

  it("edits and deactivates a person with its observed version", async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, init?: RequestInit) => {
        if (init?.method === "PATCH") {
          return response({
            data: {
              ...person,
              display_name: "陈设计师",
              status: "inactive",
              version: 4,
            },
          });
        }
        return response({
          data: [owner, person, systemActor],
          meta: { page: 1, page_size: 100, total: 3 },
        });
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    renderActors();
    await screen.findByText("陈设计");
    fireEvent.click(screen.getByRole("button", { name: "编辑陈设计" }));
    fireEvent.change(screen.getByLabelText("编辑陈设计的名称"), {
      target: { value: "陈设计师" },
    });
    fireEvent.change(screen.getByLabelText("编辑陈设计的状态"), {
      target: { value: "inactive" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存人员" }));

    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(([, init]) => init?.method === "PATCH"),
      ).toBe(true),
    );
    const patchCall = fetchMock.mock.calls.find(
      ([, init]) => init?.method === "PATCH",
    );
    expect(new Headers(patchCall?.[1]?.headers).get("If-Match")).toBe('"3"');
    expect(JSON.parse(String(patchCall?.[1]?.body))).toMatchObject({
      display_name: "陈设计师",
      status: "inactive",
      metadata: { role: "design" },
    });
  });

  it("keeps invalid metadata as a local draft and does not write", async () => {
    const fetchMock = vi.fn(async () =>
      response({
        data: [owner, systemActor],
        meta: { page: 1, page_size: 100, total: 2 },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    renderActors();
    await screen.findByText("还没有本地人员");
    fireEvent.click(screen.getByRole("button", { name: "新建人员" }));
    fireEvent.change(screen.getByLabelText("新人员名称"), {
      target: { value: "林顾问" },
    });
    fireEvent.change(screen.getByLabelText("新人员扩展信息"), {
      target: { value: "[]" },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建人员" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "必须是 JSON 对象",
    );
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(screen.getByLabelText("新人员名称")).toHaveValue("林顾问");
  });
});
