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

function makePeople(count: number) {
  return Array.from({ length: count }, (_, index) => ({
    ...person,
    id: `00000000-0000-5000-8000-${String(index + 10).padStart(12, "0")}`,
    display_name: `人员${String(index + 1).padStart(2, "0")}`,
    notes: `第 ${index + 1} 位人员`,
    version: index + 1,
  }));
}

function response(body: unknown, status = 200, requestId?: string) {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      "Content-Type": "application/json",
      ...(requestId ? { "X-Request-ID": requestId } : {}),
    },
  });
}

function actorUrl(input: RequestInfo | URL) {
  return new URL(String(input), "http://127.0.0.1");
}

function actorListResponse(
  input: RequestInfo | URL,
  people: (typeof person)[],
) {
  const url = actorUrl(input);
  const page = Number(url.searchParams.get("page"));
  const pageSize = Number(url.searchParams.get("page_size"));
  const type = url.searchParams.get("type");
  if (type === "owner") {
    return response({
      data: [owner],
      meta: { page, page_size: pageSize, total: 1 },
    });
  }
  if (type === "system") {
    return response({
      data: [systemActor],
      meta: { page, page_size: pageSize, total: 1 },
    });
  }
  if (type === "person") {
    const start = (page - 1) * pageSize;
    return response({
      data: people.slice(start, start + pageSize),
      meta: { page, page_size: pageSize, total: people.length },
    });
  }
  throw new Error(`Unexpected actor list request: ${url}`);
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, reject, resolve };
}

function renderActors() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return {
    queryClient,
    ...render(
      <QueryClientProvider client={queryClient}>
        <ActorSettings />
      </QueryClientProvider>,
    ),
  };
}

function personListCalls(fetchMock: ReturnType<typeof vi.fn>) {
  return fetchMock.mock.calls.filter(([input, init]) => {
    const url = actorUrl(input as RequestInfo | URL);
    return (
      !init?.method &&
      url.pathname === "/api/v1/actors" &&
      url.searchParams.get("type") === "person"
    );
  });
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

describe("ActorSettings", () => {
  it("keeps built-ins visible while the initial person request fails and retries the real page", async () => {
    let personAttempts = 0;
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = actorUrl(input);
        if (url.pathname === "/api/v1/actors" && !init?.method) {
          if (url.searchParams.get("type") !== "person") {
            return actorListResponse(input, []);
          }
          personAttempts += 1;
          if (personAttempts <= 3) {
            const requestId =
              new Headers(init?.headers).get("X-Request-ID") ?? "";
            return response(
              {
                code: "DATABASE_ERROR",
                message: "本地数据库暂时不可用",
                request_id: requestId,
              },
              503,
              requestId,
            );
          }
          return actorListResponse(input, [person]);
        }
        throw new Error(`Unexpected request: ${url}`);
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    renderActors();

    expect(
      await screen.findByText("无法读取本地人员", undefined, {
        timeout: 2_500,
      }),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "编辑我" })).toBeVisible();
    expect(screen.getByText("内置系统主体，不可编辑或删除。")).toBeVisible();
    expect(screen.queryByText("还没有本地人员")).not.toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent(
      /本地数据库暂时不可用 · 请求 [0-9a-f-]{36}/,
    );

    fireEvent.click(screen.getByRole("button", { name: "重试" }));

    expect(await screen.findByText("陈设计")).toBeVisible();
    expect(personAttempts).toBe(4);
  });

  it("uses independent owner/system queries and an exact sorted person page", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) =>
      actorListResponse(input, [person]),
    );
    vi.stubGlobal("fetch", fetchMock);

    renderActors();

    expect(await screen.findByText("陈设计")).toBeVisible();
    expect(screen.getByRole("button", { name: "编辑我" })).toBeVisible();
    expect(screen.getByText("内置系统主体，不可编辑或删除。")).toBeVisible();
    expect(screen.queryByLabelText("编辑系统")).not.toBeInTheDocument();
    expect(
      screen.getByText(/不会获得账号、收到任务或访问数据，也不会同步/),
    ).toBeVisible();
    expect(screen.getByText(/共 1 人 · 第 1 \/ 1 页/)).toBeVisible();

    const urls = fetchMock.mock.calls.map(([input]) => actorUrl(input));
    expect(
      urls.some(
        (url) =>
          url.searchParams.get("type") === "owner" &&
          url.searchParams.get("page") === "1" &&
          url.searchParams.get("page_size") === "20",
      ),
    ).toBe(true);
    expect(
      urls.some(
        (url) =>
          url.searchParams.get("type") === "system" &&
          url.searchParams.get("page") === "1" &&
          url.searchParams.get("page_size") === "20",
      ),
    ).toBe(true);
    expect(
      urls.some(
        (url) =>
          url.searchParams.get("type") === "person" &&
          url.searchParams.get("page") === "1" &&
          url.searchParams.get("page_size") === "20" &&
          url.searchParams.get("sort") === "display_name",
      ),
    ).toBe(true);
    expect(urls.some((url) => !url.searchParams.get("type"))).toBe(false);
    expect(urls.some((url) => url.searchParams.get("type") === "agent")).toBe(
      false,
    );
  });

  it("hides the previous page placeholder while preserving owner and system", async () => {
    const people = makePeople(21);
    const pageTwo = deferred<Response>();
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = actorUrl(input);
        if (
          url.pathname === "/api/v1/actors" &&
          !init?.method &&
          url.searchParams.get("type") === "person" &&
          url.searchParams.get("page") === "2"
        ) {
          return pageTwo.promise;
        }
        return actorListResponse(input, people);
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    renderActors();
    expect(await screen.findByText("人员01")).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "下一页" }));

    expect(await screen.findByText("正在读取第 2 页本地人员…")).toBeVisible();
    expect(screen.queryByText("人员01")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "编辑我" })).toBeVisible();
    expect(screen.getByText("内置系统主体，不可编辑或删除。")).toBeVisible();
    expect(screen.getByRole("button", { name: "上一页" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "下一页" })).toBeDisabled();

    pageTwo.resolve(
      actorListResponse(
        new URL(
          "http://local/api/v1/actors?page=2&page_size=20&type=person&sort=display_name",
        ),
        people,
      ),
    );

    expect(await screen.findByText("人员21")).toBeVisible();
    expect(screen.queryByText("人员01")).not.toBeInTheDocument();
    expect(screen.getByText(/共 21 人 · 第 2 \/ 2 页/)).toBeVisible();
  });

  it("shows a page error without reusing the old page or claiming the list is empty", async () => {
    const people = makePeople(21);
    let pageTwoAttempts = 0;
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = actorUrl(input);
        if (
          url.pathname === "/api/v1/actors" &&
          !init?.method &&
          url.searchParams.get("type") === "person" &&
          url.searchParams.get("page") === "2"
        ) {
          pageTwoAttempts += 1;
          return response(
            { code: "DATABASE_ERROR", message: "第二页读取失败" },
            503,
          );
        }
        return actorListResponse(input, people);
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    renderActors();
    expect(await screen.findByText("人员01")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "下一页" }));

    expect(
      await screen.findByText("无法读取第 2 页本地人员", undefined, {
        timeout: 2_500,
      }),
    ).toBeVisible();
    expect(screen.queryByText("人员01")).not.toBeInTheDocument();
    expect(screen.queryByText("还没有本地人员")).not.toBeInTheDocument();
    expect(screen.getByText("内置系统主体，不可编辑或删除。")).toBeVisible();
    expect(pageTwoAttempts).toBe(3);

    fireEvent.click(screen.getByRole("button", { name: "返回上一页" }));
    expect(await screen.findByText("人员01")).toBeVisible();
  });

  it("keeps fresh cards and total visible when a same-page refresh fails", async () => {
    const people = makePeople(21);
    let failRefresh = false;
    let personAttempts = 0;
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = actorUrl(input);
        if (
          url.pathname === "/api/v1/actors" &&
          !init?.method &&
          url.searchParams.get("type") === "person"
        ) {
          personAttempts += 1;
          if (failRefresh) {
            return response(
              { code: "DATABASE_ERROR", message: "刷新读取失败" },
              503,
            );
          }
        }
        return actorListResponse(input, people);
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    renderActors();
    expect(await screen.findByText("人员01")).toBeVisible();
    expect(screen.getByRole("button", { name: "下一页" })).toBeEnabled();

    failRefresh = true;
    fireEvent.click(screen.getByRole("button", { name: "刷新本地人员" }));

    expect(await screen.findByText("正在刷新本地人员…")).toBeVisible();
    expect(screen.getByText("人员01")).toBeVisible();
    expect(screen.getByRole("button", { name: "下一页" })).toBeDisabled();
    expect(
      await screen.findByText(/本地人员刷新失败/, undefined, {
        timeout: 2_500,
      }),
    ).toBeVisible();
    expect(screen.getByText("人员01")).toBeVisible();
    expect(screen.getByText(/共 21 人 · 第 1 \/ 2 页/)).toBeVisible();
    expect(screen.queryByText("还没有本地人员")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "下一页" })).toBeDisabled();
    expect(personAttempts).toBe(4);
  });

  it("shows a true empty state and keeps an invalid creation draft local", async () => {
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, _init?: RequestInit) =>
        actorListResponse(input, []),
    );
    vi.stubGlobal("fetch", fetchMock);

    renderActors();
    expect(await screen.findByText("还没有本地人员")).toBeVisible();
    expect(screen.getByText(/共 0 人 · 第 1 \/ 1 页/)).toBeVisible();
    expect(screen.getByRole("button", { name: "上一页" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "下一页" })).toBeDisabled();

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
    expect(
      fetchMock.mock.calls.some(([, init]) => init?.method === "POST"),
    ).toBe(false);
    expect(screen.getByLabelText("新人员名称")).toHaveValue("林顾问");
  });

  it("locks paging for a creation draft and waits for invalidated lists to refetch", async () => {
    let people = makePeople(21);
    const created = {
      ...person,
      id: "00000000-0000-5000-8000-000000000099",
      display_name: "A 新人员",
      notes: "线下沟通",
      metadata: { role: "design", specialty: "税务" },
      version: 1,
    };
    const post = deferred<Response>();
    const personRefetch = deferred<Response>();
    const personRefetchStarted = deferred<void>();
    let personGets = 0;
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = actorUrl(input);
        if (url.pathname === "/api/v1/actors" && init?.method === "POST") {
          return post.promise;
        }
        if (
          url.pathname === "/api/v1/actors" &&
          !init?.method &&
          url.searchParams.get("type") === "person"
        ) {
          personGets += 1;
          if (personGets === 2) {
            personRefetchStarted.resolve();
            return personRefetch.promise;
          }
        }
        return actorListResponse(input, people);
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    renderActors();
    expect(await screen.findByText("人员01")).toBeVisible();
    expect(screen.getByRole("button", { name: "下一页" })).toBeEnabled();
    fireEvent.click(screen.getByRole("button", { name: "新建人员" }));
    expect(screen.getByRole("button", { name: "下一页" })).toBeDisabled();
    fireEvent.change(screen.getByLabelText("新人员名称"), {
      target: { value: " A 新人员 " },
    });
    fireEvent.change(screen.getByLabelText("新人员备注"), {
      target: { value: "线下沟通" },
    });
    fireEvent.change(screen.getByLabelText("新人员扩展信息"), {
      target: { value: '{"specialty":"税务"}' },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建人员" }));
    expect(screen.getByRole("button", { name: "下一页" })).toBeDisabled();

    people = [created, ...people];
    post.resolve(response({ data: created }, 201));
    await personRefetchStarted.promise;

    expect(screen.getByLabelText("新人员名称")).toHaveValue(" A 新人员 ");
    expect(screen.getByRole("button", { name: "下一页" })).toBeDisabled();
    personRefetch.resolve(
      actorListResponse(
        new URL(
          "http://local/api/v1/actors?page=1&page_size=20&type=person&sort=display_name",
        ),
        people,
      ),
    );

    expect(await screen.findByText("A 新人员")).toBeVisible();
    expect(screen.queryByLabelText("新人员名称")).not.toBeInTheDocument();
    expect(personGets).toBe(2);
    const postCall = fetchMock.mock.calls.find(
      ([, init]) => init?.method === "POST",
    );
    expect(JSON.parse(String(postCall?.[1]?.body))).toEqual({
      type: "person",
      display_name: "A 新人员",
      notes: "线下沟通",
      metadata: { specialty: "税务" },
      status: "active",
    });
  });

  it("locks paging while editing and renders the refetched update", async () => {
    let people = makePeople(21);
    const original = people[0];
    const updated = {
      ...original,
      display_name: "人员01-已停用",
      status: "inactive",
      version: original.version + 1,
    };
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = actorUrl(input);
        if (url.pathname === `/api/v1/actors/${original.id}` && !init?.method) {
          return response({ data: people[0] });
        }
        if (
          url.pathname === `/api/v1/actors/${original.id}` &&
          init?.method === "PATCH"
        ) {
          people = [updated, ...people.slice(1)];
          return response({ data: updated });
        }
        return actorListResponse(input, people);
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    renderActors();
    expect(await screen.findByText("人员01")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "编辑人员01" }));
    expect(screen.getByRole("button", { name: "下一页" })).toBeDisabled();
    fireEvent.change(screen.getByLabelText("编辑人员01的名称"), {
      target: { value: "人员01-已停用" },
    });
    fireEvent.change(screen.getByLabelText("编辑人员01的状态"), {
      target: { value: "inactive" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存人员" }));

    expect(await screen.findByText("人员01-已停用")).toBeVisible();
    expect(screen.queryByLabelText("编辑人员01的名称")).not.toBeInTheDocument();
    expect(personListCalls(fetchMock)).toHaveLength(2);
    const patchCall = fetchMock.mock.calls.find(
      ([, init]) => init?.method === "PATCH",
    );
    expect(new Headers(patchCall?.[1]?.headers).get("If-Match")).toBe('"1"');
    expect(JSON.parse(String(patchCall?.[1]?.body))).toMatchObject({
      display_name: "人员01-已停用",
      status: "inactive",
    });
  });

  it("clamps only after a fresh successful shrink response", async () => {
    const initialPeople = makePeople(21);
    const remaining = initialPeople[0];
    let shrink = false;
    let pageTwoGets = 0;
    const shrinkRefresh = deferred<Response>();
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = actorUrl(input);
        if (
          url.pathname === "/api/v1/actors" &&
          !init?.method &&
          url.searchParams.get("type") === "person"
        ) {
          const page = url.searchParams.get("page");
          if (page === "2") {
            pageTwoGets += 1;
            if (pageTwoGets === 2) return shrinkRefresh.promise;
          }
        }
        return actorListResponse(input, shrink ? [remaining] : initialPeople);
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    renderActors();
    expect(await screen.findByText("人员01")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect(await screen.findByText("人员21")).toBeVisible();
    expect(screen.getByText(/共 21 人 · 第 2 \/ 2 页/)).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "刷新本地人员" }));
    expect(await screen.findByText("正在刷新本地人员…")).toBeVisible();
    expect(screen.getByText("人员21")).toBeVisible();
    expect(screen.getByText(/共 21 人 · 第 2 \/ 2 页/)).toBeVisible();

    shrink = true;
    shrinkRefresh.resolve(
      response({
        data: [],
        meta: { page: 2, page_size: 20, total: 1 },
      }),
    );

    expect(await screen.findByText("人员01")).toBeVisible();
    expect(screen.queryByText("人员21")).not.toBeInTheDocument();
    expect(await screen.findByText(/共 1 人 · 第 1 \/ 1 页/)).toBeVisible();
    expect(
      personListCalls(fetchMock).map(([input]) =>
        actorUrl(input as RequestInfo | URL).searchParams.get("page"),
      ),
    ).toEqual(["1", "2", "2", "1"]);
  });

  it("reloads a conflicted editor by id, preserves its draft, and adopts the new version", async () => {
    let listedPeople = [person];
    let detailGets = 0;
    let patchAttempts = 0;
    const latest = {
      ...person,
      display_name: "外部窗口新名称",
      notes: "外部窗口已更新",
      version: 4,
    };
    const saved = {
      ...latest,
      display_name: "我的未保存名称",
      version: 5,
    };
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = actorUrl(input);
        if (url.pathname === `/api/v1/actors/${person.id}` && !init?.method) {
          detailGets += 1;
          return response({ data: detailGets === 1 ? person : latest });
        }
        if (
          url.pathname === `/api/v1/actors/${person.id}` &&
          init?.method === "PATCH"
        ) {
          patchAttempts += 1;
          if (patchAttempts === 1) {
            return response(
              {
                code: "VERSION_CONFLICT",
                message: "人员已被其他窗口修改",
              },
              409,
            );
          }
          listedPeople = [saved];
          return response({ data: saved });
        }
        return actorListResponse(input, listedPeople);
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    renderActors();
    expect(await screen.findByText("陈设计")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "编辑陈设计" }));
    fireEvent.change(screen.getByLabelText("编辑陈设计的名称"), {
      target: { value: "我的未保存名称" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存人员" }));

    expect(await screen.findByText(/该人员已在其他窗口修改/)).toBeVisible();
    expect(screen.getByRole("button", { name: "保存人员" })).toBeDisabled();
    expect(screen.getByLabelText("编辑陈设计的名称")).toHaveValue(
      "我的未保存名称",
    );
    expect(screen.getByLabelText("编辑陈设计的名称")).toHaveValue(
      "我的未保存名称",
    );
    const detailGetsBeforeReload = detailGets;

    fireEvent.click(screen.getByRole("button", { name: "载入最新内容" }));

    expect(
      await screen.findByLabelText("编辑外部窗口新名称的名称"),
    ).toHaveValue("我的未保存名称");
    expect(screen.getByLabelText("编辑外部窗口新名称的备注")).toHaveValue(
      "负责视觉",
    );
    expect(
      screen.getByRole("button", { name: "编辑外部窗口新名称" }),
    ).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "编辑陈设计" }),
    ).not.toBeInTheDocument();
    expect(detailGets).toBe(detailGetsBeforeReload + 1);
    expect(screen.getByRole("button", { name: "保存人员" })).toBeEnabled();
    fireEvent.click(screen.getByRole("button", { name: "保存人员" }));

    expect(await screen.findByText("我的未保存名称")).toBeVisible();
    const patchCalls = fetchMock.mock.calls.filter(
      ([, init]) => init?.method === "PATCH",
    );
    expect(patchCalls).toHaveLength(2);
    expect(new Headers(patchCalls[0][1]?.headers).get("If-Match")).toBe('"3"');
    expect(new Headers(patchCalls[1][1]?.headers).get("If-Match")).toBe('"4"');
  });

  it("refreshes the person list after cancelling a conflicted edit", async () => {
    let listedPeople = [person];
    let detailGets = 0;
    let patchAttempts = 0;
    const latest = {
      ...person,
      display_name: "取消后采用的新名称",
      notes: "外部窗口已更新",
      version: 4,
    };
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = actorUrl(input);
        if (url.pathname === `/api/v1/actors/${person.id}` && !init?.method) {
          detailGets += 1;
          return response({ data: latest });
        }
        if (
          url.pathname === `/api/v1/actors/${person.id}` &&
          init?.method === "PATCH"
        ) {
          patchAttempts += 1;
          listedPeople = [latest];
          return response(
            { code: "VERSION_CONFLICT", message: "人员已被其他窗口修改" },
            409,
          );
        }
        return actorListResponse(input, listedPeople);
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    renderActors();
    expect(await screen.findByText("陈设计")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "编辑陈设计" }));
    await waitFor(() => expect(detailGets).toBe(1));
    fireEvent.change(screen.getByLabelText("编辑陈设计的名称"), {
      target: { value: "不应覆盖外部更新的草稿" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存人员" }));

    expect(await screen.findByText(/该人员已在其他窗口修改/)).toBeVisible();
    await waitFor(() => expect(detailGets).toBeGreaterThanOrEqual(2));
    const personCallsBeforeCancel = personListCalls(fetchMock).length;
    expect(personCallsBeforeCancel).toBe(1);

    fireEvent.click(screen.getByRole("button", { name: "取消" }));

    expect(
      await screen.findByRole("button", { name: "编辑取消后采用的新名称" }),
    ).toBeVisible();
    expect(screen.queryByText("陈设计")).not.toBeInTheDocument();
    expect(personListCalls(fetchMock)).toHaveLength(
      personCallsBeforeCancel + 1,
    );
    expect(patchAttempts).toBe(1);
  });

  it("keeps a conflicted draft blocked when detail reload fails", async () => {
    let detailGets = 0;
    let failDetail = false;
    let patchAttempts = 0;
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = actorUrl(input);
        if (url.pathname === `/api/v1/actors/${person.id}` && !init?.method) {
          detailGets += 1;
          if (failDetail) {
            return response(
              { code: "DATABASE_ERROR", message: "详情读取失败" },
              503,
            );
          }
          return response({ data: person });
        }
        if (
          url.pathname === `/api/v1/actors/${person.id}` &&
          init?.method === "PATCH"
        ) {
          patchAttempts += 1;
          return response(
            { code: "VERSION_CONFLICT", message: "人员已被修改" },
            409,
          );
        }
        return actorListResponse(input, [person]);
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    renderActors();
    expect(await screen.findByText("陈设计")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "编辑陈设计" }));
    await waitFor(() => expect(detailGets).toBe(1));
    fireEvent.change(screen.getByLabelText("编辑陈设计的名称"), {
      target: { value: "保留的草稿" },
    });
    failDetail = true;
    fireEvent.click(screen.getByRole("button", { name: "保存人员" }));
    expect(
      await screen.findByText(/该人员已在其他窗口修改/, undefined, {
        timeout: 2_500,
      }),
    ).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "载入最新内容" }));

    expect(
      await screen.findByText(/无法载入最新内容/, undefined, {
        timeout: 2_500,
      }),
    ).toBeVisible();
    expect(screen.getByLabelText("编辑陈设计的名称")).toHaveValue("保留的草稿");
    expect(screen.getByRole("button", { name: "保存人员" })).toBeDisabled();
    expect(patchAttempts).toBe(1);
  });
});
