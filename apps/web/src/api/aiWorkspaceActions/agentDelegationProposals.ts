import {
  canonicalUuid,
  exactKeys,
  nonemptyString,
  object,
  positiveIntegerValue,
} from "./primitives";

const delegatedAgentScopes = [
  "work",
  "clients",
  "outputs",
  "output_files",
  "actions",
  "agent_execution",
  "agent_files",
  "agent_project_files",
  "knowledge",
  "knowledge_actions",
  "finance",
  "finance_actions",
  "invoice_actions",
  "finance_exports",
] as const;

export function validAgentDelegationProposal(
  value: Record<string, unknown>,
): boolean {
  const action = value.action;
  const preview = value.preview;
  if (!object(action) || !object(preview) || !object(action.changes))
    return false;
  const changes = action.changes;
  const delegation = preview.agent_delegation;
  if (
    !exactKeys(action, ["action", "changes"]) ||
    !exactKeys(changes, ["task_name", "message", "scopes"]) ||
    !object(delegation) ||
    !exactKeys(preview, ["label", "before", "after", "agent_delegation"])
  )
    return false;
  const knowledge = delegation.knowledge_sources;
  const delegationKeys = [
    "task_name",
    "message",
    "scopes",
    ...(knowledge === undefined ? [] : ["knowledge_sources"]),
    "parent_session_id",
    "parent_generation_id",
    "child_session_id",
    "provider_id",
    "provider_version",
    "provider_config_version",
    "provider_name",
    "model",
    "leaves_device",
    "depth",
    "max_depth",
    "sibling_index",
    "max_children",
  ];
  if (!exactKeys(delegation, delegationKeys)) return false;
  const taskName = changes.task_name;
  const message = changes.message;
  const scopes = changes.scopes;
  if (
    typeof taskName !== "string" ||
    !/^[a-z][a-z0-9_]{0,39}$/.test(taskName) ||
    typeof message !== "string" ||
    message !== message.trim() ||
    message.length === 0 ||
    Array.from(message).length > 4000 ||
    new TextEncoder().encode(message).byteLength > 16_384 ||
    !Array.isArray(scopes) ||
    scopes.length < 1 ||
    scopes.length > delegatedAgentScopes.length ||
    scopes.some(
      (scope) =>
        typeof scope !== "string" ||
        !delegatedAgentScopes.includes(
          scope as (typeof delegatedAgentScopes)[number],
        ),
    ) ||
    new Set(scopes).size !== scopes.length ||
    scopes.some(
      (scope, index) =>
        index > 0 &&
        delegatedAgentScopes.indexOf(scope as never) <=
          delegatedAgentScopes.indexOf(scopes[index - 1] as never),
    )
  )
    return false;
  if (
    delegation.task_name !== taskName ||
    delegation.message !== message ||
    JSON.stringify(delegation.scopes) !== JSON.stringify(scopes) ||
    preview.label !== taskName ||
    Object.keys(preview.before as Record<string, unknown>).length !== 0 ||
    Object.keys(preview.after as Record<string, unknown>).length !== 0 ||
    typeof value.generation_id !== "string" ||
    delegation.parent_generation_id !== value.generation_id
  )
    return false;
  for (const id of [
    delegation.parent_session_id,
    delegation.parent_generation_id,
    delegation.child_session_id,
    delegation.provider_id,
  ]) {
    if (typeof id !== "string" || !canonicalUuid.test(id)) return false;
  }
  if (
    !positiveIntegerValue(delegation.provider_version) ||
    !positiveIntegerValue(delegation.provider_config_version) ||
    !nonemptyString(delegation.provider_name) ||
    !nonemptyString(delegation.model) ||
    typeof delegation.leaves_device !== "boolean" ||
    delegation.depth !== 1 ||
    delegation.max_depth !== 1 ||
    !positiveIntegerValue(delegation.sibling_index) ||
    Number(delegation.sibling_index) > 4 ||
    delegation.max_children !== 4
  )
    return false;
  const hasKnowledge = scopes.includes("knowledge");
  if (hasKnowledge) {
    if (
      !Array.isArray(knowledge) ||
      knowledge.length < 1 ||
      knowledge.length > 20 ||
      knowledge.some(
        (source) =>
          !object(source) ||
          !exactKeys(source, ["source_id", "expected_source_version"]) ||
          typeof source.source_id !== "string" ||
          !canonicalUuid.test(source.source_id) ||
          !positiveIntegerValue(source.expected_source_version),
      ) ||
      new Set(knowledge.map((source) => String(source.source_id))).size !==
        knowledge.length
    )
      return false;
  } else if (knowledge !== undefined) return false;
  const confirmed = value.status === "confirmed";
  if (
    confirmed !== (value.result_id !== null) ||
    confirmed !== (value.result_version !== null) ||
    (confirmed &&
      (value.result_id !== delegation.child_session_id ||
        value.result_version !== 1 ||
        value.route !== `/ai?session=${delegation.child_session_id}`)) ||
    (!confirmed && value.route !== "")
  )
    return false;
  const result = value.agent_delegation_result;
  if (!confirmed) return result === undefined;
  return (
    object(result) &&
    exactKeys(result, [
      "session_id",
      "generation_id",
      "status",
      "error_code",
      "pending_approvals",
    ]) &&
    result.session_id === delegation.child_session_id &&
    result.generation_id === value.id &&
    ["queued", "streaming", "completed", "failed", "cancelled"].includes(
      String(result.status),
    ) &&
    Number.isSafeInteger(result.pending_approvals) &&
    Number(result.pending_approvals) >= 0 &&
    (result.error_code === null ||
      (typeof result.error_code === "string" &&
        result.error_code.length >= 1 &&
        result.error_code.length <= 100))
  );
}

export function validAgentFollowupProposal(
  value: Record<string, unknown>,
): boolean {
  const action = value.action;
  const preview = value.preview;
  if (!object(action) || !object(preview) || !object(action.changes))
    return false;
  const changes = action.changes;
  const followup = preview.agent_followup;
  if (
    !exactKeys(action, ["action", "changes"]) ||
    !exactKeys(changes, ["child_session_id", "message", "scopes"]) ||
    !object(followup) ||
    !exactKeys(preview, ["label", "before", "after", "agent_followup"])
  )
    return false;
  const knowledge = followup.knowledge_sources;
  if (
    !exactKeys(followup, [
      "target",
      "task_name",
      "message",
      "scopes",
      ...(knowledge === undefined ? [] : ["knowledge_sources"]),
      "provider_name",
      "model",
      "leaves_device",
    ]) ||
    !object(followup.target) ||
    !exactKeys(followup.target, [
      "parent_session_id",
      "child_session_id",
      "spawn_proposal_id",
      "previous_generation_id",
      "child_version",
      "provider_id",
      "provider_version",
      "provider_config_version",
    ])
  )
    return false;
  const target = followup.target;
  for (const id of [
    target.parent_session_id,
    target.child_session_id,
    target.spawn_proposal_id,
    target.previous_generation_id,
    target.provider_id,
  ]) {
    if (typeof id !== "string" || !canonicalUuid.test(id)) return false;
  }
  const message = changes.message;
  const scopes = changes.scopes;
  if (
    changes.child_session_id !== target.child_session_id ||
    typeof followup.task_name !== "string" ||
    !/^[a-z][a-z0-9_]{0,39}$/.test(followup.task_name) ||
    typeof message !== "string" ||
    message !== message.trim() ||
    message.length === 0 ||
    Array.from(message).length > 4000 ||
    new TextEncoder().encode(message).byteLength > 16_384 ||
    followup.message !== message ||
    !Array.isArray(scopes) ||
    scopes.length < 1 ||
    scopes.length > delegatedAgentScopes.length ||
    scopes.some(
      (scope) =>
        typeof scope !== "string" ||
        !delegatedAgentScopes.includes(
          scope as (typeof delegatedAgentScopes)[number],
        ),
    ) ||
    new Set(scopes).size !== scopes.length ||
    scopes.some(
      (scope, index) =>
        index > 0 &&
        delegatedAgentScopes.indexOf(scope as never) <=
          delegatedAgentScopes.indexOf(scopes[index - 1] as never),
    ) ||
    JSON.stringify(followup.scopes) !== JSON.stringify(scopes) ||
    preview.label !== followup.task_name ||
    Object.keys(preview.before as Record<string, unknown>).length !== 0 ||
    Object.keys(preview.after as Record<string, unknown>).length !== 0 ||
    !positiveIntegerValue(target.child_version) ||
    !positiveIntegerValue(target.provider_version) ||
    !positiveIntegerValue(target.provider_config_version) ||
    !nonemptyString(followup.provider_name) ||
    !nonemptyString(followup.model) ||
    typeof followup.leaves_device !== "boolean"
  )
    return false;
  if (scopes.includes("knowledge")) {
    if (
      !Array.isArray(knowledge) ||
      knowledge.length < 1 ||
      knowledge.length > 20 ||
      knowledge.some(
        (source) =>
          !object(source) ||
          !exactKeys(source, ["source_id", "expected_source_version"]) ||
          typeof source.source_id !== "string" ||
          !canonicalUuid.test(source.source_id) ||
          !positiveIntegerValue(source.expected_source_version),
      ) ||
      new Set(knowledge.map((source) => String(source.source_id))).size !==
        knowledge.length
    )
      return false;
  } else if (knowledge !== undefined) return false;
  const confirmed = value.status === "confirmed";
  if (
    confirmed !== (value.result_id !== null) ||
    confirmed !== (value.result_version !== null) ||
    (confirmed &&
      (value.result_id !== target.child_session_id ||
        value.result_version !== 1)) ||
    (!confirmed && value.route !== "")
  )
    return false;
  const result = value.agent_followup_result;
  if (!confirmed) return result === undefined;
  return (
    object(result) &&
    exactKeys(result, [
      "session_id",
      "generation_id",
      "status",
      "error_code",
      "pending_approvals",
    ]) &&
    result.session_id === target.child_session_id &&
    result.generation_id === value.id &&
    [
      "queued",
      "streaming",
      "completed",
      "failed",
      "cancelled",
      "unavailable",
    ].includes(String(result.status)) &&
    value.route ===
      (result.status === "unavailable"
        ? ""
        : `/ai?session=${target.child_session_id}`) &&
    Number.isSafeInteger(result.pending_approvals) &&
    Number(result.pending_approvals) >= 0 &&
    (result.error_code === null ||
      (typeof result.error_code === "string" &&
        result.error_code.length >= 1 &&
        result.error_code.length <= 100))
  );
}
