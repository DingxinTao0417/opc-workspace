import type { SearchResourceType } from "../types/models";

const commandRecentsStorageKey = "opc-command-recents-v1";
const maxCommandRecents = 8;
const recentRetentionMs = 90 * 24 * 60 * 60 * 1000;

export type CommandRecent =
  | {
      kind: "command";
      commandId: string;
      usedAt: number;
    }
  | {
      kind: "resource";
      resourceType: SearchResourceType;
      resourceId: string;
      usedAt: number;
    };

export type CommandRecentInput =
  | { kind: "command"; commandId: string }
  | {
      kind: "resource";
      resourceType: SearchResourceType;
      resourceId: string;
    };

const memoryStorage = new Map<string, string>();

function getStorage(): Pick<Storage, "getItem" | "setItem"> {
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
    setItem: (key, value) => memoryStorage.set(key, value),
  };
}

function isSearchResourceType(value: unknown): value is SearchResourceType {
  return (
    value === "task" ||
    value === "project" ||
    value === "client" ||
    value === "inbox_item"
  );
}

function isRecent(value: unknown): value is CommandRecent {
  if (typeof value !== "object" || value === null) return false;
  const candidate = value as Record<string, unknown>;
  if (
    typeof candidate.usedAt !== "number" ||
    !Number.isFinite(candidate.usedAt) ||
    candidate.usedAt <= 0
  ) {
    return false;
  }
  if (candidate.kind === "command") {
    return (
      typeof candidate.commandId === "string" && candidate.commandId.length > 0
    );
  }
  return (
    candidate.kind === "resource" &&
    isSearchResourceType(candidate.resourceType) &&
    typeof candidate.resourceId === "string" &&
    candidate.resourceId.length > 0
  );
}

function recentKey(recent: CommandRecent): string {
  return recent.kind === "command"
    ? `command:${recent.commandId}`
    : `resource:${recent.resourceType}:${recent.resourceId}`;
}

function normalizeRecents(value: unknown, now = Date.now()): CommandRecent[] {
  if (!Array.isArray(value)) return [];
  const minimumTimestamp = now - recentRetentionMs;
  const deduplicated = new Map<string, CommandRecent>();
  for (const candidate of value) {
    if (!isRecent(candidate) || candidate.usedAt < minimumTimestamp) continue;
    const key = recentKey(candidate);
    const existing = deduplicated.get(key);
    if (!existing || candidate.usedAt > existing.usedAt) {
      deduplicated.set(key, candidate);
    }
  }
  return [...deduplicated.values()]
    .sort((left, right) => right.usedAt - left.usedAt)
    .slice(0, maxCommandRecents);
}

export function loadCommandRecents(now = Date.now()): CommandRecent[] {
  let parsed: unknown;
  try {
    const value = getStorage().getItem(commandRecentsStorageKey);
    parsed = value ? JSON.parse(value) : [];
  } catch {
    return [];
  }
  return normalizeRecents(parsed, now);
}

export function saveCommandRecents(recents: CommandRecent[]): CommandRecent[] {
  const normalized = normalizeRecents(recents);
  try {
    getStorage().setItem(commandRecentsStorageKey, JSON.stringify(normalized));
  } catch {
    // A blocked browser storage must not prevent commands from running.
  }
  return normalized;
}

export function recordCommandRecent(
  recents: CommandRecent[],
  recent: CommandRecentInput,
  now = Date.now(),
): CommandRecent[] {
  return saveCommandRecents([...recents, { ...recent, usedAt: now }]);
}

export function removeCommandRecent(
  recents: CommandRecent[],
  recent: CommandRecentInput,
): CommandRecent[] {
  const normalized = recents.filter((candidate) => {
    if (candidate.kind !== recent.kind) return true;
    if (candidate.kind === "command" && recent.kind === "command") {
      return candidate.commandId !== recent.commandId;
    }
    if (candidate.kind === "resource" && recent.kind === "resource") {
      return (
        candidate.resourceType !== recent.resourceType ||
        candidate.resourceId !== recent.resourceId
      );
    }
    return true;
  });
  return saveCommandRecents(normalized);
}

export function clearCommandRecentsForTests() {
  memoryStorage.delete(commandRecentsStorageKey);
  try {
    window.localStorage.removeItem(commandRecentsStorageKey);
  } catch {
    // Test helpers must work when browser storage is unavailable.
  }
}
