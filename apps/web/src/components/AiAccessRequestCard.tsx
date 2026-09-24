import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { dismissAiAccessRequest } from "../api/aiActions";
import {
  activeContinuationsKey,
  continuationKey,
  isContinuationActive,
  recentContinuationsKey,
  stopAiPlanContinuation,
  type AiPlanContinuation,
} from "../api/aiPlanContinuation";
import { ApiError } from "../api/client";
import { aiAgentInboxQueryKey, aiMessagesQueryKey } from "../api/hooks";
import type { AiMessage, AiWorkspaceScope } from "../types/models";
import type { AiAccessRequest } from "../store/aiChat";

const labels: Record<AiWorkspaceScope, string> = {
  work: "工作事项",
  clients: "客户资料",
  outputs: "执行产出",
  output_files: "产出文件正文",
  actions: "操作建议",
  agent_execution: "Agent 执行",
  agent_files: "Agent 受控文件",
  agent_project_files: "跨任务已验收文件",
  workspace_ui: "工作区导航",
  workspace_browser: "浏览器标签操作",
  knowledge: "知识检索与引用",
  knowledge_actions: "知识库管理",
  finance: "财务查询",
  finance_actions: "财务操作",
  invoice_actions: "发票操作",
  finance_exports: "财务导出",
};

export const accessRequestContinuationPrompt =
  "请继续处理我刚才的请求。先按本条消息实际授予的范围重新读取最新工作台事实；若仍缺少权限就说明缺口，不要把上一轮的权限请求当成授权或已执行操作。";

export function AiAccessRequestCard({
  request,
  disabled,
  stopBeforePrepare = false,
  stopping = false,
  onPrepare,
  onDismiss,
}: {
  request: AiAccessRequest;
  disabled: boolean;
  stopBeforePrepare?: boolean;
  stopping?: boolean;
  onPrepare: (scopes: AiWorkspaceScope[]) => void;
  onDismiss: () => void;
}) {
  return (
    <section className="ai-access-request-card" aria-label="工作台权限请求">
      <p>
        智能体建议下条消息选择：
        {request.scopes.map((scope) => labels[scope]).join("、")}
      </p>
      <p>
        这不是授权；当前回复没有获得这些权限，也不会自动重发。请核对权限详情后自行决定。
      </p>
      {stopBeforePrepare ? (
        <p>当前自动续办会先停止；之后仍须单独核对权限并手动发送。</p>
      ) : null}
      <div className="ai-access-request-actions">
        <button
          className="button button-primary"
          disabled={disabled || stopping}
          onClick={() => onPrepare(request.scopes)}
          type="button"
        >
          {stopping
            ? "正在停止自动续办…"
            : stopBeforePrepare
              ? "停止自动续办并核对权限"
              : "核对权限并准备继续"}
        </button>
        <button
          className="button button-ghost"
          disabled={stopping}
          onClick={onDismiss}
          type="button"
        >
          忽略
        </button>
      </div>
    </section>
  );
}

// Saved messages are the recovery source after refresh. The SSE store remains
// a fast, disposable display channel; neither path represents authorization.
export function AiAccessRequestPrompt({
  sessionId,
  messages,
  liveRequest,
  streamingGenerationId,
  continuation,
  disabled,
  onPrepare,
  onDismissLive,
}: {
  sessionId: string;
  messages: AiMessage[];
  liveRequest?: AiAccessRequest;
  streamingGenerationId?: string;
  continuation?: AiPlanContinuation | null;
  disabled: boolean;
  onPrepare: (scopes: AiWorkspaceScope[]) => void;
  onDismissLive: (generationId: string) => void;
}) {
  const queryClient = useQueryClient();
  const [dismissedId, setDismissedId] = useState<string>();
  const [error, setError] = useState<{ id: string; message: string }>();
  const [stopping, setStopping] = useState(false);
  const [stopError, setStopError] = useState("");
  const attemptedId = useRef<string | undefined>(undefined);
  // Only an open request is attached to a saved message. Equal-timestamp
  // user/assistant rows can sort by random UUID, so the last array item is
  // not necessarily the reply that owns the current request.
  const latest = messages
    .slice()
    .reverse()
    .find(
      (message) =>
        message.session_id === sessionId &&
        message.role === "assistant" &&
        message.access_request,
    );
  const stored =
    latest?.role === "assistant" &&
    latest.status === "completed" &&
    latest.generation_id &&
    latest.access_request
      ? {
          generationId: latest.generation_id,
          scopes: latest.access_request.scopes,
        }
      : undefined;
  const request = liveRequest ?? (streamingGenerationId ? undefined : stored);
  const currentSession = useRef(sessionId);
  const currentRequest = useRef(request?.generationId);
  const currentContinuation = useRef(continuation);
  const prepareContext = useRef({ disabled, onPrepare });
  const mounted = useRef(false);
  prepareContext.current = { disabled, onPrepare };
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  currentSession.current = sessionId;
  currentRequest.current = request?.generationId;
  currentContinuation.current = continuation;

  useEffect(() => {
    setStopping(false);
    setStopError("");
  }, [sessionId, request?.generationId]);

  useEffect(() => {
    if (
      !dismissedId ||
      stored?.generationId !== dismissedId ||
      attemptedId.current === dismissedId
    )
      return;
    attemptedId.current = dismissedId;
    void dismissAiAccessRequest(dismissedId)
      .then(() => {
        onDismissLive(dismissedId);
        void Promise.all([
          queryClient.invalidateQueries({
            queryKey: aiMessagesQueryKey(sessionId),
          }),
          queryClient.invalidateQueries({ queryKey: aiAgentInboxQueryKey }),
        ]).catch(() => undefined);
      })
      .catch(() => {
        setError({ id: dismissedId, message: "忽略权限建议失败，请重试" });
        setDismissedId(undefined);
        attemptedId.current = undefined;
      });
  }, [
    dismissedId,
    stored?.generationId,
    onDismissLive,
    queryClient,
    sessionId,
  ]);

  if (!request || request.generationId === dismissedId)
    return error && request?.generationId === error.id ? (
      <p role="alert">{error.message}</p>
    ) : null;
  const prepare = (scopes: AiWorkspaceScope[]) => {
    if (!mounted.current || prepareContext.current.disabled) return;
    setStopError("");
    if (!continuation || !isContinuationActive(continuation)) {
      onPrepare(scopes);
      return;
    }
    if (continuation.session_id !== sessionId) {
      setStopError("自动续办与当前会话不一致，请刷新后重试；未准备新消息。");
      return;
    }
    const leaseId = continuation.id;
    const generationId = request.generationId;
    setStopping(true);
    void stopAiPlanContinuation(leaseId)
      .then(async (result) => {
        if (result.session_id !== sessionId || isContinuationActive(result)) {
          throw new Error("Stopped continuation belongs to another session");
        }
        await queryClient.cancelQueries({
          queryKey: continuationKey(sessionId),
        });
        await queryClient.cancelQueries({ queryKey: activeContinuationsKey });
        const latestLease = queryClient.getQueryData<AiPlanContinuation | null>(
          continuationKey(sessionId),
        );
        if (
          latestLease &&
          ((latestLease.id !== leaseId && isContinuationActive(latestLease)) ||
            (latestLease.id === leaseId &&
              latestLease.version > result.version))
        ) {
          if (
            currentSession.current === sessionId &&
            currentRequest.current === generationId
          ) {
            setStopError(
              "自动续办状态已变化，请重新核对；没有准备或发送新消息。",
            );
          }
          return;
        }
        queryClient.setQueryData(continuationKey(sessionId), result);
        void queryClient.invalidateQueries({
          queryKey: activeContinuationsKey,
        });
        void queryClient.invalidateQueries({
          queryKey: recentContinuationsKey,
        });
        if (
          currentSession.current === sessionId &&
          currentRequest.current === generationId &&
          currentContinuation.current?.id !== leaseId &&
          isContinuationActive(currentContinuation.current)
        ) {
          setStopError(
            "另一个自动续办已启动，请重新核对；没有准备或发送新消息。",
          );
          return;
        }
        if (
          mounted.current &&
          !prepareContext.current.disabled &&
          currentSession.current === sessionId &&
          currentRequest.current === generationId
        ) {
          prepareContext.current.onPrepare(scopes);
        }
      })
      .catch(() => {
        if (
          currentSession.current === sessionId &&
          currentRequest.current === generationId
        ) {
          setStopError(
            "未能确认自动续办已停止。权限建议仍保留，请重试；没有授权或发送消息。",
          );
          void queryClient.invalidateQueries({
            queryKey: continuationKey(sessionId),
          });
        }
      })
      .finally(() => {
        if (
          currentSession.current === sessionId &&
          currentRequest.current === generationId
        ) {
          setStopping(false);
        }
      });
  };
  const dismiss = () => {
    const id = request.generationId;
    setError(undefined);
    setDismissedId(id);
    attemptedId.current = id;
    void dismissAiAccessRequest(id)
      .then(() => {
        onDismissLive(id);
        void Promise.all([
          queryClient.invalidateQueries({
            queryKey: aiMessagesQueryKey(sessionId),
          }),
          queryClient.invalidateQueries({ queryKey: aiAgentInboxQueryKey }),
        ]).catch(() => undefined);
      })
      .catch((reason) => {
        // A still-running old Sidecar can only dismiss after the successful
        // assistant message is saved. Keep its previous retry behavior during
        // a rolling local restart; the new Sidecar accepts this immediately.
        if (
          reason instanceof ApiError &&
          reason.status === 404 &&
          liveRequest?.generationId === id &&
          streamingGenerationId === id
        ) {
          attemptedId.current = undefined;
          onDismissLive(id);
          return;
        }
        setError({ id, message: "忽略权限建议失败，请重试" });
        setDismissedId((current) => (current === id ? undefined : current));
        attemptedId.current = undefined;
      });
  };
  return (
    <>
      <AiAccessRequestCard
        request={request}
        disabled={disabled}
        stopBeforePrepare={isContinuationActive(continuation)}
        stopping={stopping}
        onPrepare={prepare}
        onDismiss={dismiss}
      />
      {error && error.id === request.generationId ? (
        <p role="alert">{error.message}</p>
      ) : null}
      {stopError ? <p role="alert">{stopError}</p> : null}
    </>
  );
}
