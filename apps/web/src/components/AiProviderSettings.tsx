import {
  AlertCircle,
  Bot,
  CheckCircle2,
  KeyRound,
  LoaderCircle,
  PlugZap,
  Plus,
  RefreshCw,
  Trash2,
} from "lucide-react";
import { useState } from "react";
import { ApiError } from "../api/client";
import {
  useAiMemoriesQuery,
  useAiProvidersQuery,
  useCheckAiProviderHealth,
  useCreateAiProvider,
  useDeleteAiMemory,
  useDeleteAiProvider,
  useSetAiProviderKey,
} from "../api/hooks";
import type {
  AiProvider,
  AiProviderKind,
  AiProviderProtocol,
} from "../types/models";

function aiProviderError(error: unknown, fallback: string): string {
  if (error instanceof ApiError) {
    if (error.code === "AI_KEY_STORE_UNAVAILABLE") {
      return "操作系统安全存储不可用，无法保存 API 密钥；已拒绝保存，未写入任何明文。";
    }
    if (error.code === "AI_KEY_NOT_ALLOWED") {
      return "本地部署供应商不需要也不保存 API 密钥。";
    }
    if (error.code === "VERSION_CONFLICT") {
      return "供应商配置已变化，已刷新最新状态，请重试。";
    }
    return `${error.message}${error.requestId ? ` · 请求 ${error.requestId}` : ""}`;
  }
  return fallback;
}

const protocolLabels: Record<AiProviderProtocol, string> = {
  openai_chat: "OpenAI Chat Completions 兼容",
  anthropic_messages: "Anthropic Messages 兼容",
};

const healthErrorLabels: Record<string, string> = {
  AI_KEY_UNAVAILABLE: "尚未保存 API 密钥",
  AI_KEY_INVALID: "API 密钥被拒绝（401/403）",
  AI_ENDPOINT_UNREACHABLE: "端点无法访问",
  AI_PROVIDER_ERROR: "端点返回错误",
};

export function AiProviderSettings() {
  const providers = useAiProvidersQuery();
  const [formOpen, setFormOpen] = useState(false);

  if (providers.isPending) {
    return (
      <div aria-live="polite" className="settings-state" role="status">
        <LoaderCircle className="animate-spin" size={16} />
        正在读取 AI 供应商…
      </div>
    );
  }
  if (providers.isError || !providers.data) {
    return (
      <div className="settings-state settings-state-error" role="alert">
        <AlertCircle size={16} />
        <div>
          <strong>无法读取 AI 供应商设置</strong>
          <span>{aiProviderError(providers.error, "读取失败")}</span>
        </div>
        <button
          className="button button-secondary"
          onClick={() => void providers.refetch()}
          type="button"
        >
          <RefreshCw size={14} />
          重试
        </button>
      </div>
    );
  }

  const providerList = providers.data;

  return (
    <div className="ai-provider-settings">
      <header className="settings-content-header">
        <h3>AI 助手</h3>
        <p>
          可登记多个大模型供应商：远程 API（API key 模式）或本地部署 （Ollama /
          LM Studio 等 OpenAI 兼容端点，无需密钥），聊天页可随时切换。
          密钥只保存在操作系统安全存储，不进入数据库、日志或导出。
        </p>
      </header>

      {providerList.map((provider) => (
        <AiProviderCard key={provider.id} provider={provider} />
      ))}

      {formOpen || providerList.length === 0 ? (
        <AiProviderForm onDone={() => setFormOpen(false)} />
      ) : (
        <div className="ai-provider-footer">
          <button
            className="button button-secondary"
            onClick={() => setFormOpen(true)}
            type="button"
          >
            <Plus size={14} />
            添加供应商
          </button>
        </div>
      )}
      <AiMemoryList />
    </div>
  );
}

// AiMemoryList manages the user-confirmed long-term preferences the agent
// reuses across sessions (ADR-006).
function AiMemoryList() {
  const memories = useAiMemoriesQuery();
  const deleteMemory = useDeleteAiMemory();
  return (
    <section className="ai-memory-settings">
      <h4>长期记忆</h4>
      <p>你在对话中确认记住的偏好；会注入后续会话，可随时删除。</p>
      {memories.isPending ? (
        <div className="settings-state" role="status">
          <LoaderCircle className="animate-spin" size={16} />
          正在读取记忆…
        </div>
      ) : memories.isError || !memories.data ? (
        <div className="settings-state settings-state-error" role="alert">
          <AlertCircle size={16} />
          无法读取长期记忆
        </div>
      ) : memories.data.length === 0 ? (
        <div className="settings-state">暂无已确认的记忆。</div>
      ) : (
        <ul className="ai-memory-rows">
          {memories.data.map((memory) => (
            <li key={memory.id}>
              <span>{memory.content}</span>
              <button
                aria-label={`删除记忆：${memory.content}`}
                className="button button-quiet"
                disabled={deleteMemory.isPending}
                onClick={() => void deleteMemory.mutateAsync({ id: memory.id })}
                type="button"
              >
                <Trash2 size={13} />
                删除
              </button>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function AiProviderCard({ provider }: { provider: AiProvider }) {
  const setKey = useSetAiProviderKey();
  const checkHealth = useCheckAiProviderHealth();
  const deleteProvider = useDeleteAiProvider();
  const [apiKey, setApiKey] = useState("");
  const [feedback, setFeedback] = useState<string | null>(null);
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const actionError = setKey.error ?? checkHealth.error ?? deleteProvider.error;

  async function saveKey() {
    try {
      setFeedback(null);
      await setKey.mutateAsync({
        id: provider.id,
        apiKey: apiKey.trim(),
        expectedVersion: provider.version,
      });
      setApiKey("");
      setFeedback("API 密钥已保存到系统安全存储");
    } catch {
      // mutation state renders a safe error below
    }
  }

  async function runHealthCheck() {
    try {
      setFeedback(null);
      const next = await checkHealth.mutateAsync({
        id: provider.id,
        expectedVersion: provider.version,
      });
      setFeedback(
        next.status === "ready" ? "连接成功" : "连接失败，请检查密钥与端点",
      );
    } catch {
      // mutation state renders a safe error below
    }
  }

  async function remove() {
    try {
      setFeedback(null);
      await deleteProvider.mutateAsync({
        id: provider.id,
        expectedVersion: provider.version,
      });
      setConfirmingDelete(false);
      setFeedback(`供应商 ${provider.name} 与其密钥已删除`);
    } catch {
      // mutation state renders a safe error below
    }
  }

  return (
    <section className="ai-provider-card">
      <div className="ai-provider-heading">
        <div className="ai-provider-icon">
          <Bot size={18} />
        </div>
        <div>
          <h4>{provider.name}</h4>
          <p>
            {provider.kind === "local" ? "本地部署 · " : null}
            {protocolLabels[provider.protocol]} · {provider.model}
          </p>
        </div>
        <span
          data-status={provider.health_status}
          title={
            provider.health_error_code
              ? (healthErrorLabels[provider.health_error_code] ??
                provider.health_error_code)
              : undefined
          }
        >
          {provider.status === "ready" ? "已就绪" : "未就绪"}
        </span>
      </div>
      {provider.health_error_code ? (
        <div className="ai-provider-health-note" role="status">
          <AlertCircle size={14} />
          {healthErrorLabels[provider.health_error_code] ??
            provider.health_error_code}
        </div>
      ) : null}
      {provider.kind === "local" ? (
        <div className="ai-provider-health-note" role="status">
          <Bot size={14} />
          本地部署供应商，无需 API 密钥。
        </div>
      ) : (
        <div className="ai-provider-actions">
          <label>
            <KeyRound size={14} />
            <input
              autoComplete="off"
              onChange={(event) => setApiKey(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter" && apiKey.trim()) {
                  void saveKey();
                }
              }}
              placeholder={
                provider.has_key ? "已保存（输入可更换）" : "API key"
              }
              type="password"
              value={apiKey}
            />
          </label>
          <button
            className="button button-secondary"
            disabled={setKey.isPending || !apiKey.trim()}
            onClick={() => void saveKey()}
            type="button"
          >
            保存密钥
          </button>
        </div>
      )}
      <div className="ai-provider-actions">
        <button
          className="button button-secondary"
          disabled={checkHealth.isPending}
          onClick={() => void runHealthCheck()}
          type="button"
        >
          {checkHealth.isPending ? (
            <LoaderCircle className="animate-spin" size={14} />
          ) : (
            <PlugZap size={14} />
          )}
          测试连接
        </button>
      </div>
      {confirmingDelete ? (
        <div className="ai-provider-delete-confirm">
          <span>删除供应商会同时清除安全存储中的密钥，确定？</span>
          <button
            className="button button-secondary"
            disabled={deleteProvider.isPending}
            onClick={() => void remove()}
            type="button"
          >
            确认删除
          </button>
          <button
            className="button button-quiet"
            onClick={() => setConfirmingDelete(false)}
            type="button"
          >
            取消
          </button>
        </div>
      ) : (
        <div className="ai-provider-footer">
          <button
            className="button button-quiet"
            onClick={() => setConfirmingDelete(true)}
            type="button"
          >
            <Trash2 size={14} />
            删除供应商
          </button>
        </div>
      )}
      {feedback ? (
        <div className="automation-success" role="status">
          <CheckCircle2 size={15} />
          {feedback}
        </div>
      ) : null}
      {actionError ? (
        <div className="automation-error" role="alert">
          <AlertCircle size={15} />
          {aiProviderError(actionError, "操作失败，请重试")}
        </div>
      ) : null}
    </section>
  );
}

function AiProviderForm({ onDone }: { onDone: () => void }) {
  const createProvider = useCreateAiProvider();
  const [name, setName] = useState("");
  const [kind, setKind] = useState<AiProviderKind>("remote");
  const [protocol, setProtocol] = useState<AiProviderProtocol>("openai_chat");
  const [baseUrl, setBaseUrl] = useState("");
  const [model, setModel] = useState("");
  const localMode = kind === "local";

  async function register() {
    try {
      await createProvider.mutateAsync({
        name: name.trim(),
        kind,
        protocol: localMode ? "openai_chat" : protocol,
        base_url: baseUrl.trim(),
        model: model.trim(),
      });
      onDone();
    } catch {
      // mutation state renders a safe error below
    }
  }

  return (
    <section className="ai-provider-form">
      <label>
        名称
        <input
          onChange={(event) => setName(event.target.value)}
          placeholder="例如 DeepSeek"
          value={name}
        />
      </label>
      <label>
        类型
        <select
          onChange={(event) => setKind(event.target.value as AiProviderKind)}
          value={kind}
        >
          <option value="remote">远程 API（API key）</option>
          <option value="local">本地部署（无需密钥）</option>
        </select>
      </label>
      {localMode ? (
        <div className="ai-provider-health-note" role="status">
          <AlertCircle size={14} />
          本地部署使用 OpenAI 兼容端点，例如 Ollama
          <code>http://127.0.0.1:11434/v1</code> 或 LM Studio
          <code>http://127.0.0.1:1234/v1</code>；不保存任何密钥。
        </div>
      ) : null}
      <label>
        协议
        <select
          disabled={localMode}
          onChange={(event) =>
            setProtocol(event.target.value as AiProviderProtocol)
          }
          value={localMode ? "openai_chat" : protocol}
        >
          <option value="openai_chat">{protocolLabels.openai_chat}</option>
          {localMode ? null : (
            <option value="anthropic_messages">
              {protocolLabels.anthropic_messages}
            </option>
          )}
        </select>
      </label>
      <label>
        Base URL
        <input
          onChange={(event) => setBaseUrl(event.target.value)}
          placeholder={
            localMode
              ? "http://127.0.0.1:11434/v1"
              : "https://api.example.com/v1"
          }
          value={baseUrl}
        />
      </label>
      <label>
        模型名
        <input
          onChange={(event) => setModel(event.target.value)}
          placeholder={localMode ? "例如 qwen3" : "例如 deepseek-chat"}
          value={model}
        />
      </label>
      <div className="ai-provider-footer">
        <button
          className="button button-primary"
          disabled={
            createProvider.isPending ||
            !name.trim() ||
            !baseUrl.trim() ||
            !model.trim()
          }
          onClick={() => void register()}
          type="button"
        >
          {createProvider.isPending ? (
            <LoaderCircle className="animate-spin" size={14} />
          ) : (
            <Bot size={14} />
          )}
          登记供应商
        </button>
        <button className="button button-quiet" onClick={onDone} type="button">
          取消
        </button>
      </div>
      {createProvider.error ? (
        <div className="automation-error" role="alert">
          <AlertCircle size={15} />
          {aiProviderError(createProvider.error, "登记失败，请重试")}
        </div>
      ) : null}
    </section>
  );
}
