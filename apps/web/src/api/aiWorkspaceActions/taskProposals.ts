import {
  canonicalUuid,
  exactKeys,
  fields,
  isPositiveInteger,
  object,
  positiveIntegerValue,
  uuid,
} from "./primitives";
import type { AiInboxSplitDraft, AiTaskOption } from "./types";

export function validTaskReviewPolicyProposal(
  value: Record<string, unknown>,
): boolean {
  const action = value.action as Record<string, unknown>;
  if (action.action !== "task.update") return true;
  const changes = action.changes as Record<string, unknown>;
  const preview = value.preview as Record<string, unknown>;
  const before = preview.before as Record<string, unknown>;
  const after = preview.after as Record<string, unknown>;
  const rollupFields = [
    "subtask_total",
    "subtask_completed",
    "subtask_cancelled",
    "parent_rollup_gates_ready",
    "will_request_parent_review",
  ];
  if (!Object.hasOwn(changes, "review_policy"))
    return ["review_policy", ...rollupFields].every(
      (field) => !Object.hasOwn(before, field) && !Object.hasOwn(after, field),
    );
  const policy = (entry: unknown) => entry === "none" || entry === "manual";
  const allowedChanges = [
    "title",
    "description",
    "priority",
    "kind",
    "planned_date",
    "due_date",
    "estimated_minutes",
    "project_id",
    "parent_task_id",
    "completion_criteria",
    "tag_ids",
    "review_policy",
  ];
  const changedFields = Object.keys(changes).map((field) =>
    field === "tag_ids"
      ? "tag_names"
      : field === "parent_task_id"
        ? "parent_task_title"
        : field,
  );
  if (
    !exactKeys(action, ["action", "task_id", "expected_version", "changes"]) ||
    Object.keys(changes).some((field) => !allowedChanges.includes(field)) ||
    ["title", "description", "kind", "priority", "completion_criteria"].some(
      (field) => Object.hasOwn(changes, field) && changes[field] === null,
    ) ||
    !exactKeys(preview, ["label", "before", "after"]) ||
    !exactKeys(before, [...changedFields, "status"]) ||
    !exactKeys(after, [...changedFields, "status", ...rollupFields]) ||
    !policy(changes.review_policy) ||
    !policy(before.review_policy) ||
    after.review_policy !== changes.review_policy ||
    ![
      "todo",
      "in_progress",
      "blocked",
      "waiting_review",
      "done",
      "cancelled",
    ].includes(String(before.status)) ||
    (before.review_policy !== changes.review_policy &&
      before.status !== "todo") ||
    ["subtask_total", "subtask_completed", "subtask_cancelled"].some(
      (field) =>
        typeof after[field] !== "number" ||
        !Number.isSafeInteger(after[field]) ||
        Number(after[field]) < 0,
    ) ||
    typeof after.parent_rollup_gates_ready !== "boolean" ||
    typeof after.will_request_parent_review !== "boolean"
  )
    return false;
  const remaining =
    Number(after.subtask_total) - Number(after.subtask_cancelled);
  if (remaining < 0 || Number(after.subtask_completed) > remaining)
    return false;
  const willRequestReview =
    before.review_policy === "none" &&
    changes.review_policy === "manual" &&
    before.status === "todo" &&
    remaining > 0 &&
    Number(after.subtask_completed) === remaining &&
    after.parent_rollup_gates_ready;
  return (
    after.will_request_parent_review === willRequestReview &&
    after.status === (willRequestReview ? "waiting_review" : before.status)
  );
}

export function validTaskTagProposal(value: Record<string, unknown>): boolean {
  const action = value.action as Record<string, unknown>;
  if (!["task.create", "task.update"].includes(String(action.action)))
    return true;
  const changes = action.changes as Record<string, unknown>;
  const before = (value.preview as Record<string, unknown>).before as Record<
    string,
    unknown
  >;
  const after = (value.preview as Record<string, unknown>).after as Record<
    string,
    unknown
  >;
  if (!Object.hasOwn(changes, "tag_ids"))
    return (
      !Object.hasOwn(before, "tag_names") && !Object.hasOwn(after, "tag_names")
    );
  const ids = changes.tag_ids;
  const beforeNames = before.tag_names;
  const afterNames = after.tag_names;
  const validNames = (entry: unknown): entry is string[] =>
    Array.isArray(entry) &&
    entry.length <= 20 &&
    new Set(entry).size === entry.length &&
    entry.every(
      (name) =>
        typeof name === "string" &&
        name.trim().length > 0 &&
        Array.from(name).length <= 50,
    );
  return (
    Array.isArray(ids) &&
    ids.length <= 20 &&
    new Set(ids).size === ids.length &&
    ids.every((id) => typeof id === "string" && canonicalUuid.test(id)) &&
    validNames(afterNames) &&
    afterNames.length === ids.length &&
    (action.action === "task.create"
      ? !Object.hasOwn(before, "tag_names")
      : validNames(beforeNames))
  );
}

export function validTaskParentProposal(
  value: Record<string, unknown>,
): boolean {
  const action = value.action as Record<string, unknown>;
  if (!["task.create", "task.update"].includes(String(action.action)))
    return true;
  const changes = action.changes as Record<string, unknown>;
  const before = (value.preview as Record<string, unknown>).before as Record<
    string,
    unknown
  >;
  const after = (value.preview as Record<string, unknown>).after as Record<
    string,
    unknown
  >;
  if (!Object.hasOwn(changes, "parent_task_id"))
    return (
      !Object.hasOwn(before, "parent_task_title") &&
      !Object.hasOwn(after, "parent_task_title")
    );
  const parentId = changes.parent_task_id;
  const validTitle = (entry: unknown) =>
    entry === null ||
    (typeof entry === "string" &&
      entry.trim().length > 0 &&
      Array.from(entry).length <= 200);
  return (
    (parentId === null ||
      (typeof parentId === "string" && canonicalUuid.test(parentId))) &&
    Object.hasOwn(after, "parent_task_title") &&
    validTitle(after.parent_task_title) &&
    (action.action === "task.create"
      ? !Object.hasOwn(before, "parent_task_title")
      : Object.hasOwn(before, "parent_task_title") &&
        validTitle(before.parent_task_title))
  );
}

export function validTaskDeleteProposal(
  value: Record<string, unknown>,
): boolean {
  const action = value.action as Record<string, unknown>;
  if (action.action !== "task.delete") return true;
  const preview = value.preview as Record<string, unknown>;
  const before = preview.before as Record<string, unknown>;
  const after = preview.after as Record<string, unknown>;
  const counts = [
    "child_tasks_moved_to_top_level",
    "submissions_deleted",
    "artifacts_deleted",
    "file_artifacts_deleted",
    "assignments_deleted",
    "agent_runs_deleted",
    "focus_sessions_detached",
    "inbox_relations_detached",
    "inbox_sources_coordinated",
    "tag_links_deleted",
  ];
  return (
    Object.keys(action.changes as Record<string, unknown>).length === 0 &&
    Object.keys(preview).length === 3 &&
    Object.keys(before).length === 0 &&
    Object.keys(after).length === counts.length + 1 &&
    after.task_deleted === true &&
    counts.every(
      (key) =>
        typeof after[key] === "number" &&
        Number.isSafeInteger(after[key]) &&
        Number(after[key]) >= 0,
    )
  );
}

export function validTagProposal(value: Record<string, unknown>): boolean {
  const action = value.action as Record<string, unknown>;
  if (!String(action.action).startsWith("tag.")) return true;
  const preview = value.preview as Record<string, unknown>;
  const before = preview.before as Record<string, unknown>;
  const after = preview.after as Record<string, unknown>;
  const changes = action.changes as Record<string, unknown>;
  const creating = action.action === "tag.create";
  const updating = action.action === "tag.update";
  const deleting = action.action === "tag.delete";
  const validName = (entry: unknown) =>
    typeof entry === "string" &&
    entry === entry.trim() &&
    Array.from(entry).length >= 1 &&
    Array.from(entry).length <= 50;
  const validColor = (entry: unknown) =>
    typeof entry === "string" && /^#[0-9A-F]{6}$/.test(entry);
  if (
    !exactKeys(preview, ["label", "before", "after"]) ||
    typeof preview.label !== "string" ||
    !validName(preview.label)
  )
    return false;
  if (creating) {
    return (
      exactKeys(action, ["action", "changes"]) &&
      exactKeys(changes, ["name", "color"]) &&
      Object.keys(before).length === 0 &&
      exactKeys(after, ["name", "color"]) &&
      validName(changes.name) &&
      validColor(changes.color) &&
      changes.name === after.name &&
      changes.color === after.color &&
      preview.label === after.name
    );
  }
  if (
    !exactKeys(action, ["action", "tag_id", "expected_version", "changes"]) ||
    typeof action.tag_id !== "string" ||
    !canonicalUuid.test(action.tag_id) ||
    !positiveIntegerValue(action.expected_version)
  )
    return false;
  if (deleting) {
    return (
      Object.keys(changes).length === 0 &&
      exactKeys(before, ["name", "color", "tag_version"]) &&
      exactKeys(after, ["tag_deleted", "detached_tasks"]) &&
      validName(before.name) &&
      validColor(before.color) &&
      before.tag_version === action.expected_version &&
      after.tag_deleted === true &&
      typeof after.detached_tasks === "number" &&
      Number.isSafeInteger(after.detached_tasks) &&
      Number(after.detached_tasks) >= 0 &&
      preview.label === before.name
    );
  }
  if (!updating) return false;
  const keys = Object.keys(changes);
  return (
    keys.length >= 1 &&
    keys.length <= 2 &&
    keys.every((key) => key === "name" || key === "color") &&
    exactKeys(before, keys) &&
    exactKeys(after, keys) &&
    keys.every((key) =>
      key === "name"
        ? validName(changes[key]) &&
          validName(before[key]) &&
          changes[key] === after[key]
        : validColor(changes[key]) &&
          validColor(before[key]) &&
          changes[key] === after[key],
    ) &&
    preview.label === before.name
  );
}

function option(value: unknown, types: string[]): value is AiTaskOption {
  return (
    object(value) &&
    typeof value.id === "string" &&
    uuid.test(value.id) &&
    typeof value.label === "string" &&
    value.label.length > 0 &&
    typeof value.type === "string" &&
    types.includes(value.type) &&
    typeof value.version === "number" &&
    Number.isSafeInteger(value.version) &&
    value.version > 0
  );
}

export function splitDrafts(
  value: unknown,
  changes: Record<string, unknown>,
): value is AiInboxSplitDraft[] {
  if (
    !Array.isArray(value) ||
    value.length < 1 ||
    value.length > 20 ||
    !Array.isArray(changes.tasks) ||
    changes.tasks.length !== value.length
  )
    return false;
  const keys = new Set<string>();
  const rawTasks = changes.tasks;
  return value.every((draft, index) => {
    const raw = rawTasks[index];
    if (
      !object(draft) ||
      !object(raw) ||
      typeof draft.key !== "string" ||
      !draft.key ||
      keys.has(draft.key) ||
      typeof draft.parent_key !== "string" ||
      (draft.parent_key !== "" && !keys.has(draft.parent_key)) ||
      typeof raw.key !== "string" ||
      draft.key !== raw.key.trim() ||
      draft.parent_key !==
        (typeof raw.parent_key === "string" ? raw.parent_key.trim() : "") ||
      !fields(draft.fields) ||
      !option(draft.assignee, ["owner", "person", "agent"]) ||
      draft.assignee.id !== raw.assignee_actor_id ||
      typeof draft.is_required !== "boolean" ||
      draft.is_required !== raw.is_required ||
      !Array.isArray(draft.tags) ||
      !draft.tags.every((tag) => option(tag, ["tag"])) ||
      new Set(draft.tags.map((tag) => tag.id)).size !== draft.tags.length ||
      (draft.project !== null && !option(draft.project, ["project"])) ||
      (draft.reviewer !== null && !option(draft.reviewer, ["owner"]))
    )
      return false;
    const f = draft.fields;
    if (
      [
        "title",
        "description",
        "priority",
        "kind",
        "completion_criteria",
        "review_policy",
        "status",
      ].some((key) => typeof f[key] !== "string") ||
      typeof raw.title !== "string" ||
      f.title !== raw.title.trim() ||
      !["work", "review", "followup", "reminder"].includes(String(f.kind)) ||
      !["P0", "P1", "P2", "P3"].includes(String(f.priority)) ||
      !["none", "manual"].includes(String(f.review_policy)) ||
      f.status !== "todo" ||
      ["planned_date", "due_date"].some(
        (key) => f[key] !== null && typeof f[key] !== "string",
      ) ||
      (f.estimated_minutes !== null &&
        (typeof f.estimated_minutes !== "number" ||
          !Number.isSafeInteger(f.estimated_minutes) ||
          f.estimated_minutes < 0)) ||
      f.project_id !== (draft.project?.id ?? null) ||
      (raw.project_id ?? null) !== f.project_id ||
      (f.review_policy === "manual") !== (draft.reviewer !== null) ||
      (draft.reviewer !== null &&
        draft.reviewer.id !== "00000000-0000-5000-8000-000000000001")
    )
      return false;
    const tagIds = raw.tag_ids ?? [];
    const tags = draft.tags;
    if (
      !Array.isArray(tagIds) ||
      tagIds.length !== draft.tags.length ||
      tagIds.some((id) => !tags.some((tag) => tag.id === id))
    )
      return false;
    keys.add(draft.key);
    return true;
  });
}

const taskViewDefinitionKeys = [
  "q",
  "status",
  "priority",
  "kind",
  "project_id",
  "client_id",
  "tag_ids",
  "planned_date",
  "planned_from",
  "planned_to",
  "due_from",
  "due_to",
  "sort",
];

function validTaskViewDefinition(value: unknown): boolean {
  if (!object(value)) return false;
  if (Object.keys(value).some((key) => !taskViewDefinitionKeys.includes(key)))
    return false;
  for (const key of ["q", "status", "priority", "kind", "sort"]) {
    if (value[key] !== undefined && typeof value[key] !== "string")
      return false;
  }
  if (typeof value.q === "string" && Array.from(value.q).length > 200)
    return false;
  for (const key of ["project_id", "client_id"]) {
    if (value[key] === undefined || value[key] === "") continue;
    if (typeof value[key] !== "string" || !canonicalUuid.test(value[key]))
      return false;
  }
  if (value.tag_ids !== undefined) {
    if (
      !Array.isArray(value.tag_ids) ||
      value.tag_ids.length > 20 ||
      new Set(value.tag_ids).size !== value.tag_ids.length ||
      value.tag_ids.some(
        (id) => typeof id !== "string" || !canonicalUuid.test(id),
      )
    )
      return false;
  }
  const dateKeys = [
    "planned_date",
    "planned_from",
    "planned_to",
    "due_from",
    "due_to",
  ];
  for (const key of dateKeys) {
    // The server always emits every definition field, so an empty string means
    // "no filter" rather than a malformed date.
    if (value[key] === "") continue;
    if (
      value[key] !== undefined &&
      (typeof value[key] !== "string" ||
        !/^\d{4}-\d{2}-\d{2}$/.test(value[key] as string))
    )
      return false;
  }
  if (
    typeof value.planned_date === "string" &&
    value.planned_date !== "" &&
    ((typeof value.planned_from === "string" && value.planned_from !== "") ||
      (typeof value.planned_to === "string" && value.planned_to !== ""))
  )
    return false;
  if (
    typeof value.planned_from === "string" &&
    typeof value.planned_to === "string" &&
    value.planned_from !== "" &&
    value.planned_to !== "" &&
    value.planned_from > value.planned_to
  )
    return false;
  if (
    typeof value.due_from === "string" &&
    typeof value.due_to === "string" &&
    value.due_from !== "" &&
    value.due_to !== "" &&
    value.due_from > value.due_to
  )
    return false;
  return true;
}

// Saved views are local filter presets. The card must prove the server froze
// the same identity/version and never smuggled in a task write.
export function validTaskViewProposal(value: Record<string, unknown>): boolean {
  const action = value.action;
  const preview = value.preview;
  if (!object(action) || !object(preview)) return false;
  const name = String(action.action);
  if (
    !["task_view.create", "task_view.update", "task_view.delete"].includes(name)
  )
    return false;
  const view = preview.task_view;
  if (!object(view)) return false;
  const changes = action.changes;
  const before = preview.before;
  const after = preview.after;
  if (!object(changes) || !object(before) || !object(after)) return false;
  if (
    typeof view.name !== "string" ||
    view.name.trim().length === 0 ||
    Array.from(view.name).length > 80 ||
    !validTaskViewDefinition(view.definition)
  )
    return false;
  if (view.project_name !== undefined && typeof view.project_name !== "string")
    return false;
  if (view.client_name !== undefined && typeof view.client_name !== "string")
    return false;
  if (
    view.tag_names !== undefined &&
    (!Array.isArray(view.tag_names) ||
      view.tag_names.some((tag) => typeof tag !== "string"))
  )
    return false;
  if (name === "task_view.create") {
    if (
      action.task_saved_view_id !== undefined ||
      action.expected_version !== undefined ||
      Object.keys(changes).some(
        (key) => key !== "name" && key !== "definition",
      ) ||
      typeof changes.name !== "string" ||
      changes.name.trim() !== view.name ||
      !validTaskViewDefinition(changes.definition) ||
      JSON.stringify(changes.definition) !== JSON.stringify(view.definition) ||
      Object.keys(before).length !== 0 ||
      after.name !== view.name ||
      view.id !== undefined ||
      view.deleted === true ||
      view.next_version !== 1
    )
      return false;
    if (value.result_id !== null && typeof value.result_id !== "string")
      return false;
    if (
      value.result_id !== null &&
      !canonicalUuid.test(String(value.result_id))
    )
      return false;
    if (value.result_version !== null && value.result_version !== 1)
      return false;
    if (
      value.status === "confirmed" &&
      (value.result_id === null || value.result_version !== 1)
    )
      return false;
    return true;
  }
  if (
    typeof action.task_saved_view_id !== "string" ||
    !canonicalUuid.test(action.task_saved_view_id) ||
    !isPositiveInteger(action.expected_version) ||
    view.id !== action.task_saved_view_id ||
    view.version !== action.expected_version ||
    typeof before.name !== "string" ||
    before.name.trim().length === 0 ||
    Object.keys(before).length !== 1
  )
    return false;
  if (name === "task_view.delete") {
    if (
      Object.keys(changes).length !== 0 ||
      view.deleted !== true ||
      Object.keys(after).length !== 0
    )
      return false;
  } else {
    if (
      Object.keys(changes).length === 0 ||
      Object.keys(changes).some(
        (key) => key !== "name" && key !== "definition",
      ) ||
      (changes.name !== undefined &&
        (typeof changes.name !== "string" ||
          changes.name.trim() !== view.name)) ||
      (changes.definition !== undefined &&
        (!validTaskViewDefinition(changes.definition) ||
          JSON.stringify(changes.definition) !==
            JSON.stringify(view.definition))) ||
      after.name !== view.name ||
      Object.keys(after).length !== 1 ||
      view.deleted === true ||
      view.next_version !== view.version + 1
    )
      return false;
  }
  if (value.result_id !== null && value.result_id !== action.task_saved_view_id)
    return false;
  if (
    value.result_version !== null &&
    value.result_version !== action.expected_version &&
    value.result_version !== (action.expected_version as number) + 1
  )
    return false;
  if (
    value.status === "confirmed" &&
    (value.result_id === null || value.result_version === null)
  )
    return false;
  return true;
}

export function validTaskOrderProposal(
  value: Record<string, unknown>,
): boolean {
  const action = value.action;
  const preview = value.preview;
  if (!object(action) || !object(preview)) return false;
  const changes = action.changes;
  const order = preview.task_order;
  if (
    !exactKeys(action, ["action", "task_id", "expected_version", "changes"]) ||
    !object(changes) ||
    !exactKeys(changes, [
      "anchor_task_id",
      "expected_anchor_version",
      "placement",
    ]) ||
    !exactKeys(preview, ["label", "before", "after", "task_order"]) ||
    !object(order) ||
    !exactKeys(order, [
      "task_id",
      "title",
      "anchor_task_id",
      "anchor_title",
      "placement",
      "planned_date",
      "group_count",
      "active_count",
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
    typeof action.task_id !== "string" ||
    !canonicalUuid.test(action.task_id) ||
    !positiveIntegerValue(action.expected_version) ||
    typeof changes.anchor_task_id !== "string" ||
    !canonicalUuid.test(changes.anchor_task_id) ||
    changes.anchor_task_id === action.task_id ||
    !positiveIntegerValue(changes.expected_anchor_version) ||
    (changes.placement !== "before" && changes.placement !== "after") ||
    order.task_id !== action.task_id ||
    order.anchor_task_id !== changes.anchor_task_id ||
    order.placement !== changes.placement ||
    typeof order.title !== "string" ||
    order.title.trim().length < 2 ||
    Array.from(order.title).length > 200 ||
    typeof order.anchor_title !== "string" ||
    order.anchor_title.trim().length < 2 ||
    Array.from(order.anchor_title).length > 200 ||
    preview.label !== order.title ||
    !(
      order.planned_date === null ||
      (typeof order.planned_date === "string" &&
        /^\d{4}-\d{2}-\d{2}$/.test(order.planned_date))
    ) ||
    !positiveIntegerValue(order.group_count) ||
    order.group_count > 1000 ||
    !positiveIntegerValue(order.active_count) ||
    order.active_count < 2 ||
    order.active_count > order.group_count ||
    !positiveIntegerValue(order.before_position) ||
    !positiveIntegerValue(order.after_position) ||
    order.before_position > order.active_count ||
    order.after_position > order.active_count ||
    order.before_position === order.after_position ||
    typeof order.group_fingerprint !== "string" ||
    !/^[a-f0-9]{64}$/.test(order.group_fingerprint)
  )
    return false;
  if (value.result_id !== null && value.result_id !== action.task_id)
    return false;
  if (
    value.result_version !== null &&
    value.result_version !== action.expected_version &&
    value.result_version !== (action.expected_version as number) + 1
  )
    return false;
  if (
    value.status === "confirmed" &&
    (value.result_id !== action.task_id || value.result_version === null)
  )
    return false;
  return true;
}

export function validTaskBatchCreateProposal(
  value: Record<string, unknown>,
): boolean {
  const action = value.action;
  const preview = value.preview;
  if (!object(action) || !object(preview) || !object(action.changes))
    return false;
  const changes = action.changes;
  const batch = preview.task_batch_create;
  if (
    !exactKeys(action, [
      "action",
      "project_id",
      "expected_version",
      "changes",
    ]) ||
    !canonicalUuid.test(String(action.project_id)) ||
    !positiveIntegerValue(action.expected_version) ||
    !exactKeys(changes, ["drafts"]) ||
    !Array.isArray(changes.drafts) ||
    changes.drafts.length < 1 ||
    changes.drafts.length > 20 ||
    !object(batch) ||
    batch.project_id !== action.project_id ||
    batch.project_version !== action.expected_version ||
    typeof batch.project_name !== "string" ||
    !batch.project_name.trim() ||
    batch.count !== changes.drafts.length ||
    !Array.isArray(batch.items) ||
    batch.items.length !== changes.drafts.length ||
    !object(preview.before) ||
    Object.keys(preview.before).length !== 0 ||
    !object(preview.after) ||
    !exactKeys(preview.after, ["count"]) ||
    preview.after.count !== batch.count ||
    value.route !== `/projects/${action.project_id}`
  )
    return false;
  const allowedDraft = new Set([
    "key",
    "parent_key",
    "title",
    "description",
    "kind",
    "priority",
    "planned_date",
    "due_date",
    "estimated_minutes",
    "completion_criteria",
    "review_policy",
    "tag_ids",
    "assignee_actor_id",
  ]);
  const known = new Set<string>();
  for (const [index, rawDraft] of changes.drafts.entries()) {
    const item = batch.items[index];
    if (!object(rawDraft) || !object(item)) return false;
    const key = rawDraft.key;
    if (
      Object.keys(rawDraft).some((field) => !allowedDraft.has(field)) ||
      typeof key !== "string" ||
      !/^[a-zA-Z][a-zA-Z0-9_-]{0,49}$/.test(key) ||
      known.has(key) ||
      (rawDraft.parent_key !== undefined &&
        (typeof rawDraft.parent_key !== "string" ||
          !known.has(rawDraft.parent_key))) ||
      typeof rawDraft.title !== "string" ||
      !exactKeys(item, [
        "key",
        "title",
        "description",
        "kind",
        "priority",
        "planned_date",
        "due_date",
        "estimated_minutes",
        "completion_criteria",
        "review_policy",
        "tag_ids",
        "tag_names",
        ...(rawDraft.parent_key ? ["parent_key"] : []),
        ...(rawDraft.assignee_actor_id ? ["assignments"] : []),
      ]) ||
      item.key !== key ||
      item.parent_key !== rawDraft.parent_key ||
      item.title !== rawDraft.title.trim() ||
      typeof item.description !== "string" ||
      typeof item.completion_criteria !== "string" ||
      !["work", "review", "followup", "reminder"].includes(String(item.kind)) ||
      !["P0", "P1", "P2", "P3"].includes(String(item.priority)) ||
      !["none", "manual"].includes(String(item.review_policy)) ||
      (item.planned_date !== null &&
        (typeof item.planned_date !== "string" ||
          !/^\d{4}-\d{2}-\d{2}$/.test(item.planned_date))) ||
      (item.due_date !== null && typeof item.due_date !== "string") ||
      (item.estimated_minutes !== null &&
        (typeof item.estimated_minutes !== "number" ||
          !Number.isSafeInteger(item.estimated_minutes) ||
          item.estimated_minutes < 0)) ||
      !Array.isArray(item.tag_ids) ||
      item.tag_ids.some(
        (id) => typeof id !== "string" || !canonicalUuid.test(id),
      ) ||
      !Array.isArray(item.tag_names) ||
      item.tag_names.some((name) => typeof name !== "string")
    )
      return false;
    if (rawDraft.assignee_actor_id !== undefined) {
      if (
        typeof rawDraft.assignee_actor_id !== "string" ||
        !canonicalUuid.test(rawDraft.assignee_actor_id) ||
        !Array.isArray(item.assignments) ||
        item.assignments.length !== (item.review_policy === "manual" ? 2 : 1)
      )
        return false;
      for (const [assignmentIndex, assignment] of item.assignments.entries()) {
        if (
          !object(assignment) ||
          !exactKeys(assignment, [
            "role",
            "actor_id",
            "actor_name",
            "actor_type",
            "actor_status",
            "actor_version",
          ]) ||
          typeof assignment.actor_id !== "string" ||
          !canonicalUuid.test(assignment.actor_id) ||
          typeof assignment.actor_name !== "string" ||
          !assignment.actor_name.trim() ||
          assignment.actor_status !== "active" ||
          !positiveIntegerValue(assignment.actor_version)
        )
          return false;
        if (assignmentIndex === 0) {
          if (
            assignment.role !== "assignee" ||
            assignment.actor_id !== rawDraft.assignee_actor_id ||
            !["owner", "person", "agent"].includes(
              String(assignment.actor_type),
            )
          )
            return false;
        } else if (
          assignment.role !== "reviewer" ||
          assignment.actor_type !== "owner" ||
          assignment.actor_id !== "00000000-0000-5000-8000-000000000001"
        )
          return false;
      }
    }
    const tagIDs = rawDraft.tag_ids === undefined ? [] : rawDraft.tag_ids;
    if (
      item.description !== (rawDraft.description ?? "") ||
      item.completion_criteria !== (rawDraft.completion_criteria ?? "") ||
      item.kind !== (rawDraft.kind ?? "work") ||
      item.priority !== (rawDraft.priority ?? "P2") ||
      item.review_policy !== (rawDraft.review_policy ?? "none") ||
      item.estimated_minutes !== (rawDraft.estimated_minutes ?? null) ||
      item.planned_date !==
        (typeof rawDraft.planned_date === "string"
          ? rawDraft.planned_date.trim()
          : null) ||
      (rawDraft.due_date === undefined
        ? item.due_date !== null
        : typeof rawDraft.due_date !== "string" ||
          typeof item.due_date !== "string" ||
          !Number.isFinite(Date.parse(rawDraft.due_date)) ||
          Date.parse(rawDraft.due_date) !== Date.parse(item.due_date)) ||
      !Array.isArray(tagIDs) ||
      tagIDs.some(
        (id) => typeof id !== "string" || !canonicalUuid.test(id.trim()),
      ) ||
      JSON.stringify(item.tag_ids) !==
        JSON.stringify(
          [...new Set(tagIDs.map((id: string) => id.trim()))].sort(),
        ) ||
      item.tag_names.length !== item.tag_ids.length
    )
      return false;
    known.add(key);
  }
  if (value.status === "confirmed") {
    const result = value.task_batch_create_result;
    if (
      value.result_id !== value.id ||
      value.result_version !== 1 ||
      !object(result) ||
      result.project_id !== action.project_id ||
      result.count !== batch.count ||
      !Array.isArray(result.items) ||
      result.items.length !== batch.count
    )
      return false;
    for (const [index, item] of result.items.entries()) {
      const draft = batch.items[index];
      if (
        !object(item) ||
        !exactKeys(item, ["key", "id", "title", "route"]) ||
        item.key !== draft.key ||
        item.title !== draft.title ||
        typeof item.id !== "string" ||
        !canonicalUuid.test(item.id) ||
        item.route !== `/tasks/${item.id}`
      )
        return false;
    }
  } else if (
    value.task_batch_create_result !== undefined ||
    value.result_id !== null ||
    value.result_version !== null
  ) {
    return false;
  }
  return true;
}

function validTaskBatchActor(
  value: unknown,
  role: "assignee" | "reviewer",
  activeOnly: boolean,
): value is Record<string, unknown> {
  return (
    object(value) &&
    exactKeys(value, [
      "role",
      "actor_id",
      "actor_name",
      "actor_type",
      "actor_status",
      "actor_version",
    ]) &&
    value.role === role &&
    typeof value.actor_id === "string" &&
    canonicalUuid.test(value.actor_id) &&
    typeof value.actor_name === "string" &&
    Boolean(value.actor_name.trim()) &&
    ["owner", "person", "agent"].includes(String(value.actor_type)) &&
    (role !== "reviewer" || value.actor_type === "owner") &&
    ["active", "inactive"].includes(String(value.actor_status)) &&
    (!activeOnly || value.actor_status === "active") &&
    positiveIntegerValue(value.actor_version)
  );
}

function validTaskBatchUtcInstant(value: unknown): value is string {
  if (
    typeof value !== "string" ||
    !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(value)
  )
    return false;
  const parsed = new Date(value);
  return (
    !Number.isNaN(parsed.getTime()) &&
    parsed.toISOString().slice(0, 19) === value.slice(0, 19)
  );
}

export function validTaskBatchProposal(
  value: Record<string, unknown>,
): boolean {
  const action = value.action;
  const preview = value.preview;
  if (!object(action) || !object(preview)) return false;
  if (action.action !== "task.batch_update") return true;
  const changes = action.changes;
  const batch = preview.task_batch;
  const before = preview.before;
  const after = preview.after;
  if (!object(changes) || !object(batch) || !object(before) || !object(after))
    return false;
  const batchActions = [
    "set_priority",
    "set_due_date",
    "set_project",
    "set_planned_date",
    "add_tags",
    "remove_tags",
    "start",
    "block",
    "unblock",
    "complete",
    "cancel",
    "reopen",
    "set_assignee",
    "clear_assignee",
    "set_reviewer",
    "clear_reviewer",
  ];
  const name = changes.batch_action;
  if (typeof name !== "string" || !batchActions.includes(name)) return false;
  const role = name.endsWith("_assignee")
    ? "assignee"
    : name.endsWith("_reviewer")
      ? "reviewer"
      : null;
  const settingResponsibility = name.startsWith("set_") && role !== null;
  const items = changes.items;
  const versions = changes.expected_versions;
  if (
    !Array.isArray(items) ||
    items.length === 0 ||
    items.length > 20 ||
    new Set(items).size !== items.length ||
    items.some((id) => typeof id !== "string" || !canonicalUuid.test(id)) ||
    !Array.isArray(versions) ||
    versions.length !== items.length ||
    versions.some((version) => !positiveIntegerValue(version))
  )
    return false;
  const allowed = new Set(["batch_action", "items", "expected_versions"]);
  if (name === "set_priority") allowed.add("priority");
  if (name === "set_due_date") allowed.add("due_date");
  if (name === "set_project") allowed.add("project_id");
  if (name === "set_planned_date") allowed.add("planned_date");
  if (name === "add_tags" || name === "remove_tags") allowed.add("tag_ids");
  if (name === "block" || name === "cancel") allowed.add("reason");
  if (role !== null) allowed.add("reason");
  if (settingResponsibility) allowed.add("actor_id");
  if (Object.keys(changes).some((key) => !allowed.has(key))) return false;
  if (
    name === "set_priority" &&
    !["P0", "P1", "P2", "P3"].includes(String(changes.priority))
  )
    return false;
  if (
    name === "set_due_date" &&
    !(
      Object.hasOwn(changes, "due_date") &&
      (changes.due_date === null || validTaskBatchUtcInstant(changes.due_date))
    )
  )
    return false;
  if (
    name === "set_project" &&
    !(
      Object.hasOwn(changes, "project_id") &&
      (changes.project_id === null ||
        (typeof changes.project_id === "string" &&
          canonicalUuid.test(changes.project_id)))
    )
  )
    return false;
  if (
    name === "set_planned_date" &&
    !(
      Object.hasOwn(changes, "planned_date") &&
      (changes.planned_date === null ||
        (typeof changes.planned_date === "string" &&
          /^\d{4}-\d{2}-\d{2}$/.test(changes.planned_date)))
    )
  )
    return false;
  if (name === "add_tags" || name === "remove_tags") {
    const tagIDs = changes.tag_ids;
    if (
      !Array.isArray(tagIDs) ||
      tagIDs.length === 0 ||
      tagIDs.length > 20 ||
      new Set(tagIDs).size !== tagIDs.length ||
      tagIDs.some((id) => typeof id !== "string" || !canonicalUuid.test(id))
    )
      return false;
  }
  if (
    settingResponsibility &&
    (typeof changes.actor_id !== "string" ||
      !canonicalUuid.test(changes.actor_id))
  )
    return false;
  if (name === "block" || name === "cancel" || role !== null) {
    if (
      typeof changes.reason !== "string" ||
      Array.from(changes.reason.trim()).length < (role !== null ? 1 : 2) ||
      Array.from(changes.reason.trim()).length > 1000 ||
      batch.reason !== changes.reason.trim()
    )
      return false;
  } else if (batch.reason !== undefined) {
    return false;
  }
  if (
    Object.keys(before).length !== 0 ||
    Object.keys(after).length !== 1 ||
    after.count !== items.length ||
    batch.action !== name ||
    batch.count !== items.length ||
    !Array.isArray(batch.items) ||
    batch.items.length !== items.length
  )
    return false;
  const expectedField =
    role !== null
      ? role
      : name === "set_priority"
        ? "priority"
        : name === "set_due_date"
          ? "due_date"
          : name === "set_project"
            ? "project"
            : name === "set_planned_date"
              ? "planned_date"
              : name === "add_tags" || name === "remove_tags"
                ? "tags"
                : "status";
  for (const [index, row] of batch.items.entries()) {
    if (
      !object(row) ||
      !exactKeys(row, [
        "task_id",
        "title",
        "version",
        "field",
        "before",
        "after",
        ...(role !== null && row.assignment_id !== undefined
          ? ["assignment_id"]
          : []),
        ...(role !== null && row.before_actor !== undefined
          ? ["before_actor"]
          : []),
        ...(role !== null && row.after_actor !== undefined
          ? ["after_actor"]
          : []),
      ]) ||
      row.task_id !== items[index] ||
      row.version !== versions[index] ||
      row.field !== expectedField ||
      typeof row.title !== "string" ||
      row.title.trim().length < 2 ||
      Array.from(row.title).length > 200 ||
      typeof row.before !== "string" ||
      typeof row.after !== "string"
    )
      return false;
    if (
      name === "set_priority" &&
      (row.after !== changes.priority ||
        !["P0", "P1", "P2", "P3"].includes(row.before))
    )
      return false;
    if (
      name === "set_due_date" &&
      (row.after !== (changes.due_date ?? "未安排") ||
        (row.before !== "未安排" && !validTaskBatchUtcInstant(row.before)))
    )
      return false;
    if (role !== null) {
      const current = row.before_actor;
      const target = row.after_actor;
      if (current === undefined) {
        if (
          row.assignment_id !== undefined ||
          row.before !== "未分派" ||
          !settingResponsibility
        )
          return false;
      } else if (
        !validTaskBatchActor(current, role, false) ||
        typeof row.assignment_id !== "string" ||
        !canonicalUuid.test(row.assignment_id) ||
        row.before !== current.actor_name
      )
        return false;
      if (settingResponsibility) {
        if (
          !validTaskBatchActor(target, role, true) ||
          target.actor_id !== changes.actor_id ||
          row.after !== target.actor_name ||
          (current !== undefined && current.actor_id === target.actor_id)
        )
          return false;
      } else if (target !== undefined || row.after !== "未分派") return false;
    }
  }
  const targetKeys = [
    "task_id",
    "project_id",
    "tag_id",
    "inbox_item_id",
    "reminder_id",
    "focus_session_id",
    "client_followup_id",
    "client_activity_id",
    "automation_rule_id",
    "automation_run_id",
    "roadmap_milestone_id",
    "content_item_id",
    "knowledge_source_id",
    "knowledge_index_job_id",
    "task_saved_view_id",
    "client_id",
    "client_actor_link_id",
    "actor_id",
    "expected_version",
  ];
  if (targetKeys.some((key) => action[key] !== undefined)) return false;
  if (value.result_id !== null && value.result_id !== value.id) return false;
  if (value.result_version !== null && value.result_version !== 1) return false;
  if (
    value.status === "confirmed" &&
    (value.result_id !== value.id || value.result_version !== 1)
  )
    return false;
  return true;
}
