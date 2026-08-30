import { History, Link2, Plus, Unlink, UserRound } from "lucide-react";
import { FormEvent, useEffect, useLayoutEffect, useRef, useState } from "react";
import { ApiError } from "../api/client";
import {
  useClientActorLinksQuery,
  useClientActorOptionsQuery,
  useCreateClientActorLink,
  useDeleteClientActorLink,
} from "../api/hooks";
import { ErrorState, SkeletonRows } from "./feedback";

interface ClientActorLinksSectionProps {
  clientId: string;
  clientVersion: number;
  contactName: string | null;
}

const HISTORY_PAGE_SIZE = 6;

function formatDateTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

function actorLinkError(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) {
    if (error.code === "VERSION_CONFLICT") {
      return "客户资料已变化，已刷新最新版本，请重新确认关联。";
    }
    if (error.code === "CLIENT_CONTACT_ACTOR_ALREADY_LINKED") {
      return "该客户已经关联了本地联系人，请先解除现有关联。";
    }
    if (error.code === "CLIENT_LINK_ACTOR_UNAVAILABLE") {
      return "所选本地人员已停用或不再可用，请重新选择。";
    }
    return error.requestId
      ? `${error.message} · 请求 ${error.requestId}`
      : error.message;
  }
  return "客户联系人关联失败，请重试。";
}

export function ClientActorLinksSection({
  clientId,
  clientVersion,
  contactName,
}: ClientActorLinksSectionProps) {
  const activeQuery = useClientActorLinksQuery(clientId, {
    state: "active",
    page: 1,
    pageSize: 1,
  });
  const [historyView, setHistoryView] = useState({
    clientId,
    expanded: false,
    page: 1,
  });
  const showHistory = historyView.clientId === clientId && historyView.expanded;
  const historyPage = historyView.clientId === clientId ? historyView.page : 1;
  const historyQuery = useClientActorLinksQuery(
    clientId,
    {
      state: "unlinked",
      page: historyPage,
      pageSize: HISTORY_PAGE_SIZE,
    },
    showHistory,
  );
  const actorsQuery = useClientActorOptionsQuery(true);
  const createMutation = useCreateClientActorLink();
  const deleteMutation = useDeleteClientActorLink();
  const [mode, setMode] = useState<"existing" | "new">("existing");
  const [actorId, setActorId] = useState("");
  const [displayName, setDisplayName] = useState(contactName ?? "");
  const [notes, setNotes] = useState("");
  const [unlinkReason, setUnlinkReason] = useState("");
  const [confirmedClientVersions, setConfirmedClientVersions] = useState<
    Record<string, number>
  >(() => ({ [clientId]: clientVersion }));
  const formClientIdRef = useRef(clientId);
  const activeData = activeQuery.isPlaceholderData
    ? undefined
    : activeQuery.data;
  const historyData = historyQuery.isPlaceholderData
    ? undefined
    : historyQuery.data;
  const activeLink = activeData?.items[0] ?? null;
  const history = historyData?.items ?? [];
  const historyTotal = historyData?.meta.total ?? 0;
  const historyTotalPages = Math.max(
    1,
    Math.ceil(historyTotal / HISTORY_PAGE_SIZE),
  );
  const activeInitialError = activeQuery.isError && !activeData;
  const activeRefreshError = activeQuery.isError && Boolean(activeData);
  const activeLoading =
    !activeData &&
    (activeQuery.isPending ||
      activeQuery.isFetching ||
      activeQuery.isPlaceholderData);
  const historyInitialError = historyQuery.isError && !historyData;
  const historyRefreshError = historyQuery.isError && Boolean(historyData);
  const historyLoading =
    !historyData &&
    (historyQuery.isPending ||
      historyQuery.isFetching ||
      historyQuery.isPlaceholderData);
  const observedClientVersion = Math.max(
    clientVersion,
    activeData?.meta.clientVersion ?? 0,
    historyData?.meta.clientVersion ?? 0,
  );
  const effectiveVersion = Math.max(
    observedClientVersion,
    confirmedClientVersions[clientId] ?? 0,
  );
  const busy = createMutation.isPending || deleteMutation.isPending;
  const error =
    (createMutation.variables?.clientId === clientId
      ? actorLinkError(createMutation.error)
      : null) ??
    (deleteMutation.variables?.clientId === clientId
      ? actorLinkError(deleteMutation.error)
      : null);

  useEffect(() => {
    setHistoryView((current) =>
      current.clientId === clientId
        ? current
        : { clientId, expanded: false, page: 1 },
    );
  }, [clientId]);

  useLayoutEffect(() => {
    if (formClientIdRef.current === clientId) return;
    formClientIdRef.current = clientId;
    setMode("existing");
    setActorId("");
    setDisplayName(contactName ?? "");
    setNotes("");
    setUnlinkReason("");
  }, [clientId, contactName]);

  useLayoutEffect(() => {
    setConfirmedClientVersions((current) => {
      const nextVersion = Math.max(
        current[clientId] ?? 0,
        observedClientVersion,
      );
      return current[clientId] === nextVersion
        ? current
        : { ...current, [clientId]: nextVersion };
    });
  }, [clientId, observedClientVersion]);

  useEffect(() => {
    if (
      !showHistory ||
      !historyData ||
      historyQuery.isPlaceholderData ||
      historyQuery.isFetching ||
      historyQuery.isError ||
      historyPage <= historyTotalPages
    ) {
      return;
    }
    setHistoryView((current) =>
      current.clientId === clientId &&
      current.expanded &&
      current.page > historyTotalPages
        ? { ...current, page: historyTotalPages }
        : current,
    );
  }, [
    clientId,
    historyData,
    historyPage,
    historyQuery.isError,
    historyQuery.isFetching,
    historyQuery.isPlaceholderData,
    historyTotalPages,
    showHistory,
  ]);

  const createLink = async (event: FormEvent) => {
    event.preventDefault();
    if (mode === "existing" && !actorId) return;
    if (mode === "new" && !displayName.trim()) return;
    try {
      await createMutation.mutateAsync({
        clientId,
        input:
          mode === "existing"
            ? { actorId, expectedVersion: effectiveVersion }
            : {
                createPerson: {
                  displayName: displayName.trim(),
                  notes: notes.trim(),
                },
                expectedVersion: effectiveVersion,
              },
      });
      setActorId("");
      setNotes("");
    } catch {
      // Mutation state renders the actionable error without losing the draft.
    }
  };

  const unlink = async () => {
    if (!activeLink || !unlinkReason.trim()) return;
    try {
      await deleteMutation.mutateAsync({
        id: activeLink.id,
        clientId,
        input: {
          reason: unlinkReason.trim(),
          expectedVersion: effectiveVersion,
        },
      });
      setUnlinkReason("");
    } catch {
      // Mutation state renders the actionable error without losing the reason.
    }
  };

  return (
    <section className="project-detail-section client-actor-links-section">
      <div className="project-detail-heading">
        <div>
          <h2>本地联系人</h2>
          <p>
            将客户联系人显式关联为本地 person
            Actor，仅用于责任记录，不会创建账号、发送消息或授予访问权限。
          </p>
        </div>
        <button
          className="button button-secondary button-compact"
          onClick={() =>
            setHistoryView((current) =>
              current.clientId === clientId
                ? { ...current, expanded: !current.expanded }
                : { clientId, expanded: true, page: 1 },
            )
          }
          type="button"
        >
          <History size={13} />
          {showHistory ? "隐藏历史" : "关联历史"}
        </button>
      </div>

      {activeLoading ? <SkeletonRows count={2} /> : null}
      {activeInitialError ? (
        <ErrorState
          compact
          message="无法读取客户联系人关联。"
          onRetry={() => void activeQuery.refetch()}
        />
      ) : null}

      {activeRefreshError ? (
        <div className="inline-feedback error" role="alert">
          联系人关联刷新失败，仍显示上次结果。
          <button onClick={() => void activeQuery.refetch()} type="button">
            重试
          </button>
        </div>
      ) : null}

      {activeData && activeLink ? (
        <div className="client-actor-current">
          <div className="client-actor-profile">
            <span aria-hidden="true" className="client-actor-avatar">
              <UserRound size={16} />
            </span>
            <div>
              <strong>{activeLink.actor.displayName}</strong>
              <small>
                本地 person ·{" "}
                {activeLink.actor.status === "active" ? "启用" : "停用"}
              </small>
            </div>
            <span className="status-badge status-green">已关联</span>
          </div>
          <div className="client-actor-unlink">
            <label>
              解除原因
              <input
                disabled={busy}
                maxLength={1000}
                onChange={(event) => setUnlinkReason(event.target.value)}
                placeholder="例如：客户联系人已变更"
                value={unlinkReason}
              />
            </label>
            <button
              className="button button-secondary"
              disabled={busy || !unlinkReason.trim()}
              onClick={() => void unlink()}
              type="button"
            >
              <Unlink size={13} />
              {deleteMutation.isPending ? "解除中…" : "解除关联"}
            </button>
          </div>
        </div>
      ) : null}

      {activeData && !activeLink ? (
        <form className="client-actor-link-form" onSubmit={createLink}>
          <div
            className="client-actor-mode"
            role="tablist"
            aria-label="联系人关联方式"
          >
            <button
              aria-selected={mode === "existing"}
              className={mode === "existing" ? "active" : ""}
              onClick={() => setMode("existing")}
              role="tab"
              type="button"
            >
              <Link2 size={13} /> 关联现有人员
            </button>
            <button
              aria-selected={mode === "new"}
              className={mode === "new" ? "active" : ""}
              onClick={() => setMode("new")}
              role="tab"
              type="button"
            >
              <Plus size={13} /> 新建并关联
            </button>
          </div>

          {mode === "existing" ? (
            <label>
              本地人员
              <select
                disabled={busy || actorsQuery.isPending}
                onChange={(event) => setActorId(event.target.value)}
                value={actorId}
              >
                <option value="">请选择 active person</option>
                {(actorsQuery.data ?? []).map((actor) => (
                  <option key={actor.id} value={actor.id}>
                    {actor.displayName}
                  </option>
                ))}
              </select>
            </label>
          ) : (
            <div className="client-actor-new-fields">
              <label>
                显示名称
                <input
                  disabled={busy}
                  maxLength={100}
                  onChange={(event) => setDisplayName(event.target.value)}
                  placeholder="本地人员名称"
                  value={displayName}
                />
              </label>
              <label>
                本地备注
                <input
                  disabled={busy}
                  maxLength={2000}
                  onChange={(event) => setNotes(event.target.value)}
                  placeholder="可选，仅保存在本机"
                  value={notes}
                />
              </label>
            </div>
          )}

          {actorsQuery.isError && mode === "existing" ? (
            <div className="inline-feedback error">
              无法读取本地人员。
              <button onClick={() => void actorsQuery.refetch()} type="button">
                重试
              </button>
            </div>
          ) : null}
          <button
            className="button button-primary"
            disabled={
              busy || (mode === "existing" ? !actorId : !displayName.trim())
            }
            type="submit"
          >
            <Link2 size={13} />
            {createMutation.isPending ? "关联中…" : "确认关联"}
          </button>
        </form>
      ) : null}

      {error ? <div className="inline-feedback error">{error}</div> : null}

      {showHistory ? (
        <div className="client-actor-history">
          {historyLoading ? (
            <>
              <p className="client-actor-history-empty" role="status">
                正在读取第 {historyPage} 页关联历史…
              </p>
              <SkeletonRows count={2} />
            </>
          ) : null}
          {historyInitialError ? (
            <ErrorState
              compact
              message="无法读取联系人关联历史。"
              onRetry={() => void historyQuery.refetch()}
            />
          ) : null}
          {historyRefreshError ? (
            <div className="inline-feedback error" role="alert">
              联系人关联历史刷新失败，仍显示上次结果。
              <button onClick={() => void historyQuery.refetch()} type="button">
                重试
              </button>
            </div>
          ) : null}
          {historyData && historyQuery.isFetching && !historyQuery.isError ? (
            <p className="client-actor-history-empty" role="status">
              正在刷新联系人关联历史…
            </p>
          ) : null}
          {historyData && history.length === 0 ? (
            <p className="client-actor-history-empty">
              暂无已解除的联系人关联。
            </p>
          ) : null}
          {history.map((link) => (
            <article key={link.id}>
              <div>
                <strong>{link.actor.displayName}</strong>
                <small>
                  {formatDateTime(link.linkedAt)} 关联
                  {link.unlinkedAt
                    ? ` · ${formatDateTime(link.unlinkedAt)} 解除`
                    : ""}
                </small>
              </div>
              <p>{link.unlinkReason}</p>
            </article>
          ))}
          {historyData ? (
            <div aria-label="联系人关联历史分页" className="pagination">
              <button
                className="button button-secondary button-compact"
                disabled={
                  historyPage <= 1 ||
                  historyQuery.isFetching ||
                  historyQuery.isError
                }
                onClick={() =>
                  setHistoryView((current) =>
                    current.clientId === clientId
                      ? { ...current, page: Math.max(1, current.page - 1) }
                      : { clientId, expanded: true, page: 1 },
                  )
                }
                type="button"
              >
                上一页
              </button>
              <span>
                第 {historyPage} / {historyTotalPages} 页 · 共 {historyTotal} 条
              </span>
              <button
                className="button button-secondary button-compact"
                disabled={
                  historyPage >= historyTotalPages ||
                  historyQuery.isFetching ||
                  historyQuery.isError
                }
                onClick={() =>
                  setHistoryView((current) =>
                    current.clientId === clientId
                      ? {
                          ...current,
                          page: Math.min(historyTotalPages, current.page + 1),
                        }
                      : { clientId, expanded: true, page: 1 },
                  )
                }
                type="button"
              >
                下一页
              </button>
            </div>
          ) : null}
        </div>
      ) : null}
    </section>
  );
}
