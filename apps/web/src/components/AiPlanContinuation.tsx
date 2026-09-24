import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { getAiProviders, getAiSessions, ApiError } from "../api/client";
import { getAiGeneration } from "../api/aiActions";
import {
  activeContinuationsKey,
  continuationKey,
  continuationReasons,
  recentContinuationsKey,
  continuationStatuses,
  continuationScopeOptions,
  createAiPlanContinuation,
  getAiPlanContinuation,
  getActiveAiPlanContinuations,
  getRecentAiPlanContinuations,
  isContinuationActive,
  stopAiPlanContinuation,
  toggleContinuationScope,
  type AiPlanContinuation as Lease,
} from "../api/aiPlanContinuation";
import type {
  AiGenerationOrigin,
  AiWorkspaceGrant,
  AiWorkspaceScope,
} from "../types/models";
import { isWorkspaceIdentity } from "../lib/focusReportLocation";
import { AiKnowledgeAccess } from "./AiKnowledgeAccess";
import { AiRunProgress } from "./AiRunProgress";
import { AiCitationEvidence } from "./AiCitationEvidence";
import { renderAiRichText } from "./AiRichText";
import { Modal } from "./Modal";
import "./ai-plan-continuation.css";

export function usePlanContinuation(sessionId: string) {
  return useQuery({
    queryKey: continuationKey(sessionId),
    queryFn: ({ signal }) => getAiPlanContinuation(sessionId, signal),
    enabled: isWorkspaceIdentity(sessionId),
    retry: false,
    refetchInterval: 3000,
    structuralSharing: (old, next) => {
      const previous = old as Lease | null | undefined;
      const current = next as Lease | null;
      // Late GETs must not revive permission already stopped by a POST.
      if (
        previous &&
        current &&
        previous.id === current.id &&
        previous.version > current.version
      )
        return previous;
      return current;
    },
  });
}
export function AiAutomaticOrigin({ origin }: { origin?: AiGenerationOrigin }) {
  return origin ? (
    <small className="ai-auto-origin">
      计划自动续办 · 第 {origin.turn_index}/{origin.max_turns} 轮
    </small>
  ) : null;
}
export function AiContinuationSendNotice({
  lease,
}: {
  lease: Lease | null | undefined;
}) {
  return lease && isContinuationActive(lease) ? (
    <div className="ai-plan-continuation" role="status">
      发送新消息前，请先停止自动续办。当前草稿不会自动发送。
      <AiStopContinuation lease={lease} />
    </div>
  ) : null;
}
function errorText(error: unknown) {
  const code = error instanceof ApiError ? error.code : "";
  if (code === "AI_CONTINUATION_ACTIVE")
    return "该会话已有自动续办。请先停止后再重新授权；没有自动停止或发送草稿。";
  if (code?.includes("PROVIDER"))
    return "Provider 已变化或不可用，请重新读取并核对；没有重新授权。";
  if (
    code?.includes("PLAN") ||
    code?.includes("VERSION") ||
    code?.includes("CONFLICT")
  )
    return "会话或计划已变化，请重新打开并核对最新状态后授权。";
  return "自动续办请求未确认。请重读状态，不要据此认为已经启动或停止。";
}
export function AiStopContinuation({ lease }: { lease: Lease }) {
  const client = useQueryClient();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  return (
    <>
      <button
        type="button"
        disabled={busy}
        onClick={() => {
          setBusy(true);
          setError("");
          void stopAiPlanContinuation(lease.id)
            .then(async (result) => {
              await client.cancelQueries({
                queryKey: continuationKey(result.session_id),
              });
              await client.cancelQueries({ queryKey: activeContinuationsKey });
              client.setQueryData(continuationKey(result.session_id), result);
              void client.invalidateQueries({
                queryKey: activeContinuationsKey,
              });
              void client.invalidateQueries({
                queryKey: recentContinuationsKey,
              });
            })
            .catch((reason) => setError(errorText(reason)))
            .finally(() => setBusy(false));
        }}
      >
        {busy ? "正在请求停止…" : "停止自动续办"}
      </button>
      {error ? <span role="alert">{error}</span> : null}
    </>
  );
}
export function AiPlanContinuation({
  sessionId,
  planVersion,
  disabled = false,
}: {
  sessionId: string;
  planVersion: number;
  disabled?: boolean;
}) {
  const query = usePlanContinuation(sessionId);
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const [providerId, setProviderId] = useState("");
  const [scopes, setScopes] = useState<AiWorkspaceScope[]>([]);
  const [sources, setSources] = useState<
    NonNullable<AiWorkspaceGrant["knowledge_sources"]>
  >([]);
  const [turns, setTurns] = useState(3);
  const [ttl, setTtl] = useState(30);
  const [resumeAfterRestart, setResumeAfterRestart] = useState(false);
  const [consentIdentity, setConsentIdentity] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const providers = useQuery({
    queryKey: ["ai", "providers"],
    queryFn: getAiProviders,
    enabled: open,
    retry: false,
    staleTime: 0,
  });
  const sessions = useQuery({
    queryKey: ["ai", "sessions"],
    queryFn: getAiSessions,
    enabled: open,
    retry: false,
    staleTime: 0,
  });
  const session = sessions.data?.find((s) => s.id === sessionId);
  const provider = providers.data?.find((p) => p.id === providerId);
  const identity = JSON.stringify([
    sessionId,
    planVersion,
    session?.version,
    session?.persist,
    provider?.id,
    provider?.version,
    provider?.configVersion,
    provider?.kind,
    provider?.protocol,
    provider?.model,
    provider?.status,
    disabled,
    scopes,
    sources,
    turns,
    ttl,
    resumeAfterRestart,
  ]);
  const consent = consentIdentity === identity;
  const setConsent = (confirmed: boolean) =>
    setConsentIdentity(confirmed ? identity : null);
  const owner = useMemo(() => ({}), [identity, open]);
  const ownerRef = useRef(owner);
  ownerRef.current = owner;
  useEffect(() => {
    setConsent(false);
  }, [identity]);
  useEffect(
    () => () => {
      ownerRef.current = {};
    },
    [],
  );
  useEffect(() => {
    setOpen(false);
    setConsent(false);
    setError("");
  }, [sessionId]);
  const lease = query.data;
  const active = isContinuationActive(lease);
  const openConsent = () => {
    setScopes([]);
    setSources([]);
    setProviderId("");
    setTurns(3);
    setTtl(30);
    setResumeAfterRestart(false);
    setConsent(false);
    setError("");
    setOpen(true);
  };
  const valid =
    !!session?.persist &&
    !!provider &&
    provider.status === "ready" &&
    !!provider.configVersion &&
    scopes.includes("work") &&
    scopes.includes("actions") &&
    (!scopes.includes("knowledge") || sources.length > 0) &&
    Number.isInteger(turns) &&
    turns >= 1 &&
    turns <= 8 &&
    Number.isInteger(ttl) &&
    ttl >= 1 &&
    ttl <= 120;
  async function start() {
    if (
      !valid ||
      !consent ||
      !session ||
      !provider ||
      busy ||
      disabled ||
      active ||
      providers.isFetching ||
      sessions.isFetching
    )
      return;
    const owned = owner;
    setBusy(true);
    setError("");
    try {
      const result = await createAiPlanContinuation(sessionId, {
        expected_session_version: session.version,
        expected_plan_version: planVersion,
        provider_id: provider.id,
        expected_provider_version: provider.version,
        expected_provider_config_version: provider.configVersion!,
        workspace: {
          provider_version: provider.version,
          scopes,
          ...(scopes.includes("knowledge")
            ? { knowledge_sources: sources }
            : {}),
        },
        max_turns: turns,
        ttl_minutes: ttl,
        confirm_automatic_continuation: true,
        ...(resumeAfterRestart
          ? {
              resume_after_restart: true,
              confirm_restart_continuation: true as const,
            }
          : {}),
      });
      // The server accepted permission even if this view subsequently moved.
      // Keep metadata globally discoverable, but never affect another draft/view.
      await Promise.all([
        client.cancelQueries({ queryKey: continuationKey(result.session_id) }),
        client.cancelQueries({ queryKey: activeContinuationsKey }),
      ]);
      client.setQueryData(continuationKey(result.session_id), result);
      void client.invalidateQueries({ queryKey: activeContinuationsKey });
      if (ownerRef.current === owned) {
        setOpen(false);
        setConsent(false);
      }
    } catch (reason) {
      if (ownerRef.current === owned) {
        setError(
          resumeAfterRestart &&
            reason instanceof ApiError &&
            reason.code === "INVALID_JSON"
            ? "本地 Sidecar 尚未加载重启续办能力，请更新并重启服务后重新授权。"
            : errorText(reason),
        );
        setConsent(false);
      }
      void query.refetch();
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="ai-plan-continuation" aria-label="计划自动续办">
      {lease ? (
        <>
          <p>
            <strong>自动续办 · {continuationStatuses[lease.status]}</strong> ·
            已启动 {lease.turns_started}/{lease.max_turns} 轮
          </p>
          <p role="status">{continuationReasons[lease.reason]}</p>
          <p>
            {lease.provider.name} · {lease.provider.model} ·{" "}
            {lease.provider.protocol} ·{" "}
            {lease.provider.kind === "remote" ? "远程（内容离开本机）" : "本地"}{" "}
            · Provider v{lease.provider.version}/配置 v
            {lease.provider.config_version}
          </p>
          <p>
            计划 v{lease.initial_plan_version} → v{lease.current_plan_version} ·
            授权到期 {new Date(lease.expires_at).toLocaleString()}
          </p>
          {lease.resume_after_restart ? (
            <p>已允许重启后继续等待中的续办；运行中的生成不会重放。</p>
          ) : null}
          <p>
            范围：
            {lease.workspace.scopes
              .map(
                (s) =>
                  continuationScopeOptions.find((o) => o.id === s)?.label ?? s,
              )
              .join("、")}
          </p>
          {active ? <AiStopContinuation lease={lease} /> : null}
          {!active && lease.current_generation_id ? (
            <p>
              停止已登记，当前生成可能仍在退出；不能撤回已发送内容或保证停止远端计费。
            </p>
          ) : null}
        </>
      ) : null}
      {query.isError ? (
        <p role="status">
          暂时无法读取自动续办状态。
          <button type="button" onClick={() => void query.refetch()}>
            重读续办状态
          </button>
        </p>
      ) : null}
      {!active ? (
        <button
          type="button"
          disabled={disabled || query.isPending || query.isError || busy}
          onClick={openConsent}
        >
          设置自动续办
        </button>
      ) : null}
      <Modal
        open={open}
        title="授权有界自动续办"
        onClose={() => {
          if (!busy) {
            setOpen(false);
            setConsent(false);
          }
        }}
        dismissible={!busy}
        width="680px"
        footer={
          <>
            <button
              type="button"
              disabled={busy}
              onClick={() => setOpen(false)}
            >
              取消
            </button>
            <button
              type="button"
              disabled={
                !valid ||
                !consent ||
                busy ||
                disabled ||
                active ||
                providers.isFetching ||
                sessions.isFetching
              }
              onClick={() => void start()}
            >
              {busy ? "正在授权…" : "确认并开启自动续办"}
            </button>
          </>
        }
      >
        <p>
          仅当前保存会话、计划版本 {planVersion}
          。重新选择本次持续授权，不继承聊天框的单次权限。
        </p>
        <p>
          关闭页面或侧栏后仍会继续；默认在本地服务重启后中断。每轮是一次完整生成，可能含多次模型调用及费用，不是一次
          HTTP 请求。
        </p>
        <p>
          审批、Agent
          运行或待登记期间等待。所有业务操作仍逐项人工确认；停止续办不取消已经批准的
          Agent 执行，也不撤销已执行操作。
        </p>
        <label>
          续办 Provider
          <select
            aria-label="续办 Provider"
            value={providerId}
            disabled={busy}
            onChange={(e) => setProviderId(e.target.value)}
          >
            <option value="">请选择 Provider</option>
            {providers.data
              ?.filter((p) => p.status === "ready")
              .map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name} · {p.model}
                </option>
              ))}
          </select>
        </label>
        {provider ? (
          <p>
            {provider.protocol} ·{" "}
            {provider.kind === "remote"
              ? "远程 Provider：授权范围内的查询正文与本会话上下文可能发送到外部，发送后无法撤回。"
              : "本地 Provider：授权内容会交给配置的本地模型端点。"}{" "}
            版本 {provider.version} / 配置{" "}
            {provider.configVersion ?? "未知（请刷新）"}
          </p>
        ) : null}
        {providers.isError || sessions.isError ? (
          <p role="alert">Provider 或会话读取失败，不能授权。请关闭后重试。</p>
        ) : null}
        {session && !session.persist ? (
          <p role="alert">临时会话不能开启自动续办。</p>
        ) : null}
        <fieldset disabled={busy}>
          <legend>本次持续授权的范围（不限定于计划中的单条记录）</legend>
          <button
            type="button"
            onClick={() => {
              setScopes(["work", "outputs", "actions"]);
              setSources([]);
            }}
          >
            采用推荐范围：工作事项、产出、操作建议
          </button>
          {continuationScopeOptions.map((s) => (
            <label className="ai-continuation-scope" key={s.id}>
              <input
                type="checkbox"
                checked={scopes.includes(s.id)}
                onChange={(e) => {
                  setScopes((previous) =>
                    toggleContinuationScope(previous, s.id, e.target.checked),
                  );
                  if (s.id === "knowledge" && !e.target.checked) setSources([]);
                }}
              />
              <span>
                {s.label}
                <small>{s.description}</small>
              </span>
            </label>
          ))}
          <p>
            至少选择工作事项和操作建议。不会授权工作区/浏览器导航，不读取右侧文件、Git、终端或浏览器内容。授予正文查询时，授权并不等于模型已经读完或事实已验收。
          </p>
        </fieldset>
        {scopes.includes("knowledge") ? (
          <AiKnowledgeAccess
            value={sources}
            onChange={setSources}
            disabled={busy}
          />
        ) : null}
        <label>
          最多生成轮数
          <input
            aria-label="最多生成轮数"
            type="number"
            min={1}
            max={8}
            value={turns}
            disabled={busy}
            onChange={(e) => setTurns(Number(e.target.value))}
          />
        </label>
        <label>
          授权时限（分钟，包含等待）
          <input
            aria-label="授权时限（分钟）"
            type="number"
            min={1}
            max={120}
            value={ttl}
            disabled={busy}
            onChange={(e) => setTtl(Number(e.target.value))}
          />
        </label>
        <p>
          最多 {turns} 轮 / {ttl}{" "}
          分钟；无进展、错误、事实漂移、预算或时限到达时停止。不是金额上限，也不会自动通过验收。
        </p>
        <label>
          <input
            type="checkbox"
            checked={resumeAfterRestart}
            disabled={busy}
            onChange={(e) => setResumeAfterRestart(e.target.checked)}
          />
          我另行同意：本地服务重启后，仅在授权时限内继续尚未发出模型请求的等待项；重启时正在生成的请求会中断，不重放。恢复前仍会核验计划、Provider、权限与剩余轮数。
        </label>
        <label>
          <input
            type="checkbox"
            checked={consent}
            disabled={!valid || busy}
            onChange={(e) => setConsent(e.target.checked)}
          />
          我同意按以上
          Provider、范围、轮数和时限自动生成后续回复，业务操作仍由我逐项确认。
        </label>
        {error ? <p role="alert">{error}</p> : null}
      </Modal>
    </section>
  );
}

function invalidateSession(
  client: ReturnType<typeof useQueryClient>,
  sessionId: string,
) {
  for (const key of [
    ["ai", "messages", sessionId],
    ["ai", "work-plan", sessionId],
    ["ai", "sessions"],
    ["ai", "agent-inbox"],
    ["ai", "work-plans", "attention"],
    ["ai", "usage-summary", sessionId],
  ])
    void client.invalidateQueries({ queryKey: key });
}

/** Application-level observation only. No timer here starts a generation. */
export function AiContinuationMonitor() {
  const query = useQuery({
    queryKey: activeContinuationsKey,
    queryFn: ({ signal }) => getActiveAiPlanContinuations(signal),
    refetchInterval: 3000,
    retry: false,
  });
  const recent = useQuery({
    queryKey: recentContinuationsKey,
    queryFn: ({ signal }) => getRecentAiPlanContinuations(signal),
    refetchInterval: 10000,
    retry: false,
  });
  const client = useQueryClient();
  const previous = useRef<Lease[]>([]);
  useEffect(() => {
    if (!query.data) return;
    const old = previous.current;
    previous.current = query.data;
    for (const lease of query.data) {
      const cached = client.getQueryData<Lease | null>(
        continuationKey(lease.session_id),
      );
      if (!cached || cached.id !== lease.id || cached.version <= lease.version)
        client.setQueryData(continuationKey(lease.session_id), lease);
      if (!old.some((l) => l.id === lease.id && l.version === lease.version))
        invalidateSession(client, lease.session_id);
    }
    for (const ended of old.filter(
      (l) => !query.data.some((n) => n.id === l.id),
    )) {
      void client.invalidateQueries({
        queryKey: continuationKey(ended.session_id),
      });
      invalidateSession(client, ended.session_id);
      void client.invalidateQueries({ queryKey: recentContinuationsKey });
    }
  }, [query.data, client]);
  if (
    !query.data?.length &&
    !recent.data?.length &&
    !query.isError &&
    !recent.isError
  )
    return null;
  return (
    <aside className="ai-plan-continuation" aria-label="后台自动续办状态">
      {query.data?.map((lease) => (
        <div key={lease.id}>
          <Link to={`/ai?session=${lease.session_id}`}>
            {lease.session_title ?? lease.provider.name} · 自动续办{" "}
            {lease.turns_started}/{lease.max_turns} 轮
          </Link>
          <span> · {continuationReasons[lease.reason]} </span>
          <AiStopContinuation lease={lease} />
        </div>
      ))}
      {recent.data?.length ? (
        <details className="ai-continuation-recent">
          <summary>最近 24 小时的自动续办结果（{recent.data.length}）</summary>
          {recent.data.map((lease) => (
            <div key={lease.id}>
              <Link to={`/ai?session=${lease.session_id}`}>
                {lease.session_title ?? lease.provider.name} ·{" "}
                {continuationStatuses[lease.status]} · 已启动{" "}
                {lease.turns_started}/{lease.max_turns} 轮
              </Link>
              <span> · {continuationReasons[lease.reason]}</span>
            </div>
          ))}
        </details>
      ) : null}
      {query.isError || recent.isError ? (
        <p role="alert">
          后台续办状态暂不可完整读取，不能据此判断是否仍在运行或已经结束。
        </p>
      ) : null}
    </aside>
  );
}

export function AiAutomaticReply({
  sessionId,
  knownGenerationIds,
}: {
  sessionId: string;
  knownGenerationIds: string[];
}) {
  const lease = usePlanContinuation(sessionId);
  const client = useQueryClient();
  const generationId =
    lease.data?.current_generation_id ?? lease.data?.last_generation_id;
  const generation = useQuery({
    queryKey: ["ai", "automatic-generation", generationId],
    queryFn: async () => {
      const result = await getAiGeneration(generationId!);
      if (
        !lease.data ||
        !result.origin ||
        result.session_id !== sessionId ||
        result.origin.continuation_id !== lease.data.id ||
        result.origin.max_turns !== lease.data.max_turns ||
        result.origin.turn_index > lease.data.turns_started
      )
        throw new ApiError("自动生成来源不一致", { code: "INVALID_RESPONSE" });
      return result;
    },
    enabled: !!generationId && !knownGenerationIds.includes(generationId),
    retry: false,
    refetchInterval: (q) =>
      q.state.status === "error"
        ? isContinuationActive(lease.data)
          ? 3000
          : false
        : !q.state.data || ["queued", "streaming"].includes(q.state.data.status)
          ? 1500
          : false,
  });
  const value = generation.data;
  useEffect(() => {
    if (value) invalidateSession(client, sessionId);
  }, [value?.id, value?.status, client, sessionId]);
  if (!generationId || knownGenerationIds.includes(generationId)) return null;
  if (generation.isError)
    return (
      <p role="status">
        自动回复暂不可读取；没有重新发送或重新授权。
        <button type="button" onClick={() => void generation.refetch()}>
          重读自动回复
        </button>
      </p>
    );
  if (!value) return null;
  return (
    <article
      className="ai-msg-assistant ai-automatic-reply"
      aria-label="自动续办回复"
    >
      <AiAutomaticOrigin origin={value.origin} />
      <p>
        服务端后台生成 ·{" "}
        {value.status === "streaming" || value.status === "queued"
          ? "进行中"
          : value.status === "completed"
            ? "已结束"
            : value.status === "cancelled"
              ? "已取消"
              : "失败"}
      </p>
      <AiRunProgress
        steps={value.progress}
        live={["queued", "streaming"].includes(value.status)}
      />
      {value.reasoning ? (
        <details>
          <summary>思考过程</summary>
          <p>{value.reasoning}</p>
        </details>
      ) : null}
      {renderAiRichText(value.content, sessionId)}
      <AiCitationEvidence
        status={value.citationEvidence?.status ?? null}
        citations={value.citationEvidence?.items ?? []}
        sessionId={sessionId}
      />
      {lease.data && isContinuationActive(lease.data) ? (
        <AiStopContinuation lease={lease.data} />
      ) : null}
    </article>
  );
}
