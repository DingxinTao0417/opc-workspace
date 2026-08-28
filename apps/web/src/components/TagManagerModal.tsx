import { Plus, Save, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { ApiError } from "../api/client";
import {
  useCreateTag,
  useDeleteTag,
  useTagOptionsQuery,
  useUpdateTag,
} from "../api/hooks";
import type { Tag } from "../types/models";
import { ErrorState, SkeletonRows } from "./feedback";
import { Modal } from "./Modal";

const initialColor = "#6E7BF2";

function mutationError(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) {
    return error.requestId
      ? `${error.message} · 请求 ${error.requestId}`
      : error.message;
  }
  return "标签操作失败，请重试。";
}

function TagManagerRow({ tag }: { tag: Tag }) {
  const updateMutation = useUpdateTag();
  const deleteMutation = useDeleteTag();
  const [name, setName] = useState(tag.name);
  const [color, setColor] = useState(tag.color);
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const changed = name.trim() !== tag.name || color !== tag.color;
  const busy = updateMutation.isPending || deleteMutation.isPending;
  const error =
    mutationError(updateMutation.error) ?? mutationError(deleteMutation.error);

  useEffect(() => {
    setName(tag.name);
    setColor(tag.color);
    setConfirmingDelete(false);
  }, [tag]);

  return (
    <div className="tag-manager-row">
      <input
        aria-label={`标签名称：${tag.name}`}
        disabled={busy}
        maxLength={50}
        onChange={(event) => setName(event.target.value)}
        value={name}
      />
      <input
        aria-label={`标签颜色：${tag.name}`}
        className="task-tag-color"
        disabled={busy}
        onChange={(event) => setColor(event.target.value.toUpperCase())}
        type="color"
        value={color}
      />
      <button
        aria-label={`保存标签：${tag.name}`}
        className="button button-secondary tag-manager-icon"
        disabled={!changed || !name.trim() || busy}
        onClick={() =>
          updateMutation.mutate({
            id: tag.id,
            input: {
              name: name.trim(),
              color,
              expectedVersion: tag.version,
            },
          })
        }
        title="保存"
        type="button"
      >
        <Save size={13} />
      </button>
      {confirmingDelete ? (
        <div className="tag-manager-confirm">
          <button
            className="button button-secondary"
            disabled={busy}
            onClick={() => setConfirmingDelete(false)}
            type="button"
          >
            取消
          </button>
          <button
            className="button button-danger"
            disabled={busy}
            onClick={() =>
              deleteMutation.mutate({
                id: tag.id,
                expectedVersion: tag.version,
              })
            }
            type="button"
          >
            确认删除
          </button>
        </div>
      ) : (
        <button
          aria-label={`删除标签：${tag.name}`}
          className="button button-quiet tag-manager-icon"
          disabled={busy}
          onClick={() => setConfirmingDelete(true)}
          title="删除"
          type="button"
        >
          <Trash2 size={13} />
        </button>
      )}
      {error ? (
        <span className="tag-manager-error" role="alert">
          {error}
        </span>
      ) : null}
    </div>
  );
}

export function TagManagerModal({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const query = useTagOptionsQuery(open);
  const createMutation = useCreateTag();
  const [name, setName] = useState("");
  const [color, setColor] = useState(initialColor);
  const error = mutationError(createMutation.error);

  useEffect(() => {
    if (!open) return;
    setName("");
    setColor(initialColor);
    createMutation.reset();
  }, [open]);

  const create = () => {
    const cleanName = name.trim();
    if (!cleanName || createMutation.isPending) return;
    createMutation.mutate(
      { name: cleanName, color },
      {
        onSuccess: () => {
          setName("");
          setColor(initialColor);
        },
      },
    );
  };

  return (
    <Modal
      footer={
        <button
          className="button button-secondary"
          disabled={createMutation.isPending}
          onClick={onClose}
          type="button"
        >
          完成
        </button>
      }
      onClose={onClose}
      open={open}
      title="管理标签"
      width="600px"
    >
      <div className="tag-manager-create">
        <input
          aria-label="新标签名称"
          autoFocus
          disabled={createMutation.isPending}
          maxLength={50}
          onChange={(event) => setName(event.target.value)}
          onKeyDown={(event) => {
            if (event.key !== "Enter") return;
            event.preventDefault();
            create();
          }}
          placeholder="标签名称"
          value={name}
        />
        <input
          aria-label="新标签颜色"
          className="task-tag-color"
          disabled={createMutation.isPending}
          onChange={(event) => setColor(event.target.value.toUpperCase())}
          type="color"
          value={color}
        />
        <button
          className="button button-primary"
          disabled={!name.trim() || createMutation.isPending}
          onClick={create}
          type="button"
        >
          <Plus size={13} />
          {createMutation.isPending ? "创建中…" : "新建标签"}
        </button>
      </div>
      {error ? (
        <div className="form-error" role="alert">
          {error}
        </div>
      ) : null}
      {query.isPending ? <SkeletonRows count={4} /> : null}
      {query.isError ? (
        <ErrorState
          compact
          message="无法读取标签，请检查本地服务后重试。"
          onRetry={() => void query.refetch()}
        />
      ) : null}
      {query.isSuccess && query.data.length === 0 ? (
        <p className="tag-manager-empty">尚无标签，先创建一个。</p>
      ) : null}
      {(query.data ?? []).length > 0 ? (
        <div className="tag-manager-list">
          {(query.data ?? []).map((tag) => (
            <TagManagerRow key={tag.id} tag={tag} />
          ))}
        </div>
      ) : null}
    </Modal>
  );
}
