import { create } from "zustand";
import { isWorkspaceIdentity } from "../lib/focusReportLocation";
import {
  projectPortfolioHref,
  readProjectPortfolioLocation,
} from "../lib/projectPortfolioLocation";
import type {
  AiWorkspaceScope,
  TaskSavedViewDefinition,
} from "../types/models";

const records = {
  task: { label: "任务", prefix: "/tasks/" },
  project: { label: "项目", prefix: "/projects/" },
  client: { label: "客户", prefix: "/clients/" },
  inbox_item: { label: "收件箱事项", prefix: "/inbox/" },
  reminder: { label: "本地提醒", prefix: "/inbox?reminder=" },
  content_item: { label: "内容条目", prefix: "/content-calendar?item=" },
  roadmap_milestone: { label: "里程碑", prefix: "/roadmap?milestone=" },
} as const;

export type AiWorkbenchRecordType = keyof typeof records;
export interface AiWorkbenchHandoff {
  type: AiWorkbenchRecordType;
  id: string;
  requestId: number;
}

// Queue items (failed automation runs, failed knowledge index jobs, unfinished
// agent runs) stage an issue handoff: the label, native route and draft prompt
// are resolved by the caller. Standard handoffs carry only that user-authored
// request. Local snapshot variants are explicit, bounded user-clicked data;
// neither variant carries a grant or session identity.
export interface AiIssueHandoff {
  label: string;
  route: string;
  routeLabel?: string;
  prompt: string;
  scopes: AiWorkspaceScope[];
  attachment?:
    | "git_review"
    | "file_preview"
    | "terminal_output"
    | "browser_address"
    | "browser_page_text"
    | "browser_action_result";
  preview?: string;
  previewTruncated?: boolean;
  requestId: number;
}

export interface TaskSelectionReturn {
  ids: string[];
  definition: TaskSavedViewDefinition;
  page: number;
  view: "list" | "board";
}

const localRouteBase = "http://opc-workspace.local";
const asciiControl = /[\u0000-\u001f\u007f]/;
const invalidHandoffPreviewControl = /[\u0000-\u0009\u000b-\u001f\u007f]/;
const encodedAsciiControlOrBackslash = /%(?:0[0-9a-f]|1[0-9a-f]|7f|5c)/i;
const maxIssueHandoffPromptBytes = 24 * 1024;
const maxIssueHandoffPreviewBytes = 2 * 1024;

function isSafeLocalAppRoute(route: string) {
  if (
    !route.startsWith("/") ||
    route.startsWith("//") ||
    route.includes("\\") ||
    asciiControl.test(route)
  ) {
    return false;
  }
  try {
    const parsed = new URL(route, localRouteBase);
    if (
      encodedAsciiControlOrBackslash.test(route) &&
      (parsed.pathname !== "/projects" ||
        parsed.hash !== "" ||
        projectPortfolioHref(
          readProjectPortfolioLocation(parsed.searchParams),
        ) !==
          parsed.pathname + parsed.search)
    ) {
      return false;
    }
    return (
      parsed.origin === localRouteBase &&
      parsed.pathname.startsWith("/") &&
      !parsed.pathname.startsWith("//")
    );
  } catch {
    return false;
  }
}

function workbenchHandoffPrompt(
  type: AiWorkbenchRecordType,
  label: string,
  route: string,
  id: string,
) {
  const read = `请先使用 workspace_get 按 type=${type}、id=${id} 读取最新记录；不要按名称猜测目标，也不要把计划、建议或已提交状态说成已经执行或验收。`;
  switch (type) {
    case "task":
      return `请帮我处理这条${label}：${route}\n${read} 先用 workspace_guide(topic=tasks_projects) 了解任务与项目规则，再分析下一步并仅提出待我确认的操作建议。交接没有复制任务描述、产出或文件；如需核对提交/验收，先请我为下一条消息单独选择 outputs，再用 workspace_guide(topic=tasks_projects, related_topic=outputs)。如需启动、重试或停止 Agent，还必须由我单独选择 agent_execution；任务提交、Agent Run 成功和人工验收不是同一件事。`;
    case "project":
      return `请帮我处理这条${label}：${route}\n${read} 先用 workspace_guide(topic=tasks_projects) 了解项目与任务规则，再分析下一步并仅提出待我确认的操作建议。交接没有复制项目说明、任务、客户、财务、附件或笔记；不要因为项目状态改变而声称其任务已经完成，也不要修改任务、客户或财务事实。`;
    case "client":
      return `请帮我处理这条${label}：${route}\n${read} 先用 workspace_guide(topic=people_clients) 了解客户读取和建议边界，再分析下一步并仅提出待我确认的操作建议。交接没有复制联系人、备注、活动、项目或财务记录；不得发送外部消息、访问外部系统或把客户操作说成已经完成。`;
    case "inbox_item":
      return `请帮我处理这条${label}：${route}\n${read} 先用 workspace_guide(topic=inbox) 了解收件箱规则；若需要来源上下文，再按当前授权调用 workspace_inbox_source。交接没有复制来源 payload、活动正文或关联任务；拆分不会自动运行 Agent，强制解决不是普通解决的自动降级。只提出待我确认的操作建议。`;
    case "reminder":
      return `请帮我处理这条${label}：${route}\n${read} 先用 workspace_guide(topic=reminders) 了解本地提醒规则，再分析下一步并仅提出待我确认的操作建议。交接没有复制摘要、取消原因或审计；已触发或已取消的提醒不能继续修改或取消，且不要声称已发送系统通知。`;
    case "content_item":
      return `请帮我处理这条${label}：${route}\n${read} 先用 workspace_guide(topic=roadmap_content) 了解内容与路线图规则，再分析下一步并仅提出待我确认的操作建议。交接没有复制备注、外链、正文或任务列表；外链只是未抓取文本，不能访问、验证或声称已发布到外部平台。内容准备关系不等于修改 Task 状态。`;
    case "roadmap_milestone":
      return `请帮我处理这条${label}：${route}\n${read} 先用 workspace_guide(topic=roadmap_content) 了解路线图规则，再分析风险或仅提出待我确认的里程碑操作建议。交接没有复制说明、项目列表、进度快照或任务正文；不要修改 Project/Task 状态、自动验收任务，或把里程碑计划说成已经执行。`;
  }
}

export function workbenchHandoffDetails(
  type: AiWorkbenchRecordType,
  id: string,
) {
  if (!Object.hasOwn(records, type) || !isWorkspaceIdentity(id)) return null;
  const { label, prefix } = records[type];
  const route = `${prefix}${id}`;
  return {
    label,
    route,
    prompt: workbenchHandoffPrompt(type, label, route, id),
    scopes: (type === "client"
      ? ["work", "clients", "actions"]
      : ["work", "actions"]) as AiWorkspaceScope[],
  };
}

// Only a human click stages a record identity. No body, grant, provider, or
// session is copied, persisted, fetched, or sent as part of this handoff.
export const useAiWorkbenchHandoff = create<{
  pending: AiWorkbenchHandoff | null;
  pendingIssue: AiIssueHandoff | null;
  taskSelectionReturn: TaskSelectionReturn | null;
  revision: number;
  stage: (type: AiWorkbenchRecordType, id: string) => boolean;
  stageIssue: (input: Omit<AiIssueHandoff, "requestId">) => boolean;
  rememberTaskSelectionReturn: (input: TaskSelectionReturn) => boolean;
  clearTaskSelectionReturn: () => void;
  dismiss: (requestId: number) => void;
  dismissIssue: (requestId: number) => void;
}>((set) => ({
  pending: null,
  pendingIssue: null,
  taskSelectionReturn: null,
  revision: 0,
  stage: (type, id) => {
    if (!workbenchHandoffDetails(type, id)) return false;
    set((state) => ({
      revision: state.revision + 1,
      pending: { type, id, requestId: state.revision + 1 },
      pendingIssue: null,
      taskSelectionReturn: null,
    }));
    return true;
  },
  stageIssue: (input) => {
    if (
      !input.label.trim() ||
      !input.prompt.trim() ||
      (input.preview !== undefined &&
        (!input.preview.trim() ||
          invalidHandoffPreviewControl.test(input.preview) ||
          new TextEncoder().encode(input.preview).byteLength >
            (input.attachment === "browser_page_text"
              ? 20 * 1024
              : maxIssueHandoffPreviewBytes))) ||
      (input.previewTruncated !== undefined &&
        typeof input.previewTruncated !== "boolean") ||
      new TextEncoder().encode(input.prompt).byteLength >
        maxIssueHandoffPromptBytes ||
      !isSafeLocalAppRoute(input.route)
    )
      return false;
    set((state) => ({
      revision: state.revision + 1,
      pending: null,
      pendingIssue: { ...input, requestId: state.revision + 1 },
      taskSelectionReturn: null,
    }));
    return true;
  },
  rememberTaskSelectionReturn: (input) => {
    if (
      input.ids.length < 1 ||
      input.ids.length > 20 ||
      input.ids.some((id) => !isWorkspaceIdentity(id)) ||
      new Set(input.ids).size !== input.ids.length ||
      !Number.isSafeInteger(input.page) ||
      input.page < 1 ||
      input.page > 1_000_000 ||
      (input.view !== "list" && input.view !== "board")
    )
      return false;
    set({
      taskSelectionReturn: {
        ids: [...input.ids],
        definition: {
          ...input.definition,
          tagIds: [...input.definition.tagIds],
        },
        page: input.page,
        view: input.view,
      },
    });
    return true;
  },
  clearTaskSelectionReturn: () => set({ taskSelectionReturn: null }),
  dismiss: (requestId) =>
    set((state) =>
      state.pending?.requestId === requestId ? { pending: null } : state,
    ),
  dismissIssue: (requestId) =>
    set((state) =>
      state.pendingIssue?.requestId === requestId
        ? { pendingIssue: null }
        : state,
    ),
}));
