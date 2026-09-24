import {
  isWorkspaceIdentity,
  parseFocusReportLocation,
} from "./focusReportLocation";
import { isClientRecordRoute } from "./clientRecordLocation";
import { isAutomationLocationRoute } from "./automationLocation";
import { isFinanceLocationRoute } from "./financeLocation";
import { isTaskSubmissionRoute } from "./taskSubmissionLocation";
import { isProjectNoteRoute } from "./projectNoteLocation";

export type AiWorkspaceTool =
  | "overview"
  | "agents"
  | "files"
  | "review"
  | "terminal"
  | "browser"
  | "managed";

const aiWorkspaceTools = new Set<AiWorkspaceTool>([
  "overview",
  "agents",
  "files",
  "review",
  "terminal",
  "browser",
  "managed",
]);

export function parseAiWorkspaceToolRoute(
  value: string,
): AiWorkspaceTool | null {
  if (!value.startsWith("/ai?") || value.includes("#")) return null;
  const params = new URLSearchParams(value.slice(4));
  if (
    [...params.keys()].some((key) => key !== "workspace") ||
    params.getAll("workspace").length !== 1
  )
    return null;
  const tool = params.get("workspace");
  return tool && aiWorkspaceTools.has(tool as AiWorkspaceTool)
    ? (tool as AiWorkspaceTool)
    : null;
}

export function isAiAgentSessionRoute(value: string): boolean {
  if (!value.startsWith("/ai?") || value.includes("#")) return false;
  const params = new URLSearchParams(value.slice(4));
  return (
    [...params.keys()].every((key) => key === "session") &&
    params.getAll("session").length === 1 &&
    isWorkspaceIdentity(params.get("session"))
  );
}

export interface AgentRunLocation {
  taskId: string;
  runId: string;
  returnSession: string | null;
}

export interface TaskWorkspaceLocation {
  taskId: string;
  returnSession: string | null;
}

export interface TaskSavedViewLocation {
  viewId: string;
  returnSession: string | null;
}

export function parseTaskSavedViewLocation(
  pathname: string,
  search: string,
): TaskSavedViewLocation | null {
  if (pathname !== "/tasks" || search.includes("#")) return null;
  const params = new URLSearchParams(
    search.startsWith("?") ? search.slice(1) : search,
  );
  if (
    [...params.keys()].some(
      (key) => key !== "task_view" && key !== "return_session",
    ) ||
    params.getAll("task_view").length !== 1 ||
    !isWorkspaceIdentity(params.get("task_view")) ||
    params.getAll("return_session").length > 1 ||
    (params.has("return_session") &&
      !isWorkspaceIdentity(params.get("return_session")))
  )
    return null;
  return {
    viewId: params.get("task_view")!,
    returnSession: params.get("return_session"),
  };
}

export function parseTaskWorkspaceLocation(
  pathname: string,
  search: string,
): TaskWorkspaceLocation | null {
  const match = /^\/tasks\/([^/?#]+)$/.exec(pathname);
  if (!match || !isWorkspaceIdentity(match[1]) || search.includes("#"))
    return null;
  const params = new URLSearchParams(
    search.startsWith("?") ? search.slice(1) : search,
  );
  if (
    [...params.keys()].some((key) => key !== "return_session") ||
    params.getAll("return_session").length > 1 ||
    (params.has("return_session") &&
      !isWorkspaceIdentity(params.get("return_session")))
  )
    return null;
  return { taskId: match[1], returnSession: params.get("return_session") };
}

function isWorkspaceRecordRoute(value: string): boolean {
  const id = "[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}";
  return (
    new RegExp(`^/(?:tasks|projects|clients|inbox)/${id}$`, "i").test(value) ||
    (value.startsWith("/tasks?") &&
      parseTaskSavedViewLocation("/tasks", value.slice(6))?.returnSession ===
        null) ||
    new RegExp(
      `^/(?:roadmap\\?milestone|content-calendar\\?item|inbox\\?reminder)=${id}$`,
      "i",
    ).test(value) ||
    value === "/knowledge" ||
    new RegExp(`^/knowledge\\?source=${id}&job=${id}$`, "i").test(value)
  );
}

export function parseAgentRunLocation(
  pathname: string,
  search: string,
): AgentRunLocation | null {
  const match = /^\/tasks\/([^/?#]+)$/.exec(pathname);
  if (!match || !isWorkspaceIdentity(match[1]) || search.includes("#"))
    return null;
  const params = new URLSearchParams(
    search.startsWith("?") ? search.slice(1) : search,
  );
  if (
    [...params.keys()].some(
      (key) => key !== "agent_run" && key !== "return_session",
    ) ||
    params.getAll("agent_run").length !== 1 ||
    !isWorkspaceIdentity(params.get("agent_run")) ||
    params.getAll("return_session").length > 1 ||
    (params.has("return_session") &&
      !isWorkspaceIdentity(params.get("return_session")))
  )
    return null;
  return {
    taskId: match[1],
    runId: params.get("agent_run")!,
    returnSession: params.get("return_session"),
  };
}

export function agentRunHref(taskId: string, runId: string): string {
  return isWorkspaceIdentity(taskId) && isWorkspaceIdentity(runId)
    ? `/tasks/${taskId}?agent_run=${runId}`
    : "";
}

export function isAgentRunRoute(value: string): boolean {
  const question = value.indexOf("?");
  if (question <= 0) return false;
  const location = parseAgentRunLocation(
    value.slice(0, question),
    value.slice(question),
  );
  return location !== null && location.returnSession === null;
}

// Only controlled application detail routes and the explicit Inbox root, never
// javascript:, external URLs or arbitrary relative paths. A model-produced link
// is a navigation hint, not a verified citation.
export function isAiWorkspaceRoute(value: string): boolean {
  if (parseAiWorkspaceToolRoute(value) || isAiAgentSessionRoute(value))
    return true;
  if (
    isClientRecordRoute(value) ||
    isProjectNoteRoute(value) ||
    isAutomationLocationRoute(value) ||
    isFinanceLocationRoute(value) ||
    isTaskSubmissionRoute(value) ||
    isAgentRunRoute(value)
  )
    return true;
  if (value.startsWith("/focus?") && !value.includes("#")) {
    const params = new URLSearchParams(value.slice(7));
    return (
      !params.has("return_session") && parseFocusReportLocation(params) !== null
    );
  }
  return (
    value === "/invoices" ||
    value === "/inbox" ||
    value === "/focus" ||
    isWorkspaceRecordRoute(value)
  );
}

// Return context belongs to the displayed message, never to the model's URL.
export function aiWorkspaceHref(value: string, sessionId?: string): string {
  if (
    (value === "/inbox" ||
      value.startsWith("/focus?") ||
      isClientRecordRoute(value) ||
      isProjectNoteRoute(value) ||
      isAutomationLocationRoute(value) ||
      isFinanceLocationRoute(value) ||
      isTaskSubmissionRoute(value) ||
      isAgentRunRoute(value) ||
      isWorkspaceRecordRoute(value)) &&
    isAiWorkspaceRoute(value) &&
    isWorkspaceIdentity(sessionId)
  ) {
    return `${value}${value.includes("?") ? "&" : "?"}return_session=${sessionId}`;
  }
  return value;
}
