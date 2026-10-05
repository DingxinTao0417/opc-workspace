import type { ActorStatus, ActorType } from "./actors";
import type { PageMeta } from "./common";

export type ClientStatus = "active" | "lead" | "inactive";

export interface Client {
  id: string;
  name: string;
  contactName: string | null;
  email: string | null;
  phone: string | null;
  notes: string | null;
  status: ClientStatus;
  version: number;
  projectCount: number;
  latestActivityAt: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface ClientListResult {
  items: Client[];
  meta: PageMeta;
}

export interface ClientListParams {
  page?: number;
  pageSize?: number;
  q?: string;
  status?: ClientStatus;
  sort?: string;
}

export interface ClientInput {
  name: string;
  contactName: string | null;
  email: string | null;
  phone: string | null;
  notes: string | null;
  status: ClientStatus;
}

export interface UpdateClientInput {
  name?: string;
  contactName?: string | null;
  email?: string | null;
  phone?: string | null;
  notes?: string | null;
  status?: ClientStatus;
  expectedVersion: number;
}

export interface DeleteClientResult {
  deletedId: string;
  detachedProjects: number;
}

export type ClientFollowupStatus =
  "planned" | "completed" | "skipped" | "cancelled";

export type ClientFollowupPriority = "low" | "normal" | "high";

export interface ClientFollowup {
  id: string;
  clientId: string;
  clientName: string;
  assignedActorId: string;
  assignedActorName: string;
  assignedActorType: ActorType;
  scheduledAt: string;
  timezone: string;
  channel: string;
  purpose: string;
  notes: string | null;
  status: ClientFollowupStatus;
  priority: ClientFollowupPriority;
  completedAt: string | null;
  result: string | null;
  nextStep: string | null;
  skippedAt: string | null;
  skipReason: string | null;
  cancelledAt: string | null;
  cancelReason: string | null;
  rescheduledFromId: string | null;
  version: number;
  createdAt: string;
  updatedAt: string;
  clientVersion: number;
  nextFollowup: ClientFollowup | null;
}

export interface ClientFollowupListResult {
  items: ClientFollowup[];
  meta: PageMeta & { serverNow: string };
}

export interface ClientFollowupListParams {
  page?: number;
  pageSize?: number;
  status?: ClientFollowupStatus;
  dueState?: "overdue";
  assignedActorId?: string;
}

export interface CreateClientFollowupInput {
  clientId: string;
  assignedActorId: string;
  scheduledAt: string;
  timezone: string;
  channel: string;
  purpose: string;
  notes: string | null;
  priority: ClientFollowupPriority;
}

export interface UpdateClientFollowupInput {
  assignedActorId?: string;
  scheduledAt?: string;
  timezone?: string;
  channel?: string;
  purpose?: string;
  notes?: string | null;
  priority?: ClientFollowupPriority;
  expectedVersion: number;
}

export interface CompleteClientFollowupInput {
  result: string;
  nextStep: string | null;
  completedAt: string | null;
  nextFollowup: ClientFollowupPlanInput | null;
  expectedVersion: number;
}

export interface ClientFollowupPlanInput {
  assignedActorId: string;
  scheduledAt: string;
  timezone: string;
  channel: string;
  purpose: string;
  notes: string | null;
  priority: ClientFollowupPriority;
}

export interface SkipClientFollowupInput {
  reason: string;
  expectedVersion: number;
}

export interface CancelClientFollowupInput {
  reason: string;
  expectedVersion: number;
}

export interface RescheduleClientFollowupInput {
  assignedActorId: string;
  scheduledAt: string;
  timezone: string;
  channel: string;
  purpose: string;
  notes: string | null;
  priority: ClientFollowupPriority;
  reason: string;
  expectedVersion: number;
}

export interface RescheduleClientFollowupResult {
  previous: ClientFollowup;
  next: ClientFollowup;
}

export type ClientActivityKind = "note" | "meeting" | "system_reference";

export interface ClientActivityActor {
  id: string;
  type: ActorType;
  displayName: string;
}

export interface ClientActivity {
  id: string;
  clientId: string;
  kind: ClientActivityKind;
  title: string;
  body: string | null;
  occurredAt: string;
  createdBy: ClientActivityActor;
  sourceType: string | null;
  sourceId: string | null;
  version: number;
  deletedAt: string | null;
  deletedByActorId: string | null;
  deleteReason: string | null;
  createdAt: string;
  updatedAt: string;
  clientVersion: number;
}

export interface ClientActivityListParams {
  page?: number;
  pageSize?: number;
  kind?: ClientActivityKind;
  includeDeleted?: boolean;
}

export interface ClientActivityListResult {
  items: ClientActivity[];
  meta: PageMeta & { clientVersion: number };
}

export interface RecentClientActivity extends ClientActivity {
  clientName: string;
  clientStatus: ClientStatus;
}

export interface RecentClientActivityListParams {
  page?: number;
  pageSize?: number;
  kind?: ClientActivityKind;
}

export interface RecentClientActivityListResult {
  items: RecentClientActivity[];
  meta: PageMeta;
}

export interface CreateClientActivityInput {
  kind: Exclude<ClientActivityKind, "system_reference">;
  title: string;
  body: string;
  occurredAt: string;
}

export interface UpdateClientActivityInput {
  kind?: Exclude<ClientActivityKind, "system_reference">;
  title?: string;
  body?: string;
  occurredAt?: string;
  expectedVersion: number;
}

export interface DeleteClientActivityInput {
  reason: string;
  expectedVersion: number;
}

export type ClientAttachmentIntegrityStatus =
  "verified" | "missing" | "mismatch";

export interface ClientAttachment {
  id: string;
  clientId: string;
  activityId: string | null;
  name: string;
  mimeType: string;
  sizeBytes: number;
  sha256: string;
  recordedBy: ClientActivityActor;
  integrityStatus: ClientAttachmentIntegrityStatus;
  integrityCheckedAt: string;
  deletedAt: string | null;
  deletedByActorId: string | null;
  deleteReason: string | null;
  createdAt: string;
  clientVersion: number;
}

export interface ClientAttachmentListParams {
  page?: number;
  pageSize?: number;
  activityId?: string;
  includeDeleted?: boolean;
}

export interface ClientAttachmentListResult {
  items: ClientAttachment[];
  meta: PageMeta & { clientVersion: number };
}

export interface CreateClientAttachmentInput {
  file: File;
  name: string;
  activityId?: string | null;
  expectedVersion: number;
}

export interface DeleteClientAttachmentInput {
  reason: string;
  expectedVersion: number;
}

export interface ClientAttachmentDownload {
  blob: Blob;
  fileName: string;
  mimeType: string;
}

export interface ClientActorLinkActor {
  id: string;
  type: ActorType;
  displayName: string;
  status: ActorStatus;
  version: number;
}

export interface ClientActorLink {
  id: string;
  clientId: string;
  role: "contact";
  actor: ClientActorLinkActor;
  linkedBy: ClientActivityActor;
  linkedAt: string;
  unlinkedAt: string | null;
  unlinkedBy: ClientActivityActor | null;
  unlinkReason: string | null;
  clientVersion: number;
}

export type ClientActorLinkListState = "active" | "unlinked" | "all";

type ClientActorLinkListPageParams = {
  page?: number;
  pageSize?: number;
};

export type ClientActorLinkListParams = ClientActorLinkListPageParams &
  (
    | { state?: ClientActorLinkListState; includeUnlinked?: never }
    | { state?: never; includeUnlinked?: boolean }
  );

export interface ClientActorLinkListResult {
  items: ClientActorLink[];
  meta: PageMeta & { clientVersion: number };
}

export type CreateClientActorLinkInput =
  | {
      actorId: string;
      createPerson?: never;
      expectedVersion: number;
    }
  | {
      actorId?: never;
      createPerson: { displayName: string; notes: string };
      expectedVersion: number;
    };

export interface DeleteClientActorLinkInput {
  reason: string;
  expectedVersion: number;
}
