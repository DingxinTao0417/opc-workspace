import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import {
  actorQueryKey,
  clientDetailQueryKey,
  clientFollowupQueryKey,
  clientQueryKey,
  inboxQueryKey,
  searchQueryKey,
  invalidateClientFollowupActionFacts,
} from "./hooks";

describe("AI client followup fact refresh", () => {
  it("invalidates Client, followup, Inbox/Today, Actor and search snapshots and rejects late pre-approval responses", async () => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const keys = [
      clientQueryKey,
      clientDetailQueryKey("client"),
      clientFollowupQueryKey("client"),
      [...inboxQueryKey, "list", { sourceEntityType: "client_followup" }],
      actorQueryKey,
      searchQueryKey,
    ];
    keys.forEach((key) => client.setQueryData(key, "current"));
    let release!: (value: string) => void;
    const pending = client
      .fetchQuery({
        queryKey: clientFollowupQueryKey("client"),
        queryFn: () =>
          new Promise<string>((resolve) => {
            release = resolve;
          }),
      })
      .catch(() => "cancelled");
    await invalidateClientFollowupActionFacts(client);
    release("stale planned followup");
    await pending;
    for (const key of keys) {
      expect(client.getQueryState(key)?.isInvalidated).toBe(true);
      expect(client.getQueryData(key)).toBe("current");
    }
    client.clear();
  });
});
