import {
  canonicalUuid,
  exactKeys,
  object,
  positiveIntegerValue,
  uuid,
} from "./primitives";

const contentItemEditableFields = new Set([
  "title",
  "platform",
  "project_id",
  "notes",
  "external_link",
]);

const contentItemStatuses = new Set([
  "draft",
  "in_review",
  "scheduled",
  "published",
  "cancelled",
  "archived",
]);

function validContentTimezone(value: string): boolean {
  if (value.length > 100) return false;
  try {
    new Intl.DateTimeFormat("en", { timeZone: value });
    return true;
  } catch {
    return false;
  }
}

function validContentItemField(field: string, value: unknown): boolean {
  if (field === "title")
    return (
      typeof value === "string" &&
      value.trim().length >= 1 &&
      value.trim().length <= 200
    );
  if (field === "platform")
    return (
      typeof value === "string" &&
      value.trim().length >= 1 &&
      value.trim().length <= 64
    );
  if (field === "project_id")
    return value === null || (typeof value === "string" && uuid.test(value));
  if (field === "notes" || field === "external_link") {
    const maximum = field === "notes" ? 4_000 : 2_048;
    return (
      value === null ||
      (typeof value === "string" &&
        value.trim().length >= 1 &&
        value.trim().length <= maximum)
    );
  }
  if (field === "scheduled_at")
    return (
      value === null ||
      (typeof value === "string" &&
        /^\d{4}-\d{2}-\d{2}T/.test(value) &&
        Number.isFinite(Date.parse(value)))
    );
  if (field === "scheduled_timezone")
    return (
      value === null ||
      (typeof value === "string" &&
        value === value.trim() &&
        validContentTimezone(value))
    );
  return field === "status" && contentItemStatuses.has(String(value));
}

export function validContentItemProposal(
  value: Record<string, unknown>,
): boolean {
  const action = value.action as Record<string, unknown>;
  const preview = value.preview as Record<string, unknown>;
  const changes = action.changes as Record<string, unknown>;
  const before = preview.before as Record<string, unknown>;
  const after = preview.after as Record<string, unknown>;
  if (!exactKeys(preview, ["label", "before", "after"])) return false;

  if (action.action === "content_item.publish") {
    if (
      !exactKeys(action, [
        "action",
        "content_item_id",
        "expected_version",
        "changes",
      ]) ||
      typeof action.content_item_id !== "string" ||
      !canonicalUuid.test(action.content_item_id) ||
      !positiveIntegerValue(action.expected_version) ||
      typeof preview.label !== "string" ||
      preview.label.trim().length < 2 ||
      !object(changes) ||
      Object.keys(changes).some(
        (key) => !["published_at", "external_link"].includes(key),
      ) ||
      !exactKeys(before, ["status", "published_at", "external_link"]) ||
      !exactKeys(after, ["status", "published_at", "external_link"]) ||
      !["draft", "in_review", "scheduled"].includes(String(before.status)) ||
      before.published_at !== null ||
      !(
        before.external_link === null ||
        typeof before.external_link === "string"
      ) ||
      after.status !== "published" ||
      !(after.external_link === null || typeof after.external_link === "string")
    )
      return false;
    if (Object.hasOwn(changes, "published_at")) {
      if (
        typeof changes.published_at !== "string" ||
        !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.test(
          changes.published_at,
        ) ||
        !Number.isFinite(Date.parse(changes.published_at)) ||
        typeof after.published_at !== "string" ||
        !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(
          after.published_at,
        ) ||
        Date.parse(after.published_at) !== Date.parse(changes.published_at)
      )
        return false;
    } else if (after.published_at !== "confirmation_time") return false;
    if (Object.hasOwn(changes, "external_link")) {
      if (
        !(
          changes.external_link === null ||
          (typeof changes.external_link === "string" &&
            changes.external_link.trim().length > 0 &&
            Array.from(changes.external_link.trim()).length <= 2048)
        ) ||
        after.external_link !==
          (typeof changes.external_link === "string"
            ? changes.external_link.trim()
            : null)
      )
        return false;
    } else if (after.external_link !== before.external_link) return false;
    if (value.result_id !== null && value.result_id !== action.content_item_id)
      return false;
    if (
      value.result_version !== null &&
      value.result_version !== action.expected_version &&
      value.result_version !== Number(action.expected_version) + 1
    )
      return false;
    return (
      value.status !== "confirmed" ||
      (value.result_id === action.content_item_id &&
        value.result_version !== null)
    );
  }

  if (
    [
      "content_item.link_task",
      "content_item.set_task_required",
      "content_item.unlink_task",
    ].includes(String(action.action))
  ) {
    const linking = action.action === "content_item.link_task";
    const setting = action.action === "content_item.set_task_required";
    const unlinking = action.action === "content_item.unlink_task";
    const relationKeys = [
      "task_id",
      "task_title",
      "task_version",
      "task_status",
      "relation_state",
      "is_required",
      "content_item_version",
    ];
    const validBefore =
      exactKeys(before, [
        "relation_state",
        "is_required",
        "content_item_version",
      ]) &&
      ["linked", "unlinked"].includes(String(before.relation_state)) &&
      typeof before.is_required === "boolean" &&
      positiveIntegerValue(before.content_item_version);
    const validAfter = (record: Record<string, unknown>) =>
      exactKeys(record, relationKeys) &&
      typeof record.task_id === "string" &&
      canonicalUuid.test(record.task_id) &&
      typeof record.task_title === "string" &&
      record.task_title.trim().length > 0 &&
      positiveIntegerValue(record.task_version) &&
      [
        "todo",
        "in_progress",
        "blocked",
        "waiting_review",
        "done",
        "cancelled",
      ].includes(String(record.task_status)) &&
      ["linked", "unlinked"].includes(String(record.relation_state)) &&
      typeof record.is_required === "boolean" &&
      positiveIntegerValue(record.content_item_version);
    if (
      !exactKeys(action, [
        "action",
        "content_item_id",
        "expected_version",
        "changes",
      ]) ||
      typeof action.content_item_id !== "string" ||
      !canonicalUuid.test(action.content_item_id) ||
      !positiveIntegerValue(action.expected_version) ||
      typeof preview.label !== "string" ||
      preview.label.trim().length === 0 ||
      !validBefore ||
      !validAfter(after) ||
      !exactKeys(
        changes,
        unlinking
          ? ["task_id", "expected_task_version"]
          : ["task_id", "expected_task_version", "is_required"],
      ) ||
      changes.task_id !== after.task_id ||
      changes.expected_task_version !== after.task_version ||
      before.content_item_version !== action.expected_version ||
      after.content_item_version !== Number(action.expected_version) + 1 ||
      !positiveIntegerValue(changes.expected_task_version)
    )
      return false;
    if (linking)
      return (
        before.relation_state === "unlinked" &&
        before.is_required === false &&
        after.relation_state === "linked" &&
        after.is_required === changes.is_required &&
        typeof changes.is_required === "boolean"
      );
    if (setting)
      return (
        before.relation_state === "linked" &&
        after.relation_state === "linked" &&
        typeof changes.is_required === "boolean" &&
        after.is_required === changes.is_required &&
        before.is_required !== after.is_required
      );
    return (
      unlinking &&
      before.relation_state === "linked" &&
      after.relation_state === "unlinked" &&
      before.is_required === after.is_required
    );
  }

  if (action.action === "content_item.delete") {
    const counts = [
      "content_item_task_links_deleted",
      "content_item_inbox_sources_marked",
    ];
    return (
      exactKeys(action, [
        "action",
        "content_item_id",
        "expected_version",
        "changes",
      ]) &&
      typeof action.content_item_id === "string" &&
      uuid.test(action.content_item_id) &&
      typeof action.expected_version === "number" &&
      Number.isSafeInteger(action.expected_version) &&
      Number(action.expected_version) >= 1 &&
      object(changes) &&
      Object.keys(changes).length === 0 &&
      typeof preview.label === "string" &&
      preview.label.trim().length > 0 &&
      exactKeys(before, ["status", "content_item_version"]) &&
      before.status === "archived" &&
      before.content_item_version === action.expected_version &&
      exactKeys(after, ["content_item_deleted", ...counts]) &&
      after.content_item_deleted === true &&
      counts.every(
        (key) =>
          typeof after[key] === "number" &&
          Number.isSafeInteger(after[key]) &&
          Number(after[key]) >= 0,
      )
    );
  }

  if (action.action === "content_item.create") {
    const allowed = new Set([
      ...contentItemEditableFields,
      "status",
      "scheduled_at",
      "scheduled_timezone",
    ]);
    if (
      !Object.hasOwn(changes, "title") ||
      !Object.hasOwn(changes, "platform") ||
      Object.keys(changes).some(
        (field) =>
          !allowed.has(field) || !validContentItemField(field, changes[field]),
      ) ||
      Object.keys(before).length !== 0 ||
      !exactKeys(after, [
        "title",
        "platform",
        "status",
        "scheduled_at",
        "scheduled_timezone",
        "project_id",
        "notes",
        "external_link",
      ]) ||
      !["draft", "in_review", "scheduled"].includes(String(after.status))
    )
      return false;
    const hasAt = Object.hasOwn(changes, "scheduled_at");
    const hasTimezone = Object.hasOwn(changes, "scheduled_timezone");
    if (hasAt !== hasTimezone || (hasAt && after.status !== "scheduled"))
      return false;
  } else if (action.action === "content_item.update") {
    const keys = Object.keys(changes).sort();
    if (
      keys.length === 0 ||
      keys.some(
        (field) =>
          !contentItemEditableFields.has(field) ||
          !validContentItemField(field, changes[field]),
      ) ||
      JSON.stringify(Object.keys(before).sort()) !== JSON.stringify(keys) ||
      JSON.stringify(Object.keys(after).sort()) !== JSON.stringify(keys)
    )
      return false;
  } else if (action.action === "content_item.schedule") {
    if (
      !exactKeys(changes, ["scheduled_at", "scheduled_timezone"]) ||
      !exactKeys(before, ["scheduled_at", "scheduled_timezone", "status"]) ||
      !exactKeys(after, ["scheduled_at", "scheduled_timezone", "status"]) ||
      after.status !== "scheduled" ||
      after.scheduled_at === null ||
      after.scheduled_timezone === null
    )
      return false;
  } else if (action.action === "content_item.unschedule") {
    if (
      Object.keys(changes).length !== 0 ||
      !exactKeys(before, ["scheduled_at", "scheduled_timezone", "status"]) ||
      !exactKeys(after, ["scheduled_at", "scheduled_timezone", "status"]) ||
      after.scheduled_at !== null ||
      after.scheduled_timezone !== null
    )
      return false;
  } else {
    if (
      ![
        "content_item.review",
        "content_item.cancel",
        "content_item.reopen",
        "content_item.archive",
        "content_item.restore",
      ].includes(String(action.action)) ||
      Object.keys(changes).length !== 0 ||
      !exactKeys(before, ["status"]) ||
      !exactKeys(after, ["status"])
    )
      return false;
  }
  return (
    Object.entries(before).every(([field, entry]) =>
      validContentItemField(field, entry),
    ) &&
    Object.entries(after).every(([field, entry]) =>
      validContentItemField(field, entry),
    )
  );
}
