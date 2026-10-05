import { ApiError } from "../client";
import { aiActionFields } from "./catalog";

export const uuid =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export const canonicalUuid =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

export const object = (v: unknown): v is Record<string, unknown> =>
  typeof v === "object" && v !== null && !Array.isArray(v);

export const exactKeys = (value: Record<string, unknown>, allowed: string[]) =>
  Object.keys(value).length === allowed.length &&
  Object.keys(value).every((key) => allowed.includes(key));

export const positiveIntegerValue = (value: unknown): value is number =>
  typeof value === "number" && Number.isSafeInteger(value) && value > 0;

export const nonemptyString = (value: unknown) =>
  typeof value === "string" && value.trim().length > 0;

export const sha256 = /^[a-f0-9]{64}$/;

export function invalid(): never {
  throw new ApiError("操作确认信息不完整，请重新读取", {
    code: "INVALID_RESPONSE",
  });
}

export function fields(
  v: unknown,
): v is Record<string, string | number | boolean | null | string[]> {
  return (
    object(v) &&
    Object.entries(v).every(
      ([key, value]) =>
        Object.hasOwn(aiActionFields, key) &&
        (value === null ||
          ([
            "is_required",
            "available",
            "rule_enabled",
            "invoice_deleted",
            "pdf_removed",
            "pdf_generated",
            "pdf_replaced",
            "task_deleted",
            "tag_deleted",
            "project_deleted",
            "client_deleted",
            "content_item_deleted",
            "roadmap_milestone_deleted",
            "parent_rollup_gates_ready",
            "will_request_parent_review",
          ].includes(key) &&
            typeof value === "boolean") ||
          typeof value === "string" ||
          (key === "project_ids" &&
            Array.isArray(value) &&
            value.length <= 100 &&
            new Set(value).size === value.length &&
            value.every((id) => typeof id === "string" && uuid.test(id))) ||
          (key === "tag_names" &&
            Array.isArray(value) &&
            value.length <= 20 &&
            new Set(value).size === value.length &&
            value.every(
              (name) =>
                typeof name === "string" &&
                name.trim().length > 0 &&
                Array.from(name).length <= 50,
            )) ||
          (typeof value === "number" && Number.isFinite(value))),
    )
  );
}

// Knowledge library management carries metadata only. The card must prove the
// server frozen the same source/version and never accepted document text.
export const isPositiveInteger = (value: unknown): value is number =>
  typeof value === "number" && Number.isSafeInteger(value) && value > 0;
