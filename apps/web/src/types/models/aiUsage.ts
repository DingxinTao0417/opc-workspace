import type { AiProviderKind, AiProviderProtocol } from "./ai";

export type AiRunStepKind =
  | "generation"
  | "model_turn"
  | "tool_call"
  | "self_check"
  | "citation_validation"
  | "persistence";

export type AiRunStepStatus = "running" | "succeeded" | "failed" | "cancelled";

export interface AiRunStep {
  id: string;
  generationId: string;
  sequence: number;
  kind: AiRunStepKind;
  status: AiRunStepStatus;
  turnIndex: number | null;
  toolName: string | null;
  startedAt: string;
  completedAt: string | null;
  durationMs: number | null;
  inputBytes: number;
  outputBytes: number;
  inputTokens: number | null;
  outputTokens: number | null;
  tokenSource: "provider" | null;
  errorCode: string | null;
}

export interface AiRunStepListResult {
  items: AiRunStep[];
  meta: {
    generationId: string;
    status: "queued" | "streaming" | "completed" | "failed" | "cancelled";
    total: number;
    inputBytes: number;
    outputBytes: number;
    durationMs: number;
    inputTokens: number | null;
    outputTokens: number | null;
    tokenSource: "provider" | null;
  };
}

export interface AiUsageTotals {
  totalGenerations: number;
  completedGenerations: number;
  failedGenerations: number;
  cancelledGenerations: number;
  activeGenerations: number;
  providerUsageGenerations: number;
  unknownUsageGenerations: number;
  inputTokens: number;
  outputTokens: number;
  inputBytes: number;
  outputBytes: number;
  durationMs: number;
}

export interface AiProviderUsageSummary extends AiUsageTotals {
  providerId: string;
  providerName: string;
  providerKind: AiProviderKind;
  providerProtocol: AiProviderProtocol;
  model: string;
}

export interface AiUsageTrendPoint extends AiUsageTotals {
  day: string;
}

export interface AiUsageSummary {
  scope: {
    sessionId: string | null;
    providerId: string | null;
  };
  totals: AiUsageTotals;
  providers: AiProviderUsageSummary[];
  trendDays: number;
  trend: AiUsageTrendPoint[];
}
