import type {
  InvoiceInput,
  InvoiceListParams,
  InvoicePdfMetadata,
  TransitionInvoiceInput,
  UpdateInvoiceInput,
} from "../../types/models";
import {
  ApiError,
  createInvoice,
  deleteInvoice,
  downloadInvoicePdf,
  generateInvoicePdf,
  getInvoice,
  getInvoicePdf,
  getInvoices,
  transitionInvoice,
  updateInvoice,
} from "../client";
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useRef } from "react";
import {
  invalidateInvoiceReadModels,
  invalidateTaskFacts,
} from "./invalidation";
import {
  automationQueryKey,
  invoiceDetailQueryKey,
  invoicePdfQueryKey,
  invoiceQueryKey,
} from "./queryKeys";

export function useInvoicesQuery(
  input: InvoiceListParams = {},
  enabled = true,
) {
  return useQuery({
    queryKey: [...invoiceQueryKey, "list", input],
    queryFn: () => getInvoices(input),
    enabled,
    placeholderData: keepPreviousData,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useInvoiceQuery(id: string | null, fresh = false) {
  return useQuery({
    queryKey: invoiceDetailQueryKey(id ?? "missing"),
    queryFn: ({ signal }) => getInvoice(id!, signal),
    enabled: Boolean(id),
    retry: 1,
    ...(fresh
      ? { refetchOnMount: "always" as const, staleTime: 0, gcTime: 0 }
      : {}),
  });
}

export function useInvoicePdfQuery(id: string | null) {
  return useQuery<InvoicePdfMetadata | null>({
    queryKey: invoicePdfQueryKey(id ?? "missing"),
    queryFn: () => getInvoicePdf(id!),
    enabled: Boolean(id),
    retry: 1,
  });
}

export function useGenerateInvoicePdf() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: ({
      id,
      expectedVersion,
    }: {
      id: string;
      expectedVersion: number;
    }) => {
      const fingerprint = JSON.stringify({ id, expectedVersion });
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return generateInvoicePdf(id, expectedVersion, attempt.current.key);
    },
    onSuccess: (metadata) => {
      attempt.current = null;
      queryClient.setQueryData(
        invoicePdfQueryKey(metadata.invoiceId),
        metadata,
      );
    },
    onError: async (error, variables) => {
      if (error instanceof ApiError) {
        if (
          error.code === "IDEMPOTENCY_REPLAY_UNAVAILABLE" ||
          error.code === "IDEMPOTENCY_CONFLICT"
        ) {
          attempt.current = null;
        }
        if (error.code === "VERSION_CONFLICT") {
          await queryClient.invalidateQueries({
            queryKey: invoiceDetailQueryKey(variables.id),
            exact: true,
            refetchType: "none",
          });
        }
      }
    },
  });
}

export function useDownloadInvoicePdf() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, name }: { id: string; name: string }) =>
      downloadInvoicePdf(id, name),
    onSettled: async (_data, _error, variables) => {
      await queryClient.invalidateQueries({
        queryKey: invoicePdfQueryKey(variables.id),
        exact: true,
      });
    },
  });
}

export function useCreateInvoice() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: (input: InvoiceInput) => {
      const fingerprint = JSON.stringify(input);
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return createInvoice(input, attempt.current.key);
    },
    onSuccess: async (invoice) => {
      attempt.current = null;
      queryClient.setQueryData(invoiceDetailQueryKey(invoice.id), invoice);
      await invalidateInvoiceReadModels(queryClient);
    },
  });
}

export function useUpdateInvoice() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: UpdateInvoiceInput }) =>
      updateInvoice(id, input),
    onSuccess: async (invoice) => {
      queryClient.setQueryData(invoiceDetailQueryKey(invoice.id), invoice);
      await invalidateInvoiceReadModels(queryClient);
    },
    onError: async (error, variables) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await invalidateInvoiceReadModels(queryClient, variables.id);
      }
    },
  });
}

export function useTransitionInvoice() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: ({
      id,
      input,
    }: {
      id: string;
      input: TransitionInvoiceInput;
    }) => {
      const fingerprint = JSON.stringify({ id, ...input });
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return transitionInvoice(id, input, attempt.current.key);
    },
    onSuccess: async (invoice, variables) => {
      attempt.current = null;
      queryClient.setQueryData(invoiceDetailQueryKey(invoice.id), invoice);
      await invalidateInvoiceReadModels(queryClient);
      if (variables.input.action === "mark_overdue") {
        await Promise.all([
          invalidateTaskFacts(queryClient),
          queryClient.invalidateQueries({ queryKey: automationQueryKey }),
        ]);
      }
    },
    onError: async (error, variables) => {
      if (error instanceof ApiError) {
        if (
          error.code === "IDEMPOTENCY_REPLAY_UNAVAILABLE" ||
          error.code === "IDEMPOTENCY_CONFLICT"
        ) {
          attempt.current = null;
        }
        if (error.code === "VERSION_CONFLICT") {
          await invalidateInvoiceReadModels(queryClient, variables.id);
          if (variables.input.action === "mark_overdue") {
            await Promise.all([
              invalidateTaskFacts(queryClient),
              queryClient.invalidateQueries({ queryKey: automationQueryKey }),
            ]);
          }
        }
      }
    },
  });
}

export function useDeleteInvoice() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      expectedVersion,
    }: {
      id: string;
      expectedVersion: number;
    }) => deleteInvoice(id, expectedVersion),
    onSuccess: async (_, variables) => {
      queryClient.removeQueries({
        queryKey: invoiceDetailQueryKey(variables.id),
      });
      await invalidateInvoiceReadModels(queryClient);
    },
    onError: async (error, variables) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await invalidateInvoiceReadModels(queryClient, variables.id);
      }
    },
  });
}
