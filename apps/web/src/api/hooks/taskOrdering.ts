import type { ReorderTasksInput, Task } from "../../types/models";
import {
  ApiError,
  batchUpdateTasks,
  getAllTasks,
  getTask,
  reorderTasks,
} from "../client";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { invalidateTaskFacts } from "./invalidation";
import { isTaskFactsStale, setTaskDetailIfNotOlder } from "./tasks";

function isAmbiguousTaskWrite(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    (error.code === "NETWORK_ERROR" || error.code === "TIMEOUT")
  );
}

async function setTaskPlannedDateVerified({
  taskId,
  expectedVersion,
  plannedDate,
}: {
  taskId: string;
  expectedVersion: number;
  plannedDate: string | null;
}) {
  try {
    return await batchUpdateTasks({
      action: "set_planned_date",
      items: [{ id: taskId, expectedVersion }],
      plannedDate,
    });
  } catch (error) {
    if (!isAmbiguousTaskWrite(error)) throw error;
    try {
      const latest = await getTask(taskId);
      if (
        latest.plannedDate === plannedDate &&
        latest.version > expectedVersion
      ) {
        return {
          action: "set_planned_date" as const,
          changed: 1,
          tasks: [latest],
        };
      }
    } catch {
      // Preserve the original ambiguous write error. A failed verification
      // must not be reported as a confirmed plan change.
    }
    throw error;
  }
}

export function useSetTaskPlannedDate() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: {
      taskId: string;
      expectedVersion: number;
      plannedDate: string | null;
    }) => setTaskPlannedDateVerified(input),
    onSuccess: async (result) => {
      for (const task of result.tasks)
        setTaskDetailIfNotOlder(queryClient, task);
      await invalidateTaskFacts(queryClient);
    },
    onError: async (error) => {
      if (isTaskFactsStale(error) || isAmbiguousTaskWrite(error)) {
        await invalidateTaskFacts(queryClient);
      }
    },
  });
}

function isTerminalTask(task: Task): boolean {
  return task.status === "done" || task.status === "cancelled";
}

async function getPlanTasks(plannedDate: string | null): Promise<Task[]> {
  const tasks = await getAllTasks({
    plannedDate: plannedDate ?? undefined,
    sort: "manual_order",
  });
  return plannedDate === null
    ? tasks.filter((task) => task.plannedDate === null)
    : tasks;
}

function insertActiveTaskRelativeToTarget(
  scoped: Task[],
  source: Task,
  targetId: string | null,
  position: "before" | "after" | "end",
): Task[] {
  const active = scoped.filter(
    (task) => !isTerminalTask(task) && task.id !== source.id,
  );
  if (position === "end" || targetId === null) {
    active.push(source);
  } else {
    const targetIndex = active.findIndex((task) => task.id === targetId);
    if (targetIndex < 0) {
      throw new ApiError("目标任务已不在活动计划组，请刷新后重试", {
        code: "TASK_REORDER_SET_CHANGED",
      });
    }
    active.splice(targetIndex + (position === "after" ? 1 : 0), 0, source);
  }
  let activeIndex = 0;
  const ordered: Task[] = [];
  for (const task of scoped) {
    if (isTerminalTask(task)) {
      ordered.push(task);
      continue;
    }
    const next = active[activeIndex++];
    if (next) ordered.push(next);
  }
  ordered.push(...active.slice(activeIndex));
  return ordered;
}

export interface MoveTaskAcrossPlansResult {
  plannedDateChanged: boolean;
  task: Task;
  orderWarnings: string[];
}

export function useMoveTaskAcrossPlans() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({
      source,
      target,
      targetPlannedDate,
      position,
    }: {
      source: Task;
      target: Task | null;
      targetPlannedDate: string | null;
      position: "before" | "after" | "end";
    }) => {
      if (target && source.id === target.id) {
        return {
          plannedDateChanged: false,
          task: source,
          orderWarnings: [],
        } satisfies MoveTaskAcrossPlansResult;
      }
      const sourceDate = source.plannedDate;
      const targetDate = targetPlannedDate;
      const [sourcePlan, targetPlan] =
        sourceDate === targetDate
          ? [await getPlanTasks(sourceDate), null]
          : await Promise.all([
              getPlanTasks(sourceDate),
              getPlanTasks(targetDate),
            ]);
      if (sourcePlan.length > 1_000 || (targetPlan?.length ?? 0) >= 1_000) {
        throw new ApiError("源或目标计划日期达到 1000 项，暂时无法跨组拖拽", {
          code: "TASK_REORDER_LIMIT",
        });
      }
      const latestSource = sourcePlan.find((task) => task.id === source.id);
      const latestTarget = target
        ? (targetPlan ?? sourcePlan).find((task) => task.id === target.id)
        : null;
      if (
        !latestSource ||
        latestSource.version !== source.version ||
        isTerminalTask(latestSource) ||
        (target !== null &&
          (!latestTarget ||
            latestTarget.version !== target.version ||
            isTerminalTask(latestTarget)))
      ) {
        throw new ApiError("任务集合已经变化，请刷新后重试", {
          code: "TASK_REORDER_SET_CHANGED",
        });
      }

      if (sourceDate === targetDate) {
        const ordered = insertActiveTaskRelativeToTarget(
          sourcePlan,
          latestSource,
          target?.id ?? null,
          position,
        );
        const reordered = await reorderTasks({
          plannedDate: sourceDate,
          mode: "manual",
          items: ordered.map((task) => ({
            id: task.id,
            expectedVersion: task.version,
          })),
        });
        return {
          plannedDateChanged: false,
          task:
            reordered.tasks.find((task) => task.id === source.id) ??
            latestSource,
          orderWarnings: [],
        } satisfies MoveTaskAcrossPlansResult;
      }

      const planResult = await setTaskPlannedDateVerified({
        taskId: source.id,
        expectedVersion: source.version,
        plannedDate: targetDate,
      });
      const moved = planResult.tasks.find((task) => task.id === source.id);
      if (!moved) {
        throw new ApiError("改期响应缺少目标任务，请刷新后核对", {
          code: "INVALID_RESPONSE",
        });
      }
      const sourceOrder = sourcePlan.filter((task) => task.id !== source.id);
      const targetOrder = insertActiveTaskRelativeToTarget(
        targetPlan!,
        moved,
        target?.id ?? null,
        position,
      );
      const [sourceOrderResult, targetOrderResult] = await Promise.allSettled([
        reorderTasks({
          plannedDate: sourceDate,
          mode: "manual",
          items: sourceOrder.map((task) => ({
            id: task.id,
            expectedVersion: task.version,
          })),
        }),
        reorderTasks({
          plannedDate: targetDate,
          mode: "manual",
          items: targetOrder.map((task) => ({
            id: task.id,
            expectedVersion: task.version,
          })),
        }),
      ]);
      const orderWarnings: string[] = [];
      if (sourceOrderResult.status === "rejected")
        orderWarnings.push("源日期顺序未能保存");
      if (targetOrderResult.status === "rejected")
        orderWarnings.push("目标日期顺序未能保存");
      const finalMoved =
        targetOrderResult.status === "fulfilled"
          ? (targetOrderResult.value.tasks.find(
              (task) => task.id === source.id,
            ) ?? moved)
          : moved;
      return {
        plannedDateChanged: true,
        task: finalMoved,
        orderWarnings,
      } satisfies MoveTaskAcrossPlansResult;
    },
    onSuccess: async (result) => {
      setTaskDetailIfNotOlder(queryClient, result.task);
      await invalidateTaskFacts(queryClient);
    },
    onError: async (error) => {
      if (isTaskFactsStale(error) || isAmbiguousTaskWrite(error)) {
        await invalidateTaskFacts(queryClient);
      }
    },
  });
}

export function useReorderTasks() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: ReorderTasksInput) => reorderTasks(input),
    onSuccess: async () => invalidateTaskFacts(queryClient),
    onError: async (error) => {
      if (isTaskFactsStale(error)) {
        await invalidateTaskFacts(queryClient);
      }
    },
  });
}

export function useMoveTaskWithinPlan() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({
      taskId,
      plannedDate,
      direction,
      scope = "status",
    }: {
      taskId: string;
      plannedDate: string | null;
      direction: "up" | "down";
      scope?: "status" | "active";
    }) => {
      const tasks = await getAllTasks({
        plannedDate: plannedDate ?? undefined,
        sort: "manual_order",
      });
      const scoped = plannedDate
        ? tasks
        : tasks.filter((task) => task.plannedDate === null);
      if (scoped.length > 1_000) {
        throw new ApiError("同一计划日期超过 1000 项，暂时无法手动排序", {
          code: "TASK_REORDER_LIMIT",
        });
      }
      const currentIndex = scoped.findIndex((task) => task.id === taskId);
      if (currentIndex < 0) {
        throw new ApiError("任务已不在当前计划日期，请刷新后重试", {
          code: "TASK_REORDER_SET_CHANGED",
        });
      }
      const current = scoped[currentIndex];
      const isCandidate =
        scope === "active"
          ? (task: Task) =>
              task.status !== "done" && task.status !== "cancelled"
          : (task: Task) => task.status === current.status;
      if (!isCandidate(current)) {
        throw new ApiError("任务已不在当前活动计划组，请刷新后重试", {
          code: "TASK_REORDER_SET_CHANGED",
        });
      }
      const step = direction === "up" ? -1 : 1;
      let targetIndex = currentIndex + step;
      while (
        targetIndex >= 0 &&
        targetIndex < scoped.length &&
        !isCandidate(scoped[targetIndex])
      ) {
        targetIndex += step;
      }
      if (targetIndex < 0 || targetIndex >= scoped.length) {
        return {
          plannedDate,
          mode: "manual" as const,
          changed: 0,
          tasks: scoped,
        };
      }
      const ordered = [...scoped];
      [ordered[currentIndex], ordered[targetIndex]] = [
        ordered[targetIndex],
        ordered[currentIndex],
      ];
      return reorderTasks({
        plannedDate,
        mode: "manual",
        items: ordered.map((task) => ({
          id: task.id,
          expectedVersion: task.version,
        })),
      });
    },
    onSuccess: async () => invalidateTaskFacts(queryClient),
    onError: async (error) => {
      if (isTaskFactsStale(error)) {
        await invalidateTaskFacts(queryClient);
      }
    },
  });
}

export function useReorderActiveTasksWithinPlan() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({
      plannedDate,
      orderedTaskIds,
    }: {
      plannedDate: string | null;
      orderedTaskIds: string[];
    }) => {
      const tasks = await getAllTasks({
        plannedDate: plannedDate ?? undefined,
        sort: "manual_order",
      });
      const scoped = plannedDate
        ? tasks
        : tasks.filter((task) => task.plannedDate === null);
      if (scoped.length > 1_000) {
        throw new ApiError("同一计划日期超过 1000 项，暂时无法拖拽排序", {
          code: "TASK_REORDER_LIMIT",
        });
      }
      const active = scoped.filter(
        (task) => task.status !== "done" && task.status !== "cancelled",
      );
      const uniqueIds = new Set(orderedTaskIds);
      if (
        uniqueIds.size !== orderedTaskIds.length ||
        orderedTaskIds.length !== active.length ||
        active.some((task) => !uniqueIds.has(task.id))
      ) {
        throw new ApiError("活动任务集合已变化，请刷新后重试", {
          code: "TASK_REORDER_SET_CHANGED",
        });
      }
      const activeById = new Map(active.map((task) => [task.id, task]));
      let activeIndex = 0;
      const ordered = scoped.map((task) =>
        task.status === "done" || task.status === "cancelled"
          ? task
          : activeById.get(orderedTaskIds[activeIndex++])!,
      );
      return reorderTasks({
        plannedDate,
        mode: "manual",
        items: ordered.map((task) => ({
          id: task.id,
          expectedVersion: task.version,
        })),
      });
    },
    onSuccess: async () => invalidateTaskFacts(queryClient),
    onError: async (error) => {
      if (isTaskFactsStale(error)) {
        await invalidateTaskFacts(queryClient);
      }
    },
  });
}

export function useReorderTaskWithinPlanStatus() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({
      source,
      target,
      position,
    }: {
      source: Task;
      target: Task;
      position: "before" | "after";
    }) => {
      if (
        source.id === target.id ||
        source.plannedDate !== target.plannedDate ||
        source.status !== target.status
      ) {
        throw new ApiError("只能在同一计划日期和状态组内拖拽任务", {
          code: "TASK_REORDER_SCOPE_MISMATCH",
        });
      }
      const scoped = await getPlanTasks(source.plannedDate);
      if (scoped.length > 1_000) {
        throw new ApiError("同一计划日期超过 1000 项，暂时无法拖拽排序", {
          code: "TASK_REORDER_LIMIT",
        });
      }
      const latestSource = scoped.find((task) => task.id === source.id);
      const latestTarget = scoped.find((task) => task.id === target.id);
      if (
        !latestSource ||
        !latestTarget ||
        latestSource.version !== source.version ||
        latestTarget.version !== target.version ||
        latestSource.status !== source.status ||
        latestTarget.status !== source.status
      ) {
        throw new ApiError("当前计划状态组已经变化，请刷新后重试", {
          code: "TASK_REORDER_SET_CHANGED",
        });
      }
      const statusOrder = scoped.filter(
        (task) => task.status === source.status && task.id !== source.id,
      );
      const targetIndex = statusOrder.findIndex(
        (task) => task.id === target.id,
      );
      if (targetIndex < 0) {
        throw new ApiError("目标任务已不在当前状态组，请刷新后重试", {
          code: "TASK_REORDER_SET_CHANGED",
        });
      }
      statusOrder.splice(
        targetIndex + (position === "after" ? 1 : 0),
        0,
        latestSource,
      );
      let statusIndex = 0;
      const ordered = scoped.map((task) =>
        task.status === source.status ? statusOrder[statusIndex++]! : task,
      );
      return reorderTasks({
        plannedDate: source.plannedDate,
        mode: "manual",
        items: ordered.map((task) => ({
          id: task.id,
          expectedVersion: task.version,
        })),
      });
    },
    onSuccess: async () => invalidateTaskFacts(queryClient),
    onError: async (error) => {
      if (isTaskFactsStale(error)) await invalidateTaskFacts(queryClient);
    },
  });
}

export function useResetTaskOrder() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (plannedDate: string | null) => {
      const tasks = await getAllTasks({
        plannedDate: plannedDate ?? undefined,
        sort: "manual_order",
      });
      const scoped = plannedDate
        ? tasks
        : tasks.filter((task) => task.plannedDate === null);
      if (scoped.length > 1_000) {
        throw new ApiError("同一计划日期超过 1000 项，暂时无法重置排序", {
          code: "TASK_REORDER_LIMIT",
        });
      }
      return reorderTasks({
        plannedDate,
        mode: "default",
        items: scoped.map((task) => ({
          id: task.id,
          expectedVersion: task.version,
        })),
      });
    },
    onSuccess: async () => invalidateTaskFacts(queryClient),
    onError: async (error) => {
      if (isTaskFactsStale(error)) {
        await invalidateTaskFacts(queryClient);
      }
    },
  });
}
