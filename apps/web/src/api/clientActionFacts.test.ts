import { QueryClient } from "@tanstack/react-query";
import { expect, it } from "vitest";
import {
  actorQueryKey,
  clientQueryKey,
  financialEntryQueryKey,
  invalidateClientActionFacts,
  invalidateClientContactActionFacts,
  invalidatePersonActionFacts,
  inboxQueryKey,
  invoiceQueryKey,
  projectQueryKey,
  searchQueryKey,
  taskQueryKey,
} from "./hooks";

it("refreshes every read model that projects client names or status", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const keys = [
    clientQueryKey,
    projectQueryKey,
    invoiceQueryKey,
    financialEntryQueryKey,
    searchQueryKey,
  ];
  keys.forEach((key) => client.setQueryData(key, "current"));
  let release!: (value: string) => void;
  const pending = client
    .fetchQuery({
      queryKey: invoiceQueryKey,
      queryFn: () =>
        new Promise<string>((resolve) => {
          release = resolve;
        }),
    })
    .catch(() => "cancelled");
  await invalidateClientActionFacts(client);
  release("old client label");
  await pending;
  keys.forEach((key) => {
    expect(client.getQueryState(key)?.isInvalidated).toBe(true);
    expect(client.getQueryData(key)).toBe("current");
  });
  client.clear();
});

it("refreshes Client and Actor facts after changing a contact relationship", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const keys = [clientQueryKey, actorQueryKey, searchQueryKey];
  keys.forEach((key) => client.setQueryData(key, "current"));
  await invalidateClientContactActionFacts(client);
  keys.forEach((key) => {
    expect(client.getQueryState(key)?.isInvalidated).toBe(true);
  });
  client.clear();
});

it("refreshes every read model that projects a local person", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const keys = [
    actorQueryKey,
    taskQueryKey,
    clientQueryKey,
    inboxQueryKey,
    searchQueryKey,
  ];
  keys.forEach((key) => client.setQueryData(key, "current"));
  await invalidatePersonActionFacts(client);
  keys.forEach((key) => {
    expect(client.getQueryState(key)?.isInvalidated).toBe(true);
  });
  client.clear();
});
