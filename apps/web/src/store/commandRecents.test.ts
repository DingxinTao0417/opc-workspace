import { afterEach, describe, expect, it } from "vitest";
import {
  clearCommandRecentsForTests,
  loadCommandRecents,
  recordCommandRecent,
  removeCommandRecent,
  saveCommandRecents,
  type CommandRecent,
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

  it("keeps all supported resource types and drops malformed stored identities", () => {
    const now = Date.now();
    expect(
      saveCommandRecents([
        {
          kind: "resource",
          resourceType: "invoice",
          resourceId: "invoice-1",
          usedAt: now,
        },
        {
          kind: "resource",
          resourceType: "roadmap_milestone",
          resourceId: "milestone-1",
          usedAt: now - 1,
        },
        {
          kind: "resource",
          resourceType: "content_item",
          resourceId: "content-1",
          usedAt: now - 2,
        },
        {
          kind: "resource",
          resourceType: "knowledge_item",
          resourceId: "unsupported-1",
          usedAt: now - 3,
        },
        {
          kind: "resource",
          resourceType: "invoice",
          resourceId: "",
          usedAt: now - 4,
        },
      ] as unknown as CommandRecent[]),
    ).toEqual([
      {
        kind: "resource",
        resourceType: "invoice",
        resourceId: "invoice-1",
        usedAt: now,
      },
      {
        kind: "resource",
        resourceType: "roadmap_milestone",
        resourceId: "milestone-1",
        usedAt: now - 1,
      },
      {
        kind: "resource",
        resourceType: "content_item",
        resourceId: "content-1",
        usedAt: now - 2,
      },
    ]);
    expect(loadCommandRecents(now)).toEqual([
      {
        kind: "resource",
        resourceType: "invoice",
        resourceId: "invoice-1",
        usedAt: now,
      },
      {
        kind: "resource",
        resourceType: "roadmap_milestone",
        resourceId: "milestone-1",
        usedAt: now - 1,
      },
      {
        kind: "resource",
        resourceType: "content_item",
        resourceId: "content-1",
        usedAt: now - 2,
      },
    ]);
  });
});
