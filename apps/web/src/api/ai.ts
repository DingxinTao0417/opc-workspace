import {
  ApiError,
  getRuntimeConnection,
  aiGenerationCitations,
} from "./client";
import type {
  AiBusinessContextSelection,
  AiChatStreamEvent,
  AiWorkspacePanel,
  AiWorkspaceGrant,
  AiWorkspaceScope,
} from "../types/models";
import {
  isAiWorkspaceRecordNavigationTarget,
  isAiWorkspaceRecordNavigationTaskId,
  isAiWorkspaceRecordNavigationParentId,
  isAiWorkspaceRecordNavigationSubmissionId,
} from "../lib/aiWorkspaceNavigation";
import { parseAiRunProgress } from "./aiProgress";

export interface StreamAiChatInput {
  providerId: string;
  sessionId?: string;
  message: string;
  context?: AiBusinessContextSelection;
  workspace?: AiWorkspaceGrant;
  projectFiles?: AiProjectFileContext;
  actionReceiptGenerationId?: string;
  actionRecheckProposalId?: string;
  requestId?: string;
  signal?: AbortSignal;
  onEvent: (event: AiChatStreamEvent) => void;
}

export interface AiProjectFileContext {
  provider_id: string;
  provider_version: number;
  session_id: string;
  confirmed: true;
  expires_at: string;
  files: Array<{ path: string; content: string; sha256: string }>;
}

export class AiChatAlreadyAccepted extends ApiError {
  constructor(
    readonly generationId: string,
    readonly sessionId: string,
  ) {
    super("消息已被接收，正在恢复原来的生成", {
      code: "AI_CHAT_ALREADY_ACCEPTED",
      status: 409,
    });
  }
}

const requestableWorkspaceScopes = new Set<AiWorkspaceScope>([
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

// streamAiChat consumes the Sidecar's opc-ai-sse-v1 stream and forwards
// parsed events. Aborting the signal disconnects the request, which the
// Sidecar treats as a cancel and keeps the generated partial content.
export async function streamAiChat(input: StreamAiChatInput): Promise<void> {
  const connection = await getRuntimeConnection();
  const controller = new AbortController();
  const upstreamSignal = input.signal;
  const abortFromUpstream = () => controller.abort();
  if (upstreamSignal?.aborted) controller.abort();
  else
    upstreamSignal?.addEventListener("abort", abortFromUpstream, {
      once: true,
    });

  let response: Response;
  try {
    response = await fetch(`${connection.baseUrl}/api/v1/ai/chat`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Accept: "text/event-stream",
        "X-Request-ID": crypto.randomUUID(),
        "Idempotency-Key": input.requestId ?? crypto.randomUUID(),
        ...(connection.token
          ? { Authorization: `Bearer ${connection.token}` }
          : {}),
      },
      body: JSON.stringify({
        provider_id: input.providerId,
        session_id: input.sessionId ?? "",
        message: input.message,
        ...(input.context ? { context: input.context } : {}),
        ...(input.workspace ? { workspace: input.workspace } : {}),
        ...(input.projectFiles ? { project_files: input.projectFiles } : {}),
        ...(input.actionReceiptGenerationId
          ? { action_receipt_generation_id: input.actionReceiptGenerationId }
          : {}),
        ...(input.actionRecheckProposalId
          ? { action_recheck_proposal_id: input.actionRecheckProposalId }
          : {}),
      }),
      signal: controller.signal,
    });
  } catch (error) {
    upstreamSignal?.removeEventListener("abort", abortFromUpstream);
    if (controller.signal.aborted) return;
    throw new ApiError("无法连接本地 Sidecar", { code: "NETWORK_ERROR" });
  }

  try {
    if (!response.ok || !response.body) {
      let code = "HTTP_ERROR";
      let message = `请求失败（${response.status}）`;
      try {
        const body = (await response.json()) as {
          code?: string;
          message?: string;
          error?: { code?: string; message?: string };
          generation_id?: string;
          session_id?: string;
        };
        code =
          body.error?.code ??
          (typeof body.code === "string" ? body.code : code);
        message =
          body.error?.message ??
          (typeof body.message === "string" ? body.message : message);
        if (
          code === "AI_CHAT_ALREADY_ACCEPTED" &&
          typeof body.generation_id === "string" &&
          typeof body.session_id === "string"
        ) {
          throw new AiChatAlreadyAccepted(body.generation_id, body.session_id);
        }
      } catch (error) {
        if (error instanceof AiChatAlreadyAccepted) throw error;
        // non-JSON error body keeps the generic message
      }
      throw new ApiError(message, { code, status: response.status });
    }

    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    let terminal = false;
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      let boundary = /\r?\n\r?\n/.exec(buffer);
      let separator = boundary?.index ?? -1;
      while (separator >= 0) {
        const block = buffer.slice(0, separator);
        buffer = buffer.slice(separator + (boundary?.[0].length ?? 2));
        const event = parseAiStreamBlock(block);
        if (event) {
          input.onEvent(event);
          terminal =
            event.type === "done" ||
            event.type === "cancelled" ||
            event.type === "error";
          if (terminal) {
            await reader.cancel().catch(() => {});
            return;
          }
        }
        boundary = /\r?\n\r?\n/.exec(buffer);
        separator = boundary?.index ?? -1;
      }
    }
    if (!terminal)
      throw new ApiError("AI 回答流提前中断，已收到的内容可能不完整", {
        code: "AI_STREAM_INCOMPLETE",
      });
  } catch (error) {
    if (controller.signal.aborted) return;
    if (error instanceof ApiError) throw error;
    throw new ApiError("AI 回答流中断", { code: "AI_STREAM_ERROR" });
  } finally {
    // Invalid terminal metadata or a mismatched stream identity must also
    // release the in-flight fetch; never leave an unconsumed model stream.
    controller.abort();
    upstreamSignal?.removeEventListener("abort", abortFromUpstream);
  }
}

function parseAiStreamBlock(block: string): AiChatStreamEvent | null {
  let event = "message";
  const dataLines: string[] = [];
  for (const line of block.split("\n")) {
    if (line.startsWith("event:")) {
      event = line.slice(6).trim();
    } else if (line.startsWith("data:")) {
      dataLines.push(line.slice(5).trim());
    }
  }
  if (dataLines.length === 0) return null;
  let payload: Record<string, unknown>;
  try {
    payload = JSON.parse(dataLines.join("\n")) as Record<string, unknown>;
  } catch {
    if (event === "progress")
      throw new ApiError("运行进度响应无效", { code: "INVALID_RESPONSE" });
    return null;
  }
  if (event === "progress") {
    if (
      !payload ||
      typeof payload !== "object" ||
      Array.isArray(payload) ||
      typeof payload.generation_id !== "string" ||
      !payload.generation_id ||
      Object.keys(payload).some(
        (key) => key !== "generation_id" && key !== "step",
      )
    )
      throw new ApiError("运行进度响应无效", { code: "INVALID_RESPONSE" });
    return {
      type: "progress",
      generationId: payload.generation_id,
      step: parseAiRunProgress(payload.step),
    };
  }
  if (!payload || typeof payload !== "object" || Array.isArray(payload))
    return null;
  const generationId =
    typeof payload.generation_id === "string" ? payload.generation_id : "";
  if (!generationId) return null;
  switch (event) {
    case "project_file_proposal": {
      const idPattern =
        /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
      const {
        proposal_id: id,
        path,
        base_sha256: baseSHA256,
        content,
      } = payload;
      if (
        Object.keys(payload).some(
          (key) =>
            ![
              "generation_id",
              "proposal_id",
              "path",
              "base_sha256",
              "content",
            ].includes(key),
        ) ||
        !idPattern.test(generationId) ||
        typeof id !== "string" ||
        !idPattern.test(id) ||
        typeof path !== "string" ||
        !path ||
        path.length > 4096 ||
        typeof baseSHA256 !== "string" ||
        !/^[0-9a-f]{64}$/.test(baseSHA256) ||
        typeof content !== "string" ||
        content.includes("\0") ||
        new TextEncoder().encode(content).length > 32 * 1024 ||
        new TextDecoder("utf-8", { ignoreBOM: true }).decode(
          new TextEncoder().encode(content),
        ) !== content
      )
        throw new ApiError("文件修改建议响应无效", {
          code: "INVALID_RESPONSE",
        });
      return {
        type: "project_file_proposal",
        generationId,
        proposal: { id, path, baseSHA256, content },
      };
    }
    case "meta":
      return {
        type: "meta",
        meta: {
          protocol: text(payload.protocol),
          generation_id: generationId,
          session_id: text(payload.session_id),
          model: text(payload.model),
          provider_id: text(payload.provider_id),
          sse_protocol: text(payload.sse_protocol),
        },
      };
    case "delta":
      return {
        type: "delta",
        generationId,
        text: typeof payload.text === "string" ? payload.text : "",
      };
    case "reasoning":
      return {
        type: "reasoning",
        generationId,
        text: typeof payload.text === "string" ? payload.text : "",
      };
    case "workspace_panel": {
      const panel = typeof payload.panel === "string" ? payload.panel : "";
      if (
        Object.keys(payload).some(
          (key) => key !== "generation_id" && key !== "panel",
        ) ||
        !isAiWorkspacePanel(panel)
      )
        throw new ApiError("工作区导航响应无效", {
          code: "INVALID_RESPONSE",
        });
      return {
        type: "workspace_panel",
        generationId,
        panel,
      };
    }
    case "workspace_panels": {
      const panels = payload.panels;
      const hasSplitRatio = Object.prototype.hasOwnProperty.call(
        payload,
        "split_ratio",
      );
      const splitRatio = payload.split_ratio;
      if (
        Object.keys(payload).some(
          (key) =>
            key !== "generation_id" &&
            key !== "panels" &&
            key !== "split_ratio",
        ) ||
        !Array.isArray(panels) ||
        panels.length !== 2 ||
        panels.some(
          (panel) => typeof panel !== "string" || !isAiWorkspacePanel(panel),
        ) ||
        panels[0] === panels[1] ||
        (panels[0] === "browser" && panels[1] === "browser") ||
        (hasSplitRatio &&
          (typeof splitRatio !== "number" ||
            !Number.isFinite(splitRatio) ||
            splitRatio < 0.25 ||
            splitRatio > 0.75))
      )
        throw new ApiError("工作区分栏响应无效", {
          code: "INVALID_RESPONSE",
        });
      return {
        type: "workspace_panels",
        generationId,
        panels: panels as [AiWorkspacePanel, AiWorkspacePanel],
        ...(hasSplitRatio ? { splitRatio: splitRatio as number } : {}),
      };
    }
    case "workspace_browser_navigation": {
      const url = typeof payload.url === "string" ? payload.url : "";
      if (
        Object.keys(payload).some(
          (key) => key !== "generation_id" && key !== "url",
        ) ||
        !isAiWorkspaceBrowserNavigationUrl(url)
      )
        throw new ApiError("浏览器导航响应无效", {
          code: "INVALID_RESPONSE",
        });
      return {
        type: "workspace_browser_navigation",
        generationId,
        url,
      };
    }
    case "workspace_browser_action": {
      const action = payload.action;
      if (
        Object.keys(payload).some(
          (key) => key !== "generation_id" && key !== "action",
        ) ||
        (action !== "back" &&
          action !== "forward" &&
          action !== "reload" &&
          action !== "stop")
      )
        throw new ApiError("浏览器操作响应无效", {
          code: "INVALID_RESPONSE",
        });
      return { type: "workspace_browser_action", generationId, action };
    }
    case "workspace_access_request": {
      const scopes = payload.scopes;
      if (
        Object.keys(payload).some(
          (key) => key !== "generation_id" && key !== "scopes",
        ) ||
        !Array.isArray(scopes) ||
        scopes.length < 1 ||
        scopes.length > 16 ||
        scopes.some(
          (scope) =>
            typeof scope !== "string" ||
            !requestableWorkspaceScopes.has(scope as AiWorkspaceScope),
        ) ||
        new Set(scopes).size !== scopes.length
      )
        throw new ApiError("工作台权限请求响应无效", {
          code: "INVALID_RESPONSE",
        });
      return {
        type: "workspace_access_request",
        generationId,
        scopes: scopes as AiWorkspaceScope[],
      };
    }
    case "workspace_plan_updated": {
      const version =
        typeof payload.version === "number" ? payload.version : Number.NaN;
      const stepCount =
        typeof payload.step_count === "number"
          ? payload.step_count
          : Number.NaN;
      if (
        Object.keys(payload).some(
          (key) =>
            key !== "generation_id" &&
            key !== "version" &&
            key !== "step_count",
        ) ||
        !Number.isInteger(version) ||
        version < 1 ||
        version > 128 ||
        !Number.isInteger(stepCount) ||
        stepCount < 1 ||
        stepCount > 12
      )
        throw new ApiError("工作计划更新响应无效", {
          code: "INVALID_RESPONSE",
        });
      return {
        type: "workspace_plan_updated",
        generationId,
        version,
        stepCount,
      };
    }
    case "workspace_record_navigation": {
      const recordType =
        typeof payload.record_type === "string" ? payload.record_type : "";
      const recordId =
        typeof payload.record_id === "string" ? payload.record_id : "";
      if (!isAiWorkspaceRecordNavigationTarget(recordType, recordId))
        throw new ApiError("工作台记录导航响应无效", {
          code: "INVALID_RESPONSE",
        });
      if (recordType === "task_artifact") {
        const taskId =
          typeof payload.task_id === "string" ? payload.task_id : "";
        const submissionId =
          typeof payload.submission_id === "string"
            ? payload.submission_id
            : "";
        if (
          Object.keys(payload).some(
            (key) =>
              key !== "generation_id" &&
              key !== "record_type" &&
              key !== "record_id" &&
              key !== "task_id" &&
              key !== "submission_id",
          ) ||
          !isAiWorkspaceRecordNavigationTaskId(recordType, taskId) ||
          !isAiWorkspaceRecordNavigationSubmissionId(recordType, submissionId)
        )
          throw new ApiError("工作台记录导航响应无效", {
            code: "INVALID_RESPONSE",
          });
        return {
          type: "workspace_record_navigation",
          generationId,
          recordType,
          recordId,
          taskId,
          submissionId,
        };
      }
      if (recordType === "agent_run" || recordType === "task_submission") {
        const taskId =
          typeof payload.task_id === "string" ? payload.task_id : "";
        if (
          Object.keys(payload).some(
            (key) =>
              key !== "generation_id" &&
              key !== "record_type" &&
              key !== "record_id" &&
              key !== "task_id",
          ) ||
          !isAiWorkspaceRecordNavigationTaskId(recordType, taskId)
        )
          throw new ApiError("工作台记录导航响应无效", {
            code: "INVALID_RESPONSE",
          });
        return {
          type: "workspace_record_navigation",
          generationId,
          recordType,
          recordId,
          taskId,
        };
      }
      if (
        recordType === "project_note" ||
        recordType === "client_activity" ||
        recordType === "client_followup"
      ) {
        const parentId =
          typeof payload.parent_id === "string" ? payload.parent_id : "";
        if (
          Object.keys(payload).some(
            (key) =>
              key !== "generation_id" &&
              key !== "record_type" &&
              key !== "record_id" &&
              key !== "parent_id",
          ) ||
          !isAiWorkspaceRecordNavigationParentId(recordType, parentId)
        )
          throw new ApiError("工作台记录导航响应无效", {
            code: "INVALID_RESPONSE",
          });
        return {
          type: "workspace_record_navigation",
          generationId,
          recordType,
          recordId,
          parentId,
        };
      }
      if (
        Object.keys(payload).some(
          (key) =>
            key !== "generation_id" &&
            key !== "record_type" &&
            key !== "record_id",
        )
      )
        throw new ApiError("工作台记录导航响应无效", {
          code: "INVALID_RESPONSE",
        });
      return {
        type: "workspace_record_navigation",
        generationId,
        recordType,
        recordId,
      };
    }
    case "replace":
      return {
        type: "replace",
        generationId,
        text: typeof payload.text === "string" ? payload.text : "",
        reasoning:
          typeof payload.reasoning === "string" ? payload.reasoning : "",
      };
    case "done":
      return {
        type: "done",
        generationId,
        citationEvidence: aiGenerationCitations(payload),
      };
    case "cancelled":
      return {
        type: "cancelled",
        generationId,
        partialText:
          typeof payload.partial_text === "string" ? payload.partial_text : "",
      };
    case "error":
      return {
        type: "error",
        generationId,
        error:
          typeof payload.error === "string" ? payload.error : "AI_STREAM_ERROR",
        detail: typeof payload.detail === "string" ? payload.detail : undefined,
        ...(typeof payload.partial_text === "string"
          ? { partialText: payload.partial_text }
          : {}),
      };
    default:
      return null;
  }
}

function isAiWorkspaceBrowserNavigationUrl(value: string): boolean {
  if (!value || value.length > 4096 || /[\u0000-\u001f\u007f]/.test(value))
    return false;
  try {
    const url = new URL(value);
    return (
      (url.protocol === "http:" || url.protocol === "https:") &&
      !!url.hostname &&
      !url.username &&
      !url.password
    );
  } catch {
    return false;
  }
}

function isAiWorkspacePanel(value: string): value is AiWorkspacePanel {
  return (
    value === "overview" ||
    value === "agents" ||
    value === "files" ||
    value === "review" ||
    value === "terminal" ||
    value === "browser" ||
    value === "managed"
  );
}

function text(value: unknown): string {
  return typeof value === "string" ? value : "";
}
