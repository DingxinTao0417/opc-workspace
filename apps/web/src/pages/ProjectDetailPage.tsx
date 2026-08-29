import {
  AlertTriangle,
  ArrowLeft,
  CalendarDays,
  Clock3,
  Edit3,
  List,
  ListTree,
  Plus,
  Search,
  Trash2,
} from "lucide-react";
import { useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { ApiError } from "../api/client";
import {
  useDeleteProject,
  useProjectQuery,
  useTagOptionsQuery,
  useTaskPageQuery,
  useTransitionProject,
} from "../api/hooks";
import { EmptyState, ErrorState, SkeletonRows } from "../components/feedback";
import { PageHeader } from "../components/PageHeader";
import { ProjectFormModal } from "../components/ProjectFormModal";
import { ProjectEventsSection } from "../components/ProjectEventsSection";
import { ProjectArtifactsSection } from "../components/ProjectArtifactsSection";
import { ProjectAttachmentsSection } from "../components/ProjectAttachmentsSection";
import { ProjectNotesSection } from "../components/ProjectNotesSection";
import { TaskList } from "../components/TaskList";
import { useUiStore } from "../store/ui";
import type {
  ProjectStatus,
  ProjectTransitionAction,
  TaskKind,
  TaskPriority,
  TaskStatus,
} from "../types/models";

const statusLabels: Record<ProjectStatus, string> = {
  planning: "规划中",
  in_progress: "进行中",
  paused: "已暂停",
  completed: "已完成",
  archived: "已归档",
};

const actionLabels: Record<ProjectTransitionAction, string> = {
  start: "开始项目",
  pause: "暂停项目",
  resume: "继续项目",
  complete: "完成项目",
  reopen: "重新打开",
  archive: "归档项目",
  restore: "恢复项目",
};

function formatAmount(value: number | null): string {
  if (value === null) return "未设置";
  return new Intl.NumberFormat("zh-CN", {
    style: "currency",
    currency: "CNY",
  }).format(value / 100);
}

function formatMinutes(value: number): string {
  if (value <= 0) return "0 分钟";
  const hours = Math.floor(value / 60);
  const minutes = value % 60;
  return hours ? `${hours} 小时 ${minutes} 分钟` : `${minutes} 分钟`;
}

function errorMessage(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) {
    if (error.code === "VERSION_CONFLICT") {
      return "项目已在其他窗口修改，页面正在刷新，请重新确认操作。";
    }
    if (error.code === "INCOMPLETE_TASKS_CONFIRMATION_REQUIRED") {
      return "项目仍有未完成任务，需要确认后才能完成项目。";
    }
    return error.message;
  }
  return "项目操作失败，请重试。";
}

export function ProjectDetailPage() {
  const projectId = useParams().projectId ?? "";
  const navigate = useNavigate();
  const projectQuery = useProjectQuery(projectId || null);
  const transitionMutation = useTransitionProject();
  const deleteMutation = useDeleteProject();
  const openNewTaskForProject = useUiStore(
    (state) => state.openNewTaskForProject,
  );
  const [editing, setEditing] = useState(false);
  const [confirmAction, setConfirmAction] =
    useState<ProjectTransitionAction | null>(null);
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const [taskView, setTaskView] = useState<"tree" | "flat">("tree");
  const [taskPage, setTaskPage] = useState(1);
  const [taskSearchInput, setTaskSearchInput] = useState("");
  const [taskQueryText, setTaskQueryText] = useState("");
  const [taskStatus, setTaskStatus] = useState<TaskStatus | "">("");
  const [taskPriority, setTaskPriority] = useState<TaskPriority | "">("");
  const [taskKind, setTaskKind] = useState<TaskKind | "">("");
  const [taskTagId, setTaskTagId] = useState("");
  const [taskPlannedState, setTaskPlannedState] = useState<
    "scheduled" | "unscheduled" | ""
  >("");
  const hasTaskFilters = Boolean(
    taskQueryText ||
    taskStatus ||
    taskPriority ||
    taskKind ||
    taskTagId ||
    taskPlannedState,
  );
  const hierarchicalTasks = taskView === "tree" && !hasTaskFilters;
  const tasksQuery = useTaskPageQuery({
    page: taskPage,
    pageSize: 20,
    projectId,
    q: taskQueryText || undefined,
    rootOnly: hierarchicalTasks,
    sort: hierarchicalTasks ? "manual_order" : undefined,
    status: taskStatus || undefined,
    priority: taskPriority || undefined,
    kind: taskKind || undefined,
    tagIds: taskTagId ? [taskTagId] : undefined,
    plannedState: taskPlannedState || undefined,
  });
  const taskTagsQuery = useTagOptionsQuery(true);
  const project = projectQuery.data;
  const projectTasks = tasksQuery.data?.items ?? [];
  const projectTaskResultTotal = tasksQuery.data?.meta.total ?? 0;
  const projectTaskPages = Math.max(
    1,
    Math.ceil(projectTaskResultTotal / (tasksQuery.data?.meta.pageSize ?? 20)),
  );
  const busy = transitionMutation.isPending || deleteMutation.isPending;
  const operationError =
    errorMessage(transitionMutation.error) ??
    errorMessage(deleteMutation.error);

  useEffect(() => {
    const timer = window.setTimeout(() => {
      setTaskQueryText(taskSearchInput.trim());
      setTaskPage(1);
    }, 280);
    return () => window.clearTimeout(timer);
  }, [taskSearchInput]);

  const runTransition = (
    action: ProjectTransitionAction,
    confirmed = false,
  ) => {
    if (!project) return;
    const needsConfirmation =
      action === "archive" ||
      (action === "complete" && project.taskSummary.remaining > 0);
    if (needsConfirmation && !confirmed) {
      setConfirmAction(action);
      return;
    }
    transitionMutation.mutate(
      {
        id: project.id,
        action,
        expectedVersion: project.version,
        confirmIncompleteTasks: action === "complete" && confirmed,
      },
      {
        onError: (error) => {
          if (error instanceof ApiError) {
            if (error.code === "VERSION_CONFLICT") {
              void projectQuery.refetch();
            }
            if (
              action === "complete" &&
              error.code === "INCOMPLETE_TASKS_CONFIRMATION_REQUIRED"
            ) {
              setConfirmAction("complete");
              void projectQuery.refetch();
            }
          }
        },
        onSuccess: () => setConfirmAction(null),
      },
    );
  };

  const permanentlyDelete = () => {
    if (!project) return;
    deleteMutation.mutate(
      { id: project.id, expectedVersion: project.version },
      {
        onError: (error) => {
          if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
            void projectQuery.refetch();
          }
        },
        onSuccess: () => navigate("/projects", { replace: true }),
      },
    );
  };

  if (projectQuery.isPending) {
    return (
      <div className="page">
        <SkeletonRows count={8} />
      </div>
    );
  }

  if (projectQuery.isError || !project) {
    return (
      <div className="page">
        <Link className="project-back-link" to="/projects">
          <ArrowLeft size={14} />
          返回项目
        </Link>
        <ErrorState
          message="无法读取该项目，项目可能已删除或本地服务暂不可用。"
          onRetry={() => void projectQuery.refetch()}
          title="项目详情不可用"
        />
      </div>
    );
  }

  return (
    <div className="page project-detail-page">
      <PageHeader
        actions={
          <>
            <button
              className="button button-secondary"
              disabled={busy || project.status === "archived"}
              onClick={() => setEditing(true)}
              title={
                project.status === "archived"
                  ? "恢复项目后再编辑资料"
                  : undefined
              }
              type="button"
            >
              <Edit3 size={14} />
              编辑资料
            </button>
            {project.status !== "archived" ? (
              <button
                className="button button-primary"
                disabled={busy}
                onClick={() => openNewTaskForProject(project.id)}
                type="button"
              >
                <Plus size={14} />
                新建任务
              </button>
            ) : null}
          </>
        }
        eyebrow={
          <Link className="project-back-link" to="/projects">
            <ArrowLeft size={13} />
            全部项目
          </Link>
        }
        meta={
          <span className="page-count">{statusLabels[project.status]}</span>
        }
        title={project.name}
      />

      <section className="project-detail-hero">
        <div>
          <span
            className="project-detail-color"
            style={{ backgroundColor: project.color ?? "var(--accent)" }}
          />
          <p>{project.description || "尚未填写项目说明。"}</p>
        </div>
        <div className="project-detail-progress">
          <strong>{project.taskSummary.progressPercent}%</strong>
          <span>任务完成进度</span>
          <div className="project-progress">
            <span
              style={{ width: `${project.taskSummary.progressPercent}%` }}
            />
          </div>
        </div>
      </section>

      <div className="project-detail-stats">
        <article>
          <span>任务</span>
          <strong>
            {project.taskSummary.completed}/{project.taskSummary.total}
          </strong>
          <small>{project.taskSummary.remaining} 项未完成</small>
        </article>
        <article>
          <span>已记录工时</span>
          <strong>{formatMinutes(project.taskSummary.actualMinutes)}</strong>
          <small>来自关联任务的当前累计</small>
        </article>
        <article>
          <span>周期</span>
          <strong>
            {project.startDate ?? "未定"} → {project.dueDate ?? "未定"}
          </strong>
          <small>
            <CalendarDays size={12} /> 纯日期按本地时区解释
          </small>
        </article>
        <article>
          <span>合同金额</span>
          <strong>{formatAmount(project.amountMinor)}</strong>
          <small>{project.clientName ?? "尚未关联客户"}</small>
        </article>
      </div>

      <section className="project-detail-section">
        <div className="project-detail-heading">
          <div>
            <h2>项目任务</h2>
            <p>进度只从这里的真实任务状态派生，不单独保存百分比。</p>
          </div>
          <div className="project-task-heading-actions">
            <span>
              {hierarchicalTasks
                ? `${project.taskSummary.total} 项 · ${projectTaskResultTotal} 个根任务`
                : hasTaskFilters
                  ? `${projectTaskResultTotal} / ${project.taskSummary.total} 项`
                  : `${projectTaskResultTotal} 项`}
            </span>
            <div aria-label="项目任务视图" className="segmented" role="group">
              <button
                aria-label="任务树视图"
                aria-pressed={hierarchicalTasks}
                className={hierarchicalTasks ? "segmented-active" : ""}
                disabled={hasTaskFilters}
                onClick={() => {
                  setTaskView("tree");
                  setTaskPage(1);
                }}
                title={
                  hasTaskFilters
                    ? "清除搜索和状态条件后使用任务树"
                    : "任务树视图"
                }
                type="button"
              >
                <ListTree size={14} />
              </button>
              <button
                aria-label="平铺列表视图"
                aria-pressed={!hierarchicalTasks}
                className={!hierarchicalTasks ? "segmented-active" : ""}
                onClick={() => {
                  setTaskView("flat");
                  setTaskPage(1);
                }}
                title="平铺列表视图"
                type="button"
              >
                <List size={14} />
              </button>
            </div>
          </div>
        </div>
        <div className="project-task-toolbar">
          <label className="toolbar-search">
            <Search size={14} />
            <input
              aria-label="搜索项目任务标题或描述"
              onChange={(event) => setTaskSearchInput(event.target.value)}
              placeholder="搜索项目任务…"
              value={taskSearchInput}
            />
          </label>
          <label className="toolbar-select">
            <span className="sr-only">项目任务状态</span>
            <select
              aria-label="项目任务状态"
              onChange={(event) => {
                setTaskStatus(event.target.value as TaskStatus | "");
                setTaskPage(1);
              }}
              value={taskStatus}
            >
              <option value="">全部状态</option>
              <option value="todo">待办</option>
              <option value="in_progress">进行中</option>
              <option value="blocked">阻塞</option>
              <option value="waiting_review">待验收</option>
              <option value="done">已完成</option>
              <option value="cancelled">已取消</option>
            </select>
          </label>
          <label className="toolbar-select">
            <span className="sr-only">项目任务标签</span>
            <select
              aria-label="项目任务标签"
              disabled={taskTagsQuery.isPending || taskTagsQuery.isError}
              onChange={(event) => {
                setTaskTagId(event.target.value);
                setTaskPage(1);
              }}
              value={taskTagId}
            >
              <option value="">
                {taskTagsQuery.isError ? "标签不可用" : "全部标签"}
              </option>
              {(taskTagsQuery.data ?? []).map((tag) => (
                <option key={tag.id} value={tag.id}>
                  {tag.name}
                </option>
              ))}
            </select>
          </label>
          <label className="toolbar-select">
            <span className="sr-only">项目任务优先级</span>
            <select
              aria-label="项目任务优先级"
              onChange={(event) => {
                setTaskPriority(event.target.value as TaskPriority | "");
                setTaskPage(1);
              }}
              value={taskPriority}
            >
              <option value="">全部优先级</option>
              <option value="P0">P0 紧急</option>
              <option value="P1">P1 高</option>
              <option value="P2">P2 中</option>
              <option value="P3">P3 低</option>
            </select>
          </label>
          <label className="toolbar-select">
            <span className="sr-only">项目任务类型</span>
            <select
              aria-label="项目任务类型"
              onChange={(event) => {
                setTaskKind(event.target.value as TaskKind | "");
                setTaskPage(1);
              }}
              value={taskKind}
            >
              <option value="">全部类型</option>
              <option value="work">工作</option>
              <option value="review">审核</option>
              <option value="followup">跟进</option>
              <option value="reminder">提醒</option>
            </select>
          </label>
          <label className="toolbar-select">
            <span className="sr-only">项目任务排期</span>
            <select
              aria-label="项目任务排期"
              onChange={(event) => {
                setTaskPlannedState(
                  event.target.value as "scheduled" | "unscheduled" | "",
                );
                setTaskPage(1);
              }}
              value={taskPlannedState}
            >
              <option value="">全部排期</option>
              <option value="scheduled">已排期</option>
              <option value="unscheduled">未排期</option>
            </select>
          </label>
        </div>
        {tasksQuery.isPending ? <SkeletonRows count={4} /> : null}
        {tasksQuery.isError ? (
          <ErrorState
            compact
            message="无法读取项目任务。"
            onRetry={() => void tasksQuery.refetch()}
          />
        ) : null}
        {tasksQuery.isSuccess && projectTaskResultTotal === 0 ? (
          <EmptyState
            action={
              hasTaskFilters ? (
                <button
                  className="button button-secondary"
                  onClick={() => {
                    setTaskSearchInput("");
                    setTaskQueryText("");
                    setTaskStatus("");
                    setTaskPriority("");
                    setTaskKind("");
                    setTaskTagId("");
                    setTaskPlannedState("");
                    setTaskPage(1);
                  }}
                  type="button"
                >
                  清除条件
                </button>
              ) : project.status !== "archived" ? (
                <button
                  className="button button-primary"
                  disabled={busy}
                  onClick={() => openNewTaskForProject(project.id)}
                  type="button"
                >
                  <Plus size={14} />
                  新建项目任务
                </button>
              ) : null
            }
            message={
              hasTaskFilters
                ? "当前项目没有符合这些条件的任务。"
                : "项目还没有关联任务。"
            }
            title={hasTaskFilters ? "没有匹配任务" : "任务列表为空"}
          />
        ) : null}
        {projectTasks.length ? (
          <TaskList
            hierarchical={hierarchicalTasks}
            live={tasksQuery.isSuccess}
            showParent={!hierarchicalTasks}
            tasks={projectTasks}
          />
        ) : null}
        {tasksQuery.isSuccess && projectTaskPages > 1 ? (
          <nav aria-label="项目任务分页" className="pagination">
            <button
              className="button button-secondary"
              disabled={taskPage <= 1 || tasksQuery.isFetching}
              onClick={() => setTaskPage((value) => Math.max(1, value - 1))}
              type="button"
            >
              上一页
            </button>
            <span>
              {(taskPage - 1) * tasksQuery.data.meta.pageSize + 1}–
              {Math.min(
                taskPage * tasksQuery.data.meta.pageSize,
                projectTaskResultTotal,
              )}{" "}
              / {projectTaskResultTotal}
            </span>
            <button
              className="button button-secondary"
              disabled={taskPage >= projectTaskPages || tasksQuery.isFetching}
              onClick={() => setTaskPage((value) => value + 1)}
              type="button"
            >
              下一页
            </button>
          </nav>
        ) : null}
      </section>

      <section className="project-detail-section project-actions-section">
        <div className="project-detail-heading">
          <div>
            <h2>项目状态</h2>
            <p>状态操作不会自动完成、恢复或删除关联任务。</p>
          </div>
          <span className="status-badge">{statusLabels[project.status]}</span>
        </div>

        {confirmAction ? (
          <div className="project-confirmation" role="alert">
            <AlertTriangle size={16} />
            <div>
              <strong>
                {confirmAction === "complete"
                  ? project.taskSummary.remaining > 0
                    ? `仍有 ${project.taskSummary.remaining} 项任务未完成`
                    : "项目存在尚未完成的任务"
                  : "归档后项目将从默认列表隐藏"}
              </strong>
              <p>
                {confirmAction === "complete"
                  ? "确认完成只改变项目状态，不会修改这些任务。"
                  : "任务和历史都会保留，可以随时恢复项目。"}
              </p>
            </div>
            <button
              autoFocus
              className="button button-secondary"
              disabled={busy}
              onClick={() => setConfirmAction(null)}
              type="button"
            >
              取消
            </button>
            <button
              className="button button-primary"
              disabled={busy}
              onClick={() => runTransition(confirmAction, true)}
              type="button"
            >
              确认{actionLabels[confirmAction]}
            </button>
          </div>
        ) : (
          <div className="project-action-row">
            {project.availableActions.map((action) => (
              <button
                className={
                  action === "archive"
                    ? "button button-secondary"
                    : "button button-primary"
                }
                disabled={busy}
                key={action}
                onClick={() => runTransition(action)}
                type="button"
              >
                {actionLabels[action]}
              </button>
            ))}
          </div>
        )}

        {operationError ? (
          <div className="form-error" role="alert">
            {operationError}
          </div>
        ) : null}
      </section>

      <ProjectNotesSection
        archived={project.status === "archived"}
        projectId={project.id}
      />

      <ProjectArtifactsSection projectId={project.id} />

      <ProjectAttachmentsSection
        archived={project.status === "archived"}
        projectId={project.id}
        projectVersion={project.version}
      />

      <ProjectEventsSection projectId={project.id} />

      {project.status === "archived" ? (
        <section className="project-detail-section project-danger-zone">
          <div>
            <h2>永久删除</h2>
            <p>
              仅归档项目可永久删除；将解除 {project.taskSummary.total} 项任务和
              {project.invoiceCount}{" "}
              张发票的项目关联；项目笔记、附件记录和附件文件将随项目永久删除，
              任务和发票业务记录本身不会被删除。
            </p>
          </div>
          {confirmingDelete ? (
            <div className="project-action-row">
              <button
                autoFocus
                className="button button-secondary"
                disabled={busy}
                onClick={() => setConfirmingDelete(false)}
                type="button"
              >
                返回
              </button>
              <button
                className="button button-danger"
                disabled={busy}
                onClick={permanentlyDelete}
                type="button"
              >
                <Trash2 size={14} />
                {deleteMutation.isPending ? "正在删除…" : "确认永久删除"}
              </button>
            </div>
          ) : (
            <button
              className="button button-danger"
              onClick={() => setConfirmingDelete(true)}
              type="button"
            >
              <Trash2 size={14} />
              永久删除项目
            </button>
          )}
        </section>
      ) : null}

      <section className="project-future-note">
        <Clock3 size={15} />
        发票与收入将在后续业务版本接入，不展示模拟数据。
      </section>

      <ProjectFormModal
        onClose={() => setEditing(false)}
        onVersionConflict={() => void projectQuery.refetch()}
        open={editing}
        project={project}
      />
    </div>
  );
}
