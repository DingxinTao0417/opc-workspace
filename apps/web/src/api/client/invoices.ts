import type {
  DeleteInvoiceResult,
  Invoice,
  InvoiceInput,
  InvoiceListParams,
  InvoiceListResult,
  InvoicePdfDownload,
  InvoicePdfMetadata,
  InvoiceStatus,
  TransitionInvoiceInput,
  UpdateInvoiceInput,
} from "../../types/models";
import {
  ARTIFACT_TRANSFER_TIMEOUT_MS,
  ApiError,
  apiFetch,
  apiRequest,
  downloadFileName,
  expectedVersionHeader,
  hasExactKeys,
  invalidResponse,
  isRecord,
  nonNegativeInteger,
  positiveInteger,
  stringField,
  validInvoiceDate,
} from "./core";
import { financialAmount, nullableFinancialString } from "./finance";

const invoiceKeys = [
  "id",
  "invoice_number",
  "client_id",
  "client_name",
  "project_id",
  "project_name",
  "amount_minor",
  "currency",
  "status",
  "issue_date",
  "due_date",
  "paid_date",
  "notes",
  "financial_entry_id",
  "version",
  "created_at",
  "updated_at",
] as const;

function asInvoiceStatus(value: unknown): InvoiceStatus {
  if (
    value === "draft" ||
    value === "sent" ||
    value === "viewed" ||
    value === "paid" ||
    value === "overdue"
  ) {
    return value;
  }
  return invalidResponse("发票状态响应无效");
}

export function normalizeInvoice(value: unknown): Invoice {
  if (!isRecord(value) || !hasExactKeys(value, invoiceKeys)) {
    return invalidResponse("发票响应格式无效");
  }
  const id = stringField(value, "id");
  const invoiceNumber = stringField(value, "invoice_number");
  const clientId = stringField(value, "client_id");
  const clientName = stringField(value, "client_name");
  const currency = stringField(value, "currency");
  const issueDate = stringField(value, "issue_date");
  const dueDate = stringField(value, "due_date");
  const createdAt = stringField(value, "created_at");
  const updatedAt = stringField(value, "updated_at");
  const status = asInvoiceStatus(value.status);
  const projectId = nullableFinancialString(value.project_id, "发票项目 ID");
  const projectName = nullableFinancialString(
    value.project_name,
    "发票项目名称",
  );
  const paidDate = nullableFinancialString(value.paid_date, "发票付款日期");
  const financialEntryId = nullableFinancialString(
    value.financial_entry_id,
    "发票收入记录 ID",
  );
  if (!issueDate || !dueDate) {
    return invalidResponse("发票日期响应字段无效");
  }
  if (
    !id ||
    !invoiceNumber ||
    !clientId ||
    !clientName ||
    !currency?.match(/^[A-Z]{3}$/) ||
    !validInvoiceDate(issueDate) ||
    !validInvoiceDate(dueDate) ||
    dueDate < issueDate ||
    (projectId === null) !== (projectName === null) ||
    (paidDate !== null &&
      (!validInvoiceDate(paidDate) || paidDate < issueDate)) ||
    typeof value.notes !== "string" ||
    !createdAt ||
    !updatedAt ||
    (status === "paid") !== (paidDate !== null) ||
    (status !== "paid" && financialEntryId !== null)
  ) {
    return invalidResponse("发票响应字段无效");
  }
  return {
    id,
    invoiceNumber,
    clientId,
    clientName,
    projectId,
    projectName,
    amountMinor: financialAmount(value.amount_minor, "发票金额"),
    currency,
    status,
    issueDate,
    dueDate,
    paidDate,
    notes: value.notes,
    financialEntryId,
    version: positiveInteger(value.version, "发票版本"),
    createdAt,
    updatedAt,
  };
}

function normalizeInvoiceListResult(
  value: unknown,
  input: InvoiceListParams = {},
): InvoiceListResult {
  if (
    !isRecord(value) ||
    !hasExactKeys(value, ["data", "meta"]) ||
    !Array.isArray(value.data) ||
    !isRecord(value.meta) ||
    !hasExactKeys(value.meta, ["page", "page_size", "total"])
  ) {
    return invalidResponse("发票列表响应格式无效");
  }
  return {
    items: value.data.map(normalizeInvoice),
    meta: {
      page: positiveInteger(value.meta.page, "发票页码", input.page ?? 1),
      pageSize: positiveInteger(
        value.meta.page_size,
        "发票每页数量",
        input.pageSize ?? 50,
      ),
      total: nonNegativeInteger(value.meta.total, "发票总数"),
    },
  };
}

function normalizeInvoiceResponse(value: unknown): Invoice {
  if (!isRecord(value) || !hasExactKeys(value, ["data"])) {
    return invalidResponse("发票详情响应格式无效");
  }
  return normalizeInvoice(value.data);
}

const invoicePdfMetadataKeys = [
  "invoice_id",
  "file_name",
  "mime_type",
  "size_bytes",
  "sha256",
  "generated_from_version",
  "generated_at",
  "integrity_status",
  "integrity_checked_at",
] as const;

export function normalizeInvoicePdfMetadata(
  value: unknown,
): InvoicePdfMetadata {
  if (!isRecord(value) || !hasExactKeys(value, invoicePdfMetadataKeys)) {
    return invalidResponse("发票 PDF 元数据响应格式无效");
  }
  const invoiceId = stringField(value, "invoice_id");
  const fileName = stringField(value, "file_name");
  const mimeType = stringField(value, "mime_type");
  const sha256 = stringField(value, "sha256");
  const generatedAt = stringField(value, "generated_at");
  const integrityStatus = stringField(value, "integrity_status");
  const integrityCheckedAt = stringField(value, "integrity_checked_at");
  if (
    !invoiceId ||
    !fileName ||
    mimeType !== "application/pdf" ||
    !sha256?.match(/^[a-f0-9]{64}$/) ||
    !generatedAt ||
    Number.isNaN(Date.parse(generatedAt)) ||
    (integrityStatus !== "verified" &&
      integrityStatus !== "missing" &&
      integrityStatus !== "mismatch") ||
    !integrityCheckedAt ||
    Number.isNaN(Date.parse(integrityCheckedAt))
  ) {
    return invalidResponse("发票 PDF 元数据响应字段无效");
  }
  return {
    invoiceId,
    fileName,
    mimeType,
    sizeBytes: positiveInteger(value.size_bytes, "发票 PDF 文件大小"),
    sha256,
    generatedFromVersion: positiveInteger(
      value.generated_from_version,
      "发票 PDF 来源版本",
    ),
    generatedAt,
    integrityStatus,
    integrityCheckedAt,
  };
}

function normalizeInvoicePdfResponse(
  value: unknown,
  expectedInvoiceId: string,
): InvoicePdfMetadata {
  if (!isRecord(value) || !hasExactKeys(value, ["data"])) {
    return invalidResponse("发票 PDF 响应格式无效");
  }
  const metadata = normalizeInvoicePdfMetadata(value.data);
  if (metadata.invoiceId !== expectedInvoiceId) {
    return invalidResponse("发票 PDF 响应与请求不一致");
  }
  return metadata;
}

function invoiceSearchParams(input: InvoiceListParams = {}): URLSearchParams {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 50),
  });
  if (input.q?.trim()) params.set("q", input.q.trim());
  if (input.status) params.set("status", input.status);
  if (input.currency?.trim()) params.set("currency", input.currency.trim());
  if (input.clientId) params.set("client_id", input.clientId);
  if (input.projectId) params.set("project_id", input.projectId);
  if (input.issueFrom) params.set("issue_from", input.issueFrom);
  if (input.issueTo) params.set("issue_to", input.issueTo);
  if (input.dueFrom) params.set("due_from", input.dueFrom);
  if (input.dueTo) params.set("due_to", input.dueTo);
  if (input.sort) params.set("sort", input.sort);
  return params;
}

export async function getInvoices(
  input: InvoiceListParams = {},
): Promise<InvoiceListResult> {
  const payload = await apiRequest<unknown>(
    `/api/v1/invoices?${invoiceSearchParams(input)}`,
  );
  return normalizeInvoiceListResult(payload, input);
}

export async function getInvoice(
  id: string,
  signal?: AbortSignal,
): Promise<Invoice> {
  const payload = await apiRequest<unknown>(
    `/api/v1/invoices/${encodeURIComponent(id)}`,
    { signal },
  );
  const invoice = normalizeInvoiceResponse(payload);
  if (invoice.id !== id)
    throw new ApiError("发票身份不匹配。", { code: "INVALID_RESPONSE" });
  return invoice;
}

export async function getInvoicePdf(
  id: string,
): Promise<InvoicePdfMetadata | null> {
  try {
    const payload = await apiRequest<unknown>(
      `/api/v1/invoices/${encodeURIComponent(id)}/pdf`,
    );
    return normalizeInvoicePdfResponse(payload, id);
  } catch (error) {
    if (error instanceof ApiError && error.code === "INVOICE_PDF_NOT_FOUND") {
      return null;
    }
    throw error;
  }
}

export async function generateInvoicePdf(
  id: string,
  expectedVersion: number,
  idempotencyKey: string,
): Promise<InvoicePdfMetadata> {
  const payload = await apiRequest<unknown>(
    `/api/v1/invoices/${encodeURIComponent(id)}/generate-pdf`,
    {
      method: "POST",
      headers: {
        ...expectedVersionHeader(expectedVersion),
        "Idempotency-Key": idempotencyKey,
      },
    },
    ARTIFACT_TRANSFER_TIMEOUT_MS,
  );
  return normalizeInvoicePdfResponse(payload, id);
}

export async function downloadInvoicePdf(
  id: string,
  fallbackName: string,
  expected?: { assetId: string; sha256: string; sizeBytes: number },
  signal?: AbortSignal,
): Promise<InvoicePdfDownload> {
  if (
    expected &&
    (!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(
      expected.assetId,
    ) ||
      !/^[a-f0-9]{64}$/.test(expected.sha256) ||
      !Number.isSafeInteger(expected.sizeBytes) ||
      expected.sizeBytes <= 0 ||
      expected.sizeBytes > 50 * 1024 * 1024)
  )
    return invalidResponse("本次 PDF 文件身份无效");
  const query = expected
    ? `?${new URLSearchParams({ asset_id: expected.assetId, sha256: expected.sha256 })}`
    : "";
  return apiFetch(
    `/api/v1/invoices/${encodeURIComponent(id)}/pdf/download${query}`,
    async (response) => {
      const contentType = response.headers.get("Content-Type");
      if (!contentType?.toLowerCase().startsWith("application/pdf")) {
        return invalidResponse("发票 PDF 下载响应格式无效");
      }
      const blob = await response.blob();
      if (blob.size < 1) {
        return invalidResponse("发票 PDF 文件为空");
      }
      if (
        expected &&
        (blob.size !== expected.sizeBytes ||
          response.headers.get("X-Invoice-PDF-SHA256") !== expected.sha256)
      )
        return invalidResponse("下载响应与本次 PDF 结果不一致，已阻止保存");
      return {
        blob,
        fileName: downloadFileName(
          response.headers.get("Content-Disposition"),
          fallbackName,
        ),
      };
    },
    { signal },
    "application/pdf",
    ARTIFACT_TRANSFER_TIMEOUT_MS,
  );
}

function invoiceBody(input: Partial<InvoiceInput>) {
  return {
    client_id: input.clientId,
    project_id: input.projectId,
    amount_minor: input.amountMinor,
    currency: input.currency,
    issue_date: input.issueDate,
    due_date: input.dueDate,
    notes: input.notes,
  };
}

export async function createInvoice(
  input: InvoiceInput,
  idempotencyKey: string,
): Promise<Invoice> {
  const payload = await apiRequest<unknown>("/api/v1/invoices", {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey },
    body: JSON.stringify(invoiceBody(input)),
  });
  return normalizeInvoiceResponse(payload);
}

export async function updateInvoice(
  id: string,
  input: UpdateInvoiceInput,
): Promise<Invoice> {
  const payload = await apiRequest<unknown>(
    `/api/v1/invoices/${encodeURIComponent(id)}`,
    {
      method: "PATCH",
      headers: expectedVersionHeader(input.expectedVersion),
      body: JSON.stringify(invoiceBody(input)),
    },
  );
  return normalizeInvoiceResponse(payload);
}

export async function transitionInvoice(
  id: string,
  input: TransitionInvoiceInput,
  idempotencyKey: string,
): Promise<Invoice> {
  const body =
    input.action === "mark_paid"
      ? { action: input.action, paid_date: input.paidDate }
      : { action: input.action };
  const payload = await apiRequest<unknown>(
    `/api/v1/invoices/${encodeURIComponent(id)}/transition`,
    {
      method: "POST",
      headers: {
        ...expectedVersionHeader(input.expectedVersion),
        "Idempotency-Key": idempotencyKey,
      },
      body: JSON.stringify(body),
    },
  );
  return normalizeInvoiceResponse(payload);
}

export async function deleteInvoice(
  id: string,
  expectedVersion: number,
): Promise<DeleteInvoiceResult> {
  const payload = await apiRequest<unknown>(
    `/api/v1/invoices/${encodeURIComponent(id)}?confirm=true`,
    {
      method: "DELETE",
      headers: expectedVersionHeader(expectedVersion),
    },
  );
  if (
    !isRecord(payload) ||
    !hasExactKeys(payload, ["data"]) ||
    !isRecord(payload.data) ||
    !hasExactKeys(payload.data, ["deleted_id"]) ||
    payload.data.deleted_id !== id
  ) {
    return invalidResponse("发票删除响应格式无效");
  }
  return { deletedId: id };
}
