import type { AiProvider } from "../types/models";

/** Runtime support is narrower than the chat Provider registry. */
export function supportsAgentRunProvider(
  provider: Pick<AiProvider, "protocol" | "kind">,
): boolean {
  return (
    (provider.protocol === "openai_chat" &&
      (provider.kind === "local" || provider.kind === "remote")) ||
    (provider.protocol === "anthropic_messages" && provider.kind === "remote")
  );
}

export const agentRunProviderDisclosure = (
  provider: Pick<AiProvider, "protocol">,
) =>
  provider.protocol === "anthropic_messages"
    ? "Anthropic Messages · v5 · 最多 8192 输出 tokens；只接受完整文本结束，截断或拒绝不会登记产出。"
    : "OpenAI Chat Completions · 只接受完整文本结束，截断或拒绝不会登记产出。";
