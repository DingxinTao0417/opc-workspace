import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  createPersonActor,
  createTag,
  createTask,
  createTaskAssignment,
  deleteTag,
  deleteTask,
  endTaskAssignment,
  executeTaskLifecycleCommand,
  getAllActors,
  getActors,
  getAllProjects,
  getAllTasks,
  getTags,
  getTaskPage,
  getTaskAssignments,
  getTaskEvents,
  normalizeActor,
  normalizeActorSummary,
  normalizeTaskAssignment,
  normalizeTaskAssignmentListResult,
  normalizeTaskEventListResult,
  normalizeTaskWorkflowEvent,
  normalizeTag,
  normalizeProject,
  normalizeTask,
  resetRuntimeConnection,
  reassignTaskAssignment,
  updateTag,
  updateTask,
  updateActor,
} from "./client";

afterEach(() => {
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

function taskPayload(overrides: Record<string, unknown> = {}) {
  return {
    id: "task-1",
    title: "整理交付清单",
    description: "确认文件与时间点",
    kind: "work",
    status: "in_progress",
    priority: "P1",
    project_id: "project-1",
    project_name: "客户门户",
    parent_task_id: null,
    completion_criteria: "文件齐全",
    review_policy: "none",
    blocked_reason: null,
    blocked_at: null,
    blocked_from_status: null,
    due_date: "2026-08-26T18:00:00Z",
    planned_date: "2026-08-26",
    estimated_minutes: 45,
    actual_minutes: 12,
    manual_order: 1000,
    version: 7,
    subtask_total: 3,
    subtask_completed: 2,
    tags: [
      {
        id: "tag-1",
        name: "交付",
        color: "#6e7bf2",
        version: 2,
        created_at: "2026-08-20T00:00:00Z",
      },
    ],
    created_at: "2026-08-26T10:00:00Z",
    updated_at: "2026-08-26T10:10:00Z",
    completed_at: null,
    submitted_at: null,
    reviewed_at: null,
    ...overrides,
  };
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

const actorPayload = {
  id: "00000000-0000-5000-8000-000000000003",
  type: "person",
  display_name: "陈设计",
  status: "active",
  is_builtin: false,
  notes: "负责视觉",
  metadata: { role: "design" },
  version: 2,
  created_at: "2026-08-27T00:00:00Z",
  updated_at: "2026-08-27T00:01:00Z",
};

const ownerSummaryPayload = {
  id: "00000000-0000-5000-8000-000000000001",
  type: "owner",
  display_name: "我",
  status: "active",
  is_builtin: true,
  version: 1,
};

const personSummaryPayload = {
  id: actorPayload.id,
  type: "person",
  display_name: actorPayload.display_name,
  status: "active",
  is_builtin: false,
  version: 2,
};

function assignmentPayload(overrides: Record<string, unknown> = {}) {
  return {
    id: "assignment-1",
    task_id: "task-1",
    role: "assignee",
    actor_id: personSummaryPayload.id,
    actor: personSummaryPayload,
    assigned_by_actor_id: ownerSummaryPayload.id,
    assigned_by_actor: ownerSummaryPayload,
    assigned_at: "2026-08-27T09:00:00Z",
    unassigned_at: null,
    reason: null,
    is_active: true,
    inferred: false,
    ...overrides,
  };
}

function eventPayload(overrides: Record<string, unknown> = {}) {
  return {
    id: "event-1",
    action: "task_started",
    actor: ownerSummaryPayload,
    assignment_id: null,
    request_id: "request-1",
    command_seq: 1,
    previous: { status: "todo", version: 7 },
    current: { status: "in_progress", version: 8 },
    reason: null,
    created_at: "2026-08-27T11:00:00Z",
    ...overrides,
  };
}

describe("actor requests", () => {
  it("strictly normalizes actors and lists them with supported filters", async () => {
    expect(normalizeActor(actorPayload)).toMatchObject({
      displayName: "陈设计",
      type: "person",
      status: "active",
      isBuiltin: false,
      metadata: { role: "design" },
      version: 2,
    });
    expect(() => normalizeActor({ ...actorPayload, metadata: [] })).toThrow(
      ApiError,
    );

    const fetchMock = vi.fn(async (_input: RequestInfo | URL) =>
      jsonResponse({
        data: [actorPayload],
        meta: { page: 2, page_size: 20, total: 21 },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await getActors({
      page: 2,
      pageSize: 20,
      type: "person",
      status: "inactive",
      sort: "display_name",
    });
    const url = new URL(
      String(fetchMock.mock.calls[0][0]),
      "http://local.test",
    );
    expect(Object.fromEntries(url.searchParams)).toEqual({
      page: "2",
      page_size: "20",
      type: "person",
      status: "inactive",
      sort: "display_name",
    });
    expect(result.meta).toEqual({ page: 2, pageSize: 20, total: 21 });
  });

  it("creates only person actors and version-locks updates", async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, _init?: RequestInit) =>
        jsonResponse({ data: actorPayload }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await createPersonActor(
      {
        displayName: "陈设计",
        notes: "负责视觉",
        metadata: { role: "design" },
      },
      "actor-key",
    );
    await updateActor(actorPayload.id, {
      displayName: "陈设计师",
      status: "inactive",
      expectedVersion: 2,
    });

    expect(
      new Headers(fetchMock.mock.calls[0][1]?.headers).get("Idempotency-Key"),
    ).toBe("actor-key");
    expect(JSON.parse(String(fetchMock.mock.calls[0][1]?.body))).toMatchObject({
      type: "person",
      display_name: "陈设计",
      metadata: { role: "design" },
    });
    expect(
      new Headers(fetchMock.mock.calls[1][1]?.headers).get("If-Match"),
    ).toBe('"2"');
    expect(JSON.parse(String(fetchMock.mock.calls[1][1]?.body))).toEqual({
      display_name: "陈设计师",
      status: "inactive",
    });
  });

  it("loads every actor page without truncating assignment candidates", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = new URL(String(input), "http://local.test");
      const page = Number(url.searchParams.get("page"));
      const data = Array.from({ length: page === 1 ? 100 : 1 }, (_, index) => ({
        ...actorPayload,
        id: `actor-${page}-${index}`,
        display_name: `人员 ${page}-${index}`,
      }));
      return jsonResponse({
        data,
        meta: { page, page_size: 100, total: 101 },
      });
    });
    vi.stubGlobal("fetch", fetchMock);

    await expect(getAllActors({ status: "active" })).resolves.toHaveLength(101);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(String(fetchMock.mock.calls[1][0])).toContain("page=2");
  });
});

describe("task assignment responses", () => {
  it("strictly normalizes actor snapshots, current roles, and ended history", () => {
    expect(normalizeActorSummary(personSummaryPayload)).toMatchObject({
      displayName: "陈设计",
      type: "person",
      status: "active",
    });
    expect(normalizeTaskAssignment(assignmentPayload())).toMatchObject({
      taskId: "task-1",
      actorId: personSummaryPayload.id,
      assignedByActorId: ownerSummaryPayload.id,
      isActive: true,
      inferred: false,
    });

    const result = normalizeTaskAssignmentListResult({
      data: {
        active: {
          assignee: assignmentPayload({ inferred: true }),
          reviewer: null,
        },
        history: [
          assignmentPayload({
            id: "assignment-ended",
            unassigned_at: "2026-08-27T10:00:00Z",
            reason: "转交下一阶段",
            is_active: false,
          }),
        ],
      },
      meta: { page: 1, page_size: 20, total: 1, task_version: 7 },
    });
    expect(result).toMatchObject({
      active: { assignee: { inferred: true }, reviewer: null },
      history: [{ reason: "转交下一阶段", isActive: false }],
      meta: { page: 1, pageSize: 20, total: 1, taskVersion: 7 },
    });
  });

  it("rejects unknown roles and inconsistent active/history flags", () => {
    expect(() =>
      normalizeTaskAssignment(assignmentPayload({ role: "observer" })),
    ).toThrow(ApiError);
    expect(() =>
      normalizeTaskAssignment(
        assignmentPayload({
          is_active: false,
          unassigned_at: null,
        }),
      ),
    ).toThrow(ApiError);
    expect(() =>
      normalizeTaskAssignmentListResult({
        data: {
          active: { assignee: null, reviewer: null },
          history: [assignmentPayload()],
        },
        meta: { page: 1, page_size: 20, total: 1, task_version: 7 },
      }),
    ).toThrow(ApiError);
  });

  it("serializes history paging and role filters", async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL) =>
      jsonResponse({
        data: {
          active: { assignee: assignmentPayload(), reviewer: null },
          history: [],
        },
        meta: { page: 2, page_size: 25, total: 30, task_version: 7 },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await getTaskAssignments("task-1", {
      page: 2,
      pageSize: 25,
      role: "assignee",
      sort: " -assigned_at ",
    });

    const url = new URL(
      String(fetchMock.mock.calls[0][0]),
      "http://local.test",
    );
    expect(Object.fromEntries(url.searchParams)).toEqual({
      page: "2",
      page_size: "25",
      role: "assignee",
      sort: "-assigned_at",
    });
  });

  it("sends version and idempotency headers for assign, reassign, and end", async () => {
    const active = assignmentPayload();
    const ended = assignmentPayload({
      unassigned_at: "2026-08-27T10:00:00Z",
      reason: "工作阶段结束",
      is_active: false,
    });
    const reassigned = assignmentPayload({
      id: "assignment-2",
      actor_id: ownerSummaryPayload.id,
      actor: ownerSummaryPayload,
    });
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, _init?: RequestInit) => {
        const url = String(input);
        if (url.endsWith("/reassign")) {
          return jsonResponse({
            data: {
              previous_assignment: ended,
              assignment: reassigned,
              task: taskPayload({ version: 8 }),
            },
          });
        }
        if (url.endsWith("/end")) {
          return jsonResponse({
            data: { assignment: ended, task: taskPayload({ version: 8 }) },
          });
        }
        return jsonResponse({
          data: { assignment: active, task: taskPayload({ version: 8 }) },
        });
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    await createTaskAssignment(
      "task-1",
      {
        role: "assignee",
        actorId: personSummaryPayload.id,
        expectedVersion: 7,
      },
      "assign-key",
    );
    await reassignTaskAssignment(
      "task-1",
      {
        role: "assignee",
        actorId: ownerSummaryPayload.id,
        reason: "转交所有者",
        expectedVersion: 7,
      },
      "reassign-key",
    );
    await endTaskAssignment(
      "assignment-1",
      { reason: "工作阶段结束", expectedVersion: 7 },
      "end-key",
    );

    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(
      new Headers(fetchMock.mock.calls[0][1]?.headers).get("If-Match"),
    ).toBe('"7"');
    expect(
      new Headers(fetchMock.mock.calls[0][1]?.headers).get("Idempotency-Key"),
    ).toBe("assign-key");
    expect(JSON.parse(String(fetchMock.mock.calls[0][1]?.body))).toEqual({
      role: "assignee",
      actor_id: personSummaryPayload.id,
    });
    expect(
      new Headers(fetchMock.mock.calls[1][1]?.headers).get("Idempotency-Key"),
    ).toBe("reassign-key");
    expect(JSON.parse(String(fetchMock.mock.calls[1][1]?.body))).toEqual({
      role: "assignee",
      actor_id: ownerSummaryPayload.id,
      reason: "转交所有者",
    });
    expect(
      new Headers(fetchMock.mock.calls[2][1]?.headers).get("Idempotency-Key"),
    ).toBe("end-key");
    expect(JSON.parse(String(fetchMock.mock.calls[2][1]?.body))).toEqual({
      reason: "工作阶段结束",
    });
  });
});

describe("normalizeTask", () => {
  it("maps the versioned API snake_case fields", () => {
    const task = normalizeTask(taskPayload());

    expect(task).toMatchObject({
      id: "task-1",
      kind: "work",
      status: "in_progress",
      priority: "P1",
      projectId: "project-1",
      projectName: "客户门户",
      parentTaskId: null,
      completionCriteria: "文件齐全",
      reviewPolicy: "none",
      blockedReason: null,
      blockedAt: null,
      blockedFromStatus: null,
      estimatedMinutes: 45,
      actualMinutes: 12,
      manualOrder: 1000,
      version: 7,
      subtaskTotal: 3,
      subtaskCompleted: 2,
      tags: [
        expect.objectContaining({
          id: "tag-1",
          name: "交付",
          color: "#6E7BF2",
          version: 2,
        }),
      ],
    });
  });

  it("preserves zero-minute and zero-order values", () => {
    expect(
      normalizeTask(taskPayload({ estimated_minutes: 0, manual_order: 0 })),
    ).toMatchObject({ estimatedMinutes: 0, manualOrder: 0 });
  });

  it("accepts all controlled statuses and rejects unknown values", () => {
    for (const status of [
      "todo",
      "in_progress",
      "blocked",
      "waiting_review",
      "done",
      "cancelled",
    ]) {
      expect(normalizeTask(taskPayload({ status })).status).toBe(status);
    }
    expect(() => normalizeTask(taskPayload({ status: "archived" }))).toThrow(
      ApiError,
    );
    expect(() =>
      normalizeTask(taskPayload({ review_policy: "automatic" })),
    ).toThrow(ApiError);
  });

  it("rejects invalid task payloads", () => {
    expect(() => normalizeTask(null)).toThrow(ApiError);
  });
});

describe("normalizeTag", () => {
  it("maps tag fields and normalizes the color", () => {
    expect(
      normalizeTag({
        id: "tag-1",
        name: "交付",
        color: "#6e7bf2",
        version: 2,
        created_at: "2026-08-20T00:00:00Z",
      }),
    ).toEqual({
      id: "tag-1",
      name: "交付",
      color: "#6E7BF2",
      version: 2,
      createdAt: "2026-08-20T00:00:00Z",
    });
  });

  it("rejects malformed tag objects", () => {
    expect(() => normalizeTag("交付")).toThrow(ApiError);
    expect(() =>
      normalizeTag({
        id: "tag-1",
        name: "交付",
        color: "purple",
        version: 1,
        created_at: "2026-08-20T00:00:00Z",
      }),
    ).toThrow(ApiError);
  });
});

describe("normalizeProject", () => {
  it("maps project aggregates and lifecycle actions", () => {
    const project = normalizeProject({
      id: "project-1",
      name: "客户门户",
      description: "交付新门户",
      client_id: null,
      status: "in_progress",
      start_date: "2026-08-01",
      due_date: "2026-09-01",
      amount_minor: 128000,
      color: "#6E7BF2",
      version: 3,
      created_at: "2026-08-01T00:00:00Z",
      updated_at: "2026-08-27T00:00:00Z",
      task_summary: {
        total: 4,
        completed: 2,
        in_progress: 1,
        remaining: 2,
        progress_percent: 50,
        actual_minutes: 95,
      },
      invoice_count: 0,
      available_actions: ["pause", "complete", "archive", "invalid"],
    });

    expect(project).toMatchObject({
      id: "project-1",
      status: "in_progress",
      amountMinor: 128000,
      version: 3,
      taskSummary: {
        total: 4,
        completed: 2,
        inProgress: 1,
        progressPercent: 50,
        actualMinutes: 95,
      },
      availableActions: ["pause", "complete", "archive"],
    });
  });

  it("rejects invalid project payloads", () => {
    expect(() => normalizeProject(undefined)).toThrow(ApiError);
  });
});

describe("paged option loaders", () => {
  it("loads every task and project page instead of truncating at 100", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = new URL(String(input), "http://local.test");
      const page = Number(url.searchParams.get("page"));
      const isProject = url.pathname.endsWith("/projects");
      const count = page === 1 ? 100 : 1;
      const data = Array.from({ length: count }, (_, index) =>
        isProject
          ? {
              id: `project-${(page - 1) * 100 + index + 1}`,
              name: `项目 ${index + 1}`,
              status: "planning",
            }
          : taskPayload({
              id: `task-${(page - 1) * 100 + index + 1}`,
              title: `任务 ${index + 1}`,
              status: "todo",
              priority: "P2",
            }),
      );
      return new Response(
        JSON.stringify({
          data,
          meta: { page, page_size: 100, total: 101 },
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    });
    vi.stubGlobal("fetch", fetchMock);

    await expect(getAllProjects({ sort: "name" })).resolves.toHaveLength(101);
    await expect(
      getAllTasks({ projectId: "018f0000-0000-7000-8000-000000000001" }),
    ).resolves.toHaveLength(101);
    expect(fetchMock).toHaveBeenCalledTimes(4);
  });
});

describe("task list requests", () => {
  it("serializes every supported server-side filter and repeated tag ids", async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL) =>
      jsonResponse({
        data: [taskPayload()],
        meta: { page: 3, page_size: 25, total: 61 },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await getTaskPage({
      page: 3,
      pageSize: 25,
      q: "  交付清单  ",
      kind: "work",
      status: "in_progress",
      priority: "P1",
      projectId: "project-1",
      tagIds: ["tag-1", " tag-2 ", "tag-1"],
      plannedDate: "2026-08-26",
      plannedFrom: "2026-08-01",
      plannedTo: "2026-08-31",
      parentTaskId: "parent-1",
      rootOnly: false,
      sort: " -updated_at,title ",
    });

    const url = new URL(
      String(fetchMock.mock.calls[0][0]),
      "http://local.test",
    );
    expect(Object.fromEntries(url.searchParams)).toMatchObject({
      page: "3",
      page_size: "25",
      q: "交付清单",
      kind: "work",
      status: "in_progress",
      priority: "P1",
      project_id: "project-1",
      planned_date: "2026-08-26",
      planned_from: "2026-08-01",
      planned_to: "2026-08-31",
      parent_task_id: "parent-1",
      root_only: "false",
      sort: "-updated_at,title",
    });
    expect(url.searchParams.getAll("tag_id")).toEqual(["tag-1", "tag-2"]);
    expect(result.meta).toEqual({ page: 3, pageSize: 25, total: 61 });
  });
});

describe("controlled task lifecycle", () => {
  it("creates tasks in the server-controlled initial state without a status field", async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, _init?: RequestInit) =>
        jsonResponse({ data: taskPayload({ status: "todo", version: 1 }) }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await createTask(
      {
        title: "新任务",
        priority: "P2",
        kind: "work",
      },
      "task-key",
    );

    const body = JSON.parse(String(fetchMock.mock.calls[0][1]?.body));
    expect(body).not.toHaveProperty("status");
    expect(
      new Headers(fetchMock.mock.calls[0][1]?.headers).get("Idempotency-Key"),
    ).toBe("task-key");
  });

  it("strictly normalizes task events and their page metadata", () => {
    expect(normalizeTaskWorkflowEvent(eventPayload())).toMatchObject({
      id: "event-1",
      action: "task_started",
      actor: { displayName: "我" },
      commandSeq: 1,
      previous: { status: "todo", version: 7 },
      current: { status: "in_progress", version: 8 },
      reason: null,
    });
    expect(
      normalizeTaskEventListResult({
        data: [eventPayload()],
        meta: { page: 1, page_size: 20, total: 1, task_version: 8 },
      }),
    ).toMatchObject({
      items: [expect.objectContaining({ action: "task_started" })],
      meta: { page: 1, pageSize: 20, total: 1, taskVersion: 8 },
    });
    expect(() =>
      normalizeTaskWorkflowEvent(eventPayload({ previous: "todo" })),
    ).toThrow(ApiError);
    expect(() =>
      normalizeTaskWorkflowEvent(eventPayload({ command_seq: 0 })),
    ).toThrow(ApiError);
  });

  it("gets a paged task event timeline", async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL) =>
      jsonResponse({
        data: [eventPayload()],
        meta: { page: 2, page_size: 10, total: 11, task_version: 8 },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await getTaskEvents("task-1", { page: 2, pageSize: 10 });

    const url = new URL(String(fetchMock.mock.calls[0][0]), "http://local");
    expect(url.pathname).toBe("/api/v1/tasks/task-1/events");
    expect(Object.fromEntries(url.searchParams)).toEqual({
      page: "2",
      page_size: "10",
    });
  });

  it("posts each explicit command with version, stable key, and exact body", async () => {
    const targets = {
      start: ["task_started", "in_progress"],
      block: ["task_blocked", "blocked"],
      unblock: ["task_unblocked", "todo"],
      complete: ["task_completed", "done"],
      cancel: ["task_cancelled", "cancelled"],
      reopen: ["task_reopened", "todo"],
    } as const;
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, _init?: RequestInit) => {
        const action = new URL(String(input), "http://local").pathname
          .split("/")
          .at(-1) as keyof typeof targets;
        const [eventAction, status] = targets[action];
        return jsonResponse({
          data: {
            task: taskPayload({ status, version: 8 }),
            event: eventPayload({
              action: eventAction,
              current: { status, version: 8 },
            }),
          },
        });
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    const commands = [
      { action: "start", expectedVersion: 7 },
      { action: "block", reason: "等待客户", expectedVersion: 7 },
      { action: "unblock", expectedVersion: 7 },
      { action: "complete", expectedVersion: 7 },
      { action: "cancel", reason: "范围取消", expectedVersion: 7 },
      { action: "reopen", expectedVersion: 7 },
    ] as const;
    for (const input of commands) {
      await executeTaskLifecycleCommand("task-1", input, `key-${input.action}`);
    }

    expect(fetchMock).toHaveBeenCalledTimes(6);
    commands.forEach((input, index) => {
      const [url, init] = fetchMock.mock.calls[index];
      expect(new URL(String(url), "http://local").pathname).toBe(
        `/api/v1/tasks/task-1/${input.action}`,
      );
      expect(init?.method).toBe("POST");
      expect(new Headers(init?.headers).get("If-Match")).toBe('"7"');
      expect(new Headers(init?.headers).get("Idempotency-Key")).toBe(
        `key-${input.action}`,
      );
      expect(JSON.parse(String(init?.body))).toEqual(
        input.action === "block" || input.action === "cancel"
          ? { reason: input.reason }
          : {},
      );
    });
  });
});

describe("versioned task writes", () => {
  it("sends If-Match for task patch and delete", async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, init?: RequestInit) =>
        init?.method === "DELETE"
          ? jsonResponse({})
          : jsonResponse({ data: taskPayload({ version: 8 }) }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await updateTask("task-1", {
      title: "整理交付清单",
      description: "确认最终文件",
      kind: "work",
      priority: "P1",
      projectId: "project-1",
      parentTaskId: null,
      completionCriteria: "文件齐全",
      tagIds: ["tag-1"],
      dueDate: null,
      plannedDate: "2026-08-26",
      estimatedMinutes: 0,
      expectedVersion: 7,
    });
    await deleteTask("task-1", 9);

    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(
      new Headers(fetchMock.mock.calls[0][1]?.headers).get("If-Match"),
    ).toBe('"7"');
    expect(
      new Headers(fetchMock.mock.calls[1][1]?.headers).get("If-Match"),
    ).toBe('"9"');
    expect(JSON.parse(String(fetchMock.mock.calls[0][1]?.body))).toMatchObject({
      estimated_minutes: 0,
      tag_ids: ["tag-1"],
      parent_task_id: null,
    });
  });

  it("rejects a patch without an observed task version", async () => {
    await expect(
      updateTask("task-1", {
        title: "整理交付清单",
        description: "",
        priority: "P2",
        dueDate: null,
        plannedDate: null,
        estimatedMinutes: null,
      }),
    ).rejects.toMatchObject({ code: "EXPECTED_VERSION_REQUIRED" });
  });
});

describe("tag requests", () => {
  const tagPayload = {
    id: "tag-1",
    name: "交付",
    color: "#6E7BF2",
    version: 2,
    created_at: "2026-08-20T00:00:00Z",
  };

  it("lists tags with paging, search, and sorting", async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL) =>
      jsonResponse({
        data: [tagPayload],
        meta: { page: 2, page_size: 20, total: 21 },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await getTags({
      page: 2,
      pageSize: 20,
      query: " 交付 ",
      sort: "-created_at",
    });
    const url = new URL(
      String(fetchMock.mock.calls[0][0]),
      "http://local.test",
    );
    expect(Object.fromEntries(url.searchParams)).toEqual({
      page: "2",
      page_size: "20",
      q: "交付",
      sort: "-created_at",
    });
    expect(result).toMatchObject({
      items: [{ id: "tag-1", name: "交付", version: 2 }],
      meta: { page: 2, pageSize: 20, total: 21 },
    });
  });

  it("supports idempotent create and versioned update/delete", async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, init?: RequestInit) => {
        if (init?.method === "DELETE") {
          return jsonResponse({
            data: { deleted_id: "tag-1", detached_tasks: 3 },
          });
        }
        return jsonResponse({ data: tagPayload });
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    await createTag({ name: "交付", color: "#6E7BF2" }, "tag-key");
    await updateTag("tag-1", {
      color: "#7582F4",
      expectedVersion: 2,
    });
    await expect(deleteTag("tag-1", 3)).resolves.toEqual({
      deletedId: "tag-1",
      detachedTasks: 3,
    });

    expect(
      new Headers(fetchMock.mock.calls[0][1]?.headers).get("Idempotency-Key"),
    ).toBe("tag-key");
    expect(
      new Headers(fetchMock.mock.calls[1][1]?.headers).get("If-Match"),
    ).toBe('"2"');
    expect(String(fetchMock.mock.calls[2][0])).toContain(
      "/api/v1/tags/tag-1?confirm=true",
    );
    expect(
      new Headers(fetchMock.mock.calls[2][1]?.headers).get("If-Match"),
    ).toBe('"3"');
  });
});
