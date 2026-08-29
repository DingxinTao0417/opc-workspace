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
import { useMemo, useState, type FormEvent } from "react";
import { ApiError } from "../api/client";
import {
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
      return "该人员已在其他窗口修改。列表已刷新，请载入最新内容后再编辑。";
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
  onSubmit,
}: {
  actor: Actor;
  draft: ActorDraft;
  error: string | null;
  pending: boolean;
  onCancel: () => void;
  onChange: (draft: ActorDraft) => void;
  onReload: () => void;
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
            <button onClick={onReload} type="button">
              载入最新内容
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
          disabled={pending}
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
  const actorsQuery = useActorsQuery({ pageSize: 100 });
  const createActor = useCreatePersonActor();
  const updateActor = useUpdateActor();
  const [createOpen, setCreateOpen] = useState(false);
  const [createDraft, setCreateDraft] = useState<ActorDraft>(EMPTY_DRAFT);
  const [createError, setCreateError] = useState<string | null>(null);
  const [editingActor, setEditingActor] = useState<Actor | null>(null);
  const [editDraft, setEditDraft] = useState<ActorDraft>(EMPTY_DRAFT);
  const [editError, setEditError] = useState<string | null>(null);

  const actors = actorsQuery.data?.items ?? [];
  const builtinActors = useMemo(
    () => actors.filter((actor) => actor.type !== "person"),
    [actors],
  );
  const people = useMemo(
    () => actors.filter((actor) => actor.type === "person"),
    [actors],
  );

  const startEditing = (actor: Actor) => {
    setEditingActor(actor);
    setEditDraft(actorDraft(actor));
    setEditError(null);
  };

  const reloadEditingActor = () => {
    if (!editingActor) return;
    const latest = actors.find((actor) => actor.id === editingActor.id);
    if (latest) startEditing(latest);
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
      setEditingActor(null);
    } catch (error) {
      setEditError(formatError(error));
    }
  };

  if (actorsQuery.isPending) {
    return (
      <div className="settings-state" role="status">
        <LoaderCircle className="is-spinning" size={18} />
        正在读取本地责任主体…
      </div>
    );
  }

  if (actorsQuery.isError) {
    return (
      <div className="settings-state settings-state-error" role="alert">
        <AlertCircle size={18} />
        <div>
          <strong>无法读取人员列表</strong>
          <span>{formatError(actorsQuery.error)}</span>
        </div>
        <button
          className="button button-secondary"
          onClick={() => void actorsQuery.refetch()}
          type="button"
        >
          <RefreshCw size={14} />
          重试
        </button>
      </div>
    );
  }

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
        <div className="actor-settings-list">
          {builtinActors.map((actor) => (
            <article className="actor-settings-card" key={actor.id}>
              <div className="actor-settings-card-main">
                <div className="actor-settings-avatar" data-type={actor.type}>
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
                      : actor.type === "agent"
                        ? "Agent 执行能力将在后续版本开放。"
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
                  onCancel={() => setEditingActor(null)}
                  onChange={setEditDraft}
                  onReload={reloadEditingActor}
                  onSubmit={(event) => void submitEdit(event)}
                  pending={updateActor.isPending}
                />
              ) : null}
            </article>
          ))}
        </div>
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
          <button
            className="button button-secondary"
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

        {createOpen ? (
          <form
            className="actor-settings-create"
            onSubmit={(event) => void submitCreate(event)}
          >
            <div className="actor-settings-create-heading">
              <div>
                <strong>新建本地人员</strong>
                <span>保存为本机责任主体；任务分派入口将在后续模块接入。</span>
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

        {people.length === 0 && !createOpen ? (
          <div className="actor-settings-empty">
            <UsersRound size={20} />
            <strong>还没有本地人员</strong>
            <span>需要记录线下负责人时再创建，不会自动导入客户联系人。</span>
          </div>
        ) : (
          <div className="actor-settings-list">
            {people.map((actor) => (
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
                    onCancel={() => setEditingActor(null)}
                    onChange={setEditDraft}
                    onReload={reloadEditingActor}
                    onSubmit={(event) => void submitEdit(event)}
                    pending={updateActor.isPending}
                  />
                ) : null}
              </article>
            ))}
          </div>
        )}
      </section>
    </>
  );
}
