import {
  canonicalUuid,
  exactKeys,
  object,
  positiveIntegerValue,
  uuid,
} from "./primitives";

export function validProjectDeleteProposal(
  value: Record<string, unknown>,
): boolean {
  const action = value.action as Record<string, unknown>;
  if (action.action !== "project.delete") return true;
  const preview = value.preview as Record<string, unknown>;
  const before = preview.before as Record<string, unknown>;
  const after = preview.after as Record<string, unknown>;
  const counts = [
    "project_tasks_detached",
    "project_draft_invoices_detached",
    "project_financial_entries_detached",
    "project_notes_deleted",
    "project_attachments_deleted",
    "project_attachment_files_deleted",
    "project_inbox_sources_coordinated",
  ];
  return (
    exactKeys(action, [
      "action",
      "project_id",
      "expected_version",
      "changes",
    ]) &&
    object(action.changes) &&
    Object.keys(action.changes).length === 0 &&
    exactKeys(preview, ["label", "before", "after"]) &&
    typeof preview.label === "string" &&
    preview.label.trim().length > 0 &&
    exactKeys(before, ["status", "project_version"]) &&
    before.status === "archived" &&
    before.project_version === action.expected_version &&
    exactKeys(after, ["project_deleted", ...counts]) &&
    after.project_deleted === true &&
    counts.every(
      (key) =>
        typeof after[key] === "number" &&
        Number.isSafeInteger(after[key]) &&
        Number(after[key]) >= 0,
    )
  );
}

const roadmapEditableFields = new Set([
  "title",
  "description",
  "year",
  "quarter",
  "target_date",
  "status",
  "project_ids",
]);

function validRoadmapChange(field: string, value: unknown): boolean {
  if (field === "title")
    return (
      typeof value === "string" &&
      value.trim().length >= 1 &&
      value.trim().length <= 200
    );
  if (field === "description")
    return (
      value === null ||
      (typeof value === "string" &&
        value.trim().length >= 1 &&
        value.trim().length <= 4_000)
    );
  if (field === "year")
    return (
      typeof value === "number" &&
      Number.isSafeInteger(value) &&
      value >= 2000 &&
      value <= 2100
    );
  if (field === "quarter")
    return (
      typeof value === "number" &&
      Number.isSafeInteger(value) &&
      value >= 1 &&
      value <= 4
    );
  if (field === "target_date")
    return (
      typeof value === "string" &&
      /^\d{4}-\d{2}-\d{2}$/.test(value) &&
      Number.isFinite(Date.parse(`${value}T00:00:00Z`))
    );
  if (field === "status")
    return ["planned", "active", "achieved"].includes(String(value));
  return (
    field === "project_ids" &&
    Array.isArray(value) &&
    value.length <= 100 &&
    new Set(value).size === value.length &&
    value.every((id) => typeof id === "string" && uuid.test(id))
  );
}

function validRoadmapPreviewField(field: string, value: unknown): boolean {
  return field === "status"
    ? ["planned", "active", "achieved", "archived"].includes(String(value))
    : validRoadmapChange(field, value);
}

export function validRoadmapProposal(value: Record<string, unknown>): boolean {
  const action = value.action as Record<string, unknown>;
  const preview = value.preview as Record<string, unknown>;
  const changes = action.changes as Record<string, unknown>;
  const before = preview.before as Record<string, unknown>;
  const after = preview.after as Record<string, unknown>;
  if (
    !exactKeys(preview, ["label", "before", "after"]) ||
    Object.keys(changes).some(
      (field) =>
        !roadmapEditableFields.has(field) ||
        !validRoadmapChange(field, changes[field]),
    )
  )
    return false;
  if (action.action === "roadmap_milestone.delete") {
    const counts = [
      "roadmap_milestone_project_links_deleted",
      "roadmap_milestone_inbox_sources_marked",
    ];
    return (
      exactKeys(action, [
        "action",
        "roadmap_milestone_id",
        "expected_version",
        "changes",
      ]) &&
      typeof action.roadmap_milestone_id === "string" &&
      uuid.test(action.roadmap_milestone_id) &&
      typeof action.expected_version === "number" &&
      Number.isSafeInteger(action.expected_version) &&
      Number(action.expected_version) >= 1 &&
      Object.keys(changes).length === 0 &&
      typeof preview.label === "string" &&
      preview.label.trim().length > 0 &&
      exactKeys(before, ["status", "roadmap_milestone_version"]) &&
      before.status === "archived" &&
      before.roadmap_milestone_version === action.expected_version &&
      exactKeys(after, ["roadmap_milestone_deleted", ...counts]) &&
      after.roadmap_milestone_deleted === true &&
      counts.every(
        (key) =>
          typeof after[key] === "number" &&
          Number.isSafeInteger(after[key]) &&
          Number(after[key]) >= 0,
      )
    );
  }
  if (action.action === "roadmap_milestone.create") {
    if (
      !["title", "year", "quarter", "target_date"].every((field) =>
        Object.hasOwn(changes, field),
      ) ||
      Object.keys(before).length !== 0 ||
      !exactKeys(after, [
        "title",
        "description",
        "year",
        "quarter",
        "target_date",
        "status",
        "project_ids",
      ])
    )
      return false;
  } else if (action.action === "roadmap_milestone.update") {
    const keys = Object.keys(changes).sort();
    if (
      keys.length === 0 ||
      JSON.stringify(Object.keys(before).sort()) !== JSON.stringify(keys) ||
      JSON.stringify(Object.keys(after).sort()) !== JSON.stringify(keys)
    )
      return false;
  } else {
    if (
      !["roadmap_milestone.archive", "roadmap_milestone.restore"].includes(
        String(action.action),
      ) ||
      Object.keys(changes).length !== 0 ||
      !exactKeys(before, ["status", "project_ids"]) ||
      !exactKeys(after, ["status", "project_ids"]) ||
      !Array.isArray(before.project_ids) ||
      !Array.isArray(after.project_ids) ||
      JSON.stringify(before.project_ids) !== JSON.stringify(after.project_ids)
    )
      return false;
  }
  return (
    Object.entries(before).every(([field, entry]) =>
      validRoadmapPreviewField(field, entry),
    ) &&
    Object.entries(after).every(([field, entry]) =>
      validRoadmapPreviewField(field, entry),
    )
  );
}

export function validRoadmapOrderProposal(
  value: Record<string, unknown>,
): boolean {
  const action = value.action;
  const preview = value.preview;
  if (!object(action) || !object(preview)) return false;
  const changes = action.changes;
  const order = preview.roadmap_order;
  if (
    !exactKeys(action, [
      "action",
      "roadmap_milestone_id",
      "expected_version",
      "changes",
    ]) ||
    !object(changes) ||
    !exactKeys(changes, [
      "anchor_milestone_id",
      "expected_anchor_version",
      "placement",
    ]) ||
    !exactKeys(preview, ["label", "before", "after", "roadmap_order"]) ||
    !object(order) ||
    !exactKeys(order, [
      "milestone_id",
      "title",
      "anchor_id",
      "anchor_title",
      "placement",
      "year",
      "quarter",
      "group_count",
      "before_position",
      "after_position",
      "group_fingerprint",
    ]) ||
    !object(preview.before) ||
    Object.keys(preview.before).length !== 0 ||
    !object(preview.after) ||
    Object.keys(preview.after).length !== 0
  )
    return false;
  if (
    typeof action.roadmap_milestone_id !== "string" ||
    !canonicalUuid.test(action.roadmap_milestone_id) ||
    !positiveIntegerValue(action.expected_version) ||
    typeof changes.anchor_milestone_id !== "string" ||
    !canonicalUuid.test(changes.anchor_milestone_id) ||
    changes.anchor_milestone_id === action.roadmap_milestone_id ||
    !positiveIntegerValue(changes.expected_anchor_version) ||
    (changes.placement !== "before" && changes.placement !== "after") ||
    order.milestone_id !== action.roadmap_milestone_id ||
    order.anchor_id !== changes.anchor_milestone_id ||
    order.placement !== changes.placement ||
    typeof order.title !== "string" ||
    order.title.trim().length < 2 ||
    Array.from(order.title).length > 200 ||
    typeof order.anchor_title !== "string" ||
    order.anchor_title.trim().length < 2 ||
    Array.from(order.anchor_title).length > 200 ||
    preview.label !== order.title ||
    !positiveIntegerValue(order.year) ||
    order.year < 2000 ||
    order.year > 2100 ||
    !positiveIntegerValue(order.quarter) ||
    order.quarter > 4 ||
    !positiveIntegerValue(order.group_count) ||
    order.group_count < 2 ||
    order.group_count > 100 ||
    !positiveIntegerValue(order.before_position) ||
    order.before_position > order.group_count ||
    !positiveIntegerValue(order.after_position) ||
    order.after_position > order.group_count ||
    order.before_position === order.after_position ||
    typeof order.group_fingerprint !== "string" ||
    !/^[a-f0-9]{64}$/.test(order.group_fingerprint)
  )
    return false;
  if (
    value.result_id !== null &&
    value.result_id !== action.roadmap_milestone_id
  )
    return false;
  if (
    value.result_version !== null &&
    value.result_version !== action.expected_version &&
    value.result_version !== (action.expected_version as number) + 1
  )
    return false;
  return (
    value.status !== "confirmed" ||
    (value.result_id === action.roadmap_milestone_id &&
      value.result_version !== null)
  );
}
