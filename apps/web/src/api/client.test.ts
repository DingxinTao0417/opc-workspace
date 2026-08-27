import { describe, expect, it } from "vitest";
import { ApiError, normalizeTask } from "./client";

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
