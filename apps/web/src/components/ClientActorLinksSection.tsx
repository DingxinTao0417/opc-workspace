import { History, Link2, Plus, Unlink, UserRound } from "lucide-react";
import { FormEvent, useMemo, useState } from "react";
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
  const activeQuery = useClientActorLinksQuery(clientId, { pageSize: 20 });
  const [showHistory, setShowHistory] = useState(false);
  const historyQuery = useClientActorLinksQuery(clientId, {
    pageSize: 100,
    includeUnlinked: true,
  });
  const actorsQuery = useClientActorOptionsQuery(true);
  const createMutation = useCreateClientActorLink();
  const deleteMutation = useDeleteClientActorLink();
  const [mode, setMode] = useState<"existing" | "new">("existing");
  const [actorId, setActorId] = useState("");
  const [displayName, setDisplayName] = useState(contactName ?? "");
  const [notes, setNotes] = useState("");
  const [unlinkReason, setUnlinkReason] = useState("");
  const activeLink = activeQuery.data?.items[0] ?? null;
  const history = useMemo(
    () =>
      (historyQuery.data?.items ?? []).filter(
        (item) => item.unlinkedAt !== null,
      ),
    [historyQuery.data?.items],
  );
  const effectiveVersion = Math.max(
    clientVersion,
    activeQuery.data?.meta.clientVersion ?? 0,
    historyQuery.data?.meta.clientVersion ?? 0,
  );
  const busy = createMutation.isPending || deleteMutation.isPending;
  const error =
    actorLinkError(createMutation.error) ??
    actorLinkError(deleteMutation.error);

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
          onClick={() => setShowHistory((value) => !value)}
          type="button"
        >
          <History size={13} />
          {showHistory ? "隐藏历史" : "关联历史"}
        </button>
      </div>

      {activeQuery.isPending ? <SkeletonRows count={2} /> : null}
      {activeQuery.isError ? (
        <ErrorState
          compact
          message="无法读取客户联系人关联。"
          onRetry={() => void activeQuery.refetch()}
        />
      ) : null}

      {activeQuery.isSuccess && activeLink ? (
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

      {activeQuery.isSuccess && !activeLink ? (
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
          {historyQuery.isPending ? <SkeletonRows count={2} /> : null}
          {historyQuery.isError ? (
            <ErrorState
              compact
              message="无法读取联系人关联历史。"
              onRetry={() => void historyQuery.refetch()}
            />
          ) : null}
          {historyQuery.isSuccess && history.length === 0 ? (
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
        </div>
      ) : null}
    </section>
  );
}
