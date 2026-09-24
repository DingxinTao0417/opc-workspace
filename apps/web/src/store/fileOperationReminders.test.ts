import { afterEach, describe, expect, it } from "vitest";
import {
  loadFileOperationReminders,
  outstandingFileOperationReminders,
  resetFileOperationRemindersForTests,
  useFileOperationReminders,
  writeFileOperationRemindersForTests,
} from "./fileOperationReminders";

const session = "018f0000-0000-7000-8000-000000000901";

afterEach(() => resetFileOperationRemindersForTests());

describe("file operation reminders", () => {
  it("records intent before the native call and forgets confirmed outcomes", () => {
    const id = useFileOperationReminders
      .getState()
      .begin("ai_project_file", session);
    expect(useFileOperationReminders.getState().reminders).toEqual([
      expect.objectContaining({
        id,
        source: "ai_project_file",
        status: "in_flight",
        sessionId: session,
      }),
    ]);
    expect(
      outstandingFileOperationReminders(
        useFileOperationReminders.getState().reminders,
      ),
    ).toHaveLength(0);
    useFileOperationReminders.getState().settle(id, "confirmed");
    expect(useFileOperationReminders.getState().reminders).toEqual([]);
    expect(loadFileOperationReminders()).toEqual([]);
  });

  it("keeps unobserved outcomes until a person dismisses them", () => {
    const store = useFileOperationReminders.getState();
    const uncertain = store.begin("agent_output_file");
    const recovery = store.begin("file_recovery");
    useFileOperationReminders.getState().settle(uncertain, "uncertain");
    useFileOperationReminders.getState().settle(recovery, "recovery_required");
    const outstanding = outstandingFileOperationReminders(
      useFileOperationReminders.getState().reminders,
    );
    expect(outstanding.map((reminder) => reminder.status).sort()).toEqual([
      "recovery_required",
      "uncertain",
    ]);
    useFileOperationReminders.getState().dismiss(uncertain);
    expect(
      useFileOperationReminders.getState().reminders.map((item) => item.id),
    ).toEqual([recovery]);
  });

  it("rediscovers an operation that was in flight when the app ended", () => {
    const id = useFileOperationReminders
      .getState()
      .begin("file_replacement_restore");
    const reloaded = loadFileOperationReminders();
    expect(reloaded).toEqual([
      expect.objectContaining({ id, status: "interrupted" }),
    ]);
    expect(outstandingFileOperationReminders(reloaded)).toHaveLength(1);
  });

  it("drops malformed or content-bearing stored entries", () => {
    writeFileOperationRemindersForTests(
      JSON.stringify([
        {
          id: "018f0000-0000-7000-8000-000000000911",
          source: "file_recovery",
          status: "uncertain",
          startedAt: 1,
          path: "C:\\secret.txt",
        },
        {
          id: "not-a-uuid",
          source: "file_recovery",
          status: "uncertain",
          startedAt: 1,
        },
        {
          id: "018f0000-0000-7000-8000-000000000912",
          source: "shell",
          status: "uncertain",
          startedAt: 1,
        },
        {
          id: "018f0000-0000-7000-8000-000000000913",
          source: "file_recovery",
          status: "uncertain",
          startedAt: 2,
        },
      ]),
    );
    expect(loadFileOperationReminders().map((item) => item.id)).toEqual([
      "018f0000-0000-7000-8000-000000000913",
    ]);
    writeFileOperationRemindersForTests("{");
    expect(loadFileOperationReminders()).toEqual([]);
  });
});
