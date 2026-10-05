import type { PageMeta } from "./common";

export type ActorType = "owner" | "person" | "system" | "agent";

export type ActorStatus = "active" | "inactive";

export interface Actor {
  id: string;
  type: ActorType;
  displayName: string;
  status: ActorStatus;
  isBuiltin: boolean;
  notes: string;
  metadata: Record<string, unknown>;
  /** Read-only link; older API responses may omit it. */
  agentAdapterId?: string | null;
  version: number;
  createdAt: string;
  updatedAt: string;
}

export interface ActorListParams {
  page?: number;
  pageSize?: number;
  type?: ActorType;
  status?: ActorStatus;
  sort?: string;
}

export interface ActorListResult {
  items: Actor[];
  meta: PageMeta;
}

export interface ActorSummary {
  id: string;
  type: ActorType;
  displayName: string;
  status: ActorStatus;
  isBuiltin: boolean;
  version: number;
}

export interface CreatePersonActorInput {
  displayName: string;
  notes?: string;
  metadata?: Record<string, unknown>;
  status?: ActorStatus;
}

export interface UpdateActorInput {
  displayName?: string;
  notes?: string;
  metadata?: Record<string, unknown>;
  status?: ActorStatus;
  expectedVersion: number;
}
