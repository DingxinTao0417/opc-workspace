import { exactKeys, object, uuid } from "./primitives";

const clientEditableFields = [
  "name",
  "contact_name",
  "email",
  "phone",
  "notes",
  "status",
] as const;

function validClientField(field: string, value: unknown): boolean {
  if (field === "name")
    return (
      typeof value === "string" &&
      value === value.trim() &&
      Array.from(value).length >= 1 &&
      Array.from(value).length <= 200
    );
  if (["contact_name", "phone", "notes"].includes(field)) {
    const maximum = field === "notes" ? 10_000 : field === "phone" ? 50 : 200;
    return (
      value === null ||
      (typeof value === "string" &&
        value === value.trim() &&
        value.length > 0 &&
        Array.from(value).length <= maximum)
    );
  }
  if (field === "email")
    return (
      value === null ||
      (typeof value === "string" &&
        value === value.trim() &&
        value.length <= 320 &&
        /^[^\s@]+@[^\s@]+$/.test(value))
    );
  return (
    field === "status" && ["active", "lead", "inactive"].includes(String(value))
  );
}

export function validClientProposal(value: Record<string, unknown>): boolean {
  const action = value.action as Record<string, unknown>;
  const preview = value.preview as Record<string, unknown>;
  const changes = action.changes as Record<string, unknown>;
  const before = preview.before as Record<string, unknown>;
  const after = preview.after as Record<string, unknown>;
  if (action.action === "client.delete") {
    const counts = [
      "client_projects_detached",
      "client_activities_deleted",
      "client_contact_history_deleted",
      "client_attachments_deleted",
      "client_attachment_files_deleted",
    ];
    return (
      exactKeys(action, [
        "action",
        "client_id",
        "expected_version",
        "changes",
      ]) &&
      object(changes) &&
      Object.keys(changes).length === 0 &&
      exactKeys(preview, ["label", "before", "after"]) &&
      typeof preview.label === "string" &&
      preview.label.trim().length > 0 &&
      exactKeys(before, ["status", "client_version"]) &&
      before.status === "inactive" &&
      before.client_version === action.expected_version &&
      exactKeys(after, ["client_deleted", ...counts]) &&
      after.client_deleted === true &&
      counts.every(
        (key) =>
          typeof after[key] === "number" &&
          Number.isSafeInteger(after[key]) &&
          Number(after[key]) >= 0,
      )
    );
  }
  if (
    !exactKeys(preview, ["label", "before", "after"]) ||
    Object.keys(changes).some(
      (field) =>
        !clientEditableFields.includes(
          field as (typeof clientEditableFields)[number],
        ) || !validClientField(field, changes[field]),
    )
  )
    return false;
  if (action.action === "client.create") {
    if (
      !exactKeys(action, ["action", "changes"]) ||
      !Object.hasOwn(changes, "name") ||
      Object.keys(before).length !== 0 ||
      !exactKeys(after, [...clientEditableFields]) ||
      preview.label !== after.name ||
      Object.entries(after).some(
        ([field, entry]) => !validClientField(field, entry),
      )
    )
      return false;
    return Object.entries(changes).every(
      ([field, entry]) => after[field] === entry,
    );
  }
  if (action.action !== "client.update") return false;
  const keys = Object.keys(changes).sort();
  return (
    exactKeys(action, ["action", "client_id", "expected_version", "changes"]) &&
    keys.length > 0 &&
    JSON.stringify(Object.keys(before).sort()) === JSON.stringify(keys) &&
    JSON.stringify(Object.keys(after).sort()) === JSON.stringify(keys) &&
    Object.entries(before).every(([field, entry]) =>
      validClientField(field, entry),
    ) &&
    Object.entries(after).every(([field, entry]) =>
      validClientField(field, entry),
    ) &&
    Object.entries(changes).every(([field, entry]) => after[field] === entry) &&
    preview.label !== ""
  );
}

const personEditableFields = ["display_name", "notes", "status"] as const;

function validPersonField(field: string, value: unknown): boolean {
  if (field === "display_name")
    return (
      typeof value === "string" &&
      value === value.trim() &&
      Array.from(value).length >= 1 &&
      Array.from(value).length <= 100 &&
      // eslint-disable-next-line no-control-regex -- rejects control characters on purpose
      !/[\u0000-\u001f\u007f]/u.test(value)
    );
  if (field === "notes")
    return (
      typeof value === "string" &&
      Array.from(value).length <= 2_000 &&
      // eslint-disable-next-line no-control-regex -- rejects control characters on purpose
      !/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/u.test(value)
    );
  return field === "status" && ["active", "inactive"].includes(String(value));
}

export function validPersonProposal(value: Record<string, unknown>): boolean {
  const action = value.action as Record<string, unknown>;
  const preview = value.preview as Record<string, unknown>;
  const changes = action.changes as Record<string, unknown>;
  const before = preview.before as Record<string, unknown>;
  const after = preview.after as Record<string, unknown>;
  if (
    !exactKeys(preview, ["label", "before", "after"]) ||
    Object.keys(changes).some(
      (field) =>
        !personEditableFields.includes(
          field as (typeof personEditableFields)[number],
        ) || !validPersonField(field, changes[field]),
    )
  )
    return false;
  if (action.action === "person.create")
    return (
      exactKeys(action, ["action", "changes"]) &&
      Object.hasOwn(changes, "display_name") &&
      !Object.hasOwn(changes, "status") &&
      Object.keys(before).length === 0 &&
      exactKeys(after, ["display_name", "notes", "status", "person_type"]) &&
      preview.label === after.display_name &&
      after.person_type === "person" &&
      after.status === "active" &&
      validPersonField("display_name", after.display_name) &&
      validPersonField("notes", after.notes) &&
      Object.entries(changes).every(([field, entry]) => after[field] === entry)
    );
  if (action.action !== "person.update") return false;
  const keys = Object.keys(changes).sort();
  return (
    exactKeys(action, ["action", "actor_id", "expected_version", "changes"]) &&
    keys.length > 0 &&
    JSON.stringify(Object.keys(before).sort()) === JSON.stringify(keys) &&
    JSON.stringify(Object.keys(after).sort()) === JSON.stringify(keys) &&
    Object.entries(before).every(([field, entry]) =>
      validPersonField(field, entry),
    ) &&
    Object.entries(after).every(([field, entry]) =>
      validPersonField(field, entry),
    ) &&
    Object.entries(changes).every(([field, entry]) => after[field] === entry) &&
    typeof preview.label === "string" &&
    preview.label.length > 0
  );
}

export function validClientContactProposal(
  value: Record<string, unknown>,
): boolean {
  const action = value.action as Record<string, unknown>;
  const preview = value.preview as Record<string, unknown>;
  const changes = action.changes as Record<string, unknown>;
  const before = preview.before as Record<string, unknown>;
  const after = preview.after as Record<string, unknown>;
  const linking = action.action === "client_contact.link";
  const unlinking = action.action === "client_contact.unlink";
  const identityKeys = [
    "client_id",
    "client_name",
    "client_status",
    "actor_id",
    "actor_name",
    "actor_type",
    "actor_status",
    "actor_version",
    "role",
    "link_state",
  ];
  const validIdentity = (
    record: Record<string, unknown>,
    state: "active" | "unlinked",
  ) =>
    exactKeys(record, [
      ...identityKeys,
      ...(state === "unlinked" ? ["reason"] : []),
    ]) &&
    typeof record.client_id === "string" &&
    uuid.test(record.client_id) &&
    typeof record.client_name === "string" &&
    record.client_name.length > 0 &&
    ["active", "lead", "inactive"].includes(String(record.client_status)) &&
    typeof record.actor_id === "string" &&
    uuid.test(record.actor_id) &&
    typeof record.actor_name === "string" &&
    record.actor_name.length > 0 &&
    record.actor_type === "person" &&
    record.actor_status === "active" &&
    typeof record.actor_version === "number" &&
    Number.isSafeInteger(record.actor_version) &&
    record.actor_version > 0 &&
    record.role === "contact" &&
    record.link_state === state &&
    (state !== "unlinked" ||
      (typeof record.reason === "string" &&
        record.reason.trim() === record.reason &&
        record.reason.length > 0 &&
        Array.from(record.reason).length <= 1_000));
  if (
    !exactKeys(preview, ["label", "before", "after"]) ||
    (!linking && !unlinking) ||
    preview.label !== after.client_name ||
    action.client_id !== after.client_id ||
    !validIdentity(after, unlinking ? "unlinked" : "active")
  )
    return false;
  if (linking) {
    return (
      exactKeys(action, [
        "action",
        "client_id",
        "expected_version",
        "changes",
      ]) &&
      exactKeys(changes, ["actor_id"]) &&
      exactKeys(before, ["link_state"]) &&
      before.link_state === "none" &&
      changes.actor_id === after.actor_id
    );
  }
  return (
    exactKeys(action, [
      "action",
      "client_id",
      "client_actor_link_id",
      "expected_version",
      "changes",
    ]) &&
    typeof action.client_actor_link_id === "string" &&
    uuid.test(action.client_actor_link_id) &&
    exactKeys(changes, ["reason"]) &&
    validIdentity(before, "active") &&
    identityKeys
      .filter((key) => key !== "link_state")
      .every((key) => before[key] === after[key]) &&
    changes.reason === after.reason
  );
}
