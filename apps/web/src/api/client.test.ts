import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  getAllProjects,
  getAllTasks,
  normalizeProject,
  normalizeTask,
  resetRuntimeConnection,
} from "./client";

afterEach(() => {
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

describe("normalizeTask", () => {
  it("maps the versioned API snake_case fields", () => {
    const task = normalizeTask({
      id: "task-1",
      title: "整理交付清单",
      description: "确认文件与时间点",
      status: "in_progress",
      priority: "P1",
      project_id: "project-1",
      project_name: "客户门户",
      due_date: "2026-08-26T18:00:00Z",
      planned_date: "2026-08-26",
      estimated_minutes: 45,
      actual_minutes: 12,
      created_at: "2026-08-26T10:00:00Z",
      updated_at: "2026-08-26T10:10:00Z",
      completed_at: null,
    });

    expect(task).toMatchObject({
      id: "task-1",
      status: "in_progress",
      priority: "P1",
      projectId: "project-1",
      projectName: "客户门户",
      estimatedMinutes: 45,
      actualMinutes: 12,
    });
  });

  it("rejects invalid task payloads", () => {
    expect(() => normalizeTask(null)).toThrow(ApiError);
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
      const prefix = isProject ? "project" : "task";
      const count = page === 1 ? 100 : 1;
      const data = Array.from({ length: count }, (_, index) => ({
        id: `${prefix}-${(page - 1) * 100 + index + 1}`,
        ...(isProject
          ? { name: `项目 ${index + 1}`, status: "planning" }
          : { title: `任务 ${index + 1}`, status: "todo", priority: "P2" }),
      }));
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
