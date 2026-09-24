import { QueryClient } from "@tanstack/react-query";
import { expect, it } from "vitest";
import {
  automationRulesQueryKey,
  automationRunsQueryKey,
  automationRunDetailQueryKey,
  inboxQueryKey,
  reminderQueryKey,
  invalidateAutomationActionFacts,
  taskQueryKey,
  projectQueryKey,
  searchQueryKey,
  roadmapMilestoneQueryKey,
  contentItemQueryKey,
} from "./hooks";

it.each([false, true])(
  "invalidates automation and effect read models without accepting late responses (retry=%s)",
  async (retry) => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const keys = [
      automationRulesQueryKey,
      automationRunsQueryKey,
      automationRunDetailQueryKey("run"),
      inboxQueryKey,
      reminderQueryKey,
      ...(retry
        ? [
            taskQueryKey,
            projectQueryKey,
            searchQueryKey,
            roadmapMilestoneQueryKey,
            contentItemQueryKey,
            ["stats", "today"],
          ]
        : []),
    ];
    keys.forEach((key) => client.setQueryData(key, "current"));
    let release!: (value: string) => void;
    const pending = client
      .fetchQuery({
        queryKey: keys[0],
        queryFn: () =>
          new Promise<string>((resolve) => {
            release = resolve;
          }),
      })
      .catch(() => "cancelled");
    await invalidateAutomationActionFacts(client, retry);
    release("old disabled rule");
    await pending;
    keys.forEach((key) => {
      expect(client.getQueryState(key)?.isInvalidated).toBe(true);
      expect(client.getQueryData(key)).toBe("current");
    });
    client.clear();
  },
);
