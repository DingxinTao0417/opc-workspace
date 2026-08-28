import { Check, Save, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { ApiError } from "../api/client";
import {
  useCreateTaskSavedView,
  useDeleteTaskSavedView,
  useTaskSavedViewsQuery,
  useUpdateTaskSavedView,
} from "../api/hooks";
import type { TaskSavedViewDefinition } from "../types/models";

interface TaskSavedViewsControlProps {
  definition: TaskSavedViewDefinition;
  onApply: (definition: TaskSavedViewDefinition) => void;
}

function mutationErrorText(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) {
    if (error.code === "TASK_SAVED_VIEW_NAME_EXISTS") {
      return "已有同名视图，请换一个名称。";
    }
    if (error.code === "TASK_SAVED_VIEW_LIMIT_REACHED") {
      return "最多保存 20 个任务视图，请先删除不再使用的视图。";
    }
    if (error.code === "VERSION_CONFLICT") {
      return "这个视图已在其他位置更新，列表已刷新，请重新操作。";
    }
    return error.requestId
      ? `${error.message} · 请求 ${error.requestId}`
      : error.message;
  }
  return "保存视图操作失败，请重试。";
}

export function TaskSavedViewsControl({
  definition,
  onApply,
}: TaskSavedViewsControlProps) {
  const query = useTaskSavedViewsQuery();
  const createMutation = useCreateTaskSavedView();
  const updateMutation = useUpdateTaskSavedView();
  const deleteMutation = useDeleteTaskSavedView();
  const [selectedId, setSelectedId] = useState("");
  const [name, setName] = useState("");
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const views = query.data ?? [];
  const selected = views.find((view) => view.id === selectedId) ?? null;
  const pending =
    createMutation.isPending ||
    updateMutation.isPending ||
    deleteMutation.isPending;
  const error =
    mutationErrorText(createMutation.error) ??
    mutationErrorText(updateMutation.error) ??
    mutationErrorText(deleteMutation.error);

  useEffect(() => {
    if (selectedId && query.isSuccess && !selected) {
      setSelectedId("");
      setConfirmingDelete(false);
    }
  }, [query.isSuccess, selected, selectedId]);

  const resetMutations = () => {
    createMutation.reset();
    updateMutation.reset();
    deleteMutation.reset();
  };

  return (
    <div className="task-saved-views">
      <label>
        <span>已保存视图</span>
        <select
          aria-label="已保存视图"
          disabled={query.isPending || query.isError || pending}
          onChange={(event) => {
            const id = event.target.value;
            setSelectedId(id);
            setConfirmingDelete(false);
            resetMutations();
            const view = views.find((item) => item.id === id);
            if (view) onApply(view.definition);
          }}
          value={selectedId}
        >
          <option value="">
            {query.isPending
              ? "读取中…"
              : query.isError
                ? "读取失败"
                : views.length === 0
                  ? "暂无保存视图"
                  : "选择视图"}
          </option>
          {views.map((view) => (
            <option key={view.id} value={view.id}>
              {view.name}
            </option>
          ))}
        </select>
      </label>

      <label>
        <span>新视图名称</span>
        <input
          aria-label="新视图名称"
          disabled={pending || views.length >= 20}
          maxLength={80}
          onChange={(event) => {
            setName(event.target.value);
            resetMutations();
          }}
          placeholder="例如：本周高优任务"
          value={name}
        />
      </label>

      <div className="task-saved-view-actions">
        <button
          className="button button-secondary"
          disabled={pending || !name.trim() || views.length >= 20}
          onClick={() =>
            createMutation.mutate(
              { name: name.trim(), definition },
              {
                onSuccess: (view) => {
                  setSelectedId(view.id);
                  setName("");
                },
              },
            )
          }
          type="button"
        >
          <Save size={13} />
          保存当前
        </button>
        <button
          className="button button-secondary"
          disabled={pending || !selected}
          onClick={() => {
            if (!selected) return;
            resetMutations();
            updateMutation.mutate({
              id: selected.id,
              input: {
                expectedVersion: selected.version,
                definition,
              },
            });
          }}
          type="button"
        >
          <Check size={13} />
          更新所选
        </button>
        <button
          className="button button-quiet button-danger"
          disabled={pending || !selected}
          onClick={() => setConfirmingDelete(true)}
          type="button"
        >
          <Trash2 size={13} />
          删除
        </button>
      </div>

      {query.isError ? (
        <button
          className="form-inline-action task-saved-view-feedback"
          onClick={() => void query.refetch()}
          type="button"
        >
          保存视图读取失败，重试
        </button>
      ) : null}
      {error ? (
        <p className="form-error task-saved-view-feedback" role="alert">
          {error}
        </p>
      ) : null}
      {views.length >= 20 ? (
        <p className="field-hint task-saved-view-feedback">
          已达到 20 个视图上限，请先删除不再使用的视图。
        </p>
      ) : null}
      {confirmingDelete && selected ? (
        <div className="task-saved-view-confirm" role="alert">
          <span>确认永久删除“{selected.name}”？</span>
          <button
            className="button button-quiet"
            disabled={pending}
            onClick={() => setConfirmingDelete(false)}
            type="button"
          >
            取消
          </button>
          <button
            className="button button-danger"
            disabled={pending}
            onClick={() => {
              deleteMutation.mutate(
                { id: selected.id, expectedVersion: selected.version },
                {
                  onSuccess: () => {
                    setSelectedId("");
                    setConfirmingDelete(false);
                  },
                },
              );
            }}
            type="button"
          >
            确认删除
          </button>
        </div>
      ) : null}
    </div>
  );
}
