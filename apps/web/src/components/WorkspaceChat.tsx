import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ArrowUp, MessageCircle, Square } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { createAiSession } from "../api/client";
import { useAiMessagesInfiniteQuery, useAiProvidersQuery } from "../api/hooks";
import { workspaceError } from "../api/workspace";
import { stripAiTaskBlock } from "../lib/aiTaskCard";
import { useAiChatStore } from "../store/aiChat";
import type { AiWorkspaceGrant, AiWorkspaceScope } from "../types/models";
import { workbenchHandoffDetails } from "../store/aiWorkbenchHandoff";
import {
  useWorkspacePanels,
  type WorkspacePanel,
} from "../store/workspacePanels";
import { AiCopyButton } from "./AiCopyButton";
import { AiCitationEvidence } from "./AiCitationEvidence";
import { AiRunProgress } from "./AiRunProgress";
import { AiWorkPlan } from "./AiWorkPlan";
import {
  AiAutomaticOrigin,
  AiAutomaticReply,
  AiContinuationSendNotice,
  usePlanContinuation,
} from "./AiPlanContinuation";
import { isContinuationActive } from "../api/aiPlanContinuation";
import { AiActionReceiptAttachment } from "./AiActionReceiptAttachment";
import type { AiActionContinuation } from "../lib/aiActionContinuation";
import {
  AiIssueHandoffCard,
  AiWorkbenchHandoffCard,
} from "./AiWorkbenchHandoff";
import { renderAiRichText } from "./AiRichText";
import {
  AiWorkspaceAccess,
  AiWorkspaceGrantSummary,
} from "./AiWorkspaceAccess";
import {
  AiAccessRequestPrompt,
  accessRequestContinuationPrompt,
} from "./AiAccessRequestCard";
import { AiWorkspaceActions } from "./AiWorkspaceActions";

export function WorkspaceChat({ panel }: { panel: WorkspacePanel }) {
  const providers = useAiProvidersQuery();
  const chat = useAiChatStore();
  const update = useWorkspacePanels((s) => s.update);
  const messages = useAiMessagesInfiniteQuery(panel.chatSessionId ?? "");
  const automaticContinuation = usePlanContinuation(panel.chatSessionId ?? "");
  const client = useQueryClient();
  const [error, setError] = useState("");
  const [creating, setCreating] = useState(false);
  const [workspaceApproval, setWorkspaceApproval] = useState<{
    providerId: string;
    providerVersion: number;
    sessionId: string;
    grant: AiWorkspaceGrant;
  } | null>(null);
  const [workspaceAccessRequest, setWorkspaceAccessRequest] = useState(0);
  const [actionReceiptGenerationId, setActionReceiptGenerationId] =
    useState<string>();
  const [actionRecheckProposalId, setActionRecheckProposalId] =
    useState<string>();
  const receiptPreparedAt = useRef(0);
  const receiptAttachmentId = useRef<string>();
  const draftRevision = useRef(0);
  const observedDraft = useRef({ panelId: panel.id, value: panel.draft ?? "" });
  if (
    observedDraft.current.panelId !== panel.id ||
    observedDraft.current.value !== (panel.draft ?? "")
  ) {
    draftRevision.current++;
    observedDraft.current = { panelId: panel.id, value: panel.draft ?? "" };
  }
  // Every explicit edit/preparation is a new draft, even if its text matches.
  // This identity is local to this mounted composer, never persisted in tabs.
  const setDraft = useCallback(
    (value: string) => {
      draftRevision.current++;
      observedDraft.current = { panelId: panel.id, value };
      update(panel.id, { draft: value });
    },
    [panel.id, update],
  );
  const pendingReceiptDraft = useRef<{
    generationId: string;
    message: string;
    revision: number;
  } | null>(null);
  const [recommendedWorkspaceScopes, setRecommendedWorkspaceScopes] = useState<
    AiWorkspaceScope[]
  >(["work", "actions"]);
  const sending = useRef(false);
  const scroll = useRef<HTMLDivElement>(null);
  const available = providers.data?.filter((p) => p.status === "ready") ?? [];
  const providerId = panel.providerId || available[0]?.id || "";
  const activeProvider =
    available.find((provider) => provider.id === providerId) ?? null;
  const panelSessionId = panel.chatSessionId ?? "";
  const workspaceGrant =
    workspaceApproval &&
    activeProvider &&
    workspaceApproval.providerId === activeProvider.id &&
    workspaceApproval.providerVersion === activeProvider.version &&
    workspaceApproval.sessionId === panelSessionId
      ? workspaceApproval.grant
      : undefined;
  const owner = `side:${panel.id}`;
  useEffect(() => {
    receiptAttachmentId.current = undefined;
    setActionReceiptGenerationId(undefined);
    setActionRecheckProposalId(undefined);
  }, [panelSessionId, activeProvider?.id, activeProvider?.version]);
  useEffect(() => {
    const accepted = chat.acceptedCommand;
    if (
      actionReceiptGenerationId &&
      accepted &&
      accepted.sequence > receiptPreparedAt.current &&
      accepted.owner === owner &&
      accepted.sessionId === panelSessionId &&
      !!receiptAttachmentId.current &&
      accepted.actionReceiptAttachmentId === receiptAttachmentId.current &&
      accepted.actionReceiptGenerationId === actionReceiptGenerationId &&
      accepted.actionRecheckProposalId === actionRecheckProposalId
    ) {
      receiptAttachmentId.current = undefined;
      setActionReceiptGenerationId(undefined);
      setActionRecheckProposalId(undefined);
      setWorkspaceApproval(null);
      const pending = pendingReceiptDraft.current;
      const current = useWorkspacePanels
        .getState()
        .tabs.find((tab) => tab.id === panel.id);
      if (
        pending?.generationId === actionReceiptGenerationId &&
        pending.revision === draftRevision.current &&
        current?.draft === pending.message
      )
        setDraft("");
      pendingReceiptDraft.current = null;
    }
  }, [
    chat.acceptedCommand,
    actionReceiptGenerationId,
    actionRecheckProposalId,
    owner,
    panelSessionId,
    panel.id,
    setDraft,
  ]);
  const continuationSource = JSON.stringify([
    owner,
    panelSessionId,
    activeProvider?.id,
    activeProvider?.version,
    !!chat.streaming,
    creating,
  ]);
  const continuationSourceRef = useRef({ key: continuationSource, epoch: 0 });
  if (continuationSourceRef.current.key !== continuationSource)
    continuationSourceRef.current = {
      key: continuationSource,
      epoch: continuationSourceRef.current.epoch + 1,
    };
  const continuationEpoch = continuationSourceRef.current.epoch;
  // Recovery knows the durable session, not the original component owner.
  const streaming =
    chat.streaming &&
    (chat.streamOwner === owner ||
      chat.streaming.sessionId === panel.chatSessionId)
      ? chat.streaming
      : null;
  const persisted = [...(messages.data?.pages ?? [])]
    .reverse()
    .flatMap((p) => p.data);
  const known = new Set(persisted.map((m) => m.generation_id));
  const retained = chat.retainedTurns.filter(
    (t) => t.sessionId === panel.chatSessionId && !known.has(t.generationId),
  );
  const interrupted =
    chat.interrupted &&
    chat.interrupted.sessionId === panel.chatSessionId &&
    !known.has(chat.interrupted.generationId ?? "") &&
    !retained.some(
      (turn) => turn.generationId === chat.interrupted?.generationId,
    )
      ? chat.interrupted
      : null;
  const openMain = () => {
    if (panel.chatSessionId) chat.setActiveSessionId(panel.chatSessionId);
  };
  const appendDraft = (prompt: string) => {
    const current =
      useWorkspacePanels.getState().tabs.find((tab) => tab.id === panel.id)
        ?.draft ?? "";
    setDraft(
      current.endsWith(prompt)
        ? current
        : current.trim()
          ? `${current}\n\n${prompt}`
          : prompt,
    );
  };
  const send = async () => {
    if (isContinuationActive(automaticContinuation.data)) {
      setError("该会话正在自动续办，请先停止自动续办后再发送。草稿已保留。");
      return;
    }
    if (
      !panel.draft?.trim() ||
      !providerId ||
      chat.streaming ||
      (actionRecheckProposalId && !workspaceGrant) ||
      sending.current
    )
      return;
    sending.current = true;
    setCreating(true);
    setError("");
    const draft = panel.draft;
    const draftRevisionForRequest = draftRevision.current;
    if (actionReceiptGenerationId)
      pendingReceiptDraft.current = {
        generationId: actionReceiptGenerationId,
        message: draft,
        revision: draftRevisionForRequest,
      };
    const grantForRequest = workspaceGrant;
    const attachmentIdForRequest = receiptAttachmentId.current;
    try {
      const sessionId = panel.chatSessionId || (await createAiSession()).id;
      // Creating a side chat must not change the main conversation selection.
      if (!useWorkspacePanels.getState().tabs.some((t) => t.id === panel.id))
        return;
      if (!panel.chatSessionId && grantForRequest) {
        setWorkspaceApproval((current) =>
          current && current.sessionId === ""
            ? { ...current, sessionId }
            : current,
        );
      }
      update(panel.id, { chatSessionId: sessionId, providerId });
      const result = await chat.send({
        sessionId,
        providerId,
        message: draft,
        owner,
        ...(grantForRequest ? { workspace: grantForRequest } : {}),
        ...(actionReceiptGenerationId ? { actionReceiptGenerationId } : {}),
        ...(actionReceiptGenerationId && attachmentIdForRequest
          ? { actionReceiptAttachmentId: attachmentIdForRequest }
          : {}),
        ...(actionRecheckProposalId ? { actionRecheckProposalId } : {}),
      });
      const sameAttachment =
        receiptAttachmentId.current === attachmentIdForRequest;
      if (
        sameAttachment &&
        (result.accepted ||
          result.errorCode === "AI_WORKSPACE_PROVIDER_CHANGED")
      )
        setWorkspaceApproval(null);
      if (result.accepted && sameAttachment) {
        receiptAttachmentId.current = undefined;
        setActionReceiptGenerationId(undefined);
        setActionRecheckProposalId(undefined);
        pendingReceiptDraft.current = null;
      }
      if (
        result.accepted &&
        draftRevision.current === draftRevisionForRequest &&
        useWorkspacePanels.getState().tabs.find((t) => t.id === panel.id)
          ?.draft === draft
      )
        setDraft("");
      if (result.error) setError(result.error);
      await client.invalidateQueries({ queryKey: ["ai"] });
    } catch (reason) {
      setError(workspaceError(reason));
    } finally {
      sending.current = false;
      setCreating(false);
    }
  };
  useEffect(() => {
    const node = scroll.current;
    if (node) node.scrollTop = node.scrollHeight;
  }, [streaming?.text, persisted.length, retained.length]);
  const text = (value: string) =>
    value
      ? stripAiTaskBlock(value) ||
        "该回复包含旧版任务建议，请在主对话中查看并确认。"
      : "";
  const prepareContinuation = (
    prompt: string,
    scopes: AiWorkspaceScope[],
    generationId?: string,
    recheckProposalId?: string,
  ) => {
    if (!activeProvider || chat.streaming || creating || !panelSessionId)
      return;
    setWorkspaceApproval(null);
    receiptAttachmentId.current = generationId
      ? crypto.randomUUID()
      : undefined;
    setActionReceiptGenerationId(generationId);
    setActionRecheckProposalId(recheckProposalId);
    receiptPreparedAt.current =
      useAiChatStore.getState().acceptedCommand?.sequence ?? 0;
    pendingReceiptDraft.current = null;
    appendDraft(prompt);
    setRecommendedWorkspaceScopes(scopes);
    setWorkspaceAccessRequest((request) => request + 1);
  };
  const prepareContinuationRef = useRef(prepareContinuation);
  prepareContinuationRef.current = prepareContinuation;
  const continuePlan = useMemo(
    () =>
      !activeProvider || chat.streaming || creating
        ? undefined
        : (prompt: string, scopes: AiWorkspaceScope[]) => {
            if (continuationSourceRef.current.epoch !== continuationEpoch)
              return;
            prepareContinuationRef.current(prompt, scopes);
          },
    [continuationEpoch, !!activeProvider, !!chat.streaming, creating],
  );
  const continueActions = useMemo(
    () =>
      !activeProvider || chat.streaming || creating
        ? undefined
        : (request: AiActionContinuation) => {
            if (
              continuationSourceRef.current.epoch !== continuationEpoch ||
              (request.sourceSessionId &&
                request.sourceSessionId !== panelSessionId)
            )
              return;
            prepareContinuationRef.current(
              request.prompt,
              request.scopes,
              request.generationId,
              request.recheckProposalId,
            );
          },
    [
      continuationEpoch,
      panelSessionId,
      !!activeProvider,
      !!chat.streaming,
      creating,
    ],
  );
  return (
    <div className="ws-chat">
      <div className="ws-toolbar">
        <MessageCircle size={15} />
        <span>独立会话</span>
        <button disabled={!panel.chatSessionId} onClick={openMain}>
          在主对话中打开
        </button>
      </div>
      <div className="ws-chat-messages" ref={scroll}>
        <AiWorkPlan
          key={panelSessionId}
          sessionId={panelSessionId}
          live={!!streaming}
          onContinue={continuePlan}
          onContinueActions={continueActions}
        />
        {!panel.chatSessionId && (
          <div className="ws-empty">
            <MessageCircle size={30} />
            <h3>侧边聊聊</h3>
            <p>
              保留主对话，另开一个话题。不会读取文件、网页或终端输出；主对话和侧边聊天共用单次生成额度。
            </p>
          </div>
        )}
        {messages.isError && (
          <button className="ws-error" onClick={() => void messages.refetch()}>
            会话读取失败，点击重试
          </button>
        )}
        {messages.hasNextPage && (
          <button
            onClick={() => void messages.fetchNextPage()}
            disabled={messages.isFetchingNextPage}
          >
            加载较早消息
          </button>
        )}
        {persisted.map((message) => (
          <div
            className={`ws-chat-message is-${message.role}`}
            key={message.id}
          >
            <AiAutomaticOrigin origin={message.origin} />
            {message.reasoning && (
              <details>
                <summary>思考过程</summary>
                <p>{message.reasoning}</p>
              </details>
            )}
            {message.role === "assistant" ? (
              renderAiRichText(text(message.content), message.session_id)
            ) : (
              <p>{message.content}</p>
            )}
            {message.role === "assistant" && (
              <>
                <AiCitationEvidence
                  status={message.citation_status}
                  citations={message.citations}
                  sessionId={message.session_id}
                />
                {message.generation_id ? (
                  <AiWorkspaceActions
                    generationId={message.generation_id}
                    sessionId={message.session_id}
                    onContinue={
                      message.session_id === panelSessionId
                        ? continueActions
                        : undefined
                    }
                  />
                ) : null}
                <AiCopyButton text={text(message.content)} />
              </>
            )}
          </div>
        ))}
        <AiAutomaticReply
          sessionId={panelSessionId}
          knownGenerationIds={persisted
            .filter((m) => m.role === "assistant")
            .map((m) => m.generation_id ?? "")}
        />
        {retained.map((turn) => (
          <div key={turn.generationId}>
            {turn.userText && (
              <div className="ws-chat-message is-user">
                <p>{turn.userText}</p>
              </div>
            )}
            <div className="ws-chat-message is-assistant">
              <AiRunProgress steps={turn.progress} />
              {renderAiRichText(text(turn.text), turn.sessionId)}
              <small>{turn.status !== "completed" ? turn.status : ""}</small>
              <AiCitationEvidence
                status={
                  turn.citationEvidence?.status ??
                  (turn.status === "completed" || turn.status === "incomplete"
                    ? null
                    : "not_requested")
                }
                citations={turn.citationEvidence?.items ?? []}
                sessionId={turn.sessionId}
                incomplete={turn.status === "incomplete"}
              />
            </div>
          </div>
        ))}
        {interrupted && !streaming && (
          <div className="ws-chat-message is-assistant">
            <AiRunProgress steps={interrupted.progress} />
            {renderAiRichText(text(interrupted.text), interrupted.sessionId)}
            <small>连接已中断，正在核对生成结果。</small>
          </div>
        )}
        {streaming && (
          <>
            <div className="ws-chat-message is-user">
              <p>{chat.sentMessage}</p>
            </div>
            <div className="ws-chat-message is-assistant">
              <AiRunProgress
                steps={streaming.progress}
                live={!chat.streamError}
              />
              {streaming.reasoning && (
                <details>
                  <summary>思考过程</summary>
                  <p>{streaming.reasoning}</p>
                </details>
              )}
              {renderAiRichText(
                text(streaming.text) ||
                  (streaming.progress?.length ? "" : "正在思考…"),
                streaming.sessionId,
              )}
            </div>
          </>
        )}
      </div>
      {(error ||
        ((chat.streamOwner === owner || streaming || interrupted) &&
          chat.streamError)) && (
        <p className="ws-error" role="alert">
          {error || chat.streamError}
        </p>
      )}
      <AiContinuationSendNotice lease={automaticContinuation.data} />
      <form
        className="ws-chat-composer"
        onSubmit={(e) => {
          e.preventDefault();
          void send();
        }}
      >
        <AiAccessRequestPrompt
          sessionId={panelSessionId}
          messages={persisted}
          liveRequest={chat.accessRequests[panelSessionId]}
          continuation={automaticContinuation.data}
          streamingGenerationId={
            chat.streaming?.sessionId === panelSessionId
              ? chat.streaming.generationId
              : undefined
          }
          disabled={!activeProvider || !!chat.streaming || creating}
          onPrepare={(scopes) => {
            if (!activeProvider || chat.streaming || creating) return;
            prepareContinuation(accessRequestContinuationPrompt, scopes);
          }}
          onDismissLive={(generationId) =>
            chat.dismissAccessRequest(panelSessionId, generationId)
          }
        />
        <AiWorkbenchHandoffCard
          disabled={!activeProvider || !!chat.streaming || creating}
          onPrepare={(handoff) => {
            const detail = workbenchHandoffDetails(handoff.type, handoff.id);
            if (!detail || !activeProvider || chat.streaming || creating)
              return;
            // Preparing a side-chat handoff never reuses an older one-message
            // grant. The user must choose permissions again before sending.
            setWorkspaceApproval(null);
            setActionReceiptGenerationId(undefined);
            setActionRecheckProposalId(undefined);
            appendDraft(detail.prompt);
            setRecommendedWorkspaceScopes(detail.scopes);
            setWorkspaceAccessRequest((request) => request + 1);
          }}
        />
        <AiIssueHandoffCard
          disabled={!activeProvider || !!chat.streaming || creating}
          onPrepare={(handoff) => {
            if (!activeProvider || chat.streaming || creating) return;
            // Queue issue handoff carries only the prepared prompt and
            // recommended scopes; it never sends, executes, or reads side
            // workspace files/browser/terminal content by itself.
            setWorkspaceApproval(null);
            setActionReceiptGenerationId(undefined);
            setActionRecheckProposalId(undefined);
            appendDraft(handoff.prompt);
            if (handoff.scopes.length > 0) {
              setRecommendedWorkspaceScopes(handoff.scopes);
              setWorkspaceAccessRequest((request) => request + 1);
            } else {
              setRecommendedWorkspaceScopes(["work", "actions"]);
            }
          }}
        />
        {actionReceiptGenerationId ? (
          <AiActionReceiptAttachment
            disabled={!!chat.streaming || creating}
            recheckProposalId={actionRecheckProposalId}
            onRemove={() => {
              receiptAttachmentId.current = undefined;
              setActionReceiptGenerationId(undefined);
              setActionRecheckProposalId(undefined);
            }}
          />
        ) : null}
        <textarea
          aria-label="侧边聊天输入"
          placeholder="另开一个话题…"
          value={panel.draft ?? ""}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (
              e.key === "Enter" &&
              !e.shiftKey &&
              !e.nativeEvent.isComposing
            ) {
              e.preventDefault();
              void send();
            }
          }}
        />
        <div className="ws-chat-composer-tools">
          {activeProvider ? (
            <AiWorkspaceAccess
              key={`${activeProvider.id}:${activeProvider.version}:${panelSessionId}`}
              provider={activeProvider}
              openRequest={workspaceAccessRequest}
              recommendedScopes={recommendedWorkspaceScopes}
              value={workspaceGrant}
              disabled={!!chat.streaming || creating}
              onChange={(grant) =>
                setWorkspaceApproval(
                  grant
                    ? {
                        providerId: activeProvider.id,
                        providerVersion: activeProvider.version,
                        sessionId: panelSessionId,
                        grant,
                      }
                    : null,
                )
              }
            />
          ) : null}
          <select
            aria-label="侧边聊天模型"
            value={providerId}
            onChange={(e) => {
              setWorkspaceApproval(null);
              update(panel.id, { providerId: e.target.value });
            }}
          >
            <option value="" disabled>
              选择模型
            </option>
            {available.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name} · {p.model}
              </option>
            ))}
          </select>
          {streaming ? (
            <button
              type="button"
              className="ws-chat-send"
              aria-label="停止侧边生成"
              onClick={() => void chat.stop()}
            >
              <Square size={16} />
            </button>
          ) : (
            <button
              className="ws-chat-send"
              aria-label="发送侧边消息"
              disabled={
                !providerId ||
                !panel.draft?.trim() ||
                !!chat.streaming ||
                (!!actionRecheckProposalId && !workspaceGrant) ||
                creating
              }
            >
              <ArrowUp size={17} />
            </button>
          )}
        </div>
        <AiWorkspaceGrantSummary value={workspaceGrant} />
      </form>
      <p className="ws-chat-disclaimer">
        输入、会话历史与已启用记忆会发送至所选模型；右侧文件、网页与终端不会自动读取。手动消息使用单次授权；计划自动续办需另行明确持续授权，所有业务操作仍逐项确认。旧版任务与记忆建议仍在主对话处理。
      </p>
    </div>
  );
}
