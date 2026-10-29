import { beforeEach, describe, expect, it } from "vitest";
import {
  clearTaskViewMode,
  readTaskViewMode,
  rememberTaskViewMode,
  sanitizeTaskViewMode,
  TASK_VIEW_STORAGE_KEY,
} from "./taskViewMode";

function testStorage(): Storage {
  const backing = new Map<string, string>();
  return {
    get length() {
      return backing.size;
    },
    clear: () => backing.clear(),
    getItem: (key) => backing.get(key) ?? null,
    key: (index) => [...backing.keys()][index] ?? null,
    removeItem: (key) => {
      backing.delete(key);
    },
    setItem: (key, value) => {
      backing.set(key, value);
    },
  };
}

beforeEach(() => {
  Object.defineProperty(window, "localStorage", {
    configurable: true,
    value: testStorage(),
  });
});

describe("sanitizeTaskViewMode", () => {
  it("accepts the three modes and falls back otherwise", () => {
    expect(sanitizeTaskViewMode("list")).toBe("list");
    expect(sanitizeTaskViewMode("board")).toBe("board");
    expect(sanitizeTaskViewMode("agenda")).toBe("agenda");
    expect(sanitizeTaskViewMode("calendar")).toBe("list");
    expect(sanitizeTaskViewMode(null)).toBe("list");
    expect(sanitizeTaskViewMode("nope", "agenda")).toBe("agenda");
  });
});

describe("task view memory", () => {
  it("reads the default list before anything is remembered", () => {
    expect(readTaskViewMode()).toBe("list");
  });

  it("round-trips the remembered mode", () => {
    rememberTaskViewMode("agenda");
    expect(readTaskViewMode()).toBe("agenda");
    rememberTaskViewMode("board");
    expect(readTaskViewMode()).toBe("board");
  });

  it("ignores foreign values written to the key", () => {
    window.localStorage.setItem(TASK_VIEW_STORAGE_KEY, "calendar");
    expect(readTaskViewMode()).toBe("list");
  });

  it("clears back to the default", () => {
    rememberTaskViewMode("agenda");
    clearTaskViewMode();
    expect(readTaskViewMode()).toBe("list");
  });
});
