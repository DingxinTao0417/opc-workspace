import {
  ChevronDown,
  ChevronRight,
  Columns3,
  List,
  Plus,
  RotateCcw,
  Search,
  Settings2,
  SlidersHorizontal,
  X,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { ApiError } from "../api/client";
import {
  useBatchUpdateTasks,
  useMoveTaskWithinPlan,
  useProjectOptionsQuery,
  useReorderTaskWithinPlanStatus,
  useResetTaskOrder,
  useTagOptionsQuery,
  useTaskPageQuery,
} from "../api/hooks";
import { EmptyState, ErrorState, SkeletonRows } from "../components/feedback";
import { PageHeader } from "../components/PageHeader";
import { TagManagerModal } from "../components/TagManagerModal";
import { TaskList } from "../components/TaskList";
import { useUiStore } from "../store/ui";
import type {
  BatchUpdateTasksInput,
  Task,
  TaskKind,
  TaskPriority,
  TaskStatus,
} from "../types/models";

const groups: { status: TaskStatus; label: string }[] = [
  { status: "todo", label: "待办" },
  { status: "in_progress", label: "进行中" },
  { status: "blocked", label: "阻塞" },
  { status: "waiting_review", label: "待验收" },
  { status: "done", label: "已完成" },
  { status: "cancelled", label: "已取消" },
];

type BatchAction = BatchUpdateTasksInput["action"];

function applyTaskOrder(tasks: Task[], orderedIds?: string[]): Task[] {
  if (!orderedIds || orderedIds.length !== tasks.length) return tasks;
  const byId = new Map(tasks.map((task) => [task.id, task]));
  if (orderedIds.some((id) => !byId.has(id))) return tasks;
  return orderedIds.map((id) => byId.get(id)!);
}

function previewTaskDrop(
  tasks: Task[],
  source: Task,
  target: Task,
): { orderedIds: string[]; position: "before" | "after" } | null {
  if (source.status !== target.status) return null;
  const ids = tasks.map((task) => task.id);
  const sourceIndex = ids.indexOf(source.id);
  const targetIndex = ids.indexOf(target.id);
  if (sourceIndex < 0 || targetIndex < 0 || sourceIndex === targetIndex)
    return null;
  const position = sourceIndex < targetIndex ? "after" : "before";
  ids.splice(sourceIndex, 1);
  const nextTargetIndex = ids.indexOf(target.id);
  ids.splice(nextTargetIndex + (position === "after" ? 1 : 0), 0, source.id);
  return { orderedIds: ids, position };
}

function apiErrorText(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) {
    if (
      error.code === "VERSION_CONFLICT" ||
      error.code === "TASK_BATCH_SET_CHANGED"
    ) {
      return "所选任务已发生变化，已刷新列表；请重新选择后再试。";
    }
    if (error.code === "TASK_REORDER_SET_CHANGED") {
      return "当前计划组已发生变化，已刷新列表；请重试。";
    }
    return error.requestId
      ? `${error.message} · 请求 ${error.requestId}`
      : error.message;
  }
  return "任务操作失败，请重试。";
}

export function TasksPage() {
  const setNewTaskOpen = useUiStore((state) => state.setNewTaskOpen);
  const [searchInput, setSearchInput] = useState("");
  const [queryText, setQueryText] = useState("");
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [tagManagerOpen, setTagManagerOpen] = useState(false);
  const [status, setStatus] = useState<TaskStatus | "">("");
  const [priority, setPriority] = useState<TaskPriority | "">("");
  const [kind, setKind] = useState<TaskKind | "">("");
  const [projectId, setProjectId] = useState("");
  const [tagIds, setTagIds] = useState<string[]>([]);
  const [plannedDate, setPlannedDate] = useState("");
  const [sort, setSort] = useState("");
  const [collapsedStatuses, setCollapsedStatuses] = useState<Set<TaskStatus>>(
    () => new Set(["cancelled"]),
  );
  const [page, setPage] = useState(1);
  const [selectedTasks, setSelectedTasks] = useState<Record<string, Task>>({});
  const [batchAction, setBatchAction] = useState<BatchAction>("set_project");
  const [batchProjectId, setBatchProjectId] = useState("");
  const [batchPlannedDate, setBatchPlannedDate] = useState("");
  const [batchTagId, setBatchTagId] = useState("");
  const [dragPreview, setDragPreview] = useState<
    Partial<Record<TaskStatus, string[]>>
  >({});

  useEffect(() => {
    const timer = window.setTimeout(() => {
      setQueryText(searchInput.trim());
      setPage(1);
    }, 280);
    return () => window.clearTimeout(timer);
  }, [searchInput]);

  const tagKey = tagIds.join(",");
  const hasFilters = Boolean(
    queryText ||
    status ||
    priority ||
    kind ||
    projectId ||
    tagIds.length ||
    plannedDate,
  );
  const hierarchical = !hasFilters;
  const query = useTaskPageQuery({
    page,
    pageSize: 50,
    q: queryText || undefined,
    status: status || undefined,
    priority: priority || undefined,
    kind: kind || undefined,
    projectId: projectId || undefined,
    tagIds,
    plannedDate: plannedDate || undefined,
    rootOnly: hierarchical,
    sort: sort || undefined,
  });
  const projectsQuery = useProjectOptionsQuery(true);
  const tagsQuery = useTagOptionsQuery(true);
  const batchMutation = useBatchUpdateTasks();
  const moveMutation = useMoveTaskWithinPlan();
  const dragMutation = useReorderTaskWithinPlanStatus();
  const resetOrderMutation = useResetTaskOrder();
  const tasks = query.data?.items ?? [];
  const total = query.data?.meta.total ?? 0;
  const totalPages = Math.max(
    1,
    Math.ceil(total / (query.data?.meta.pageSize ?? 50)),
  );
  const selectedItems = useMemo(
    () => Object.values(selectedTasks),
    [selectedTasks],
  );
  const selectedIds = useMemo(
    () => new Set(selectedItems.map((task) => task.id)),
    [selectedItems],
  );
  const activeFilterCount =
    Number(Boolean(status)) +
    Number(Boolean(priority)) +
    Number(Boolean(kind)) +
    Number(Boolean(projectId)) +
    tagIds.length +
    Number(Boolean(plannedDate));
  const onlyPlanFilter =
    Boolean(plannedDate) &&
    !(
      searchInput.trim() ||
      queryText ||
      status ||
      priority ||
      kind ||
      projectId ||
      tagIds.length
    );
  const allowReorder = sort === "manual_order" && onlyPlanFilter;
  const writeReady =
    query.isSuccess &&
    !query.isPlaceholderData &&
    !query.isFetching &&
    !batchMutation.isPending &&
    !moveMutation.isPending &&
    !dragMutation.isPending &&
    !resetOrderMutation.isPending;
  const batchError = apiErrorText(batchMutation.error);
  const reorderError =
    apiErrorText(dragMutation.error) ??
    apiErrorText(moveMutation.error) ??
    apiErrorText(resetOrderMutation.error);
  const reorderPending =
    dragMutation.isPending ||
    moveMutation.isPending ||
    resetOrderMutation.isPending;

  useEffect(() => {
    setSelectedTasks({});
    setDragPreview({});
    batchMutation.reset();
    dragMutation.reset();
  }, [
    page,
    queryText,
    status,
    priority,
    kind,
    projectId,
    tagKey,
    plannedDate,
    sort,
  ]);

  useEffect(() => {
    if (
      query.isSuccess &&
      page > 1 &&
      query.data.items.length === 0 &&
      query.data.meta.total > 0
    ) {
      setPage((value) => Math.max(1, value - 1));
    }
  }, [page, query.data, query.isSuccess]);

  const clearFilters = () => {
    setStatus("");
    setPriority("");
    setKind("");
    setProjectId("");
    setTagIds([]);
    setPlannedDate("");
    setSearchInput("");
    setQueryText("");
    setPage(1);
  };

  const toggleTag = (id: string) => {
    setTagIds((current) =>
      current.includes(id)
        ? current.filter((item) => item !== id)
        : [...current, id],
    );
    setPage(1);
  };

  const applyBatch = () => {
    const items = selectedItems.map((task) => ({
      id: task.id,
      expectedVersion: task.version,
    }));
    if (items.length === 0) return;
    let input: BatchUpdateTasksInput;
    if (batchAction === "set_project") {
      input = {
        action: "set_project",
        items,
        projectId: batchProjectId || null,
      };
    } else if (batchAction === "set_planned_date") {
      input = {
        action: "set_planned_date",
        items,
        plannedDate: batchPlannedDate || null,
      };
    } else {
      if (!batchTagId) return;
      input = { action: batchAction, items, tagIds: [batchTagId] };
    }
    batchMutation.mutate(input, {
      onError: (error) => {
        if (
          error instanceof ApiError &&
          (error.code === "VERSION_CONFLICT" ||
            error.code === "TASK_BATCH_SET_CHANGED")
        ) {
          setSelectedTasks({});
          void query.refetch();
        }
      },
      onSuccess: () => setSelectedTasks({}),
    });
  };

  const allPageSelected =
    tasks.length > 0 && tasks.every((task) => selectedIds.has(task.id));

  const dropTask = (status: TaskStatus, source: Task, target: Task) => {
    const groupedTasks = applyTaskOrder(
      tasks.filter((task) => task.status === status),
      dragPreview[status],
    );
    const preview = previewTaskDrop(groupedTasks, source, target);
    if (!preview) return;
    moveMutation.reset();
    resetOrderMutation.reset();
    dragMutation.reset();
    setDragPreview((current) => ({
      ...current,
      [status]: preview.orderedIds,
    }));
    dragMutation.mutate(
      { source, target, position: preview.position },
      {
        onSettled: () =>
          setDragPreview((current) => {
            const next = { ...current };
            delete next[status];
            return next;
          }),
      },
    );
  };

  return (
    <div className="page">
      <PageHeader
        actions={
          <button
            className="button button-primary"
            onClick={() => setNewTaskOpen(true)}
            type="button"
          >
            <Plus size={15} />
            新建任务
          </button>
        }
        meta={
          <span className="page-count">
            {query.isPending
              ? "读取中"
              : query.isSuccess
                ? `${total} ${hierarchical ? "个根任务" : "项"}${query.isFetching ? " · 更新中" : ""}`
                : "数据不可用"}
          </span>
        }
        title="任务"
      />

      <div className="toolbar task-toolbar">
        <label className="toolbar-search">
          <Search size={15} />
          <input
            aria-label="搜索任务标题或描述"
            onChange={(event) => setSearchInput(event.target.value)}
            placeholder="搜索标题或描述…"
            value={searchInput}
          />
        </label>
        <button
          aria-expanded={filtersOpen}
          className={
            filtersOpen || activeFilterCount > 0
              ? "button button-secondary task-filter-active"
              : "button button-secondary"
          }
          onClick={() => setFiltersOpen((value) => !value)}
          type="button"
        >
          <SlidersHorizontal size={14} />
          筛选{activeFilterCount > 0 ? ` · ${activeFilterCount}` : ""}
        </button>
        <label className="toolbar-select">
          <span className="sr-only">任务排序</span>
          <select
            aria-label="任务排序"
            onChange={(event) => {
              setSort(event.target.value);
              setPage(1);
            }}
            value={sort}
          >
            <option value="">智能排序</option>
            <option value="manual_order">手动顺序</option>
            <option value="priority">优先级</option>
            <option value="due_date">截止时间</option>
            <option value="planned_date">计划日期</option>
            <option value="-updated_at">最近更新</option>
            <option value="created_at">创建时间</option>
            <option value="title">标题</option>
          </select>
        </label>
        <div aria-label="视图" className="segmented">
          <button
            aria-pressed="true"
            className="segmented-active"
            type="button"
          >
            <List size={15} />
          </button>
          <button
            aria-label="看板视图将在后续版本提供"
            disabled
            title="后续版本"
            type="button"
          >
            <Columns3 size={15} />
          </button>
        </div>
      </div>

      {filtersOpen ? (
        <section aria-label="任务筛选条件" className="task-filter-panel">
          <label>
            <span>状态</span>
            <select
              onChange={(event) => {
                setStatus(event.target.value as TaskStatus | "");
                setPage(1);
              }}
              value={status}
            >
              <option value="">全部</option>
              <option value="in_progress">进行中</option>
              <option value="todo">待办</option>
              <option value="blocked">阻塞</option>
              <option value="waiting_review">待验收</option>
              <option value="done">已完成</option>
              <option value="cancelled">已取消</option>
            </select>
          </label>
          <label>
            <span>优先级</span>
            <select
              onChange={(event) => {
                setPriority(event.target.value as TaskPriority | "");
                setPage(1);
              }}
              value={priority}
            >
              <option value="">全部</option>
              <option value="P0">P0 紧急</option>
              <option value="P1">P1 高</option>
              <option value="P2">P2 中</option>
              <option value="P3">P3 低</option>
            </select>
          </label>
          <label>
            <span>类型</span>
            <select
              onChange={(event) => {
                setKind(event.target.value as TaskKind | "");
                setPage(1);
              }}
              value={kind}
            >
              <option value="">全部</option>
              <option value="work">工作</option>
              <option value="review">复核</option>
              <option value="followup">跟进</option>
              <option value="reminder">提醒</option>
            </select>
          </label>
          <label>
            <span>项目</span>
            <select
              disabled={projectsQuery.isPending || projectsQuery.isError}
              onChange={(event) => {
                setProjectId(event.target.value);
                setPage(1);
              }}
              value={projectId}
            >
              <option value="">全部项目</option>
              {(projectsQuery.data ?? []).map((project) => (
                <option key={project.id} value={project.id}>
                  {project.name}
                </option>
              ))}
            </select>
          </label>
          <label>
            <span>计划日期</span>
            <input
              onChange={(event) => {
                setPlannedDate(event.target.value);
                setPage(1);
              }}
              type="date"
              value={plannedDate}
            />
          </label>
          <div className="task-filter-tags">
            <div>
              <span>标签（同时包含）</span>
              <button
                className="form-inline-action"
                onClick={() => setTagManagerOpen(true)}
                type="button"
              >
                <Settings2 size={11} />
                管理
              </button>
            </div>
            {tagsQuery.isPending ? <span>读取中…</span> : null}
            {tagsQuery.isError ? (
              <button
                className="form-inline-action"
                onClick={() => void tagsQuery.refetch()}
                type="button"
              >
                标签读取失败，重试
              </button>
            ) : null}
            <div className="task-filter-tag-options">
              {(tagsQuery.data ?? []).map((tag) => (
                <button
                  aria-pressed={tagIds.includes(tag.id)}
                  className={
                    tagIds.includes(tag.id)
                      ? "task-tag-option task-tag-option-active"
                      : "task-tag-option"
                  }
                  key={tag.id}
                  onClick={() => toggleTag(tag.id)}
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
          </div>
          <button
            className="button button-quiet task-filter-clear"
            disabled={!hasFilters}
            onClick={clearFilters}
            type="button"
          >
            <X size={13} />
            清除条件
          </button>
        </section>
      ) : null}

      {selectedItems.length > 0 ? (
        <section aria-label="批量操作" className="task-batch-bar">
          <label className="task-select-page">
            <input
              checked={allPageSelected}
              disabled={!writeReady || batchMutation.isPending}
              onChange={(event) => {
                if (event.target.checked) {
                  setSelectedTasks((current) => {
                    const next = { ...current };
                    for (const task of tasks) next[task.id] = task;
                    return next;
                  });
                } else {
                  setSelectedTasks({});
                }
              }}
              type="checkbox"
            />
            已选 {selectedItems.length} 项
          </label>
          {selectedItems.length >= 100 ? (
            <span>每次批量操作最多选择 100 项。</span>
          ) : null}
          <select
            aria-label="批量操作类型"
            onChange={(event) => {
              setBatchAction(event.target.value as BatchAction);
              batchMutation.reset();
            }}
            value={batchAction}
          >
            <option value="set_project">移动到项目</option>
            <option value="set_planned_date">设置计划日期</option>
            <option value="add_tags">添加标签</option>
            <option value="remove_tags">移除标签</option>
          </select>
          {batchAction === "set_project" ? (
            <select
              aria-label="批量目标项目"
              onChange={(event) => setBatchProjectId(event.target.value)}
              value={batchProjectId}
            >
              <option value="">未归项目</option>
              {(projectsQuery.data ?? []).map((project) => (
                <option key={project.id} value={project.id}>
                  {project.name}
                </option>
              ))}
            </select>
          ) : null}
          {batchAction === "set_planned_date" ? (
            <input
              aria-label="批量计划日期，留空表示清除"
              onChange={(event) => setBatchPlannedDate(event.target.value)}
              title="留空表示清除计划日期"
              type="date"
              value={batchPlannedDate}
            />
          ) : null}
          {batchAction === "add_tags" || batchAction === "remove_tags" ? (
            <select
              aria-label="批量目标标签"
              onChange={(event) => setBatchTagId(event.target.value)}
              value={batchTagId}
            >
              <option value="">选择标签…</option>
              {(tagsQuery.data ?? []).map((tag) => (
                <option key={tag.id} value={tag.id}>
                  {tag.name}
                </option>
              ))}
            </select>
          ) : null}
          <button
            className="button button-primary"
            disabled={
              !writeReady ||
              batchMutation.isPending ||
              ((batchAction === "add_tags" || batchAction === "remove_tags") &&
                !batchTagId)
            }
            onClick={applyBatch}
            type="button"
          >
            {batchMutation.isPending ? "应用中…" : "应用"}
          </button>
          <button
            className="button button-quiet"
            disabled={batchMutation.isPending}
            onClick={() => setSelectedTasks({})}
            type="button"
          >
            取消选择
          </button>
          {batchError ? (
            <span className="task-batch-error" role="alert">
              {batchError}
            </span>
          ) : null}
        </section>
      ) : null}

      {sort === "manual_order" ? (
        <div className="task-order-banner">
          {allowReorder ? (
            <>
              <span>当前可按“{plannedDate}”计划组持久调整顺序。</span>
              <button
                className="button button-quiet"
                disabled={!writeReady || resetOrderMutation.isPending}
                onClick={() => {
                  setDragPreview({});
                  dragMutation.reset();
                  moveMutation.reset();
                  resetOrderMutation.mutate(plannedDate);
                }}
                type="button"
              >
                <RotateCcw size={12} />
                恢复默认顺序
              </button>
            </>
          ) : (
            <span>选择一个精确计划日期，并清除其他筛选后即可调整顺序。</span>
          )}
          {reorderError ? (
            <span className="task-batch-error" role="alert">
              {reorderError}
            </span>
          ) : null}
          {reorderPending ? <span role="status">正在保存任务顺序…</span> : null}
        </div>
      ) : null}

      {query.isError ? (
        <ErrorState
          message="无法连接任务 API；请确认本地服务已启动后重试。"
          onRetry={() => void query.refetch()}
        />
      ) : null}
      {query.isPending ? <SkeletonRows count={7} /> : null}

      {query.isSuccess && tasks.length === 0 ? (
        <EmptyState
          action={
            hasFilters ? (
              <button
                className="button button-secondary"
                onClick={clearFilters}
                type="button"
              >
                清除筛选
              </button>
            ) : (
              <button
                className="button button-primary"
                onClick={() => setNewTaskOpen(true)}
                type="button"
              >
                <Plus size={15} />
                新建第一项任务
              </button>
            )
          }
          message={
            hasFilters
              ? "调整关键词或筛选条件后再试。"
              : "本地数据库中还没有任务。"
          }
          title={hasFilters ? "没有匹配的任务" : "任务列表是空的"}
        />
      ) : null}

      {tasks.length > 0 ? (
        <div className="task-groups">
          {groups.map((group) => {
            const groupedTasks = applyTaskOrder(
              tasks.filter((task) => task.status === group.status),
              dragPreview[group.status],
            );
            if (!groupedTasks.length) return null;
            const collapsed = collapsedStatuses.has(group.status);
            return (
              <section className="task-group" key={group.status}>
                <div className="task-group-heading">
                  {group.status === "cancelled" ? (
                    <button
                      aria-expanded={!collapsed}
                      className="task-group-toggle"
                      onClick={() =>
                        setCollapsedStatuses((current) => {
                          const next = new Set(current);
                          if (next.has(group.status)) next.delete(group.status);
                          else next.add(group.status);
                          return next;
                        })
                      }
                      type="button"
                    >
                      {collapsed ? (
                        <ChevronRight size={13} />
                      ) : (
                        <ChevronDown size={13} />
                      )}
                      <h2>{group.label}</h2>
                    </button>
                  ) : (
                    <h2>{group.label}</h2>
                  )}
                  <span title="当前页数量">本页 {groupedTasks.length}</span>
                </div>
                {!collapsed ? (
                  <TaskList
                    allowDrag={allowReorder}
                    allowReorder={allowReorder}
                    dragPending={dragMutation.isPending}
                    hierarchical={hierarchical}
                    live={writeReady}
                    onMove={(task, direction) => {
                      setDragPreview({});
                      dragMutation.reset();
                      moveMutation.mutate({
                        taskId: task.id,
                        plannedDate: task.plannedDate,
                        direction,
                      });
                    }}
                    onDropTask={(source, target) =>
                      dropTask(group.status, source, target)
                    }
                    onSelectionChange={(task, selected) =>
                      setSelectedTasks((current) => {
                        const next = { ...current };
                        if (selected) {
                          if (
                            !next[task.id] &&
                            Object.keys(next).length >= 100
                          ) {
                            return current;
                          }
                          next[task.id] = task;
                        } else delete next[task.id];
                        return next;
                      })
                    }
                    reorderPendingId={
                      moveMutation.isPending
                        ? (moveMutation.variables?.taskId ?? null)
                        : null
                    }
                    selectedIds={selectedIds}
                    selectionLimitReached={selectedItems.length >= 100}
                    showParent={!hierarchical}
                    tasks={groupedTasks}
                  />
                ) : null}
              </section>
            );
          })}
        </div>
      ) : null}

      {query.isSuccess && totalPages > 1 ? (
        <nav aria-label="任务分页" className="pagination task-pagination">
          <button
            className="button button-secondary"
            disabled={page <= 1 || query.isFetching}
            onClick={() => setPage((value) => Math.max(1, value - 1))}
            type="button"
          >
            上一页
          </button>
          <span>
            {(page - 1) * query.data.meta.pageSize + 1}–
            {Math.min(page * query.data.meta.pageSize, total)} / {total}
          </span>
          <button
            className="button button-secondary"
            disabled={page >= totalPages || query.isFetching}
            onClick={() => setPage((value) => value + 1)}
            type="button"
          >
            下一页
          </button>
        </nav>
      ) : null}

      <TagManagerModal
        onClose={() => setTagManagerOpen(false)}
        open={tagManagerOpen}
      />
    </div>
  );
}
