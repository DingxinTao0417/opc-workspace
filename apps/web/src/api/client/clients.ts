import type {
  Client,
  ClientInput,
  ClientListParams,
  ClientListResult,
  ClientStatus,
  DeleteClientResult,
  UpdateClientInput,
} from "../../types/models";
import {
  apiRequest,
  expectedVersionHeader,
  fieldValue,
  invalidResponse,
  isRecord,
  nonNegativeInteger,
  positiveInteger,
  stringField,
} from "./core";

export function asClientStatus(value: unknown): ClientStatus {
  if (value === "active" || value === "lead" || value === "inactive") {
    return value;
  }
  return invalidResponse("客户状态响应无效");
}

export function clientOptionalString(
  value: unknown,
  field: string,
): string | null {
  if (value === null) return null;
  if (typeof value === "string") return value || null;
  return invalidResponse(`${field}响应无效`);
}

export function normalizeClient(value: unknown): Client {
  if (!isRecord(value)) return invalidResponse("客户响应格式无效");
  const id = stringField(value, "id");
  const name = stringField(value, "name");
  const createdAt = stringField(value, "created_at", "createdAt");
  const updatedAt = stringField(value, "updated_at", "updatedAt");
  if (!id || !name || !createdAt || !updatedAt) {
    return invalidResponse("客户响应格式无效");
  }

  return {
    id,
    name,
    contactName: clientOptionalString(
      fieldValue(value, "contact_name", "contactName"),
      "客户联系人",
    ),
    email: clientOptionalString(value.email, "客户邮箱"),
    phone: clientOptionalString(value.phone, "客户电话"),
    notes: clientOptionalString(value.notes, "客户备注"),
    status: asClientStatus(value.status),
    version: positiveInteger(value.version, "客户版本"),
    projectCount: nonNegativeInteger(
      fieldValue(value, "project_count", "projectCount"),
      "客户项目数",
    ),
    latestActivityAt: clientOptionalString(
      fieldValue(value, "latest_activity_at", "latestActivityAt"),
      "客户最近活动时间",
    ),
    createdAt,
    updatedAt,
  };
}

export async function getClients(
  input: ClientListParams = {},
  signal?: AbortSignal,
): Promise<ClientListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 50),
  });
  if (input.q?.trim()) params.set("q", input.q.trim());
  if (input.status) params.set("status", input.status);
  if (input.sort?.trim()) params.set("sort", input.sort.trim());
  const payload = await apiRequest<unknown>(`/api/v1/clients?${params}`, {
    signal,
  });
  if (
    !isRecord(payload) ||
    !Array.isArray(payload.data) ||
    !isRecord(payload.meta)
  ) {
    return invalidResponse("客户列表响应格式无效");
  }
  return {
    items: payload.data.map(normalizeClient),
    meta: {
      page: positiveInteger(payload.meta.page, "客户列表页码"),
      pageSize: positiveInteger(
        fieldValue(payload.meta, "page_size", "pageSize"),
        "客户列表每页数量",
      ),
      total: nonNegativeInteger(payload.meta.total, "客户列表总数"),
    },
  };
}

export async function getClient(
  id: string,
  signal?: AbortSignal,
): Promise<Client> {
  const payload = await apiRequest<unknown>(
    `/api/v1/clients/${encodeURIComponent(id)}`,
    { signal },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeClient(body);
}

export async function createClient(
  input: ClientInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<Client> {
  const payload = await apiRequest<unknown>("/api/v1/clients", {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey },
    body: JSON.stringify({
      name: input.name,
      contact_name: input.contactName,
      email: input.email,
      phone: input.phone,
      notes: input.notes,
      status: input.status,
    }),
  });
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeClient(body);
}

export async function updateClient(
  id: string,
  input: UpdateClientInput,
): Promise<Client> {
  const payload = await apiRequest<unknown>(
    `/api/v1/clients/${encodeURIComponent(id)}`,
    {
      method: "PATCH",
      headers: expectedVersionHeader(input.expectedVersion),
      body: JSON.stringify({
        ...(input.name === undefined ? {} : { name: input.name }),
        ...(input.contactName === undefined
          ? {}
          : { contact_name: input.contactName }),
        ...(input.email === undefined ? {} : { email: input.email }),
        ...(input.phone === undefined ? {} : { phone: input.phone }),
        ...(input.notes === undefined ? {} : { notes: input.notes }),
        ...(input.status === undefined ? {} : { status: input.status }),
      }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeClient(body);
}

export async function deleteClient(
  id: string,
  expectedVersion: number,
): Promise<DeleteClientResult> {
  const payload = await apiRequest<unknown>(
    `/api/v1/clients/${encodeURIComponent(id)}?confirm=true`,
    {
      method: "DELETE",
      headers: expectedVersionHeader(expectedVersion),
    },
  );
  const body =
    isRecord(payload) && isRecord(payload.data) ? payload.data : payload;
  if (!isRecord(body)) return invalidResponse("客户删除响应格式无效");
  return {
    deletedId: stringField(body, "deleted_id", "deletedId") ?? id,
    detachedProjects: nonNegativeInteger(
      fieldValue(body, "detached_projects", "detachedProjects"),
      "客户解绑项目数",
    ),
  };
}
