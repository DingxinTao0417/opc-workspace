export type AgentAdapterStatus = "enabled" | "disabled";

export type AgentAdapterHealthStatus =
  "unknown" | "blocked" | "healthy" | "unhealthy";

export type AgentAdapterIsolationStatus =
  "unverified" | "verified" | "unsupported";

export interface AgentAdapterManifest {
  executionMode: "short_lived_process";
  capabilities: string[];
  requirements: string[];
}

export interface AgentAdapter {
  id: string;
  adapterKey: string;
  kind: "builtin";
  displayName: string;
  protocolVersion: "opc-agent-pipe-v1";
  manifest: AgentAdapterManifest;
  status: AgentAdapterStatus;
  healthStatus: AgentAdapterHealthStatus;
  healthErrorCode: string | null;
  isolationStatus: AgentAdapterIsolationStatus;
  executionReady: boolean;
  lastHealthAt: string | null;
  readiness: {
    canEnable: boolean;
    unavailableCode: string;
    requiredGates: string[];
  };
  version: number;
  createdAt: string;
  updatedAt: string;
}
