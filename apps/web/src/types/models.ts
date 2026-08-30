export type TaskStatus =
  "todo" | "in_progress" | "blocked" | "waiting_review" | "done" | "cancelled";
export type TaskPriority = "P0" | "P1" | "P2" | "P3";
export type TaskKind = "work" | "review" | "followup" | "reminder";
export type TaskReviewPolicy = "none" | "manual";
export type TaskSubmissionStatus =
  "pending_review" | "accepted" | "changes_requested" | "withdrawn";
export type TaskSubmissionOrigin = "manual" | "child_rollup";
export type TaskArtifactStorageKind = "text" | "link" | "structured" | "file";
export type TaskArtifactIntegrityStatus =
  "unverified" | "verified" | "missing" | "mismatch";
export type TaskLifecycleAction =
  "start" | "block" | "unblock" | "complete" | "cancel" | "reopen";
export type ProjectStatus =
  "planning" | "in_progress" | "paused" | "completed" | "archived";
export type ClientStatus = "active" | "lead" | "inactive";
export type ProjectTransitionAction =
  "start" | "pause" | "resume" | "complete" | "reopen" | "archive" | "restore";
export type ActorType = "owner" | "person" | "system" | "agent";
export type ActorStatus = "active" | "inactive";
export type AssignmentRole = "assignee" | "reviewer";

export type AppSettingKey =
  "workspace" | "general" | "appearance" | "focus" | "storage";

export interface WorkspaceSettingValue {
  displayName: string;
  avatarRef: string | null;
}

export interface GeneralSettingValue {
  defaultRoute: "today" | "tasks" | "projects" | "clients" | "focus";
  showRightOverview: boolean;
  reduceMotion: boolean;
  closeToTray: boolean;
}

export interface AppearanceSettingValue {
  theme: "light" | "dark";
}

export interface FocusSettingValue {
  focusMinutes: number;
  breakMinutes: number;
  cycles: number;
  autoStartBreak: boolean;
  autoStartFocus: boolean;
  soundEnabled: boolean;
}

export interface StorageSettingValue {
  lowSpaceThresholdGiB: number;
}

interface AppSettingItemBase {
  schemaVersion: 2;
  version: number;
  stored: boolean;
  updatedByActorId: string | null;
  updatedAt: string | null;
}

export type AppSettingItem = AppSettingItemBase &
  (
    | { key: "workspace"; value: WorkspaceSettingValue }
    | { key: "general"; value: GeneralSettingValue }
    | { key: "appearance"; value: AppearanceSettingValue }
    | { key: "focus"; value: FocusSettingValue }
    | { key: "storage"; value: StorageSettingValue }
  );

export interface AppSettingsResult {
  schemaVersion: 2;
  items: AppSettingItem[];
}

export type AppSettingUpdate =
  | {
      key: "workspace";
      expectedVersion: number;
      value: WorkspaceSettingValue;
    }
  | {
      key: "general";
      expectedVersion: number;
      value: GeneralSettingValue;
    }
  | {
      key: "appearance";
      expectedVersion: number;
      value: AppearanceSettingValue;
    }
  | {
      key: "focus";
      expectedVersion: number;
      value: FocusSettingValue;
    }
  | {
      key: "storage";
      expectedVersion: number;
      value: StorageSettingValue;
    };

export interface Actor {
  id: string;
  type: ActorType;
  displayName: string;
  status: ActorStatus;
  isBuiltin: boolean;
  notes: string;
  metadata: Record<string, unknown>;
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

export interface TaskAssignment {
  id: string;
  taskId: string;
  role: AssignmentRole;
  actorId: string;
  actor: ActorSummary;
  assignedByActorId: string;
  assignedByActor: ActorSummary;
  assignedAt: string;
  unassignedAt: string | null;
  reason: string | null;
  isActive: boolean;
  inferred: boolean;
}

export interface TaskAssignmentListParams {
  page?: number;
  pageSize?: number;
  role?: AssignmentRole;
  sort?: string;
}

export interface TaskAssignmentListMeta extends PageMeta {
  taskVersion: number;
}

export interface TaskAssignmentListResult {
  active: Record<AssignmentRole, TaskAssignment | null>;
  history: TaskAssignment[];
  meta: TaskAssignmentListMeta;
}

export interface CreateTaskAssignmentInput {
  role: AssignmentRole;
  actorId: string;
  expectedVersion: number;
}

export interface ReassignTaskAssignmentInput extends CreateTaskAssignmentInput {
  reason: string;
}

export interface EndTaskAssignmentInput {
  reason: string;
  expectedVersion: number;
}

export interface TaskAssignmentMutationResult {
  assignment: TaskAssignment;
  task: Task;
}

export interface ReassignTaskAssignmentResult extends TaskAssignmentMutationResult {
  previousAssignment: TaskAssignment;
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

export interface Tag {
  id: string;
  name: string;
  color: string;
  version: number;
  createdAt: string;
}

export interface Task {
  id: string;
  title: string;
  description: string;
  kind: TaskKind;
  status: TaskStatus;
  priority: TaskPriority;
  projectId: string | null;
  projectName?: string;
  parentTaskId: string | null;
  parentTaskTitle?: string;
  completionCriteria: string;
  reviewPolicy: TaskReviewPolicy;
  blockedReason: string | null;
  blockedAt: string | null;
  blockedFromStatus: Extract<
    TaskStatus,
    "todo" | "in_progress" | "waiting_review"
  > | null;
  dueDate: string | null;
  plannedDate: string | null;
  estimatedMinutes: number | null;
  actualMinutes: number;
  manualOrder: number | null;
  version: number;
  subtaskTotal: number;
  subtaskCompleted: number;
  /** Added in schema v30; optional keeps durable v29 idempotency snapshots readable. */
  subtaskCancelled?: number;
  createdAt: string;
  updatedAt: string;
  completedAt: string | null;
  submittedAt: string | null;
  reviewedAt: string | null;
  currentSubmissionId: string | null;
  tags: Tag[];
}

export interface TaskArtifactSummary {
  id: string;
  taskId: string;
  submissionId: string;
  submissionStatus: TaskSubmissionStatus;
  position: number;
  storageKind: TaskArtifactStorageKind;
  name: string;
  mimeType: string | null;
  sizeBytes: number | null;
  sha256: string | null;
  requiresFollowup: boolean;
  producedByActorId: string;
  producedByActor: ActorSummary;
  recordedByActorId: string;
  recordedByActor: ActorSummary;
  integrityStatus: TaskArtifactIntegrityStatus;
  integrityCheckedAt: string | null;
  deletedAt: string | null;
  deletedByActorId: string | null;
  deletedByActor: ActorSummary | null;
  deleteReason: string | null;
  createdAt: string;
}

export interface TaskArtifact extends TaskArtifactSummary {
  contentText: string | null;
  referenceUrl: string | null;
  structuredJson: Record<string, unknown> | null;
}

export interface TaskSubmission {
  id: string;
  taskId: string;
  sequence: number;
  status: TaskSubmissionStatus;
  origin: TaskSubmissionOrigin;
  summary: string;
  submittedByActorId: string;
  submittedByActor: ActorSummary;
  submittedAt: string;
  reviewedByActorId: string | null;
  reviewedByActor: ActorSummary | null;
  reviewedAt: string | null;
  reviewReason: string | null;
  withdrawnByActorId: string | null;
  withdrawnByActor: ActorSummary | null;
  withdrawnAt: string | null;
  isInferred: boolean;
  artifacts: TaskArtifactSummary[];
}

export interface TaskSubmissionListParams {
  page?: number;
  pageSize?: number;
}

export interface TaskArtifactListParams {
  page?: number;
  pageSize?: number;
  submissionId?: string;
  includeDeleted?: boolean;
}

export interface TaskAggregateListMeta extends PageMeta {
  taskVersion: number;
}

export interface TaskSubmissionListResult {
  items: TaskSubmission[];
  meta: TaskAggregateListMeta;
}

export interface TaskArtifactListResult {
  items: TaskArtifactSummary[];
  meta: TaskAggregateListMeta;
}

export interface ProjectArtifactItem {
  artifact: TaskArtifactSummary;
  task: {
    id: string;
    title: string;
    status: TaskStatus;
  };
  submissionSequence: number;
  followup: {
    inboxItemId: string;
    inboxItemVersion: number;
    status: InboxItemStatus;
    resolutionPolicy: InboxResolutionPolicy;
    sourceDeletedAt: string | null;
    progress: InboxTaskProgress;
  } | null;
}

export interface ProjectArtifactListParams {
  page?: number;
  pageSize?: number;
  includeDeleted?: boolean;
}

export interface ProjectArtifactListResult {
  items: ProjectArtifactItem[];
  meta: PageMeta & { projectVersion: number };
}

interface NewArtifactBase {
  clientRef: string;
  name: string;
  requiresFollowup: boolean;
}

export type NewTaskArtifactInput =
  | (NewArtifactBase & {
      storageKind: "text";
      contentText: string;
    })
  | (NewArtifactBase & {
      storageKind: "link";
      referenceUrl: string;
    })
  | (NewArtifactBase & {
      storageKind: "structured";
      structuredJson: Record<string, unknown>;
    })
  | (NewArtifactBase & {
      storageKind: "file";
      file: File;
    });

export interface SubmitTaskOutputInput {
  summary: string;
  artifacts: NewTaskArtifactInput[];
  expectedVersion: number;
}

export interface SubmitTaskOutputResult {
  task: Task;
  submission: TaskSubmission;
  artifacts: TaskArtifactSummary[];
  event: TaskWorkflowEvent;
}

export type ReviewTaskSubmissionInput =
  | {
      decision: "accept";
      expectedVersion: number;
    }
  | {
      decision: "request_changes";
      reason: string;
      expectedVersion: number;
    };

export interface ReviewTaskSubmissionResult {
  task: Task;
  submission: TaskSubmission;
  event: TaskWorkflowEvent;
}

export interface DeleteTaskArtifactInput {
  reason: string;
  expectedVersion: number;
}

export interface DeleteTaskArtifactResult {
  task: Task;
  artifact: TaskArtifactSummary;
  event: TaskWorkflowEvent;
}

export interface TaskArtifactDownload {
  blob: Blob;
  fileName: string;
  mimeType: string;
}

export interface TaskWorkflowEvent {
  id: string;
  action: string;
  actor: ActorSummary | null;
  assignmentId: string | null;
  submissionId: string | null;
  artifactId: string | null;
  requestId: string | null;
  commandSeq: number | null;
  previous: Record<string, unknown> | null;
  current: Record<string, unknown> | null;
  reason: string | null;
  createdAt: string;
}

export interface TaskEventListParams {
  page?: number;
  pageSize?: number;
}

export interface TaskEventListMeta extends PageMeta {
  taskVersion: number;
}

export interface TaskEventListResult {
  items: TaskWorkflowEvent[];
  meta: TaskEventListMeta;
}

export type TaskLifecycleCommandInput =
  | {
      action: Exclude<TaskLifecycleAction, "block" | "cancel">;
      expectedVersion: number;
    }
  | {
      action: "block" | "cancel";
      reason: string;
      expectedVersion: number;
    };

export interface TaskLifecycleCommandResult {
  task: Task;
  event: TaskWorkflowEvent;
}

export interface HealthResponse {
  status: string;
  app: {
    name: string;
    version: string;
    commit: string;
  };
  api: {
    version: string;
  };
  schema: {
    version: number;
  };
}

export interface TodayStats {
  date: string;
  tasks: {
    total: number;
    completed: number;
    remaining: number;
    overdue: number;
    dueSoon: number;
    estimatedMinutes: number;
    actualMinutes: number;
  };
  focus: {
    sessions: number;
    seconds: number;
    minutes: number;
  };
}

export interface InboxStats {
  serverNow: string;
  pending: number;
  unread: number;
  tracking: number;
  blocked: number;
  waitingReview: number;
}

export type FocusSessionStatus =
  | "planned"
  | "active"
  | "paused"
  | "recovery_pending"
  | "completed"
  | "cancelled"
  | "interrupted";

export type FocusSessionEndReason =
  "user_stop" | "completed" | "cancelled" | "crash_recovery";

export type FocusRecoveryAction =
  "include_gap_resume" | "exclude_gap_resume" | "interrupt";

export interface FocusSession {
  id: string;
  taskId: string | null;
  taskTitle: string | null;
  status: FocusSessionStatus;
  plannedSeconds: number;
  accumulatedSeconds: number;
  startedAt: string;
  endedAt: string | null;
  lastResumedAt: string | null;
  lastHeartbeatAt: string | null;
  endReason: FocusSessionEndReason | null;
  version: number;
  createdAt: string;
  updatedAt: string;
}

export interface FocusSessionSnapshot {
  session: FocusSession | null;
  serverNow: string;
  receivedAtMs: number;
}

export type FocusSessionHistoryStatus =
  "terminal" | "completed" | "cancelled" | "interrupted";

export interface FocusSessionListParams {
  page?: number;
  pageSize?: number;
  status?: FocusSessionHistoryStatus;
  taskId?: string;
  projectId?: string;
}

export interface FocusSessionListResult {
  items: FocusSession[];
  meta: PageMeta;
}

export interface FocusReportDay {
  date: string;
  sessions: number;
  seconds: number;
  minutes: number;
}

export interface FocusReportProject {
  projectId: string | null;
  projectName: string | null;
  sessions: number;
  seconds: number;
  minutes: number;
}

export interface FocusReportHour {
  hour: number;
  sessions: number;
  seconds: number;
  minutes: number;
}

export interface FocusReportHeatmapCell {
  weekday: number;
  hour: number;
  sessions: number;
  seconds: number;
  minutes: number;
}

export interface FocusReportTag {
  tagId: string | null;
  tagName: string | null;
  tagColor: string | null;
  sessions: number;
  seconds: number;
  minutes: number;
}

export interface FocusReport {
  dateFrom: string;
  dateTo: string;
  timezone: string;
  totals: {
    sessions: number;
    seconds: number;
    minutes: number;
  };
  days: FocusReportDay[];
  projects: FocusReportProject[];
  hours: FocusReportHour[];
  heatmap: FocusReportHeatmapCell[];
  tags: FocusReportTag[];
  currentStreakDays: number;
  longestStreakDays: number;
}

export interface FocusReportParams {
  dateFrom: string;
  dateTo: string;
  timezone: string;
  projectId?: string;
}

export interface CreateFocusSessionInput {
  taskId: string | null;
  plannedSeconds: number;
}

export interface FocusSessionCommandInput {
  id: string;
  expectedVersion: number;
}

export interface RecoverFocusSessionInput extends FocusSessionCommandInput {
  action: FocusRecoveryAction;
}

export interface NewTaskInput {
  title: string;
  description?: string;
  kind?: TaskKind;
  priority: TaskPriority;
  projectId?: string | null;
  parentTaskId?: string | null;
  completionCriteria?: string;
  reviewPolicy?: TaskReviewPolicy;
  tagIds?: string[];
  dueDate?: string | null;
  plannedDate?: string | null;
  estimatedMinutes?: number | null;
  manualOrder?: number | null;
}

export interface UpdateTaskInput {
  title: string;
  description: string;
  kind?: TaskKind;
  priority: TaskPriority;
  projectId?: string | null;
  parentTaskId?: string | null;
  completionCriteria?: string;
  reviewPolicy?: TaskReviewPolicy;
  tagIds?: string[];
  dueDate: string | null;
  plannedDate: string | null;
  estimatedMinutes: number | null;
  expectedVersion?: number;
}

export interface PageMeta {
  page: number;
  pageSize: number;
  total: number;
}

export type SearchResourceType = "task" | "project" | "client" | "inbox_item";

export interface SearchResult {
  resourceType: SearchResourceType;
  resourceId: string;
  title: string;
  subtitle: string;
  matchedFields: string[];
  route: string;
  status: string;
  updatedAt: string;
}

export interface SearchListParams {
  q: string;
  types?: SearchResourceType[];
  page?: number;
  pageSize?: number;
}

export interface SearchListResult {
  items: SearchResult[];
  meta: PageMeta;
}

export interface TaskListResult {
  items: Task[];
  meta: PageMeta;
}

export interface TaskVersionItem {
  id: string;
  expectedVersion: number;
}

export type BatchUpdateTasksInput =
  | {
      action: "set_project";
      items: TaskVersionItem[];
      projectId: string | null;
    }
  | {
      action: "set_planned_date";
      items: TaskVersionItem[];
      plannedDate: string | null;
    }
  | {
      action: "add_tags" | "remove_tags";
      items: TaskVersionItem[];
      tagIds: string[];
    }
  | {
      action: Exclude<TaskLifecycleAction, "block" | "cancel">;
      items: TaskVersionItem[];
    }
  | {
      action: "block" | "cancel";
      items: TaskVersionItem[];
      reason: string;
    };

export interface BatchUpdateTasksResult {
  action: BatchUpdateTasksInput["action"];
  changed: number;
  tasks: Task[];
}

export interface ReorderTasksInput {
  plannedDate: string | null;
  mode: "manual" | "default";
  items: TaskVersionItem[];
}

export interface ReorderTasksResult {
  plannedDate: string | null;
  mode: ReorderTasksInput["mode"];
  changed: number;
  tasks: Task[];
}

export interface TaskListParams {
  page?: number;
  pageSize?: number;
  q?: string;
  kind?: TaskKind;
  status?: TaskStatus | "active";
  priority?: TaskPriority;
  projectId?: string;
  clientId?: string;
  tagIds?: string[];
  plannedDate?: string;
  plannedFrom?: string;
  plannedTo?: string;
  plannedState?: "scheduled" | "unscheduled";
  dueFrom?: string;
  dueTo?: string;
  dueState?: "overdue" | "due_soon";
  parentTaskId?: string;
  rootOnly?: boolean;
  sort?: string;
}

export interface TaskSavedViewDefinition {
  q: string;
  status: TaskStatus | "active" | "";
  priority: TaskPriority | "";
  kind: TaskKind | "";
  projectId: string;
  clientId: string;
  tagIds: string[];
  plannedDate: string;
  plannedFrom: string;
  plannedTo: string;
  dueFrom: string;
  dueTo: string;
  sort: string;
}

export interface TaskSavedView {
  id: string;
  name: string;
  definition: TaskSavedViewDefinition;
  schemaVersion: number;
  version: number;
  createdAt: string;
  updatedAt: string;
}

export interface CreateTaskSavedViewInput {
  name: string;
  definition: TaskSavedViewDefinition;
}

export interface UpdateTaskSavedViewInput {
  expectedVersion: number;
  name?: string;
  definition?: TaskSavedViewDefinition;
}

export interface DeleteTaskSavedViewResult {
  deletedId: string;
}

export interface TagListResult {
  items: Tag[];
  meta: PageMeta;
}

export interface TagListParams {
  page?: number;
  pageSize?: number;
  query?: string;
  sort?: string;
}

export interface TagInput {
  name: string;
  color: string;
}

export interface UpdateTagInput {
  name?: string;
  color?: string;
  expectedVersion: number;
}

export interface DeleteTagResult {
  deletedId: string;
  detachedTasks: number;
}

export interface ProjectTaskSummary {
  total: number;
  completed: number;
  inProgress: number;
  remaining: number;
  progressPercent: number;
  actualMinutes: number;
}

export interface Project {
  id: string;
  name: string;
  description: string;
  clientId: string | null;
  clientName: string | null;
  status: ProjectStatus;
  startDate: string | null;
  dueDate: string | null;
  amountMinor: number | null;
  color: string | null;
  version: number;
  archivedFromStatus: Exclude<ProjectStatus, "archived"> | null;
  createdAt: string;
  updatedAt: string;
  taskSummary: ProjectTaskSummary;
  invoiceCount: number;
  availableActions: ProjectTransitionAction[];
}

export interface ProjectListResult {
  items: Project[];
  meta: PageMeta;
}

export interface ProjectWorkflowEvent {
  id: string;
  action: string;
  actor: ActorSummary | null;
  requestId: string | null;
  previous: Record<string, unknown> | null;
  current: Record<string, unknown> | null;
  createdAt: string;
}

export interface ProjectEventListParams {
  page?: number;
  pageSize?: number;
}

export interface ProjectEventListMeta extends PageMeta {
  projectVersion: number;
}

export interface ProjectEventListResult {
  items: ProjectWorkflowEvent[];
  meta: ProjectEventListMeta;
}

export interface ProjectNoteActor {
  id: string;
  type: ActorType;
  displayName: string;
}

export interface ProjectNote {
  id: string;
  projectId: string;
  title: string;
  body: string | null;
  occurredAt: string;
  createdBy: ProjectNoteActor;
  version: number;
  deletedAt: string | null;
  deletedByActorId: string | null;
  deleteReason: string | null;
  createdAt: string;
  updatedAt: string;
  projectVersion: number;
}

export interface ProjectNoteListParams {
  page?: number;
  pageSize?: number;
  includeDeleted?: boolean;
}

export interface ProjectNoteListResult {
  items: ProjectNote[];
  meta: PageMeta & { projectVersion: number };
}

export interface CreateProjectNoteInput {
  title: string;
  body: string;
  occurredAt: string;
}

export interface UpdateProjectNoteInput {
  title?: string;
  body?: string;
  occurredAt?: string;
  expectedVersion: number;
}

export interface DeleteProjectNoteInput {
  reason: string;
  expectedVersion: number;
}

export interface ProjectListParams {
  page?: number;
  pageSize?: number;
  query?: string;
  status?: ProjectStatus;
  clientId?: string;
  includeArchived?: boolean;
  sort?: string;
}

export interface ProjectInput {
  name: string;
  description: string;
  clientId: string | null;
  startDate: string | null;
  dueDate: string | null;
  amountMinor: number | null;
  color: string | null;
}

export interface UpdateProjectInput extends ProjectInput {
  expectedVersion: number;
}

export interface DeleteProjectResult {
  deletedId: string;
  detachedTasks: number;
  detachedInvoices: number;
}

export type RoadmapMilestoneStatus =
  "planned" | "active" | "achieved" | "archived";

export interface RoadmapMilestoneProject {
  id: string;
  name: string;
  status: ProjectStatus;
}

export interface RoadmapMilestoneTaskSummary {
  total: number;
  completed: number;
  inProgress: number;
  progressPercent: number;
}

export interface RoadmapMilestone {
  id: string;
  title: string;
  description: string | null;
  year: number;
  quarter: 1 | 2 | 3 | 4;
  targetDate: string;
  status: RoadmapMilestoneStatus;
  manualOrder: number;
  archivedFromStatus: Exclude<RoadmapMilestoneStatus, "archived"> | null;
  version: number;
  createdAt: string;
  updatedAt: string;
  projects: RoadmapMilestoneProject[];
  taskSummary: RoadmapMilestoneTaskSummary;
}

export interface RoadmapMilestoneListParams {
  page?: number;
  pageSize?: number;
  year?: number;
  quarter?: 1 | 2 | 3 | 4;
  status?: RoadmapMilestoneStatus;
  projectId?: string;
  includeArchived?: boolean;
  sort?: "target_date";
}

export interface RoadmapMilestoneListResult {
  items: RoadmapMilestone[];
  meta: PageMeta;
}

export interface CreateRoadmapMilestoneInput {
  title: string;
  description: string | null;
  year: number;
  quarter: 1 | 2 | 3 | 4;
  targetDate: string;
  status?: Exclude<RoadmapMilestoneStatus, "archived">;
  projectIds: string[];
}

export interface UpdateRoadmapMilestoneInput {
  title?: string;
  description?: string | null;
  year?: number;
  quarter?: 1 | 2 | 3 | 4;
  targetDate?: string;
  status?: Exclude<RoadmapMilestoneStatus, "archived">;
  projectIds?: string[];
  expectedVersion: number;
}

export interface DeleteRoadmapMilestoneResult {
  deletedId: string;
}

export interface ReorderRoadmapMilestonesInput {
  items: Array<{
    id: string;
    expectedVersion: number;
  }>;
}

export type ContentItemStatus =
  "draft" | "in_review" | "scheduled" | "published" | "cancelled" | "archived";

export interface ContentItemTask {
  id: string;
  title: string;
  status: TaskStatus;
  isRequired: boolean;
}

export interface ContentItem {
  id: string;
  title: string;
  platform: string;
  status: ContentItemStatus;
  scheduledAt: string | null;
  scheduledTimezone: string | null;
  publishedAt: string | null;
  projectId: string | null;
  notes: string | null;
  externalLink: string | null;
  manualOrder: number;
  archivedFromStatus: Exclude<ContentItemStatus, "archived"> | null;
  version: number;
  createdAt: string;
  updatedAt: string;
  tasks: ContentItemTask[];
  requiredTaskTotal: number;
  requiredTaskDone: number;
}

export interface ContentItemListParams {
  page?: number;
  pageSize?: number;
  scheduledFrom?: string;
  scheduledTo?: string;
  platform?: string;
  status?: ContentItemStatus;
  projectId?: string;
  includeArchived?: boolean;
}

export interface ContentItemListResult {
  items: ContentItem[];
  meta: PageMeta;
}

export interface CreateContentItemInput {
  title: string;
  platform: string;
  status?: Exclude<ContentItemStatus, "published" | "archived">;
  scheduledAt?: string | null;
  scheduledTimezone?: string | null;
  projectId?: string | null;
  notes?: string | null;
  externalLink?: string | null;
}

export interface UpdateContentItemInput {
  title?: string;
  platform?: string;
  status?: Exclude<ContentItemStatus, "published">;
  projectId?: string | null;
  notes?: string | null;
  externalLink?: string | null;
  expectedVersion: number;
}

export interface ScheduleContentItemInput {
  scheduledAt: string | null;
  scheduledTimezone: string | null;
  expectedVersion: number;
}

export interface PublishContentItemInput {
  publishedAt?: string;
  externalLink?: string | null;
  expectedVersion: number;
}

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

export interface ProjectAttachment {
  id: string;
  projectId: string;
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
  projectVersion: number;
}

export interface ProjectAttachmentListParams {
  page?: number;
  pageSize?: number;
  includeDeleted?: boolean;
}

export interface ProjectAttachmentListResult {
  items: ProjectAttachment[];
  meta: PageMeta & { projectVersion: number };
}

export interface CreateProjectAttachmentInput {
  file: File;
  name: string;
  expectedVersion: number;
}

export interface DeleteProjectAttachmentInput {
  reason: string;
  expectedVersion: number;
}

export type ProjectAttachmentDownload = ClientAttachmentDownload;

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

export interface ClientActorLinkListParams {
  page?: number;
  pageSize?: number;
  includeUnlinked?: boolean;
}

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

export type InboxItemKind = "manual" | "reminder" | "event";
export type InboxItemPriority = TaskPriority;
export type InboxItemStatus = "open" | "tracking" | "resolved" | "dismissed";
export type InboxItemView = "inbox" | "snoozed" | "archive";
export type InboxItemRisk = "tracking" | "blocked" | "waiting_review";
export type InboxResolutionPolicy = "manual" | "all_required_tasks_done";
export type InboxResolutionMode = "manual" | "forced" | "automatic";
export type InboxItemAction =
  | "read"
  | "edit"
  | "snooze"
  | "unsnooze"
  | "resolve"
  | "force-resolve"
  | "dismiss"
  | "reopen";

export interface InboxItem {
  id: string;
  kind: InboxItemKind;
  title: string;
  summary: string;
  sourceEntityType:
    | "manual"
    | "reminder"
    | "task_artifact"
    | "task"
    | "task_due"
    | "client_followup"
    | "content_item"
    | "roadmap_milestone"
    | "project_completion"
    | "system_maintenance";
  sourceEntityId: string | null;
  sourceEventKey: string | null;
  sourceDeletedAt: string | null;
  priority: InboxItemPriority;
  status: InboxItemStatus;
  resolutionPolicy: InboxResolutionPolicy;
  dueAt: string | null;
  readAt: string | null;
  triagedAt: string | null;
  snoozedUntil: string | null;
  resolvedByActorId: string | null;
  resolvedAt: string | null;
  resolutionReason: string | null;
  resolutionMode: InboxResolutionMode | null;
  dismissedByActorId: string | null;
  dismissedAt: string | null;
  dismissReason: string | null;
  payloadJson: Record<string, unknown>;
  version: number;
  createdAt: string;
  updatedAt: string;
  availableActions: InboxItemAction[];
}

export interface InboxItemListParams {
  view?: InboxItemView;
  q?: string;
  priority?: InboxItemPriority;
  risk?: InboxItemRisk;
  sourceEntityType?: InboxItem["sourceEntityType"];
  page?: number;
  pageSize?: number;
}

export interface InboxItemListMeta extends PageMeta {
  unreadTotal: number;
  snapshotAt: string;
  serverNow: string;
}

export interface InboxItemListResult {
  items: InboxItem[];
  meta: InboxItemListMeta;
}

export type InboxTaskRelationType = "created" | "linked";

export interface InboxTaskSummary {
  id: string;
  title: string;
  status: TaskStatus;
  priority: TaskPriority;
  kind: TaskKind;
  projectId: string | null;
  projectName: string | null;
  version: number;
}

export interface InboxItemTaskRelation {
  id: string;
  inboxItemId: string;
  taskRefId: string;
  taskId: string | null;
  taskTitleSnapshot: string;
  task: InboxTaskSummary | null;
  relationType: InboxTaskRelationType;
  isRequired: boolean;
  position: number;
  linkedByActorId: string;
  linkedByActor: ActorSummary;
  linkedAt: string;
  unlinkedByActorId: string | null;
  unlinkedByActor: ActorSummary | null;
  unlinkedAt: string | null;
  unlinkReason: string | null;
  isActive: boolean;
  taskDeleted: boolean;
}

export interface InboxTaskProgress {
  activeTotal: number;
  requiredTotal: number;
  requiredDone: number;
  requiredRemaining: number;
  requiredBlocked: number;
  requiredWaitingReview: number;
  requiredCancelled: number;
  percent: number | null;
  allRequiredDone: boolean;
}

export interface InboxItemTaskListMeta extends PageMeta {
  inboxItemVersion: number;
  progress: InboxTaskProgress;
}

export interface InboxItemTaskListResult {
  active: InboxItemTaskRelation[];
  history: InboxItemTaskRelation[];
  meta: InboxItemTaskListMeta;
}

export interface InboxItemTaskListParams {
  page?: number;
  pageSize?: number;
}

export interface InboxItemTaskMutationResult {
  inboxItem: InboxItem;
  relation: InboxItemTaskRelation;
  progress: InboxTaskProgress;
}

export interface LinkInboxItemTaskInput {
  inboxItemId: string;
  taskId: string;
  isRequired: boolean;
  expectedVersion: number;
}

export interface UpdateInboxItemTaskRequirementInput extends LinkInboxItemTaskInput {}

export interface UnlinkInboxItemTaskInput {
  inboxItemId: string;
  taskId: string;
  reason: string;
  expectedVersion: number;
}

export interface InboxSplitTaskInput {
  key: string;
  parentKey: string | null;
  title: string;
  description: string;
  kind: TaskKind;
  priority: TaskPriority;
  projectId: string | null;
  completionCriteria: string;
  tagIds: string[];
  dueDate: string | null;
  plannedDate: string | null;
  estimatedMinutes: number | null;
  reviewPolicy: TaskReviewPolicy;
  isRequired: boolean;
  assigneeActorId: string;
}

export interface SplitInboxItemInput {
  inboxItemId: string;
  expectedVersion: number;
  resolutionPolicy: InboxResolutionPolicy;
  tasks: InboxSplitTaskInput[];
}

export interface InboxSplitTaskResult {
  key: string;
  task: Task;
  assignments: TaskAssignment[];
  relation: InboxItemTaskRelation;
}

export interface SplitInboxItemResult {
  inboxItem: InboxItem;
  created: InboxSplitTaskResult[];
  progress: InboxTaskProgress;
}

export interface ForceResolveInboxItemInput {
  id: string;
  expectedVersion: number;
  reason: string;
  confirm: true;
}

export interface CreateInboxItemInput {
  title: string;
  summary: string;
  priority: InboxItemPriority;
  dueAt: string | null;
}

export interface UpdateInboxItemInput {
  title?: string;
  summary?: string;
  priority?: InboxItemPriority;
  dueAt?: string | null;
  expectedVersion: number;
}

export type InboxItemCommandInput =
  | {
      action: "read" | "unsnooze" | "reopen";
      id: string;
      expectedVersion: number;
    }
  | {
      action: "snooze";
      id: string;
      snoozedUntil: string;
      expectedVersion: number;
    }
  | {
      action: "resolve" | "dismiss";
      id: string;
      reason: string;
      expectedVersion: number;
    };

export interface MarkAllInboxReadInput {
  throughCreatedAt: string;
}

export interface MarkAllInboxReadResult {
  throughCreatedAt: string;
  markedCount: number;
}

export interface InboxWorkflowEvent {
  id: string;
  action: string;
  actorId: string | null;
  actor: ActorSummary | null;
  requestId: string | null;
  previous: Record<string, unknown> | null;
  current: Record<string, unknown> | null;
  reason: string | null;
  createdAt: string;
}

export interface InboxEventListParams {
  page?: number;
  pageSize?: number;
}

export interface InboxEventListMeta extends PageMeta {
  inboxItemVersion: number;
}

export interface InboxEventListResult {
  items: InboxWorkflowEvent[];
  meta: InboxEventListMeta;
}

export type ReminderStatus = "scheduled" | "fired" | "cancelled";
export type ReminderAction = "edit" | "cancel";
export type ReminderRecurrenceType =
  "none" | "daily" | "weekly" | "weekdays" | "monthly";
export type ReminderSort =
  | "title"
  | "status"
  | "priority"
  | "trigger_at"
  | "created_at"
  | "updated_at"
  | "-title"
  | "-status"
  | "-priority"
  | "-trigger_at"
  | "-created_at"
  | "-updated_at";

export interface Reminder {
  id: string;
  sourceEntityType: "manual" | "automation";
  sourceEntityId: string | null;
  title: string;
  summary: string;
  priority: InboxItemPriority;
  triggerAt: string;
  status: ReminderStatus;
  sourceEventKey: string;
  createdByActorId: string;
  seriesId: string;
  recurrenceType: ReminderRecurrenceType;
  recurrenceInterval: number;
  recurrenceTimezone: string;
  occurrenceNumber: number;
  recurrenceAnchorDay: number;
  firedAt: string | null;
  inboxItemId: string | null;
  cancelledByActorId: string | null;
  cancelledAt: string | null;
  cancelReason: string | null;
  version: number;
  createdAt: string;
  updatedAt: string;
  availableActions: ReminderAction[];
}

export interface ReminderListParams {
  status?: ReminderStatus;
  q?: string;
  sort?: ReminderSort;
  page?: number;
  pageSize?: number;
}

export interface ReminderListMeta extends PageMeta {
  serverNow: string;
}

export interface ReminderListResult {
  items: Reminder[];
  meta: ReminderListMeta;
}

export interface CreateReminderInput {
  title: string;
  summary: string;
  priority: InboxItemPriority;
  triggerAt: string;
  recurrenceType: ReminderRecurrenceType;
  recurrenceInterval: number;
  recurrenceTimezone: string;
}

export interface UpdateReminderInput {
  title?: string;
  summary?: string;
  priority?: InboxItemPriority;
  triggerAt?: string;
  recurrenceType?: ReminderRecurrenceType;
  recurrenceInterval?: number;
  recurrenceTimezone?: string;
  expectedVersion: number;
}

export interface CancelReminderInput {
  id: string;
  reason: string;
  expectedVersion: number;
}

export type AutomationRuleStatus = "enabled" | "disabled" | "unavailable";
export type AutomationTriggerType = "event" | "schedule";
export type AutomationActionType = "inbox_item" | "task" | "reminder";
export type AutomationRunStatus =
  "succeeded" | "failed" | "skipped" | "cancelled";

export interface AutomationConfig {
  priority?: InboxItemPriority;
  localTime?: string;
  timezone?: string;
}

export interface AutomationRule {
  id: string;
  presetKey: string;
  name: string;
  description: string;
  status: AutomationRuleStatus;
  available: boolean;
  unavailableReason: string;
  triggerType: AutomationTriggerType;
  triggerLabel: string;
  actionType: AutomationActionType;
  actionLabel: string;
  config: AutomationConfig;
  nextRunAt: string | null;
  permissions: string[];
  version: number;
  createdAt: string;
  updatedAt: string;
}

export interface AutomationPreview {
  canEnable: boolean;
  unavailableReason: string;
  triggerSummary: string;
  actionSummary: string;
  config: AutomationConfig;
  nextRunAt: string | null;
  permissions: string[];
}

export interface AutomationRun {
  id: string;
  ruleId: string;
  presetKey: string;
  ruleName: string;
  ruleVersion: number;
  triggerType: AutomationTriggerType;
  sourceEventId: string | null;
  scheduledFor: string | null;
  status: AutomationRunStatus;
  attempt: number;
  retryOfRunId: string | null;
  retryable: boolean;
  retryAt: string | null;
  causedByRunId: string | null;
  causalDepth: number;
  configSnapshot: Record<string, unknown>;
  actionSnapshot: Record<string, unknown>;
  errorCode: string | null;
  resultType: string | null;
  resultId: string | null;
  resultSummary: string;
  startedAt: string;
  endedAt: string;
}

export interface AutomationRunListParams {
  ruleId?: string;
  status?: AutomationRunStatus;
  page?: number;
  pageSize?: number;
}

export interface AutomationRunListResult {
  items: AutomationRun[];
  meta: PageMeta;
}

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

export type BackupVerificationStatus = "verified" | "unverified" | "invalid";

export interface BackupSummary {
  id: string;
  createdAt: string;
  verifiedAt: string | null;
  verificationStatus: BackupVerificationStatus;
  note: string;
  appVersion: string;
  apiVersion: string;
  schemaVersion: number;
  artifactCount: number;
  artifactBytes: number;
  databaseBytes: number;
  totalBytes: number;
  error: string | null;
}

export interface CreateBackupInput {
  note: string;
}

export interface BackupRestoreDrillResult {
  backupId: string;
  drilledAt: string;
  sourceSchemaVersion: number;
  resultSchemaVersion: number;
  artifactCount: number;
  temporaryDataCleaned: boolean;
}

export interface ScheduledBackupRestoreResult {
  backupId: string;
  rollbackBackupId: string;
  requestedAt: string;
  restartRequired: true;
}

export interface BusinessDataExportDownload {
  blob: Blob;
  fileName: string;
  formatVersion: 1;
}

export interface BusinessPackageDownload {
  blob: Blob;
  fileName: string;
  formatVersion: 1;
}

export interface BusinessPackageImportPreview {
  formatVersion: 1;
  schemaVersion: number;
  exportedAt: string;
  tableCounts: Record<string, number>;
  totalRows: number;
  fileCount: number;
  fileBytes: number;
  canApply: boolean;
  blocker: "target_not_empty" | null;
}

export interface BusinessPackageImportResult {
  importedRows: number;
  importedFiles: number;
  backupId: string;
}

export interface RestoreDiagnostics {
  status:
    | "idle"
    | "restart_required"
    | "restored"
    | "cleanup_required"
    | "attention_required";
  restartRequired: boolean;
  appliedThisStartup: boolean;
  cleanupRequired: boolean;
  attentionRequired: boolean;
  backupId: string | null;
  rollbackBackupId: string | null;
  requestedAt: string | null;
  residualAppliedCount: number;
  failedAttemptCount: number;
  invalidEntryCount: number;
}

export interface BusinessImportPreview {
  formatVersion: 1;
  schemaVersion: number;
  exportedAt: string;
  tableCounts: Record<string, number>;
  totalRows: number;
  canApply: boolean;
  blocker: "target_not_empty" | null;
}

export interface BusinessImportResult {
  importedRows: number;
  backupId: string;
}

export interface DiagnosticPackageDownload {
  blob: Blob;
  fileName: string;
  formatVersion: 1;
}

export type StorageCapacityStatus = "healthy" | "low" | "unavailable";

export interface StorageCapacityLocation {
  kind: "database" | "artifacts" | "backups";
  status: StorageCapacityStatus;
  availableBytes: number | null;
  totalBytes: number | null;
  sharedVolume: boolean;
}

export interface StorageCapacityResult {
  checkedAt: string;
  thresholdGiB: number;
  locations: StorageCapacityLocation[];
}
