import { QueryClient } from "@tanstack/react-query";
import { expect, it } from "vitest";
import {
  financialEntryQueryKey,
  financialEntryDetailQueryKey,
  incomeStatsQueryKey,
  invalidateFinancialActionFacts,
} from "./hooks";

it("cancels stale ledger and shared statistics requests before refreshing financial facts", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const keys = [
    [...financialEntryQueryKey, "list", {}],
    financialEntryDetailQueryKey("entry"),
    [...incomeStatsQueryKey, { currency: "CNY" }],
  ];
  keys.forEach((key) => client.setQueryData(key, "current"));
  const releases: Array<(value: string) => void> = [];
  const pending = keys.map((queryKey) =>
    client
      .fetchQuery({
        queryKey,
        queryFn: () =>
          new Promise<string>((resolve) => {
            releases.push(resolve);
          }),
      })
      .catch(() => "cancelled"),
  );
  await invalidateFinancialActionFacts(client);
  releases.forEach((release) => release("old snapshot"));
  await Promise.all(pending);
  keys.forEach((key) => {
    expect(client.getQueryState(key)?.isInvalidated).toBe(true);
    expect(client.getQueryData(key)).toBe("current");
  });
  client.clear();
});
