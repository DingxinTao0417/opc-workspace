import type { PageMeta } from "./common";

export type InvoiceStatus = "draft" | "sent" | "viewed" | "paid" | "overdue";

export type InvoiceTransitionAction =
  "mark_sent" | "mark_viewed" | "mark_paid" | "mark_overdue";

export interface Invoice {
  id: string;
  invoiceNumber: string;
  clientId: string;
  clientName: string;
  projectId: string | null;
  projectName: string | null;
  amountMinor: number;
  currency: string;
  status: InvoiceStatus;
  issueDate: string;
  dueDate: string;
  paidDate: string | null;
  notes: string;
  financialEntryId: string | null;
  version: number;
  createdAt: string;
  updatedAt: string;
}

export interface InvoicePdfMetadata {
  invoiceId: string;
  fileName: string;
  mimeType: "application/pdf";
  sizeBytes: number;
  sha256: string;
  generatedFromVersion: number;
  generatedAt: string;
  integrityStatus: "verified" | "missing" | "mismatch";
  integrityCheckedAt: string;
}

export interface InvoicePdfDownload {
  blob: Blob;
  fileName: string;
}

export interface InvoiceListParams {
  page?: number;
  pageSize?: number;
  q?: string;
  status?: InvoiceStatus;
  currency?: string;
  clientId?: string;
  projectId?: string;
  issueFrom?: string;
  issueTo?: string;
  dueFrom?: string;
  dueTo?: string;
  sort?: string;
}

export interface InvoiceListResult {
  items: Invoice[];
  meta: PageMeta;
}

export interface InvoiceInput {
  clientId: string;
  projectId: string | null;
  amountMinor: number;
  currency: string;
  issueDate: string;
  dueDate: string;
  notes: string;
}

export interface UpdateInvoiceInput extends Partial<InvoiceInput> {
  expectedVersion: number;
}

export interface TransitionInvoiceInput {
  action: InvoiceTransitionAction;
  paidDate?: string;
  expectedVersion: number;
}

export interface DeleteInvoiceResult {
  deletedId: string;
}
