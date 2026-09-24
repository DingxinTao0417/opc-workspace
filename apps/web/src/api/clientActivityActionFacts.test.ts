import { QueryClient } from "@tanstack/react-query";
import { expect, it } from "vitest";
import {
  clientQueryKey,
  clientDetailQueryKey,
  clientActivityQueryKey,
  recentClientActivityQueryKey,
  searchQueryKey,
  invalidateClientActivityActionFacts,
} from "./hooks";

it("refreshes client detail/timeline/recent cards/search without accepting a late pre-approval response", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const keys = [
    clientQueryKey,
    clientDetailQueryKey("customer"),
    clientActivityQueryKey("customer"),
    recentClientActivityQueryKey,
    searchQueryKey,
  ];
  keys.forEach((key) => client.setQueryData(key, "current"));
  let release!: (value: string) => void;
  const pending = client
    .fetchQuery({
      queryKey: keys[3],
      queryFn: () =>
        new Promise<string>((resolve) => {
          release = resolve;
        }),
    })
    .catch(() => "cancelled");
  await invalidateClientActivityActionFacts(client);
  release("old activity snapshot");
  await pending;
  keys.forEach((key) => {
    expect(client.getQueryState(key)?.isInvalidated).toBe(true);
    expect(client.getQueryData(key)).toBe("current");
  });
  client.clear();
});
