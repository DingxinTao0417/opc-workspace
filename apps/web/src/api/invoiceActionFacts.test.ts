import { QueryClient } from "@tanstack/react-query";
import { expect, it } from "vitest";
import {
  invoiceQueryKey,
  invoiceDetailQueryKey,
  invoicePdfQueryKey,
  financialEntryQueryKey,
  incomeStatsQueryKey,
  inboxQueryKey,
  projectQueryKey,
  searchQueryKey,
  automationRulesQueryKey,
  automationRunsQueryKey,
  taskQueryKey,
  invalidateInvoiceActionFacts,
} from "./hooks";

it.each([false, true])(
  "cancels late invoice, income and related responses before refresh (overdue=%s)",
  async (overdue) => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const keys = [
      invoiceQueryKey,
      invoiceDetailQueryKey("invoice"),
      invoicePdfQueryKey("invoice"),
      financialEntryQueryKey,
      incomeStatsQueryKey,
      inboxQueryKey,
      projectQueryKey,
      searchQueryKey,
      ...(overdue
        ? [automationRulesQueryKey, automationRunsQueryKey, taskQueryKey]
        : []),
    ];
    keys.forEach((key) => client.setQueryData(key, "current"));
    const releases: Array<(v: string) => void> = [];
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
    await invalidateInvoiceActionFacts(client, overdue);
    releases.forEach((release) => release("stale invoice facts"));
    await Promise.all(pending);
    keys.forEach((key) => {
      expect(client.getQueryState(key)?.isInvalidated, String(key)).toBe(true);
      expect(client.getQueryData(key), String(key)).toBe("current");
    });
    client.clear();
  },
);
