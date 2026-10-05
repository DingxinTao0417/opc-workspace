import type { PageMeta } from "./common";

export type FinancialEntryType = "income" | "expense";

export type FinancialEntryStatus = "pending" | "confirmed" | "voided";

export interface FinancialEntry {
  id: string;
  type: FinancialEntryType;
  amountMinor: number;
  currency: string;
  occurredOn: string;
  status: FinancialEntryStatus;
  category: string;
  clientId: string | null;
  clientName: string | null;
  projectId: string | null;
  projectName: string | null;
  invoiceId: string | null;
  invoiceNumber: string | null;
  notes: string;
  createdByActorId: string;
  voidedAt: string | null;
  voidReason: string | null;
  version: number;
  createdAt: string;
  updatedAt: string;
}

export interface FinancialEntryListParams {
  page?: number;
  pageSize?: number;
  type?: FinancialEntryType;
  status?: FinancialEntryStatus;
  currency?: string;
  category?: string;
  clientId?: string;
  projectId?: string;
  dateFrom?: string;
  dateTo?: string;
  includeVoided?: boolean;
  sort?: string;
}

export interface FinancialEntryListResult {
  items: FinancialEntry[];
  meta: PageMeta;
}

export interface FinancialEntryInput {
  type: FinancialEntryType;
  amountMinor: number;
  currency: string;
  occurredOn: string;
  status: Exclude<FinancialEntryStatus, "voided">;
  category: string;
  clientId: string | null;
  projectId: string | null;
  notes: string;
}

export interface UpdateFinancialEntryInput extends Partial<FinancialEntryInput> {
  expectedVersion: number;
}

export interface VoidFinancialEntryInput {
  reason: string;
  expectedVersion: number;
}

export interface IncomeStats {
  currency: string;
  dateFrom: string;
  dateTo: string;
  confirmedIncomeMinor: number;
  confirmedExpenseMinor: number;
  pendingIncomeMinor: number;
  pendingExpenseMinor: number;
  netCashFlowMinor: number;
  confirmedIncomeCount: number;
  averageIncomeMinor: number;
  entryCount: number;
}

export interface IncomeStatsParams {
  currency: string;
  dateFrom: string;
  dateTo: string;
}

export interface FinancialEntriesCSVDownload {
  blob: Blob;
  fileName: string;
}
