import { describe, expect, it } from "vitest";
import type { Task } from "../types/models";
import {
  formatDueDayHeader,
  formatTaskDue,
  localDayKey,
  taskDueDayKey,
  taskDueState,
  taskDueStateClass,
} from "./taskDue";

const NOW = new Date(2026, 9, 29, 10, 0, 0).getTime();

function dueTask(
  dueDate: string | null,
  status: Task["status"] = "todo",
): Pick<Task, "dueDate" | "status"> {
  return { dueDate, status };
}

describe("formatTaskDue", () => {
  it("labels today and tomorrow in local time", () => {
    expect(
      formatTaskDue(new Date(2026, 9, 29, 18, 30).toISOString(), NOW),
    ).toBe("今天 18:30");
    expect(formatTaskDue(new Date(2026, 9, 30, 9, 5).toISOString(), NOW)).toBe(
      "明天 09:05",
    );
  });

  it("falls back to month/day and adds the year across years", () => {
    expect(formatTaskDue(new Date(2026, 10, 3, 8, 0).toISOString(), NOW)).toBe(
      "11/03 08:00",
    );
    expect(formatTaskDue(new Date(2027, 0, 2, 8, 5).toISOString(), NOW)).toBe(
      "2027/01/02 08:05",
    );
  });

  it("returns null for unparsable values", () => {
    expect(formatTaskDue("not-a-date", NOW)).toBeNull();
  });
});

describe("taskDueDayKey", () => {
  it("uses the local calendar day, not the stored UTC prefix", () => {
    const local = new Date(2026, 9, 30, 1, 0);
    expect(taskDueDayKey(dueTask(local.toISOString()))).toBe(
      `${local.getFullYear()}-10-30`,
    );
  });

  it("returns null without a due date or with an invalid one", () => {
    expect(taskDueDayKey(dueTask(null))).toBeNull();
    expect(taskDueDayKey(dueTask("nope"))).toBeNull();
  });
});

describe("taskDueState", () => {
  it("marks active tasks overdue at or past the due moment", () => {
    expect(taskDueState(dueTask(new Date(NOW).toISOString()), NOW)).toBe(
      "overdue",
    );
    expect(taskDueState(dueTask(new Date(NOW - 1).toISOString()), NOW)).toBe(
      "overdue",
    );
  });

  it("marks the 24h lead window due_soon, inclusive of the boundary", () => {
    expect(
      taskDueState(dueTask(new Date(NOW + 86_400_000).toISOString()), NOW),
    ).toBe("due_soon");
    expect(
      taskDueState(dueTask(new Date(NOW + 86_400_001).toISOString()), NOW),
    ).toBeNull();
  });

  it("ignores terminal statuses and missing due dates", () => {
    expect(
      taskDueState(dueTask(new Date(NOW - 1).toISOString(), "done"), NOW),
    ).toBeNull();
    expect(
      taskDueState(dueTask(new Date(NOW - 1).toISOString(), "cancelled"), NOW),
    ).toBeNull();
    expect(taskDueState(dueTask(null), NOW)).toBeNull();
  });
});

describe("taskDueStateClass", () => {
  it("maps states to modifier classes", () => {
    expect(taskDueStateClass("overdue")).toBe(" task-due-overdue");
    expect(taskDueStateClass("due_soon")).toBe(" task-due-soon");
    expect(taskDueStateClass(null)).toBe("");
  });
});

describe("formatDueDayHeader", () => {
  it("labels today, tomorrow and plain dates with the weekday", () => {
    expect(formatDueDayHeader(localDayKey(NOW), NOW)).toBe("今天 周四");
    expect(formatDueDayHeader(localDayKey(NOW + 86_400_000), NOW)).toBe(
      "明天 周五",
    );
    expect(formatDueDayHeader("2026-11-03", NOW)).toBe("11月3日 周二");
  });

  it("passes unknown keys through", () => {
    expect(formatDueDayHeader("abc", NOW)).toBe("abc");
  });
});
