import {
  AlertCircle,
  BellRing,
  CheckCircle2,
  Clock3,
  Inbox,
  LoaderCircle,
  RefreshCw,
  RotateCcw,
  ShieldCheck,
  Zap,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { ApiError } from "../api/client";
import {
  useAutomationRulesQuery,
  useAutomationRunsQuery,
  usePreviewAutomationRule,
  useRetryAutomationRun,
  useSetAutomationRuleEnabled,
  useUpdateAutomationRule,
} from "../api/hooks";
import type {
  AutomationConfig,
  AutomationRule,
  AutomationRun,
} from "../types/models";

function automationError(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.code === "VERSION_CONFLICT") {
      return "规则已在另一个窗口改变，已刷新最新状态，请确认后重试。";
    }
    return `${error.message}${error.requestId ? ` · 请求 ${error.requestId}` : ""}`;
  }
  return "自动化操作失败，请确认本地服务已就绪后重试。";
}

function sameConfig(left: AutomationConfig, right: AutomationConfig): boolean {
  return JSON.stringify(left) === JSON.stringify(right);
}

function formatDateTime(value: string | null): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(date);
}

function runStatusLabel(run: AutomationRun): string {
  if (run.status === "succeeded") return "成功";
  if (run.status === "failed") return "失败";
  if (run.status === "skipped") return "已折叠";
  return "已取消";
}

function ruleStatusLabel(rule: AutomationRule): string {
  if (rule.status === "enabled") return "已启用";
  if (rule.status === "unavailable") return "待依赖";
  return "未启用";
}

export function AutomationSettings() {
  const rulesQuery = useAutomationRulesQuery();
  const runsQuery = useAutomationRunsQuery({ pageSize: 20 });
  const preview = usePreviewAutomationRule();
  const updateRule = useUpdateAutomationRule();
  const setEnabled = useSetAutomationRuleEnabled();
  const retryRun = useRetryAutomationRun();
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [draft, setDraft] = useState<AutomationConfig>({});
  const [feedback, setFeedback] = useState<string | null>(null);

  const selected = useMemo(
    () => rulesQuery.data?.find((rule) => rule.id === selectedId) ?? null,
    [rulesQuery.data, selectedId],
  );

  useEffect(() => {
    if (!rulesQuery.data?.length) return;
    if (
      !selectedId ||
      !rulesQuery.data.some((rule) => rule.id === selectedId)
    ) {
      setSelectedId(rulesQuery.data[0].id);
    }
  }, [rulesQuery.data, selectedId]);

  useEffect(() => {
    if (!selected) return;
    setDraft(selected.config);
    setFeedback(null);
  }, [selected?.id, selected?.version]);

  useEffect(() => {
    if (!selected) return;
    const timer = window.setTimeout(() => {
      preview.mutate({ id: selected.id, config: draft });
    }, 250);
    return () => window.clearTimeout(timer);
    // The mutation object is intentionally excluded; only the selected draft
    // should schedule a new server-authoritative preview.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selected?.id, draft]);

  const pending = updateRule.isPending || setEnabled.isPending;
  const dirty = selected ? !sameConfig(selected.config, draft) : false;
  const actionError = updateRule.error ?? setEnabled.error ?? preview.error;

  async function saveDraft(): Promise<AutomationRule | null> {
    if (!selected) return null;
    if (!dirty) return selected;
    const saved = await updateRule.mutateAsync({
      id: selected.id,
      config: draft,
      expectedVersion: selected.version,
    });
    setDraft(saved.config);
    setFeedback("配置已保存");
    return saved;
  }

  async function toggleRule() {
    if (!selected || !selected.available) return;
    try {
      setFeedback(null);
      const current = (await saveDraft()) ?? selected;
      const next = await setEnabled.mutateAsync({
        id: current.id,
        enabled: current.status !== "enabled",
        expectedVersion: current.version,
      });
      setFeedback(next.status === "enabled" ? "自动化已启用" : "自动化已停用");
    } catch {
      // The mutation exposes the safe user-facing error below.
    }
  }

  async function save() {
    try {
      setFeedback(null);
      await saveDraft();
    } catch {
      // The mutation exposes the safe user-facing error below.
    }
  }

  if (rulesQuery.isPending) {
    return (
      <div aria-live="polite" className="settings-state" role="status">
        <LoaderCircle className="animate-spin" size={16} />
        正在读取本地自动化规则…
      </div>
    );
  }

  if (rulesQuery.isError || !rulesQuery.data) {
    return (
      <div className="settings-state settings-state-error" role="alert">
        <AlertCircle size={16} />
        <div>
          <strong>无法读取自动化设置</strong>
          <span>{automationError(rulesQuery.error)}</span>
        </div>
        <button
          className="button button-secondary"
          onClick={() => void rulesQuery.refetch()}
          type="button"
        >
          <RefreshCw size={14} />
          重试
        </button>
      </div>
    );
  }

  if (!selected) {
    return (
      <div className="automation-empty">
        <Zap size={18} />
        <strong>暂无自动化预设</strong>
        <span>本地服务尚未提供可配置规则。</span>
      </div>
    );
  }

  const localTimezone =
    Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";

  return (
    <div className="automation-settings">
      <header className="settings-content-header">
        <h3>自动化</h3>
        <p>启用受限的本地预设，让明确事件或时间触发 Inbox 与 Reminder 动作。</p>
      </header>

      <div className="automation-boundary-note">
        <ShieldCheck size={16} />
        <span>
          仅执行白名单本地动作；不运行脚本、SQL、HTTP，不对外发送内容。
        </span>
      </div>

      <div className="automation-rule-tabs" aria-label="自动化预设">
        {rulesQuery.data.map((rule) => (
          <button
            aria-current={rule.id === selected.id ? "true" : undefined}
            className="automation-rule-tab"
            data-active={rule.id === selected.id}
            key={rule.id}
            onClick={() => setSelectedId(rule.id)}
            type="button"
          >
            <span>{rule.name}</span>
            <small data-status={rule.status}>{ruleStatusLabel(rule)}</small>
          </button>
        ))}
      </div>

      <section className="automation-editor">
        <div className="automation-editor-heading">
          <div>
            <h4>{selected.name}</h4>
            <p>{selected.description}</p>
          </div>
          <button
            aria-label={
              selected.status === "enabled" ? "停用自动化" : "启用自动化"
            }
            aria-pressed={selected.status === "enabled"}
            className="settings-toggle"
            data-checked={selected.status === "enabled"}
            disabled={pending || !selected.available}
            onClick={() => void toggleRule()}
            type="button"
          >
            <span />
          </button>
        </div>

        <div className="automation-flow">
          <div>
            <Clock3 size={15} />
            <span>触发</span>
            <strong>{selected.triggerLabel}</strong>
          </div>
          <div>
            <Inbox size={15} />
            <span>动作</span>
            <strong>{selected.actionLabel}</strong>
          </div>
        </div>

        {selected.triggerType === "event" ? (
          <label className="automation-field">
            <span>事项优先级</span>
            <select
              disabled={!selected.available || pending}
              onChange={(event) =>
                setDraft({
                  priority: event.target.value as AutomationConfig["priority"],
                })
              }
              value={draft.priority ?? "P1"}
            >
              <option value="P0">P0 · 紧急</option>
              <option value="P1">P1 · 高</option>
              <option value="P2">P2 · 中</option>
              <option value="P3">P3 · 低</option>
            </select>
          </label>
        ) : (
          <div className="automation-schedule-fields">
            <label className="automation-field">
              <span>当地时间</span>
              <input
                disabled={!selected.available || pending}
                onChange={(event) =>
                  setDraft((current) => ({
                    ...current,
                    localTime: event.target.value,
                  }))
                }
                type="time"
                value={draft.localTime ?? "09:00"}
              />
            </label>
            <label className="automation-field automation-timezone-field">
              <span>IANA 时区</span>
              <div>
                <input
                  disabled={!selected.available || pending}
                  onChange={(event) =>
                    setDraft((current) => ({
                      ...current,
                      timezone: event.target.value,
                    }))
                  }
                  spellCheck={false}
                  value={draft.timezone ?? "UTC"}
                />
                <button
                  className="button button-secondary"
                  disabled={!selected.available || pending}
                  onClick={() =>
                    setDraft((current) => ({
                      ...current,
                      timezone: localTimezone,
                    }))
                  }
                  type="button"
                >
                  使用本机时区
                </button>
              </div>
            </label>
          </div>
        )}

        {!selected.available ? (
          <div className="automation-unavailable" role="note">
            <AlertCircle size={15} />
            <span>{selected.unavailableReason}</span>
          </div>
        ) : null}

        <div className="automation-preview" aria-live="polite">
          <div className="automation-preview-heading">
            <strong>执行预览</strong>
            {preview.isPending ? (
              <LoaderCircle className="animate-spin" size={14} />
            ) : null}
          </div>
          {preview.data ? (
            <>
              <p>
                {preview.data.triggerSummary} → {preview.data.actionSummary}
              </p>
              <span>下次计划：{formatDateTime(preview.data.nextRunAt)}</span>
              <ul>
                {preview.data.permissions.map((permission) => (
                  <li key={permission}>{permission}</li>
                ))}
              </ul>
            </>
          ) : (
            <span>
              {preview.isError
                ? automationError(preview.error)
                : "正在生成预览…"}
            </span>
          )}
        </div>

        <div className="automation-editor-actions">
          <button
            className="button button-quiet"
            disabled={!dirty || pending}
            onClick={() => setDraft(selected.config)}
            type="button"
          >
            <RotateCcw size={14} />
            撤销修改
          </button>
          <button
            className="button button-primary"
            disabled={
              !dirty || pending || !selected.available || preview.isError
            }
            onClick={() => void save()}
            type="button"
          >
            {updateRule.isPending ? (
              <LoaderCircle className="animate-spin" size={14} />
            ) : null}
            保存配置
          </button>
        </div>

        {feedback ? (
          <p className="automation-success">
            <CheckCircle2 size={14} />
            {feedback}
          </p>
        ) : null}
        {actionError ? (
          <p className="automation-error" role="alert">
            <AlertCircle size={14} />
            {automationError(actionError)}
          </p>
        ) : null}
      </section>

      <section className="automation-history">
        <div className="automation-history-heading">
          <div>
            <h4>最近运行</h4>
            <p>每次尝试都保留独立审计记录，失败重试不会覆盖原记录。</p>
          </div>
          <button
            className="button button-secondary"
            disabled={runsQuery.isFetching}
            onClick={() => void runsQuery.refetch()}
            type="button"
          >
            <RefreshCw
              className={runsQuery.isFetching ? "animate-spin" : undefined}
              size={14}
            />
            刷新
          </button>
        </div>
        {runsQuery.isError ? (
          <div className="automation-error" role="alert">
            <AlertCircle size={14} />
            {automationError(runsQuery.error)}
          </div>
        ) : runsQuery.isPending ? (
          <div className="settings-state">
            <LoaderCircle className="animate-spin" size={15} />
            正在读取运行记录…
          </div>
        ) : runsQuery.data?.items.length ? (
          <div className="automation-run-list">
            {runsQuery.data.items.map((run) => (
              <article
                className="automation-run"
                data-status={run.status}
                key={run.id}
              >
                <div className="automation-run-icon">
                  <BellRing size={15} />
                </div>
                <div className="automation-run-copy">
                  <div>
                    <strong>{run.ruleName}</strong>
                    <span>
                      {runStatusLabel(run)} · 第 {run.attempt} 次
                    </span>
                  </div>
                  <p>
                    {run.resultSummary || run.errorCode || "未返回执行摘要"}
                  </p>
                  <small>
                    {formatDateTime(run.startedAt)}
                    {run.scheduledFor
                      ? ` · 计划 ${formatDateTime(run.scheduledFor)}`
                      : ""}
                  </small>
                </div>
                {run.status === "failed" && run.retryable ? (
                  <button
                    className="button button-secondary"
                    disabled={retryRun.isPending}
                    onClick={() => retryRun.mutate(run.id)}
                    type="button"
                  >
                    重试
                  </button>
                ) : null}
              </article>
            ))}
          </div>
        ) : (
          <div className="automation-empty">
            <Zap size={17} />
            <strong>暂无运行记录</strong>
            <span>启用规则并满足触发条件后，记录会显示在这里。</span>
          </div>
        )}
        {retryRun.error ? (
          <p className="automation-error" role="alert">
            <AlertCircle size={14} />
            {automationError(retryRun.error)}
          </p>
        ) : null}
      </section>
    </div>
  );
}
