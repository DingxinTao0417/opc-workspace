export type TaskViewMode = "list" | "board" | "agenda";

export const TASK_VIEW_STORAGE_KEY = "opc-task-view-v1";

const memoryStorage = new Map<string, string>();

function getStorage(): Pick<Storage, "getItem" | "setItem" | "removeItem"> {
  try {
    const storage = window.localStorage;
    if (
      storage &&
      typeof storage.getItem === "function" &&
      typeof storage.setItem === "function"
    ) {
      return storage;
    }
  } catch {
    // Fall through to an in-memory store for unavailable browser storage.
  }
  return {
    getItem: (key) => memoryStorage.get(key) ?? null,
    setItem: (key, value) => {
      memoryStorage.set(key, value);
    },
    removeItem: (key) => {
      memoryStorage.delete(key);
    },
  };
}

export function sanitizeTaskViewMode(
  value: unknown,
  fallback: TaskViewMode = "list",
): TaskViewMode {
  return value === "list" || value === "board" || value === "agenda"
    ? value
    : fallback;
}

export function readTaskViewMode(): TaskViewMode {
  return sanitizeTaskViewMode(getStorage().getItem(TASK_VIEW_STORAGE_KEY));
}

export function rememberTaskViewMode(view: TaskViewMode): void {
  getStorage().setItem(TASK_VIEW_STORAGE_KEY, view);
}

export function clearTaskViewMode(): void {
  getStorage().removeItem(TASK_VIEW_STORAGE_KEY);
}
