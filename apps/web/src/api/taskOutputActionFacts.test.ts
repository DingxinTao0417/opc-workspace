import { QueryClient } from "@tanstack/react-query";
import { expect, it } from "vitest";
import {
  invalidateTaskOutputActionFacts,
  taskDetailQueryKey,
  taskSubmissionQueryKey,
  taskArtifactQueryKey,
  taskArtifactDetailQueryKey,
  taskAssignmentQueryKey,
  taskEventQueryKey,
  projectArtifactQueryKey,
  inboxTaskRelationQueryKey,
  inboxStatsQueryKey,
  searchQueryKey,
  roadmapMilestoneQueryKey,
  contentItemQueryKey,
} from "./hooks";

it("refreshes submission, artifact detail and dependent business facts without accepting late pre-review snapshots", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const keys = [
    taskDetailQueryKey("task"),
    taskDetailQueryKey("parent"),
    [...taskSubmissionQueryKey("task"), "history"],
    [...taskArtifactQueryKey("task"), { submissionId: "submission" }],
    taskArtifactDetailQueryKey("artifact"),
    taskAssignmentQueryKey("task"),
    taskEventQueryKey("task"),
    projectArtifactQueryKey("project"),
    inboxTaskRelationQueryKey("inbox"),
    inboxStatsQueryKey,
    [...searchQueryKey, { query: "task" }],
    [...roadmapMilestoneQueryKey, {}],
    [...contentItemQueryKey, {}],
    ["stats", "today"],
  ];
  keys.forEach((key) => client.setQueryData(key, "current"));
  const unrelated = ["ai", "messages", "session"];
  client.setQueryData(unrelated, "conversation");
  const releases: Array<(value: string) => void> = [];
  const pending = keys.map((queryKey) =>
    client
      .fetchQuery({
        queryKey,
        // Deliberately ignore AbortSignal: cancellation must also protect the cache.
        queryFn: () => new Promise<string>((resolve) => releases.push(resolve)),
      })
      .catch(() => "cancelled"),
  );
  await invalidateTaskOutputActionFacts(client);
  releases.forEach((resolve) => resolve("old pending_review snapshot"));
  await Promise.all(pending);
  for (const key of keys) {
    expect(client.getQueryState(key)?.isInvalidated).toBe(true);
    expect(client.getQueryData(key)).toBe("current");
  }
  expect(client.getQueryState(unrelated)?.isInvalidated).toBe(false);
  expect(client.getQueryData(unrelated)).toBe("conversation");
  client.clear();
});
