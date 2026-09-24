import { create } from "zustand";
import { isWorkspaceIdentity } from "../lib/focusReportLocation";

/**
 * Local, content-free reminders for single-file operations whose outcome this
 * window did not observe. They are written BEFORE the native call so an app
 * exit mid-operation is rediscovered on the next start. They are observations
 * of the UI, not execution receipts: recovery records and the current file
 * remain the only authority, and nothing here can retry an operation.
 */
const storageKey = "opc-file-operation-reminders-v1";
const maxReminders = 20;

export type FileOperationSource =
  | "ai_project_file"
  | "agent_output_file"
  | "file_recovery"
  | "file_replacement_restore";

export type FileOperationReminderStatus =
  "in_flight" | "interrupted" | "uncertain" | "recovery_required";

export interface FileOperationReminder {
  id: string;
  source: FileOperationSource;
  status: FileOperationReminderStatus;
  startedAt: number;
  settledAt?: number;
  sessionId?: string;
}

export type FileOperationSettlement =
  "confirmed" | "uncertain" | "recovery_required";

const sources: readonly FileOperationSource[] = [
  "ai_project_file",
  "agent_output_file",
  "file_recovery",
  "file_replacement_restore",
];
const statuses: readonly FileOperationReminderStatus[] = [
  "in_flight",
  "interrupted",
  "uncertain",
  "recovery_required",
];

const memoryStorage = new Map<string, string>();

function storage(): Pick<Storage, "getItem" | "setItem" | "removeItem"> {
  try {
    const local = window.localStorage;
    if (local && typeof local.getItem === "function") return local;
  } catch {
    // Fall through to memory when browser storage is unavailable.
  }
  return {
    getItem: (key) => memoryStorage.get(key) ?? null,
    setItem: (key, value) => void memoryStorage.set(key, value),
    removeItem: (key) => void memoryStorage.delete(key),
  };
}

function timestamp(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value > 0;
}

function isReminder(value: unknown): value is FileOperationReminder {
  if (typeof value !== "object" || value === null || Array.isArray(value))
    return false;
  const candidate = value as Record<string, unknown>;
  const allowed = [
    "id",
    "source",
    "status",
    "startedAt",
    "settledAt",
    "sessionId",
  ];
  return (
    Object.keys(candidate).every((key) => allowed.includes(key)) &&
    typeof candidate.id === "string" &&
    isWorkspaceIdentity(candidate.id) &&
    sources.includes(candidate.source as FileOperationSource) &&
    statuses.includes(candidate.status as FileOperationReminderStatus) &&
    timestamp(candidate.startedAt) &&
    (candidate.settledAt === undefined || timestamp(candidate.settledAt)) &&
    (candidate.sessionId === undefined ||
      (typeof candidate.sessionId === "string" &&
        isWorkspaceIdentity(candidate.sessionId)))
  );
}

/** Any operation still in flight when this window starts ended unobserved. */
export function loadFileOperationReminders(): FileOperationReminder[] {
  let parsed: unknown;
  try {
    parsed = JSON.parse(storage().getItem(storageKey) ?? "[]");
  } catch {
    return [];
  }
  if (!Array.isArray(parsed)) return [];
  return parsed
    .filter(isReminder)
    .map((reminder) =>
      reminder.status === "in_flight"
        ? { ...reminder, status: "interrupted" as const }
        : reminder,
    )
    .sort((left, right) => right.startedAt - left.startedAt)
    .slice(0, maxReminders);
}

function save(reminders: FileOperationReminder[]) {
  try {
    storage().setItem(storageKey, JSON.stringify(reminders));
  } catch {
    // A blocked storage must not block the reviewed native operation.
  }
}

interface FileOperationReminderState {
  reminders: FileOperationReminder[];
  begin: (source: FileOperationSource, sessionId?: string) => string;
  settle: (id: string, settlement: FileOperationSettlement) => void;
  dismiss: (id: string) => void;
}

export const useFileOperationReminders = create<FileOperationReminderState>(
  (set, get) => {
    const commit = (reminders: FileOperationReminder[]) => {
      const bounded = reminders.slice(0, maxReminders);
      save(bounded);
      set({ reminders: bounded });
    };
    return {
      reminders: loadFileOperationReminders(),
      begin: (source, sessionId) => {
        const id = crypto.randomUUID();
        commit([
          {
            id,
            source,
            status: "in_flight",
            startedAt: Date.now(),
            ...(sessionId && isWorkspaceIdentity(sessionId)
              ? { sessionId }
              : {}),
          },
          ...get().reminders,
        ]);
        return id;
      },
      settle: (id, settlement) => {
        const reminders = get().reminders;
        if (!reminders.some((reminder) => reminder.id === id)) return;
        commit(
          settlement === "confirmed"
            ? reminders.filter((reminder) => reminder.id !== id)
            : reminders.map((reminder) =>
                reminder.id === id
                  ? { ...reminder, status: settlement, settledAt: Date.now() }
                  : reminder,
              ),
        );
      },
      dismiss: (id) =>
        commit(get().reminders.filter((reminder) => reminder.id !== id)),
    };
  },
);

/** Reminders a person must still check; operations in this window excluded. */
export function outstandingFileOperationReminders(
  reminders: FileOperationReminder[],
) {
  return reminders.filter((reminder) => reminder.status !== "in_flight");
}

export function writeFileOperationRemindersForTests(raw: string) {
  storage().setItem(storageKey, raw);
}

export function resetFileOperationRemindersForTests() {
  memoryStorage.delete(storageKey);
  try {
    window.localStorage.removeItem(storageKey);
  } catch {
    // Test helpers must work when browser storage is unavailable.
  }
  useFileOperationReminders.setState({ reminders: [] });
}
