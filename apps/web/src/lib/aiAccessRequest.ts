import type { AiWorkspaceScope } from "../types/models";

const requestableScopes = new Set<AiWorkspaceScope>([
  "work",
  "clients",
  "outputs",
  "output_files",
  "actions",
  "agent_execution",
  "agent_files",
  "agent_project_files",
  "workspace_ui",
  "workspace_browser",
  "knowledge",
  "knowledge_actions",
  "finance",
  "finance_actions",
  "invoice_actions",
  "finance_exports",
]);

export interface AiAccessRequestData {
  scopes: AiWorkspaceScope[];
}

export function parseAiAccessRequest(
  value: unknown,
): AiAccessRequestData | undefined {
  if (value == null) return undefined;
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new TypeError("Invalid AI access request");
  const row = value as Record<string, unknown>;
  if (Object.keys(row).length !== 1 || !Array.isArray(row.scopes))
    throw new TypeError("Invalid AI access request");
  const scopes = row.scopes;
  if (
    scopes.length < 1 ||
    scopes.length > requestableScopes.size ||
    scopes.some(
      (scope) =>
        typeof scope !== "string" ||
        !requestableScopes.has(scope as AiWorkspaceScope),
    ) ||
    new Set(scopes).size !== scopes.length
  )
    throw new TypeError("Invalid AI access request");
  return { scopes: scopes as AiWorkspaceScope[] };
}
