import { afterEach, describe, expect, it } from "vitest";
import {
  clearCommandRecentsForTests,
  loadCommandRecents,
  recordCommandRecent,
  removeCommandRecent,
} from "./commandRecents";

afterEach(clearCommandRecentsForTests);

describe("command recents", () => {
  it("stores only bounded non-sensitive command and resource identities", () => {
    const now = Date.now();
    let recents = recordCommandRecent(
      [],
      { kind: "command", commandId: "today" },
      now - 20,
    );
    recents = recordCommandRecent(
      recents,
      { kind: "resource", resourceType: "task", resourceId: "task-1" },
      now - 10,
    );
    recents = recordCommandRecent(
      recents,
      { kind: "command", commandId: "today" },
      now,
    );

    expect(recents).toEqual([
      { kind: "command", commandId: "today", usedAt: now },
      {
        kind: "resource",
        resourceType: "task",
        resourceId: "task-1",
        usedAt: now - 10,
      },
    ]);
    expect(loadCommandRecents(now)).toEqual(recents);
  });

  it("drops expired and explicitly removed entries", () => {
    const now = Date.now();
    let recents = recordCommandRecent(
      [],
      { kind: "command", commandId: "today" },
      now - 91 * 24 * 60 * 60 * 1000,
    );
    recents = recordCommandRecent(
      recents,
      { kind: "resource", resourceType: "task", resourceId: "task-1" },
      now,
    );
    expect(recents).toEqual([
      {
        kind: "resource",
        resourceType: "task",
        resourceId: "task-1",
        usedAt: now,
      },
    ]);
    expect(
      removeCommandRecent(recents, {
        kind: "resource",
        resourceType: "task",
        resourceId: "task-1",
      }),
    ).toEqual([]);
  });
});
