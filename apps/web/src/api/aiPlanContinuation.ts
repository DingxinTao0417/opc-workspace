import { apiRequest, ApiError } from "./client";
import { isWorkspaceIdentity } from "../lib/focusReportLocation";
import type {
  AiWorkspaceGrant,
  AiWorkspaceScope,
  AiProvider,
} from "../types/models";

export const continuationReasons = {
  ready: "已就绪，等待启动下一轮",
  pending_approval: "等待人工核对或逐项审批",
  run_active: "等待 Agent 执行结束",
  output_pending: "等待人工恢复产出登记（不会重跑模型）",
  provider_busy: "等待 Provider 空闲",
  generating: "正在自动生成下一轮",
  plan_complete: "计划已满足，自动续办结束",
  user_stopped: "已请求停止自动续办",
  no_progress: "未取得新进展，已停止",
  provider_changed: "Provider 已变化，需要重新授权",
  scope_changed: "授权范围或知识来源已变化，需要重新授权",
  generation_failed: "自动生成失败，已停止",
  generation_cancelled: "自动生成被取消，已停止",
  evidence_unavailable: "执行证据不可核验，已停止",
  plan_changed: "计划发生外部变化，需要重新授权",
  turn_limit: "已达到轮数上限",
  time_limit: "授权已到期",
  process_restart: "本地服务已重启，需要重新授权",
  shutdown: "本地服务停止，需要重新授权",
  restore_pending: "数据恢复中，续办已中断",
} as const;
export const continuationStatuses = {
  waiting: "等待中",
  running: "自动生成中",
  completed: "已完成",
  stopped: "已停止",
  failed: "已失败",
  exhausted: "轮数已用完",
  expired: "已到期",
  interrupted: "已中断",
} as const;
export const continuationScopeOptions: {
  id: AiWorkspaceScope;
  label: string;
  description: string;
}[] = [
  {
    id: "work",
    label: "工作事项",
    description:
      "任务、项目、收件箱、提醒、排期、专注、项目笔记完整正文及责任元数据。范围不限当前计划的记录。",
  },
  {
    id: "outputs",
    label: "执行产出",
    description:
      "任务批次、摘要、验收意见、历史产出及 Agent 文本结果；文件仅元数据。",
  },
  {
    id: "actions",
    label: "操作建议与计划",
    description:
      "修订当前计划、起草工作事项与自动化操作；每项仍须人工批准，不自动执行或验收。",
  },
  {
    id: "clients",
    label: "客户资料",
    description:
      "客户、联系关系、本地活动及回访正文；可能含敏感信息，不实际联系客户。",
  },
  {
    id: "finance",
    label: "财务查询",
    description:
      "收支、发票的金额、分类、日期、状态及同币种统计。不是银行到账证明。",
  },
  {
    id: "finance_actions",
    label: "财务操作建议",
    description: "起草记账、修改或作废；须独立人工核验及确认，不执行银行付款。",
  },
  {
    id: "invoice_actions",
    label: "发票操作建议",
    description:
      "起草本地发票状态、PDF、删除等操作；均需独立确认，不发送发票。",
  },
  {
    id: "finance_exports",
    label: "财务导出建议",
    description: "起草本地 CSV 导出，人工确认后自行下载，不自动发送文件。",
  },
  {
    id: "agent_execution",
    label: "Agent 执行建议",
    description:
      "起草启动、重试、停止或恢复登记；每项需独立人工确认，可能产生额外模型费用。",
  },
  {
    id: "agent_files",
    label: "Agent 受控文件",
    description:
      "为执行选择受控文件和文件产出；须逐次文件与 Provider 确认，不开放任意路径。",
  },
  {
    id: "agent_project_files",
    label: "同项目已验收文件",
    description:
      "显式选择其他已完成任务的已验收受控文件；还须独立跨任务交接确认。",
  },
  {
    id: "output_files",
    label: "产出文件正文",
    description:
      "按需读取任意任务当前或历史未删受控 UTF-8 产出（每文件64KiB）；正文可发送给模型，不是已验收证明。",
  },
  {
    id: "knowledge",
    label: "知识检索",
    description:
      "在下方明确选择的来源与版本内搜索、读取完整片段；可能发送敏感正文。",
  },
  {
    id: "knowledge_actions",
    label: "知识库管理建议",
    description:
      "读取索引元数据，起草新增、重建或删除等建议；每项需人工确认，独立于知识检索。",
  },
];
const dependencies: Partial<Record<AiWorkspaceScope, AiWorkspaceScope[]>> = {
  actions: ["work"],
  outputs: ["work"],
  output_files: ["work", "outputs"],
  agent_execution: ["work", "outputs", "actions"],
  agent_files: ["work", "outputs", "actions", "agent_execution"],
  agent_project_files: [
    "work",
    "outputs",
    "actions",
    "agent_execution",
    "agent_files",
  ],
  finance_actions: ["finance"],
  invoice_actions: ["finance"],
  finance_exports: ["finance"],
};
export function toggleContinuationScope(
  scopes: AiWorkspaceScope[],
  scope: AiWorkspaceScope,
  enabled: boolean,
): AiWorkspaceScope[] {
  const next = new Set(scopes);
  if (enabled) {
    next.add(scope);
    for (const dep of dependencies[scope] ?? []) next.add(dep);
  } else {
    next.delete(scope);
    for (const [key, deps] of Object.entries(dependencies))
      if (deps.includes(scope)) next.delete(key as AiWorkspaceScope);
  }
  return continuationScopeOptions
    .filter((item) => next.has(item.id))
    .map((item) => item.id);
}
export interface AiPlanContinuation {
  id: string;
  session_id: string;
  session_title?: string;
  version: number;
  status: keyof typeof continuationStatuses;
  reason: keyof typeof continuationReasons;
  initial_plan_version: number;
  current_plan_version: number;
  provider: Pick<
    AiProvider,
    "id" | "name" | "kind" | "protocol" | "model" | "version"
  > & { config_version: number };
  workspace: AiWorkspaceGrant;
  resume_after_restart?: boolean;
  max_turns: number;
  turns_started: number;
  current_generation_id: string | null;
  last_generation_id: string | null;
  created_at: string;
  updated_at: string;
  expires_at: string;
}
export interface CreateAiPlanContinuation {
  expected_session_version: number;
  expected_plan_version: number;
  provider_id: string;
  expected_provider_version: number;
  expected_provider_config_version: number;
  workspace: AiWorkspaceGrant;
  max_turns: number;
  ttl_minutes: number;
  confirm_automatic_continuation: true;
  resume_after_restart?: boolean;
  confirm_restart_continuation?: true;
}
function record(v: unknown): v is Record<string, unknown> {
  return !!v && typeof v === "object" && !Array.isArray(v);
}
function exact(v: Record<string, unknown>, keys: string[]) {
  return Object.keys(v).every((k) => keys.includes(k));
}
function integer(
  v: unknown,
  min = 1,
  max = Number.MAX_SAFE_INTEGER,
): v is number {
  return Number.isSafeInteger(v) && Number(v) >= min && Number(v) <= max;
}
function identity(v: unknown): v is string {
  return typeof v === "string" && isWorkspaceIdentity(v);
}
function date(v: unknown): v is string {
  if (typeof v !== "string") return false;
  const parts =
    /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d{1,9}))?(Z|[+-](\d{2}):(\d{2}))$/.exec(
      v,
    );
  if (!parts) return false;
  const [
    ,
    year,
    month,
    day,
    hour,
    minute,
    second,
    ,
    zone,
    offsetHour,
    offsetMinute,
  ] = parts;
  const y = Number(year),
    m = Number(month),
    d = Number(day);
  const days = [
    31,
    y % 4 === 0 && (y % 100 !== 0 || y % 400 === 0) ? 29 : 28,
    31,
    30,
    31,
    30,
    31,
    31,
    30,
    31,
    30,
    31,
  ];
  return (
    m >= 1 &&
    m <= 12 &&
    d >= 1 &&
    d <= days[m - 1] &&
    Number(hour) <= 23 &&
    Number(minute) <= 59 &&
    Number(second) <= 59 &&
    (zone === "Z" ||
      (Number(offsetHour) <= 23 && Number(offsetMinute) <= 59)) &&
    Number.isFinite(Date.parse(v))
  );
}
function instant(v: string): bigint {
  const fraction = /\.(\d{1,9})(?=Z|[+-])/.exec(v);
  return (
    BigInt(Date.parse(v.replace(/\.\d{1,9}(?=Z|[+-])/, ""))) * 1_000_000n +
    BigInt((fraction?.[1] ?? "").padEnd(9, "0"))
  );
}
function fail(): never {
  throw new ApiError("自动续办响应或请求无效，未授予或恢复权限", {
    code: "INVALID_RESPONSE",
  });
}
function parseWorkspace(v: unknown, version: number): AiWorkspaceGrant {
  if (
    !record(v) ||
    !exact(v, ["provider_version", "scopes", "knowledge_sources"]) ||
    v.provider_version !== version ||
    !Array.isArray(v.scopes) ||
    new Set(v.scopes).size !== v.scopes.length ||
    !v.scopes.includes("work") ||
    !v.scopes.includes("actions") ||
    v.scopes.some((s) => !continuationScopeOptions.some((o) => o.id === s))
  )
    return fail();
  const scopes = v.scopes as AiWorkspaceScope[];
  if (
    scopes.some((s) =>
      (dependencies[s] ?? []).some((dep) => !scopes.includes(dep)),
    )
  )
    return fail();
  if (scopes.includes("knowledge")) {
    if (
      !Array.isArray(v.knowledge_sources) ||
      v.knowledge_sources.length < 1 ||
      v.knowledge_sources.length > 20 ||
      v.knowledge_sources.some(
        (s) =>
          !record(s) ||
          !exact(s, ["source_id", "expected_source_version"]) ||
          !identity(s.source_id) ||
          !integer(s.expected_source_version),
      ) ||
      new Set(v.knowledge_sources.map((s) => s.source_id)).size !==
        v.knowledge_sources.length
    )
      return fail();
  } else if (v.knowledge_sources !== undefined) return fail();
  return v as unknown as AiWorkspaceGrant;
}
export function parseAiPlanContinuation(
  v: unknown,
  sessionId?: string,
): AiPlanContinuation {
  if (
    !record(v) ||
    !exact(v, [
      "id",
      "session_id",
      "session_title",
      "version",
      "status",
      "reason",
      "initial_plan_version",
      "current_plan_version",
      "provider",
      "workspace",
      "resume_after_restart",
      "max_turns",
      "turns_started",
      "current_generation_id",
      "last_generation_id",
      "created_at",
      "updated_at",
      "expires_at",
    ]) ||
    !identity(v.id) ||
    !identity(v.session_id) ||
    (sessionId !== undefined && v.session_id !== sessionId) ||
    (v.session_title !== undefined &&
      (typeof v.session_title !== "string" ||
        !v.session_title.trim() ||
        v.session_title.length > 200)) ||
    !integer(v.version) ||
    !integer(v.initial_plan_version, 1, 128) ||
    !integer(v.current_plan_version, 1, 128) ||
    v.current_plan_version < v.initial_plan_version ||
    typeof v.status !== "string" ||
    !Object.hasOwn(continuationStatuses, v.status) ||
    typeof v.reason !== "string" ||
    !Object.hasOwn(continuationReasons, v.reason) ||
    !integer(v.max_turns, 1, 8) ||
    (v.resume_after_restart !== undefined &&
      typeof v.resume_after_restart !== "boolean") ||
    !integer(v.turns_started, 0, v.max_turns) ||
    ![v.current_generation_id, v.last_generation_id].every(
      (id) => id === null || identity(id),
    ) ||
    ![v.created_at, v.updated_at, v.expires_at].every(date)
  )
    return fail();
  const duration =
    instant(v.expires_at as string) - instant(v.created_at as string);
  if (duration <= 0n || duration > 120n * 60n * 1_000_000_000n) return fail();
  const p = v.provider;
  if (
    !record(p) ||
    !exact(p, [
      "id",
      "name",
      "kind",
      "protocol",
      "model",
      "version",
      "config_version",
    ]) ||
    !identity(p.id) ||
    typeof p.name !== "string" ||
    !p.name.trim() ||
    typeof p.model !== "string" ||
    !p.model.trim() ||
    !["local", "remote"].includes(String(p.kind)) ||
    !["openai_chat", "anthropic_messages"].includes(String(p.protocol)) ||
    !integer(p.version) ||
    !integer(p.config_version)
  )
    return fail();
  parseWorkspace(v.workspace, p.version);
  if (
    v.current_generation_id !== null &&
    (v.current_generation_id !== v.last_generation_id || v.turns_started === 0)
  )
    return fail();
  if ((v.turns_started === 0) !== (v.last_generation_id === null))
    return fail();
  if (
    v.status === "running" &&
    (v.reason !== "generating" ||
      v.current_generation_id === null ||
      v.turns_started === 0)
  )
    return fail();
  return v as unknown as AiPlanContinuation;
}
export const isContinuationActive = (
  v: AiPlanContinuation | null | undefined,
) => v?.status === "waiting" || v?.status === "running";
export const continuationKey = (id: string) =>
  ["ai", "plan-continuation", id] as const;
export const activeContinuationsKey = [
  "ai",
  "active-plan-continuations",
] as const;
export const recentContinuationsKey = [
  "ai",
  "recent-plan-continuations",
] as const;
export async function getAiPlanContinuation(
  sessionId: string,
  signal?: AbortSignal,
): Promise<AiPlanContinuation | null> {
  if (!identity(sessionId)) return fail();
  const p = await apiRequest<unknown>(
    `/api/v1/ai/sessions/${sessionId}/continuation`,
    { signal },
  );
  if (!record(p) || !("data" in p)) return fail();
  return p.data === null ? null : parseAiPlanContinuation(p.data, sessionId);
}
export async function getActiveAiPlanContinuations(
  signal?: AbortSignal,
): Promise<AiPlanContinuation[]> {
  const p = await apiRequest<unknown>(
    "/api/v1/ai/continuations?status=active",
    { signal },
  );
  if (!record(p) || !Array.isArray(p.data) || p.data.length > 8) return fail();
  const leases = p.data.map((v) => parseAiPlanContinuation(v));
  if (
    leases.some((l) => !isContinuationActive(l)) ||
    new Set(leases.map((l) => l.id)).size !== leases.length ||
    new Set(leases.map((l) => l.session_id)).size !== leases.length
  )
    return fail();
  return leases;
}
export async function getRecentAiPlanContinuations(
  signal?: AbortSignal,
): Promise<AiPlanContinuation[]> {
  const p = await apiRequest<unknown>(
    "/api/v1/ai/continuations?status=recent",
    { signal },
  );
  if (!record(p) || !Array.isArray(p.data) || p.data.length > 10) return fail();
  const leases = p.data.map((v) => parseAiPlanContinuation(v));
  if (
    leases.some(isContinuationActive) ||
    new Set(leases.map((l) => l.id)).size !== leases.length
  )
    return fail();
  return leases;
}
export async function createAiPlanContinuation(
  sessionId: string,
  input: CreateAiPlanContinuation,
): Promise<AiPlanContinuation> {
  if (
    !record(input) ||
    !exact(input, [
      "expected_session_version",
      "expected_plan_version",
      "provider_id",
      "expected_provider_version",
      "expected_provider_config_version",
      "workspace",
      "max_turns",
      "ttl_minutes",
      "confirm_automatic_continuation",
      "resume_after_restart",
      "confirm_restart_continuation",
    ]) ||
    !identity(sessionId) ||
    !identity(input.provider_id) ||
    !integer(input.expected_session_version) ||
    !integer(input.expected_plan_version, 1, 128) ||
    !integer(input.expected_provider_version) ||
    !integer(input.expected_provider_config_version) ||
    !integer(input.max_turns, 1, 8) ||
    !integer(input.ttl_minutes, 1, 120) ||
    input.confirm_automatic_continuation !== true ||
    (input.resume_after_restart !== undefined &&
      typeof input.resume_after_restart !== "boolean") ||
    (input.resume_after_restart === true) !==
      (input.confirm_restart_continuation === true) ||
    (input.confirm_restart_continuation !== undefined &&
      input.confirm_restart_continuation !== true)
  )
    return fail();
  parseWorkspace(input.workspace, input.expected_provider_version);
  const p = await apiRequest<unknown>(
    `/api/v1/ai/sessions/${sessionId}/continuation`,
    { method: "POST", body: JSON.stringify(input) },
  );
  const lease = parseAiPlanContinuation(
    record(p) ? p.data : undefined,
    sessionId,
  );
  const knowledgeProof = (workspace: AiWorkspaceGrant) =>
    JSON.stringify(
      (workspace.knowledge_sources ?? [])
        .map((s) => [s.source_id, s.expected_source_version])
        .sort((a, b) => String(a[0]).localeCompare(String(b[0]))),
    );
  if (
    knowledgeProof(lease.workspace) !== knowledgeProof(input.workspace) ||
    lease.provider.id !== input.provider_id ||
    lease.provider.version !== input.expected_provider_version ||
    lease.provider.config_version !== input.expected_provider_config_version ||
    lease.initial_plan_version !== input.expected_plan_version ||
    lease.max_turns !== input.max_turns ||
    (input.resume_after_restart === true &&
      lease.resume_after_restart !== true) ||
    instant(lease.expires_at) - instant(lease.created_at) !==
      BigInt(input.ttl_minutes) * 60n * 1_000_000_000n ||
    JSON.stringify(lease.workspace.scopes.slice().sort()) !==
      JSON.stringify(input.workspace.scopes.slice().sort())
  )
    return fail();
  return lease;
}
export async function stopAiPlanContinuation(
  id: string,
): Promise<AiPlanContinuation> {
  if (!identity(id)) return fail();
  const p = await apiRequest<unknown>(`/api/v1/ai/continuations/${id}/stop`, {
    method: "POST",
  });
  const lease = parseAiPlanContinuation(record(p) ? p.data : undefined);
  if (lease.id !== id || isContinuationActive(lease)) return fail();
  return lease;
}
