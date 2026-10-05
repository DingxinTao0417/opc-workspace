import type {
  FinancialEntriesCSVDownload,
  FinancialEntry,
  FinancialEntryInput,
  FinancialEntryListParams,
  FinancialEntryListResult,
  FinancialEntryStatus,
  FinancialEntryType,
  IncomeStats,
  IncomeStatsParams,
  UpdateFinancialEntryInput,
  VoidFinancialEntryInput,
} from "../../types/models";
import {
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
} from "./core";

const financialEntryKeys = [
  "id",
  "type",
  "amount_minor",
  "currency",
  "occurred_on",
  "status",
  "category",
  "client_id",
  "client_name",
  "project_id",
  "project_name",
  "invoice_id",
  "invoice_number",
  "notes",
  "created_by_actor_id",
  "voided_at",
  "void_reason",
  "version",
  "created_at",
  "updated_at",
] as const;

function asFinancialEntryType(value: unknown): FinancialEntryType {
  if (value === "income" || value === "expense") return value;
  return invalidResponse("财务记录类型响应无效");
}

function asFinancialEntryStatus(value: unknown): FinancialEntryStatus {
  if (value === "pending" || value === "confirmed" || value === "voided") {
    return value;
  }
  return invalidResponse("财务记录状态响应无效");
}

export function financialAmount(value: unknown, field: string): number {
  if (
    typeof value === "number" &&
    Number.isSafeInteger(value) &&
    value >= 1 &&
    value <= 9_000_000_000_000_000
  ) {
    return value;
  }
  return invalidResponse(`${field} 响应无效`);
}

export function nullableFinancialString(
  value: unknown,
  field: string,
): string | null {
  if (value === null) return null;
  if (typeof value === "string" && value.length > 0) return value;
  return invalidResponse(`${field} 响应无效`);
}

export function normalizeFinancialEntry(value: unknown): FinancialEntry {
  if (!isRecord(value) || !hasExactKeys(value, financialEntryKeys)) {
    return invalidResponse("财务记录响应格式无效");
  }
  const id = stringField(value, "id");
  const currency = stringField(value, "currency");
  const occurredOn = stringField(value, "occurred_on");
  const category = stringField(value, "category");
  const createdByActorId = stringField(value, "created_by_actor_id");
  const createdAt = stringField(value, "created_at");
  const updatedAt = stringField(value, "updated_at");
  if (
    !id ||
    !currency?.match(/^[A-Z]{3}$/) ||
    !occurredOn?.match(/^\d{4}-\d{2}-\d{2}$/) ||
    !category ||
    typeof value.notes !== "string" ||
    !createdByActorId ||
    !createdAt ||
    !updatedAt
  ) {
    return invalidResponse("财务记录响应字段无效");
  }
  const status = asFinancialEntryStatus(value.status);
  const voidedAt = nullableFinancialString(value.voided_at, "财务作废时间");
  const voidReason = nullableFinancialString(value.void_reason, "财务作废原因");
  if (
    (status === "voided" && (!voidedAt || !voidReason)) ||
    (status !== "voided" && (voidedAt !== null || voidReason !== null))
  ) {
    return invalidResponse("财务记录作废审计响应无效");
  }
  return {
    id,
    type: asFinancialEntryType(value.type),
    amountMinor: financialAmount(value.amount_minor, "财务金额"),
    currency,
    occurredOn,
    status,
    category,
    clientId: nullableFinancialString(value.client_id, "财务客户 ID"),
    clientName: nullableFinancialString(value.client_name, "财务客户名称"),
    projectId: nullableFinancialString(value.project_id, "财务项目 ID"),
    projectName: nullableFinancialString(value.project_name, "财务项目名称"),
    invoiceId: nullableFinancialString(value.invoice_id, "财务发票 ID"),
    invoiceNumber: nullableFinancialString(
      value.invoice_number,
      "财务发票编号",
    ),
    notes: value.notes,
    createdByActorId,
    voidedAt,
    voidReason,
    version: positiveInteger(value.version, "财务记录版本"),
    createdAt,
    updatedAt,
  };
}

function normalizeFinancialEntryListResult(
  value: unknown,
  input: FinancialEntryListParams = {},
): FinancialEntryListResult {
  if (!isRecord(value) || !Array.isArray(value.data) || !isRecord(value.meta)) {
    return invalidResponse("财务记录列表响应格式无效");
  }
  return {
    items: value.data.map(normalizeFinancialEntry),
    meta: {
      page: positiveInteger(value.meta.page, "财务记录页码", input.page ?? 1),
      pageSize: positiveInteger(
        value.meta.page_size,
        "财务记录每页数量",
        input.pageSize ?? 50,
      ),
      total: nonNegativeInteger(value.meta.total, "财务记录总数"),
    },
  };
}

export function normalizeIncomeStats(value: unknown): IncomeStats {
  const body = isRecord(value) && isRecord(value.data) ? value.data : value;
  const keys = [
    "currency",
    "date_from",
    "date_to",
    "confirmed_income_minor",
    "confirmed_expense_minor",
    "pending_income_minor",
    "pending_expense_minor",
    "net_cash_flow_minor",
    "confirmed_income_count",
    "average_income_minor",
    "entry_count",
  ] as const;
  if (!isRecord(body) || !hasExactKeys(body, keys)) {
    return invalidResponse("收入统计响应格式无效");
  }
  const currency = stringField(body, "currency");
  const dateFrom = stringField(body, "date_from");
  const dateTo = stringField(body, "date_to");
  const money = (key: string) => {
    const raw = body[key];
    if (
      typeof raw === "number" &&
      Number.isSafeInteger(raw) &&
      Math.abs(raw) <= 9_000_000_000_000_000
    ) {
      return raw;
    }
    return invalidResponse("收入统计金额响应无效");
  };
  if (
    !currency?.match(/^[A-Z]{3}$/) ||
    !dateFrom?.match(/^\d{4}-\d{2}-\d{2}$/) ||
    !dateTo?.match(/^\d{4}-\d{2}-\d{2}$/)
  ) {
    return invalidResponse("收入统计范围响应无效");
  }
  return {
    currency,
    dateFrom,
    dateTo,
    confirmedIncomeMinor: money("confirmed_income_minor"),
    confirmedExpenseMinor: money("confirmed_expense_minor"),
    pendingIncomeMinor: money("pending_income_minor"),
    pendingExpenseMinor: money("pending_expense_minor"),
    netCashFlowMinor: money("net_cash_flow_minor"),
    confirmedIncomeCount: nonNegativeInteger(
      body.confirmed_income_count,
      "已确认收入笔数",
    ),
    averageIncomeMinor: money("average_income_minor"),
    entryCount: nonNegativeInteger(body.entry_count, "财务记录笔数"),
  };
}

function financialEntrySearchParams(
  input: FinancialEntryListParams = {},
): URLSearchParams {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 50),
  });
  if (input.type) params.set("type", input.type);
  if (input.status) params.set("status", input.status);
  if (input.currency?.trim()) params.set("currency", input.currency.trim());
  if (input.category?.trim()) params.set("category", input.category.trim());
  if (input.clientId) params.set("client_id", input.clientId);
  if (input.projectId) params.set("project_id", input.projectId);
  if (input.dateFrom) params.set("date_from", input.dateFrom);
  if (input.dateTo) params.set("date_to", input.dateTo);
  if (input.includeVoided) params.set("include_voided", "true");
  if (input.sort) params.set("sort", input.sort);
  return params;
}

export async function getFinancialEntries(
  input: FinancialEntryListParams = {},
  signal?: AbortSignal,
): Promise<FinancialEntryListResult> {
  const payload = await apiRequest<unknown>(
    `/api/v1/financial-entries?${financialEntrySearchParams(input)}`,
    { signal },
  );
  return normalizeFinancialEntryListResult(payload, input);
}

export async function getFinancialEntry(
  id: string,
  signal?: AbortSignal,
): Promise<FinancialEntry> {
  const payload = await apiRequest<unknown>(
    `/api/v1/financial-entries/${encodeURIComponent(id)}`,
    { signal },
  );
  const entry = normalizeFinancialEntry(
    isRecord(payload) && "data" in payload ? payload.data : payload,
  );
  if (entry.id !== id)
    throw new ApiError("财务记录身份不匹配。", { code: "INVALID_RESPONSE" });
  return entry;
}

function financialEntryBody(input: Partial<FinancialEntryInput>) {
  return {
    type: input.type,
    amount_minor: input.amountMinor,
    currency: input.currency,
    occurred_on: input.occurredOn,
    status: input.status,
    category: input.category,
    client_id: input.clientId,
    project_id: input.projectId,
    notes: input.notes,
  };
}

export async function createFinancialEntry(
  input: FinancialEntryInput,
  idempotencyKey: string,
): Promise<FinancialEntry> {
  const payload = await apiRequest<unknown>("/api/v1/financial-entries", {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey },
    body: JSON.stringify(financialEntryBody(input)),
  });
  return normalizeFinancialEntry(
    isRecord(payload) && "data" in payload ? payload.data : payload,
  );
}

export async function updateFinancialEntry(
  id: string,
  input: UpdateFinancialEntryInput,
): Promise<FinancialEntry> {
  const payload = await apiRequest<unknown>(
    `/api/v1/financial-entries/${encodeURIComponent(id)}`,
    {
      method: "PATCH",
      headers: expectedVersionHeader(input.expectedVersion),
      body: JSON.stringify(financialEntryBody(input)),
    },
  );
  return normalizeFinancialEntry(
    isRecord(payload) && "data" in payload ? payload.data : payload,
  );
}

export async function voidFinancialEntry(
  id: string,
  input: VoidFinancialEntryInput,
): Promise<FinancialEntry> {
  const payload = await apiRequest<unknown>(
    `/api/v1/financial-entries/${encodeURIComponent(id)}?confirm=true`,
    {
      method: "DELETE",
      headers: expectedVersionHeader(input.expectedVersion),
      body: JSON.stringify({ reason: input.reason }),
    },
  );
  return normalizeFinancialEntry(
    isRecord(payload) && "data" in payload ? payload.data : payload,
  );
}

export async function getIncomeStats(
  input: IncomeStatsParams,
  signal?: AbortSignal,
): Promise<IncomeStats> {
  const params = new URLSearchParams({
    currency: input.currency,
    date_from: input.dateFrom,
    date_to: input.dateTo,
  });
  return normalizeIncomeStats(
    await apiRequest<unknown>(`/api/v1/stats/income?${params}`, { signal }),
  );
}

export async function downloadFinancialEntriesCSV(
  input: FinancialEntryListParams = {},
): Promise<FinancialEntriesCSVDownload> {
  const params = financialEntrySearchParams({
    ...input,
    page: 1,
    pageSize: 100,
  });
  params.set("confirm", "true");
  params.delete("page");
  params.delete("page_size");
  return apiFetch(
    `/api/v1/financial-entries/export.csv?${params}`,
    async (response) => {
      if (!response.headers.get("Content-Type")?.startsWith("text/csv")) {
        return invalidResponse("财务 CSV 导出响应格式无效");
      }
      return {
        blob: await response.blob(),
        fileName: downloadFileName(
          response.headers.get("Content-Disposition"),
          "financial-entries.csv",
        ),
      };
    },
    {},
    "text/csv",
  );
}

export async function downloadApprovedFinancialCSV(
  approval: {
    id: string;
    fingerprint: string;
    sha256: string;
    sizeBytes: number;
  },
  signal?: AbortSignal,
): Promise<FinancialEntriesCSVDownload> {
  if (
    !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(
      approval.id,
    ) ||
    !/^[a-f0-9]{64}$/.test(approval.fingerprint) ||
    !/^[a-f0-9]{64}$/.test(approval.sha256) ||
    !Number.isSafeInteger(approval.sizeBytes) ||
    approval.sizeBytes < 1 ||
    approval.sizeBytes > 16 * 1024 * 1024
  )
    return invalidResponse("财务导出身份无效");
  return apiFetch(
    `/api/v1/ai/actions/${approval.id}/export.csv?fingerprint=${approval.fingerprint}`,
    async (response) => {
      if (
        !response.headers.get("Content-Type")?.startsWith("text/csv") ||
        response.headers.get("X-Financial-CSV-SHA256") !== approval.sha256
      )
        return invalidResponse("财务 CSV 身份校验失败");
      const blob = await response.blob();
      if (blob.size !== approval.sizeBytes)
        return invalidResponse("财务 CSV 大小校验失败");
      return { blob, fileName: `financial-entries-${approval.id}.csv` };
    },
    { signal },
    "text/csv",
    120_000,
  );
}
