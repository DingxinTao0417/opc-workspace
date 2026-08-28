import { Plus, RefreshCw } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { ApiError } from "../api/client";
import { useCreateTag, useTagOptionsQuery } from "../api/hooks";

const defaultTagColor = "#6E7BF2";

function errorText(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) {
    return error.requestId
      ? `${error.message} · 请求 ${error.requestId}`
      : error.message;
  }
  return "标签操作失败，请重试。";
}

export function TaskTagPicker({
  selectedIds,
  onChange,
  enabled = true,
  disabled = false,
}: {
  selectedIds: string[];
  onChange: (ids: string[]) => void;
  enabled?: boolean;
  disabled?: boolean;
}) {
  const tagsQuery = useTagOptionsQuery(enabled);
  const createMutation = useCreateTag();
  const [newName, setNewName] = useState("");
  const [newColor, setNewColor] = useState(defaultTagColor);
  const selected = useMemo(() => new Set(selectedIds), [selectedIds]);
  const selectedIdsRef = useRef(selectedIds);
  const error = errorText(createMutation.error);

  useEffect(() => {
    selectedIdsRef.current = selectedIds;
  }, [selectedIds]);

  const toggle = (id: string) => {
    if (selected.has(id)) {
      onChange(selectedIds.filter((item) => item !== id));
      return;
    }
    if (selectedIds.length < 20) onChange([...selectedIds, id]);
  };

  const create = () => {
    const name = newName.trim();
    if (!name || name.length > 50 || createMutation.isPending) return;
    createMutation.mutate(
      { name, color: newColor },
      {
        onSuccess: (tag) => {
          const current = selectedIdsRef.current;
          if (!current.includes(tag.id) && current.length < 20) {
            onChange([...current, tag.id]);
          }
          setNewName("");
          setNewColor(defaultTagColor);
        },
      },
    );
  };

  return (
    <div className="task-tag-picker">
      {tagsQuery.isPending ? (
        <span className="task-tag-hint">正在读取标签…</span>
      ) : null}
      {tagsQuery.isError ? (
        <div className="task-tag-error" role="alert">
          <span>标签列表读取失败。</span>
          <button
            className="form-inline-action"
            onClick={() => void tagsQuery.refetch()}
            type="button"
          >
            <RefreshCw size={11} />
            重试
          </button>
        </div>
      ) : null}
      {tagsQuery.isSuccess && tagsQuery.data.length === 0 ? (
        <span className="task-tag-hint">尚无标签，可在下方创建。</span>
      ) : null}
      {(tagsQuery.data ?? []).length > 0 ? (
        <div aria-label="选择任务标签" className="task-tag-options">
          {(tagsQuery.data ?? []).map((tag) => (
            <button
              aria-pressed={selected.has(tag.id)}
              className={
                selected.has(tag.id)
                  ? "task-tag-option task-tag-option-active"
                  : "task-tag-option"
              }
              disabled={disabled || createMutation.isPending}
              key={tag.id}
              onClick={() => toggle(tag.id)}
              type="button"
            >
              <span
                aria-hidden="true"
                className="task-tag-dot"
                style={{ backgroundColor: tag.color }}
              />
              {tag.name}
            </button>
          ))}
        </div>
      ) : null}
      <div className="task-tag-create">
        <input
          aria-label="新标签名称"
          disabled={disabled || createMutation.isPending}
          maxLength={50}
          onChange={(event) => setNewName(event.target.value)}
          onKeyDown={(event) => {
            if (event.key !== "Enter") return;
            event.preventDefault();
            create();
          }}
          placeholder="新建标签…"
          value={newName}
        />
        <input
          aria-label="新标签颜色"
          className="task-tag-color"
          disabled={disabled || createMutation.isPending}
          onChange={(event) => setNewColor(event.target.value.toUpperCase())}
          type="color"
          value={newColor}
        />
        <button
          aria-label="创建并选中标签"
          className="button button-secondary task-tag-add"
          disabled={
            disabled ||
            createMutation.isPending ||
            newName.trim().length === 0 ||
            selectedIds.length >= 20
          }
          onClick={create}
          type="button"
        >
          <Plus size={13} />
          添加
        </button>
      </div>
      {selectedIds.length >= 20 ? (
        <span className="task-tag-hint">每项任务最多可选择 20 个标签。</span>
      ) : null}
      {error ? (
        <span className="form-field-error" role="alert">
          {error}
        </span>
      ) : null}
    </div>
  );
}
