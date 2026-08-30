import {
  AlertCircle,
  LoaderCircle,
  Pencil,
  Plus,
  RefreshCw,
  ShieldCheck,
  UserRound,
  UsersRound,
  X,
} from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { ApiError } from "../api/client";
import {
  actorQueryKey,
  useActorQuery,
  useActorsQuery,
  useCreatePersonActor,
  useUpdateActor,
} from "../api/hooks";
import type { Actor, ActorStatus } from "../types/models";

interface ActorDraft {
  displayName: string;
  notes: string;
  metadataText: string;
  status: ActorStatus;
}

const EMPTY_DRAFT: ActorDraft = {
  displayName: "",
  notes: "",
  metadataText: "{}",
  status: "active",
};

const PERSON_PAGE_SIZE = 20;

const actorTypeLabels: Record<Actor["type"], string> = {
  owner: "所有者",
  person: "本地人员",
  system: "系统",
  agent: "本地 Agent",
};

function actorDraft(actor: Actor): ActorDraft {
  return {
    displayName: actor.displayName,
    notes: actor.notes,
    metadataText: JSON.stringify(actor.metadata, null, 2),
    status: actor.status,
  };
}

function formatError(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.code === "VERSION_CONFLICT") {
      return "该人员已在其他窗口修改。请载入最新内容后再编辑。";
    }
    if (error.code === "ACTOR_HAS_ACTIVE_ASSIGNMENTS") {
      return "该人员仍有负责中的任务，请先改派这些任务，再停用人员。";
    }
    if (error.code === "ACTOR_HAS_PLANNED_CLIENT_FOLLOWUPS") {
      return "该人员仍有待回访计划，请先改派、完成、跳过或取消这些回访，再停用人员。";
    }
    const request = error.requestId ? ` · 请求 ${error.requestId}` : "";
    return `${error.message}${request}`;
  }
  return "操作失败，请稍后重试。";
}

function parseMetadata(value: string): Record<string, unknown> {
  const trimmed = value.trim();
  if (!trimmed) return {};
  let parsed: unknown;
  try {
    parsed = JSON.parse(trimmed);
  } catch {
    throw new Error('扩展信息必须是有效的 JSON 对象。示例：{"role":"设计"}');
  }
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
    throw new Error("扩展信息必须是 JSON 对象，不能是数组或空值。");
  }
  return parsed as Record<string, unknown>;
}

function actorInitial(actor: Actor): string {
  return Array.from(actor.displayName.trim())[0]?.toUpperCase() || "?";
}

function ActorBadge({ actor }: { actor: Actor }) {
  return (
    <span className="actor-settings-badges">
      <span className="actor-settings-type">{actorTypeLabels[actor.type]}</span>
      <span className="actor-settings-status" data-status={actor.status}>
        {actor.status === "active" ? "启用" : "已停用"}
      </span>
    </span>
  );
}

function ActorEditor({
  actor,
  draft,
  error,
  pending,
  onCancel,
  onChange,
  onReload,
  reloadPending,
  saveDisabled,
  onSubmit,
}: {
  actor: Actor;
  draft: ActorDraft;
  error: string | null;
  pending: boolean;
  onCancel: () => void;
  onChange: (draft: ActorDraft) => void;
  onReload: () => void;
  reloadPending: boolean;
  saveDisabled: boolean;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
}) {
  const person = actor.type === "person";
  return (
    <form className="actor-settings-editor" onSubmit={onSubmit}>
      <label className="actor-settings-field">
        <span>名称</span>
        <input
          aria-label={`编辑${actor.displayName}的名称`}
          autoFocus
          maxLength={100}
          onChange={(event) =>
            onChange({ ...draft, displayName: event.target.value })
          }
          value={draft.displayName}
        />
      </label>
      {person ? (
        <>
          <label className="actor-settings-field">
            <span>备注</span>
            <textarea
              aria-label={`编辑${actor.displayName}的备注`}
              maxLength={2000}
              onChange={(event) =>
                onChange({ ...draft, notes: event.target.value })
              }
              placeholder="例如：负责视觉设计；仅供本机识别"
              rows={3}
              value={draft.notes}
            />
          </label>
          <label className="actor-settings-field">
            <span>扩展信息（JSON，可选）</span>
            <textarea
              aria-label={`编辑${actor.displayName}的扩展信息`}
              maxLength={8000}
              onChange={(event) =>
                onChange({ ...draft, metadataText: event.target.value })
              }
              placeholder={'{"role":"设计"}'}
              rows={3}
              spellCheck={false}
              value={draft.metadataText}
            />
            <small>仅保存非敏感本地标签，不要填写密码、令牌或 API 密钥。</small>
          </label>
          <label className="actor-settings-field actor-settings-status-field">
            <span>状态</span>
            <select
              aria-label={`编辑${actor.displayName}的状态`}
              onChange={(event) =>
                onChange({
                  ...draft,
                  status: event.target.value as ActorStatus,
                })
              }
              value={draft.status}
            >
              <option value="active">启用，保留为可分派主体</option>
              <option value="inactive">停用，排除后续新分派</option>
            </select>
          </label>
        </>
      ) : null}
      {error ? (
        <div className="actor-settings-error" role="alert">
          <AlertCircle size={14} />
          <span>{error}</span>
          {error.includes("载入最新内容") ? (
            <button disabled={reloadPending} onClick={onReload} type="button">
              {reloadPending ? "正在载入…" : "载入最新内容"}
            </button>
          ) : null}
        </div>
      ) : null}
      <div className="actor-settings-form-actions">
        <button
          className="button button-quiet"
          disabled={pending}
          onClick={onCancel}
          type="button"
        >
          取消
        </button>
        <button
          className="button button-primary"
          disabled={pending || saveDisabled}
          type="submit"
        >
          {pending ? <LoaderCircle className="is-spinning" size={14} /> : null}
          保存人员
        </button>
      </div>
    </form>
  );
}

export function ActorSettings() {
  const queryClient = useQueryClient();
  const [personPage, setPersonPage] = useState(1);
  const ownerQuery = useActorsQuery({
    page: 1,
    pageSize: PERSON_PAGE_SIZE,
    type: "owner",
  });
  const systemQuery = useActorsQuery({
    page: 1,
    pageSize: PERSON_PAGE_SIZE,
    type: "system",
  });
  const peopleQuery = useActorsQuery({
    page: personPage,
    pageSize: PERSON_PAGE_SIZE,
    type: "person",
    sort: "display_name",
  });
  const createActor = useCreatePersonActor();
  const updateActor = useUpdateActor();
  const [createOpen, setCreateOpen] = useState(false);
  const [createDraft, setCreateDraft] = useState<ActorDraft>(EMPTY_DRAFT);
  const [createError, setCreateError] = useState<string | null>(null);
  const [editingActor, setEditingActor] = useState<Actor | null>(null);
  const [editDraft, setEditDraft] = useState<ActorDraft>(EMPTY_DRAFT);
  const [editError, setEditError] = useState<string | null>(null);
  const [editVersionBlocked, setEditVersionBlocked] = useState(false);
  const editingActorIdRef = useRef<string | null>(null);
  const editHadVersionConflictRef = useRef(false);
  const editingActorQuery = useActorQuery(editingActor?.id ?? null);

  const builtinActors = useMemo(
    () => [
      ...(ownerQuery.data?.items.filter((actor) => actor.type === "owner") ??
        []),
      ...(systemQuery.data?.items.filter((actor) => actor.type === "system") ??
        []),
    ],
    [ownerQuery.data, systemQuery.data],
  );
  const peopleDataVisible = Boolean(
    peopleQuery.data &&
    !peopleQuery.isPlaceholderData &&
    peopleQuery.data.meta.page === personPage,
  );
  const people = useMemo(
    () =>
      peopleDataVisible
        ? (peopleQuery.data?.items.filter((actor) => actor.type === "person") ??
          [])
        : [],
    [peopleDataVisible, peopleQuery.data],
  );
  const displayedPeople = useMemo(() => {
    if (editingActor?.type !== "person") {
      return people;
    }
    if (people.some((actor) => actor.id === editingActor.id)) {
      return people.map((actor) =>
        actor.id === editingActor.id ? editingActor : actor,
      );
    }
    return [editingActor, ...people];
  }, [editingActor, people]);
  const peopleTotal = peopleDataVisible
    ? (peopleQuery.data?.meta.total ?? 0)
    : 0;
  const peoplePageSize = peopleDataVisible
    ? (peopleQuery.data?.meta.pageSize ?? PERSON_PAGE_SIZE)
    : PERSON_PAGE_SIZE;
  const peopleTotalPages = Math.max(1, Math.ceil(peopleTotal / peoplePageSize));
  const formOpen = createOpen || editingActor !== null;
  const paginationLocked =
    formOpen ||
    createActor.isPending ||
    updateActor.isPending ||
    peopleQuery.isFetching ||
    peopleQuery.isPlaceholderData ||
    peopleQuery.isError;
  const builtinInitialPending = ownerQuery.isPending || systemQuery.isPending;
  const builtinInitialError =
    (ownerQuery.isError && !ownerQuery.data) ||
    (systemQuery.isError && !systemQuery.data);
  const builtinRefreshError =
    (ownerQuery.isError && Boolean(ownerQuery.data)) ||
    (systemQuery.isError && Boolean(systemQuery.data));
  const peoplePageError = peopleQuery.isError && !peopleDataVisible;
  const peopleRefreshError = peopleQuery.isError && peopleDataVisible;
  const changingPeoplePage =
    peopleQuery.isPlaceholderData ||
    (peopleQuery.isFetching &&
      Boolean(peopleQuery.data) &&
      peopleQuery.data?.meta.page !== personPage);
  const refreshingPeople =
    peopleQuery.isFetching &&
    !peopleQuery.isPlaceholderData &&
    Boolean(peopleQuery.data);
  const pageOutOfRange =
    !formOpen && peopleDataVisible && personPage > peopleTotalPages;

  useEffect(() => {
    if (
      formOpen ||
      !peopleQuery.data ||
      !peopleQuery.isSuccess ||
      peopleQuery.isError ||
      peopleQuery.isFetching ||
      peopleQuery.isPlaceholderData ||
      personPage <= peopleTotalPages
    ) {
      return;
    }
    void queryClient.invalidateQueries({
      exact: true,
      queryKey: [
        ...actorQueryKey,
        "list",
        {
          page: peopleTotalPages,
          pageSize: PERSON_PAGE_SIZE,
          type: "person",
          sort: "display_name",
        },
      ],
      refetchType: "none",
    });
    setPersonPage(peopleTotalPages);
  }, [
    formOpen,
    peopleQuery.data,
    peopleQuery.isError,
    peopleQuery.isFetching,
    peopleQuery.isPlaceholderData,
    peopleQuery.isSuccess,
    peopleTotalPages,
    personPage,
    queryClient,
  ]);

  const startEditing = (actor: Actor) => {
    editingActorIdRef.current = actor.id;
    editHadVersionConflictRef.current = false;
    setEditingActor(actor);
    setEditDraft(actorDraft(actor));
    setEditError(null);
    setEditVersionBlocked(false);
  };

  const cancelEditing = () => {
    const conflictedActorType = editHadVersionConflictRef.current
      ? editingActor?.type
      : null;
    editingActorIdRef.current = null;
    editHadVersionConflictRef.current = false;
    setEditingActor(null);
    setEditError(null);
    setEditVersionBlocked(false);
    if (conflictedActorType === "person") {
      void peopleQuery.refetch();
    } else if (conflictedActorType === "owner") {
      void ownerQuery.refetch();
    }
  };

  const reloadEditingActor = async () => {
    if (!editingActor) return;
    const actorId = editingActor.id;
    const latest = await editingActorQuery.refetch();
    if (editingActorIdRef.current !== actorId) return;
    if (latest.isSuccess && latest.data) {
      setEditingActor(latest.data);
      setEditError(null);
      setEditVersionBlocked(false);
      return;
    }
    setEditVersionBlocked(true);
    setEditError(
      `无法载入最新内容。${formatError(latest.error ?? editingActorQuery.error)}`,
    );
  };

  const submitCreate = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const displayName = createDraft.displayName.trim();
    if (!displayName) {
      setCreateError("请输入人员名称。");
      return;
    }
    let metadata: Record<string, unknown>;
    try {
      metadata = parseMetadata(createDraft.metadataText);
    } catch (error) {
      setCreateError(
        error instanceof Error ? error.message : "扩展信息格式无效。",
      );
      return;
    }
    setCreateError(null);
    try {
      await createActor.mutateAsync({
        displayName,
        notes: createDraft.notes.trim(),
        metadata,
        status: createDraft.status,
      });
      setCreateDraft(EMPTY_DRAFT);
      setCreateOpen(false);
    } catch (error) {
      setCreateError(formatError(error));
    }
  };

  const submitEdit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!editingActor) return;
    if (editVersionBlocked) {
      setEditError("请先载入最新内容，再保存当前草稿。");
      return;
    }
    const displayName = editDraft.displayName.trim();
    if (!displayName) {
      setEditError("请输入人员名称。");
      return;
    }
    let metadata: Record<string, unknown> | undefined;
    if (editingActor.type === "person") {
      try {
        metadata = parseMetadata(editDraft.metadataText);
      } catch (error) {
        setEditError(
          error instanceof Error ? error.message : "扩展信息格式无效。",
        );
        return;
      }
    }
    setEditError(null);
    try {
      await updateActor.mutateAsync({
        id: editingActor.id,
        input:
          editingActor.type === "owner"
            ? {
                displayName,
                expectedVersion: editingActor.version,
              }
            : {
                displayName,
                notes: editDraft.notes.trim(),
                metadata,
                status: editDraft.status,
                expectedVersion: editingActor.version,
              },
      });
      editHadVersionConflictRef.current = false;
      cancelEditing();
    } catch (error) {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        editHadVersionConflictRef.current = true;
        setEditVersionBlocked(true);
      }
      setEditError(formatError(error));
    }
  };

  return (
    <>
      <header className="settings-content-header">
        <h3>人员与责任</h3>
        <p>管理本机工作流中的所有者、外部责任人和系统主体。</p>
      </header>

      <div className="actor-settings-notice">
        <ShieldCheck size={17} />
        <div>
          <strong>这里只记录责任归属</strong>
          <span>
            本地人员不会获得账号、收到任务或访问数据，也不会同步到远程服务。
          </span>
        </div>
      </div>

      <section
        className="actor-settings-section"
        aria-labelledby="builtin-actors-heading"
      >
        <div className="actor-settings-section-heading">
          <div>
            <h4 id="builtin-actors-heading">内置主体</h4>
            <p>所有者代表当前设备操作者；系统主体仅记录内部维护责任。</p>
          </div>
        </div>
        {builtinInitialPending ? (
          <div className="settings-state" role="status">
            <LoaderCircle className="is-spinning" size={18} />
            正在读取内置责任主体…
          </div>
        ) : null}
        {!builtinInitialPending && builtinInitialError ? (
          <div className="settings-state settings-state-error" role="alert">
            <AlertCircle size={18} />
            <div>
              <strong>无法读取内置责任主体</strong>
              <span>{formatError(ownerQuery.error ?? systemQuery.error)}</span>
            </div>
            <button
              className="button button-secondary"
              onClick={() => {
                void ownerQuery.refetch();
                void systemQuery.refetch();
              }}
              type="button"
            >
              <RefreshCw size={14} />
              重试
            </button>
          </div>
        ) : null}
        {!builtinInitialPending && !builtinInitialError ? (
          <>
            {builtinRefreshError ? (
              <div className="actor-settings-error" role="alert">
                <AlertCircle size={14} />
                <span>
                  内置责任主体刷新失败，当前显示上次成功读取的结果。{" "}
                  {formatError(ownerQuery.error ?? systemQuery.error)}
                </span>
                <button
                  onClick={() => {
                    void ownerQuery.refetch();
                    void systemQuery.refetch();
                  }}
                  type="button"
                >
                  重试刷新
                </button>
              </div>
            ) : null}
            <div className="actor-settings-list">
              {builtinActors.map((actor) => (
                <article className="actor-settings-card" key={actor.id}>
                  <div className="actor-settings-card-main">
                    <div
                      className="actor-settings-avatar"
                      data-type={actor.type}
                    >
                      {actor.type === "system" ? (
                        <ShieldCheck size={17} />
                      ) : (
                        actorInitial(actor)
                      )}
                    </div>
                    <div className="actor-settings-copy">
                      <div className="actor-settings-title">
                        <strong>{actor.displayName}</strong>
                        <ActorBadge actor={actor} />
                      </div>
                      <p>
                        {actor.type === "owner"
                          ? "当前设备的唯一所有者，可修改展示名称。"
                          : "内置系统主体，不可编辑或删除。"}
                      </p>
                    </div>
                    {actor.type === "owner" ? (
                      <button
                        aria-label={`编辑${actor.displayName}`}
                        className="button button-quiet actor-settings-edit"
                        disabled={updateActor.isPending}
                        onClick={() => startEditing(actor)}
                        type="button"
                      >
                        <Pencil size={14} />
                        编辑
                      </button>
                    ) : null}
                  </div>
                  {editingActor?.id === actor.id ? (
                    <ActorEditor
                      actor={actor}
                      draft={editDraft}
                      error={editError}
                      onCancel={cancelEditing}
                      onChange={setEditDraft}
                      onReload={() => void reloadEditingActor()}
                      onSubmit={(event) => void submitEdit(event)}
                      pending={updateActor.isPending}
                      reloadPending={editingActorQuery.isFetching}
                      saveDisabled={editVersionBlocked}
                    />
                  ) : null}
                </article>
              ))}
            </div>
          </>
        ) : null}
      </section>

      <section
        className="actor-settings-section"
        aria-labelledby="person-actors-heading"
      >
        <div className="actor-settings-section-heading">
          <div>
            <h4 id="person-actors-heading">本地人员</h4>
            <p>用于标记线下责任人；停用会保留历史记录。</p>
          </div>
          <div className="actor-settings-badges">
            <button
              aria-label="刷新本地人员"
              className="button button-quiet"
              disabled={paginationLocked || peopleQuery.isPending}
              onClick={() => void peopleQuery.refetch()}
              type="button"
            >
              <RefreshCw
                className={refreshingPeople ? "is-spinning" : undefined}
                size={14}
              />
              刷新
            </button>
            <button
              className="button button-secondary"
              disabled={
                peopleQuery.isPending ||
                peoplePageError ||
                createActor.isPending ||
                updateActor.isPending
              }
              onClick={() => {
                setCreateOpen(true);
                setCreateError(null);
              }}
              type="button"
            >
              <Plus size={14} />
              新建人员
            </button>
          </div>
        </div>

        {createOpen ? (
          <form
            className="actor-settings-create"
            onSubmit={(event) => void submitCreate(event)}
          >
            <div className="actor-settings-create-heading">
              <div>
                <strong>新建本地人员</strong>
                <span>
                  保存后可在任务详情和 Inbox 拆分中选择为本地负责人或复核人；
                  仅记录本机责任，不会创建线上账号。
                </span>
              </div>
              <button
                aria-label="关闭新建人员表单"
                disabled={createActor.isPending}
                onClick={() => {
                  setCreateOpen(false);
                  setCreateDraft(EMPTY_DRAFT);
                  setCreateError(null);
                }}
                type="button"
              >
                <X size={15} />
              </button>
            </div>
            <label className="actor-settings-field">
              <span>名称</span>
              <input
                aria-label="新人员名称"
                autoFocus
                maxLength={100}
                onChange={(event) =>
                  setCreateDraft({
                    ...createDraft,
                    displayName: event.target.value,
                  })
                }
                placeholder="例如：陈设计"
                value={createDraft.displayName}
              />
            </label>
            <label className="actor-settings-field">
              <span>备注</span>
              <textarea
                aria-label="新人员备注"
                maxLength={2000}
                onChange={(event) =>
                  setCreateDraft({ ...createDraft, notes: event.target.value })
                }
                placeholder="记录职责、协作方式等本地说明"
                rows={3}
                value={createDraft.notes}
              />
            </label>
            <label className="actor-settings-field">
              <span>扩展信息（JSON，可选）</span>
              <textarea
                aria-label="新人员扩展信息"
                maxLength={8000}
                onChange={(event) =>
                  setCreateDraft({
                    ...createDraft,
                    metadataText: event.target.value,
                  })
                }
                placeholder={'{"role":"设计"}'}
                rows={3}
                spellCheck={false}
                value={createDraft.metadataText}
              />
              <small>
                只用于非敏感本地标签，不要填写密码、令牌或 API 密钥。
              </small>
            </label>
            {createError ? (
              <div className="actor-settings-error" role="alert">
                <AlertCircle size={14} />
                <span>{createError}</span>
              </div>
            ) : null}
            <div className="actor-settings-form-actions">
              <button
                className="button button-quiet"
                disabled={createActor.isPending}
                onClick={() => {
                  setCreateOpen(false);
                  setCreateDraft(EMPTY_DRAFT);
                  setCreateError(null);
                }}
                type="button"
              >
                取消
              </button>
              <button
                className="button button-primary"
                disabled={createActor.isPending}
                type="submit"
              >
                {createActor.isPending ? (
                  <LoaderCircle className="is-spinning" size={14} />
                ) : null}
                创建人员
              </button>
            </div>
          </form>
        ) : null}

        {peopleQuery.isPending ? (
          <div className="settings-state" role="status">
            <LoaderCircle className="is-spinning" size={18} />
            正在读取本地人员…
          </div>
        ) : null}
        {!peopleQuery.isPending && peoplePageError ? (
          <div className="settings-state settings-state-error" role="alert">
            <AlertCircle size={18} />
            <div>
              <strong>
                {personPage === 1
                  ? "无法读取本地人员"
                  : `无法读取第 ${personPage} 页本地人员`}
              </strong>
              <span>{formatError(peopleQuery.error)}</span>
            </div>
            <button
              className="button button-secondary"
              onClick={() => void peopleQuery.refetch()}
              type="button"
            >
              <RefreshCw size={14} />
              {personPage === 1 ? "重试" : `重试第 ${personPage} 页`}
            </button>
            {personPage > 1 ? (
              <button
                className="button button-secondary"
                onClick={() => setPersonPage((page) => Math.max(1, page - 1))}
                type="button"
              >
                返回上一页
              </button>
            ) : null}
          </div>
        ) : null}
        {!peopleQuery.isPending && !peoplePageError && changingPeoplePage ? (
          <>
            <div className="settings-state" role="status">
              <LoaderCircle className="is-spinning" size={18} />
              正在读取第 {personPage} 页本地人员…
            </div>
            <nav aria-label="本地人员分页" className="pagination">
              <button
                className="button button-secondary"
                disabled
                type="button"
              >
                上一页
              </button>
              <span>第 {personPage} 页读取中</span>
              <button
                className="button button-secondary"
                disabled
                type="button"
              >
                下一页
              </button>
            </nav>
          </>
        ) : null}
        {!peopleQuery.isPending &&
        !peoplePageError &&
        !changingPeoplePage &&
        pageOutOfRange ? (
          <div className="settings-state" role="status">
            <LoaderCircle className="is-spinning" size={18} />
            本页已无人员，正在返回第 {peopleTotalPages} 页…
          </div>
        ) : null}
        {!peopleQuery.isPending &&
        !peoplePageError &&
        !changingPeoplePage &&
        !pageOutOfRange &&
        peopleDataVisible ? (
          <>
            {peopleRefreshError ? (
              <div className="actor-settings-error" role="alert">
                <AlertCircle size={14} />
                <span>
                  本地人员刷新失败，当前显示上次成功读取的结果。{" "}
                  {formatError(peopleQuery.error)}
                </span>
                <button
                  onClick={() => void peopleQuery.refetch()}
                  type="button"
                >
                  重试刷新
                </button>
              </div>
            ) : null}
            {refreshingPeople ? (
              <div className="settings-state" role="status">
                <LoaderCircle className="is-spinning" size={18} />
                正在刷新本地人员…
              </div>
            ) : null}
            {displayedPeople.length === 0 && !createOpen ? (
              <div className="actor-settings-empty">
                <UsersRound size={20} />
                <strong>还没有本地人员</strong>
                <span>
                  需要记录线下负责人时再创建，不会自动导入客户联系人。
                </span>
              </div>
            ) : (
              <div className="actor-settings-list">
                {displayedPeople.map((actor) => (
                  <article
                    className="actor-settings-card"
                    data-inactive={actor.status === "inactive"}
                    key={actor.id}
                  >
                    <div className="actor-settings-card-main">
                      <div className="actor-settings-avatar" data-type="person">
                        <UserRound size={17} />
                      </div>
                      <div className="actor-settings-copy">
                        <div className="actor-settings-title">
                          <strong>{actor.displayName}</strong>
                          <ActorBadge actor={actor} />
                        </div>
                        <p>{actor.notes || "暂无备注"}</p>
                      </div>
                      <button
                        aria-label={`编辑${actor.displayName}`}
                        className="button button-quiet actor-settings-edit"
                        disabled={updateActor.isPending}
                        onClick={() => startEditing(actor)}
                        type="button"
                      >
                        <Pencil size={14} />
                        编辑
                      </button>
                    </div>
                    {editingActor?.id === actor.id ? (
                      <ActorEditor
                        actor={actor}
                        draft={editDraft}
                        error={editError}
                        onCancel={cancelEditing}
                        onChange={setEditDraft}
                        onReload={() => void reloadEditingActor()}
                        onSubmit={(event) => void submitEdit(event)}
                        pending={updateActor.isPending}
                        reloadPending={editingActorQuery.isFetching}
                        saveDisabled={editVersionBlocked}
                      />
                    ) : null}
                  </article>
                ))}
              </div>
            )}
            <nav aria-label="本地人员分页" className="pagination">
              <button
                className="button button-secondary"
                disabled={paginationLocked || personPage <= 1}
                onClick={() => setPersonPage((page) => Math.max(1, page - 1))}
                type="button"
              >
                上一页
              </button>
              <span>
                共 {peopleTotal} 人 · 第 {personPage} / {peopleTotalPages} 页
              </span>
              <button
                className="button button-secondary"
                disabled={paginationLocked || personPage >= peopleTotalPages}
                onClick={() => setPersonPage((page) => page + 1)}
                type="button"
              >
                下一页
              </button>
            </nav>
          </>
        ) : null}
      </section>
    </>
  );
}
