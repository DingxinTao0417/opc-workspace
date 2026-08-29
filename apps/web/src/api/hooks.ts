import {
  keepPreviousData,
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { useRef } from "react";
import {
  ApiError,
  applyBusinessDataImport,
  applyBusinessPackageImport,
  batchUpdateTasks,
  createBackup,
  drillBackupRestore,
  cancelFocusSession,
  createClient,
  createClientActivity,
  createClientAttachment,
  createClientActorLink,
  createFocusSession,
  createReminder,
  createPersonActor,
  createProjectNote,
  createProjectAttachment,
  createTag,
  createTask,
  createTaskSavedView,
  createTaskAssignment,
  createProject,
  deleteClient,
  deleteClientActivity,
  deleteClientAttachment,
  deleteClientActorLink,
  deleteBackup,
  deleteProject,
  deleteProjectNote,
  deleteProjectAttachment,
  deleteTag,
  deleteTask,
  deleteTaskSavedView,
  deleteTaskArtifact,
  cancelReminder,
  downloadTaskArtifact,
  downloadClientAttachment,
  downloadProjectAttachment,
  downloadBusinessDataExport,
  downloadBusinessPackage,
  downloadDiagnosticPackage,
  previewBusinessDataImport,
  previewBusinessPackageImport,
  endTaskAssignment,
  executeTaskLifecycleCommand,
  getAllActors,
  getAllClients,
  getAllProjects,
  getAllTags,
  getAllTasks,
  getBackups,
  getInboxStats,
  getActor,
  getActors,
  getClient,
  getClientActivities,
  getClientAttachments,
  getClientActorLinks,
  getClients,
  getActiveFocusSession,
  getFocusReport,
  getFocusSessions,
  getHealth,
  getAppSettings,
  commitAppSettingsWithAvatar,
  getInboxItem,
  getInboxItemEvents,
  getInboxItemTasks,
  getInboxItems,
  getReminder,
  getReminders,
  getSearchResults,
  getTags,
  getTask,
  getTaskArtifact,
  getTaskArtifacts,
  getTaskAssignments,
  getTaskEvents,
  getTaskSubmissions,
  getTaskPage,
  getTaskSavedViews,
  getTasks,
  getTodayStats,
  getProject,
  getProjectArtifacts,
  getProjectAttachments,
  getProjectEvents,
  getProjectNotes,
  getProjects,
  pauseFocusSession,
  createInboxItem,
  executeInboxItemCommand,
  forceResolveInboxItem,
  markAllInboxItemsRead,
  linkInboxItemTask,
  recoverFocusSession,
  resetRuntimeConnection,
  reorderTasks,
  reassignTaskAssignment,
  resumeFocusSession,
  reviewTaskSubmission,
  scheduleBackupRestore,
  submitTaskOutput,
  splitInboxItem,
  stopFocusSession,
  updateClient,
  updateClientActivity,
  updateInboxItem,
  updateInboxItemTaskRequirement,
  updateTag,
  updateTask,
  updateTaskSavedView,
  transitionProject,
  updateActor,
  updateProject,
  updateProjectNote,
  updateReminder,
  updateAppSettings,
  unlinkInboxItemTask,
  verifyBackup,
} from "./client";
import type {
  ActorListParams,
  AppSettingUpdate,
  CreateBackupInput,
  BatchUpdateTasksInput,
  CreateTaskSavedViewInput,
  ClientInput,
  ClientActivityListParams,
  ClientAttachmentListParams,
  ClientActorLinkListParams,
  ClientListParams,
  CreateClientActivityInput,
  CreateClientAttachmentInput,
  CreateClientActorLinkInput,
  CreateFocusSessionInput,
  CreateReminderInput,
  CreatePersonActorInput,
  CreateProjectNoteInput,
  CreateProjectAttachmentInput,
  CreateTaskAssignmentInput,
  DeleteTaskArtifactInput,
  DeleteClientActivityInput,
  DeleteClientAttachmentInput,
  DeleteClientActorLinkInput,
  DeleteProjectNoteInput,
  DeleteProjectAttachmentInput,
  EndTaskAssignmentInput,
  FocusSessionCommandInput,
  FocusReportParams,
  FocusSessionListParams,
  FocusSessionSnapshot,
  ForceResolveInboxItemInput,
  CreateInboxItemInput,
  InboxEventListParams,
  InboxItemCommandInput,
  InboxItemListParams,
  InboxItemTaskListParams,
  LinkInboxItemTaskInput,
  MarkAllInboxReadInput,
  CancelReminderInput,
  NewTaskInput,
  ProjectInput,
  ProjectArtifactListParams,
  ProjectAttachmentListParams,
  ProjectEventListParams,
  ProjectNoteListParams,
  ProjectListParams,
  ProjectTransitionAction,
  ReminderListParams,
  SearchListParams,
  ReorderTasksInput,
  SplitInboxItemInput,
  TagInput,
  TagListParams,
  Task,
  TaskArtifactListParams,
  TaskAssignmentListParams,
  TaskEventListParams,
  TaskLifecycleCommandInput,
  TaskListParams,
  UpdateTaskSavedViewInput,
  TaskSubmissionListParams,
  SubmitTaskOutputInput,
  ReviewTaskSubmissionInput,
  ReassignTaskAssignmentInput,
  RecoverFocusSessionInput,
  UpdateActorInput,
  UpdateClientInput,
  UpdateClientActivityInput,
  UpdateInboxItemInput,
  UpdateInboxItemTaskRequirementInput,
  UpdateTagInput,
  UpdateProjectInput,
  UpdateProjectNoteInput,
  UpdateReminderInput,
  UpdateTaskInput,
  UnlinkInboxItemTaskInput,
} from "../types/models";

export const settingsQueryKey = ["settings"] as const;

export const searchQueryKey = ["search"] as const;

export function useSearchQuery(input: SearchListParams, enabled = true) {
  return useQuery({
    queryKey: [...searchQueryKey, input],
    queryFn: () => getSearchResults(input),
    enabled,
    retry: 1,
    retryDelay: 500,
    staleTime: 5_000,
  });
}

export const backupQueryKey = ["backups"] as const;

export function useBackupsQuery(enabled = true) {
  return useQuery({
    queryKey: backupQueryKey,
    queryFn: getBackups,
    enabled,
    retry: 1,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useCreateBackup() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: (input: CreateBackupInput) => {
      const fingerprint = JSON.stringify(input);
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return createBackup(input, attempt.current.key);
    },
    onSuccess: async () => {
      attempt.current = null;
      await queryClient.invalidateQueries({ queryKey: backupQueryKey });
    },
  });
}

export function useVerifyBackup() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => verifyBackup(id),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: backupQueryKey });
    },
  });
}

export function useDrillBackupRestore() {
  return useMutation({
    mutationFn: (id: string) => drillBackupRestore(id),
  });
}

export function useScheduleBackupRestore() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => scheduleBackupRestore(id),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: backupQueryKey });
    },
  });
}

export function useDeleteBackup() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => deleteBackup(id),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: backupQueryKey });
    },
  });
}

export function useExportBusinessData() {
  return useMutation({ mutationFn: downloadBusinessDataExport });
}

export function useExportBusinessPackage() {
  return useMutation({ mutationFn: downloadBusinessPackage });
}

export function usePreviewBusinessDataImport() {
  return useMutation({ mutationFn: previewBusinessDataImport });
}

export function useApplyBusinessDataImport() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: applyBusinessDataImport,
    onSuccess: async () => {
      await queryClient.invalidateQueries();
    },
  });
}

export function usePreviewBusinessPackageImport() {
  return useMutation({ mutationFn: previewBusinessPackageImport });
}

export function useApplyBusinessPackageImport() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: applyBusinessPackageImport,
    onSuccess: async () => {
      await queryClient.invalidateQueries();
    },
  });
}

export function useDownloadDiagnosticPackage() {
  return useMutation({ mutationFn: downloadDiagnosticPackage });
}

export function useAppSettingsQuery(enabled = true) {
  return useQuery({
    queryKey: settingsQueryKey,
    queryFn: getAppSettings,
    enabled,
    retry: 2,
    retryDelay: 500,
    staleTime: 30_000,
  });
}

export function useUpdateAppSettings() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (updates: AppSettingUpdate[]) => updateAppSettings(updates),
    onSuccess: (settings) => {
      queryClient.setQueryData(settingsQueryKey, settings);
    },
    onError: async (error) => {
      if (
        error instanceof ApiError &&
        error.code === "SETTINGS_VERSION_CONFLICT"
      ) {
        await queryClient.invalidateQueries({ queryKey: settingsQueryKey });
      }
    },
  });
}

export function useCommitAppSettingsWithAvatar() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: {
      operation: "replace" | "remove";
      updates: AppSettingUpdate[];
      file?: File;
    }) =>
      commitAppSettingsWithAvatar(input.operation, input.updates, input.file),
    onSuccess: (settings) => {
      queryClient.setQueryData(settingsQueryKey, settings);
    },
    onError: async (error) => {
      if (
        error instanceof ApiError &&
        error.code === "SETTINGS_VERSION_CONFLICT"
      ) {
        await queryClient.invalidateQueries({ queryKey: settingsQueryKey });
      }
    },
  });
}

export const actorQueryKey = ["actors"] as const;
export const actorDetailQueryKey = (id: string) =>
  [...actorQueryKey, "detail", id] as const;

export function useActorsQuery(input: ActorListParams = {}, enabled = true) {
  return useQuery({
    queryKey: [...actorQueryKey, "list", input],
    queryFn: () => getActors(input),
    enabled,
    placeholderData: keepPreviousData,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useAssignmentActorOptionsQuery(enabled = true) {
  return useQuery({
    queryKey: [...actorQueryKey, "assignment-options"],
    queryFn: () => getAllActors({ status: "active" }),
    enabled,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useClientActorOptionsQuery(enabled = true) {
  return useQuery({
    queryKey: [...actorQueryKey, "client-contact-options"],
    queryFn: () => getAllActors({ type: "person", status: "active" }),
    enabled,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useActorQuery(id: string | null) {
  return useQuery({
    queryKey: actorDetailQueryKey(id ?? "closed"),
    queryFn: () => getActor(id!),
    enabled: Boolean(id),
    retry: 1,
  });
}

export function useCreatePersonActor() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: (input: CreatePersonActorInput) => {
      const fingerprint = JSON.stringify(input);
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return createPersonActor(input, attempt.current.key);
    },
    onSuccess: async (actor) => {
      attempt.current = null;
      queryClient.setQueryData(actorDetailQueryKey(actor.id), actor);
      await queryClient.invalidateQueries({ queryKey: actorQueryKey });
    },
  });
}

export function useUpdateActor() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: UpdateActorInput }) =>
      updateActor(id, input),
    onSuccess: async (actor) => {
      queryClient.setQueryData(actorDetailQueryKey(actor.id), actor);
      await queryClient.invalidateQueries({ queryKey: actorQueryKey });
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await queryClient.invalidateQueries({ queryKey: actorQueryKey });
      }
    },
  });
}

export const clientQueryKey = ["clients"] as const;
export const clientDetailQueryKey = (id: string) =>
  [...clientQueryKey, "detail", id] as const;

export function useClientsQuery(input: ClientListParams = {}, enabled = true) {
  return useQuery({
    queryKey: [...clientQueryKey, "list", input],
    queryFn: () => getClients(input),
    enabled,
    placeholderData: keepPreviousData,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useClientOptionsQuery(enabled = true) {
  return useQuery({
    queryKey: [...clientQueryKey, "options"],
    queryFn: () => getAllClients({ sort: "name" }),
    enabled,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useClientQuery(id: string | null) {
  return useQuery({
    queryKey: clientDetailQueryKey(id ?? "missing"),
    queryFn: () => getClient(id!),
    enabled: Boolean(id),
    retry: 1,
  });
}

export function useCreateClient() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: (input: ClientInput) => {
      const fingerprint = JSON.stringify(input);
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return createClient(input, attempt.current.key);
    },
    onSuccess: async (client) => {
      attempt.current = null;
      queryClient.setQueryData(clientDetailQueryKey(client.id), client);
      await queryClient.invalidateQueries({ queryKey: clientQueryKey });
    },
  });
}

export function useUpdateClient() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: UpdateClientInput }) =>
      updateClient(id, input),
    onSuccess: async (client) => {
      queryClient.setQueryData(clientDetailQueryKey(client.id), client);
      await queryClient.invalidateQueries({ queryKey: clientQueryKey });
      await queryClient.invalidateQueries({ queryKey: projectQueryKey });
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await queryClient.invalidateQueries({ queryKey: clientQueryKey });
        await queryClient.invalidateQueries({ queryKey: projectQueryKey });
      }
    },
  });
}

export function useDeleteClient() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      expectedVersion,
    }: {
      id: string;
      expectedVersion: number;
    }) => deleteClient(id, expectedVersion),
    onSuccess: async (_, variables) => {
      queryClient.removeQueries({
        queryKey: clientDetailQueryKey(variables.id),
      });
      await queryClient.invalidateQueries({ queryKey: clientQueryKey });
      await queryClient.invalidateQueries({ queryKey: projectQueryKey });
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await queryClient.invalidateQueries({ queryKey: clientQueryKey });
        await queryClient.invalidateQueries({ queryKey: projectQueryKey });
      }
    },
  });
}

export const clientActivityQueryKey = (clientId: string) =>
  [...clientDetailQueryKey(clientId), "activities"] as const;

export function useClientActivitiesQuery(
  clientId: string | null,
  input: ClientActivityListParams = {},
) {
  return useQuery({
    queryKey: [...clientActivityQueryKey(clientId ?? "missing"), "list", input],
    queryFn: () => getClientActivities(clientId!, input),
    enabled: Boolean(clientId),
    placeholderData: keepPreviousData,
    retry: 1,
  });
}

export function useCreateClientActivity() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: ({
      clientId,
      input,
    }: {
      clientId: string;
      input: CreateClientActivityInput;
    }) => {
      const fingerprint = JSON.stringify({ clientId, input });
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return createClientActivity(clientId, input, attempt.current.key);
    },
    onSuccess: async (activity) => {
      attempt.current = null;
      await queryClient.invalidateQueries({
        queryKey: clientActivityQueryKey(activity.clientId),
      });
      await queryClient.invalidateQueries({ queryKey: clientQueryKey });
    },
  });
}

export function useUpdateClientActivity() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      input,
    }: {
      id: string;
      input: UpdateClientActivityInput;
    }) => updateClientActivity(id, input),
    onSuccess: async (activity) => {
      await queryClient.invalidateQueries({
        queryKey: clientActivityQueryKey(activity.clientId),
      });
      await queryClient.invalidateQueries({ queryKey: clientQueryKey });
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await queryClient.invalidateQueries({ queryKey: clientQueryKey });
      }
    },
  });
}

export function useDeleteClientActivity() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      input,
    }: {
      id: string;
      input: DeleteClientActivityInput;
    }) => deleteClientActivity(id, input),
    onSuccess: async (activity) => {
      await queryClient.invalidateQueries({
        queryKey: clientActivityQueryKey(activity.clientId),
      });
      await queryClient.invalidateQueries({ queryKey: clientQueryKey });
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await queryClient.invalidateQueries({ queryKey: clientQueryKey });
      }
    },
  });
}

export const clientAttachmentQueryKey = (clientId: string) =>
  [...clientDetailQueryKey(clientId), "attachments"] as const;

export function useClientAttachmentsQuery(
  clientId: string | null,
  input: ClientAttachmentListParams = {},
) {
  return useQuery({
    queryKey: [
      ...clientAttachmentQueryKey(clientId ?? "missing"),
      "list",
      input,
    ],
    queryFn: () => getClientAttachments(clientId!, input),
    enabled: Boolean(clientId),
    placeholderData: keepPreviousData,
    retry: 1,
  });
}

export function useCreateClientAttachment() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: ({
      clientId,
      input,
    }: {
      clientId: string;
      input: CreateClientAttachmentInput;
    }) => {
      const fingerprint = JSON.stringify({
        clientId,
        name: input.name,
        activityId: input.activityId,
        expectedVersion: input.expectedVersion,
        fileName: input.file.name,
        fileSize: input.file.size,
        fileModified: input.file.lastModified,
      });
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return createClientAttachment(clientId, input, attempt.current.key);
    },
    onSuccess: async (attachment) => {
      attempt.current = null;
      await queryClient.invalidateQueries({
        queryKey: clientAttachmentQueryKey(attachment.clientId),
      });
      await queryClient.invalidateQueries({ queryKey: clientQueryKey });
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await queryClient.invalidateQueries({ queryKey: clientQueryKey });
      }
    },
  });
}

export function useDeleteClientAttachment() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: ({
      id,
      clientId,
      input,
    }: {
      id: string;
      clientId: string;
      input: DeleteClientAttachmentInput;
    }) => {
      const fingerprint = JSON.stringify({ id, clientId, input });
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return deleteClientAttachment(id, input, attempt.current.key);
    },
    onSuccess: async (attachment) => {
      attempt.current = null;
      await queryClient.invalidateQueries({
        queryKey: clientAttachmentQueryKey(attachment.clientId),
      });
      await queryClient.invalidateQueries({ queryKey: clientQueryKey });
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await queryClient.invalidateQueries({ queryKey: clientQueryKey });
      }
    },
  });
}

export function useDownloadClientAttachment() {
  return useMutation({
    mutationFn: ({ id, name }: { id: string; name: string }) =>
      downloadClientAttachment(id, name),
  });
}

export const clientActorLinkQueryKey = (clientId: string) =>
  [...clientDetailQueryKey(clientId), "actor-links"] as const;

export function useClientActorLinksQuery(
  clientId: string | null,
  input: ClientActorLinkListParams = {},
) {
  return useQuery({
    queryKey: [
      ...clientActorLinkQueryKey(clientId ?? "missing"),
      "list",
      input,
    ],
    queryFn: () => getClientActorLinks(clientId!, input),
    enabled: Boolean(clientId),
    placeholderData: keepPreviousData,
    retry: 1,
  });
}

export function useCreateClientActorLink() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: ({
      clientId,
      input,
    }: {
      clientId: string;
      input: CreateClientActorLinkInput;
    }) => {
      const fingerprint = JSON.stringify({ clientId, input });
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return createClientActorLink(clientId, input, attempt.current.key);
    },
    onSuccess: async (link) => {
      attempt.current = null;
      await queryClient.invalidateQueries({
        queryKey: clientActorLinkQueryKey(link.clientId),
      });
      await queryClient.invalidateQueries({ queryKey: clientQueryKey });
      await queryClient.invalidateQueries({ queryKey: actorQueryKey });
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await queryClient.invalidateQueries({ queryKey: clientQueryKey });
      }
    },
  });
}

export function useDeleteClientActorLink() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: ({
      id,
      clientId,
      input,
    }: {
      id: string;
      clientId: string;
      input: DeleteClientActorLinkInput;
    }) => {
      const fingerprint = JSON.stringify({ id, clientId, input });
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return deleteClientActorLink(id, input, attempt.current.key);
    },
    onSuccess: async (link) => {
      attempt.current = null;
      await queryClient.invalidateQueries({
        queryKey: clientActorLinkQueryKey(link.clientId),
      });
      await queryClient.invalidateQueries({ queryKey: clientQueryKey });
      await queryClient.invalidateQueries({ queryKey: actorQueryKey });
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await queryClient.invalidateQueries({ queryKey: clientQueryKey });
      }
    },
  });
}

export const inboxQueryKey = ["inbox-items"] as const;
export const inboxStatsQueryKey = [...inboxQueryKey, "stats"] as const;

export function useInboxStatsQuery() {
  return useQuery({
    queryKey: inboxStatsQueryKey,
    queryFn: getInboxStats,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
    refetchInterval: 15_000,
  });
}
export const INBOX_LIST_REFRESH_INTERVAL_MS = 15_000;
export const inboxDetailQueryKey = (id: string) =>
  [...inboxQueryKey, "detail", id] as const;
export const inboxEventQueryKey = (id: string) =>
  [...inboxQueryKey, "events", id] as const;
export const inboxTaskRelationQueryKey = (id: string) =>
  [...inboxQueryKey, "tasks", id] as const;

export const reminderQueryKey = ["reminders"] as const;
export const reminderDetailQueryKey = (id: string) =>
  [...reminderQueryKey, "detail", id] as const;

export function useRemindersQuery(
  input: ReminderListParams = {},
  enabled = true,
) {
  return useQuery({
    queryKey: [...reminderQueryKey, "list", input],
    queryFn: () => getReminders(input),
    enabled,
    placeholderData: keepPreviousData,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
    refetchInterval: INBOX_LIST_REFRESH_INTERVAL_MS,
  });
}

export function useReminderQuery(id: string | null) {
  return useQuery({
    queryKey: reminderDetailQueryKey(id ?? "closed"),
    queryFn: () => getReminder(id!),
    enabled: Boolean(id),
    retry: 1,
    refetchInterval: INBOX_LIST_REFRESH_INTERVAL_MS,
  });
}

async function invalidateReminderFacts(
  queryClient: QueryClient,
  id?: string,
): Promise<void> {
  await queryClient.invalidateQueries({ queryKey: reminderQueryKey });
  if (id) {
    await queryClient.invalidateQueries({
      queryKey: reminderDetailQueryKey(id),
    });
  }
}

function reminderFactsNeedRefresh(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    (error.status === 404 ||
      error.status === 409 ||
      error.code === "VERSION_CONFLICT")
  );
}

export function useCreateReminder() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: (input: CreateReminderInput) => {
      const fingerprint = JSON.stringify(input);
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return createReminder(input, attempt.current.key);
    },
    onSuccess: async (reminder) => {
      attempt.current = null;
      queryClient.setQueryData(reminderDetailQueryKey(reminder.id), reminder);
      await invalidateReminderFacts(queryClient, reminder.id);
    },
  });
}

export function useUpdateReminder() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: UpdateReminderInput }) =>
      updateReminder(id, input),
    onSuccess: async (reminder) => {
      queryClient.setQueryData(reminderDetailQueryKey(reminder.id), reminder);
      await invalidateReminderFacts(queryClient, reminder.id);
    },
    onError: async (error, variables) => {
      if (reminderFactsNeedRefresh(error)) {
        await invalidateReminderFacts(queryClient, variables.id);
      }
    },
  });
}

export function useCancelReminder() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: (input: CancelReminderInput) => {
      const fingerprint = JSON.stringify(input);
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return cancelReminder(input, attempt.current.key);
    },
    onSuccess: async (reminder) => {
      attempt.current = null;
      queryClient.setQueryData(reminderDetailQueryKey(reminder.id), reminder);
      await invalidateReminderFacts(queryClient, reminder.id);
    },
    onError: async (error, variables) => {
      if (reminderFactsNeedRefresh(error)) {
        await invalidateReminderFacts(queryClient, variables.id);
      }
    },
  });
}

export function useInboxItemsQuery(
  input: InboxItemListParams = {},
  enabled = true,
) {
  return useQuery({
    queryKey: [...inboxQueryKey, "list", input],
    queryFn: () => getInboxItems(input),
    enabled,
    placeholderData: keepPreviousData,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
    refetchInterval: INBOX_LIST_REFRESH_INTERVAL_MS,
  });
}

export function useInboxItemQuery(id: string | null) {
  return useQuery({
    queryKey: inboxDetailQueryKey(id ?? "closed"),
    queryFn: () => getInboxItem(id!),
    enabled: Boolean(id),
    retry: 1,
  });
}

export function useInboxItemEventsQuery(
  id: string | null,
  input: Omit<InboxEventListParams, "page"> = {},
  enabled = true,
) {
  const query = { pageSize: input.pageSize ?? 20 };
  return useInfiniteQuery({
    queryKey: [...inboxEventQueryKey(id ?? "closed"), "timeline", query],
    queryFn: ({ pageParam }) =>
      getInboxItemEvents(id!, { ...query, page: pageParam }),
    initialPageParam: 1,
    getNextPageParam: (lastPage) =>
      lastPage.meta.page * lastPage.meta.pageSize < lastPage.meta.total
        ? lastPage.meta.page + 1
        : undefined,
    enabled: Boolean(id) && enabled,
    retry: 1,
    staleTime: 10_000,
  });
}

export function useInboxItemTasksQuery(
  id: string | null,
  input: Omit<InboxItemTaskListParams, "page"> = {},
  enabled = true,
) {
  const query = { pageSize: input.pageSize ?? 20 };
  return useInfiniteQuery({
    queryKey: [...inboxTaskRelationQueryKey(id ?? "closed"), "history", query],
    queryFn: ({ pageParam }) =>
      getInboxItemTasks(id!, { ...query, page: pageParam }),
    initialPageParam: 1,
    getNextPageParam: (lastPage) =>
      lastPage.meta.page * lastPage.meta.pageSize < lastPage.meta.total
        ? lastPage.meta.page + 1
        : undefined,
    enabled: Boolean(id) && enabled,
    retry: 1,
    staleTime: 10_000,
  });
}

async function invalidateInboxFacts(
  queryClient: QueryClient,
  id?: string,
): Promise<void> {
  await queryClient.invalidateQueries({ queryKey: inboxQueryKey });
  if (id) {
    await queryClient.invalidateQueries({ queryKey: inboxEventQueryKey(id) });
  }
}

function inboxFactsNeedRefresh(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    (error.status === 404 ||
      error.status === 409 ||
      error.code === "VERSION_CONFLICT")
  );
}

export function useCreateInboxItem() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: (input: CreateInboxItemInput) => {
      const fingerprint = JSON.stringify(input);
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return createInboxItem(input, attempt.current.key);
    },
    onSuccess: async (item) => {
      attempt.current = null;
      queryClient.setQueryData(inboxDetailQueryKey(item.id), item);
      await invalidateInboxFacts(queryClient, item.id);
    },
  });
}

export function useUpdateInboxItem() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: UpdateInboxItemInput }) =>
      updateInboxItem(id, input),
    onSuccess: async (item) => {
      queryClient.setQueryData(inboxDetailQueryKey(item.id), item);
      await invalidateInboxFacts(queryClient, item.id);
    },
    onError: async (error, variables) => {
      if (inboxFactsNeedRefresh(error)) {
        await invalidateInboxFacts(queryClient, variables.id);
      }
    },
  });
}

export function useInboxItemCommand() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: (input: InboxItemCommandInput) => {
      const fingerprint = JSON.stringify(input);
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return executeInboxItemCommand(input, attempt.current.key);
    },
    onSuccess: async (item) => {
      attempt.current = null;
      queryClient.setQueryData(inboxDetailQueryKey(item.id), item);
      await invalidateInboxFacts(queryClient, item.id);
    },
    onError: async (error, variables) => {
      if (inboxFactsNeedRefresh(error)) {
        await invalidateInboxFacts(queryClient, variables.id);
      }
    },
  });
}

export function useMarkAllInboxItemsRead() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: (input: MarkAllInboxReadInput) => {
      const fingerprint = JSON.stringify(input);
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return markAllInboxItemsRead(input.throughCreatedAt, attempt.current.key);
    },
    onSuccess: async () => {
      attempt.current = null;
      await invalidateInboxFacts(queryClient);
    },
  });
}

function useStableInboxTaskMutationKey<TInput>() {
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return {
    keyFor(input: TInput): string {
      const fingerprint = JSON.stringify(input);
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return attempt.current.key;
    },
    reset(): void {
      attempt.current = null;
    },
  };
}

async function applyInboxTaskMutationResult(
  queryClient: QueryClient,
  result: Awaited<ReturnType<typeof linkInboxItemTask>>,
): Promise<void> {
  queryClient.setQueryData(
    inboxDetailQueryKey(result.inboxItem.id),
    result.inboxItem,
  );
  await invalidateInboxFacts(queryClient, result.inboxItem.id);
}

export function useLinkInboxItemTask() {
  const queryClient = useQueryClient();
  const attempt = useStableInboxTaskMutationKey<LinkInboxItemTaskInput>();
  return useMutation({
    mutationFn: (input: LinkInboxItemTaskInput) =>
      linkInboxItemTask(input, attempt.keyFor(input)),
    onSuccess: async (result) => {
      attempt.reset();
      await applyInboxTaskMutationResult(queryClient, result);
    },
    onError: async (error, input) => {
      if (inboxFactsNeedRefresh(error)) {
        await invalidateInboxFacts(queryClient, input.inboxItemId);
      }
    },
  });
}

export function useUpdateInboxItemTaskRequirement() {
  const queryClient = useQueryClient();
  const attempt =
    useStableInboxTaskMutationKey<UpdateInboxItemTaskRequirementInput>();
  return useMutation({
    mutationFn: (input: UpdateInboxItemTaskRequirementInput) =>
      updateInboxItemTaskRequirement(input, attempt.keyFor(input)),
    onSuccess: async (result) => {
      attempt.reset();
      await applyInboxTaskMutationResult(queryClient, result);
    },
    onError: async (error, input) => {
      if (inboxFactsNeedRefresh(error)) {
        await invalidateInboxFacts(queryClient, input.inboxItemId);
      }
    },
  });
}

export function useUnlinkInboxItemTask() {
  const queryClient = useQueryClient();
  const attempt = useStableInboxTaskMutationKey<UnlinkInboxItemTaskInput>();
  return useMutation({
    mutationFn: (input: UnlinkInboxItemTaskInput) =>
      unlinkInboxItemTask(input, attempt.keyFor(input)),
    onSuccess: async (result) => {
      attempt.reset();
      await applyInboxTaskMutationResult(queryClient, result);
    },
    onError: async (error, input) => {
      if (inboxFactsNeedRefresh(error)) {
        await invalidateInboxFacts(queryClient, input.inboxItemId);
      }
    },
  });
}

export function useSplitInboxItem() {
  const queryClient = useQueryClient();
  const attempt = useStableInboxTaskMutationKey<SplitInboxItemInput>();
  return useMutation({
    mutationFn: (input: SplitInboxItemInput) =>
      splitInboxItem(input, attempt.keyFor(input)),
    onSuccess: async (result) => {
      attempt.reset();
      queryClient.setQueryData(
        inboxDetailQueryKey(result.inboxItem.id),
        result.inboxItem,
      );
      await Promise.all([
        invalidateInboxFacts(queryClient, result.inboxItem.id),
        queryClient.invalidateQueries({ queryKey: taskQueryKey }),
      ]);
    },
    onError: async (error, input) => {
      if (inboxFactsNeedRefresh(error)) {
        await invalidateInboxFacts(queryClient, input.inboxItemId);
      }
    },
  });
}

export function useForceResolveInboxItem() {
  const queryClient = useQueryClient();
  const attempt = useStableInboxTaskMutationKey<ForceResolveInboxItemInput>();
  return useMutation({
    mutationFn: (input: ForceResolveInboxItemInput) =>
      forceResolveInboxItem(input, attempt.keyFor(input)),
    onSuccess: async (item) => {
      attempt.reset();
      queryClient.setQueryData(inboxDetailQueryKey(item.id), item);
      await invalidateInboxFacts(queryClient, item.id);
    },
    onError: async (error, input) => {
      if (inboxFactsNeedRefresh(error)) {
        await invalidateInboxFacts(queryClient, input.id);
      }
    },
  });
}

export const taskQueryKey = ["tasks"] as const;

export const taskDetailQueryKey = (id: string) => ["tasks", id] as const;

export function useHealthQuery(enabled = true) {
  return useQuery({
    queryKey: ["health"],
    queryFn: getHealth,
    enabled,
    retry: false,
    refetchInterval: 15_000,
  });
}

export function useTasksQuery(
  options: { projectId?: string; loadAll?: boolean } = {},
) {
  return useQuery({
    queryKey: [...taskQueryKey, "list", options],
    queryFn: () =>
      options.loadAll
        ? getAllTasks({ projectId: options.projectId })
        : getTasks({ projectId: options.projectId }),
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

function shiftDateKey(dateKey: string, days: number): string {
  const [year, month, day] = dateKey.split("-").map(Number);
  const date = new Date(Date.UTC(year, month - 1, day + days));
  return date.toISOString().slice(0, 10);
}

function endOfWeekKey(dateKey: string): string {
  const [year, month, day] = dateKey.split("-").map(Number);
  const weekday = new Date(Date.UTC(year, month - 1, day)).getUTCDay();
  return shiftDateKey(dateKey, weekday === 0 ? 0 : 7 - weekday);
}

export interface TodayTaskGroups {
  overdue: Task[];
  today: Task[];
  thisWeek: Task[];
  unscheduled: Task[];
}

export function useTodayTaskGroupsQuery(dateKey: string) {
  return useQuery({
    queryKey: [...taskQueryKey, "today-groups", dateKey],
    queryFn: async (): Promise<TodayTaskGroups> => {
      const tomorrow = shiftDateKey(dateKey, 1);
      const endOfWeek = endOfWeekKey(dateKey);
      const [overdue, today, thisWeek, unscheduled] = await Promise.all([
        getAllTasks({ status: "active", plannedTo: shiftDateKey(dateKey, -1) }),
        getAllTasks({ status: "active", plannedDate: dateKey }),
        tomorrow <= endOfWeek
          ? getAllTasks({
              status: "active",
              plannedFrom: tomorrow,
              plannedTo: endOfWeek,
            })
          : Promise.resolve([]),
        getAllTasks({ status: "active", plannedState: "unscheduled" }),
      ]);
      return { overdue, today, thisWeek, unscheduled };
    },
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export const taskPageQueryKey = (input: TaskListParams) =>
  [...taskQueryKey, "page", input] as const;

export function useTaskPageQuery(input: TaskListParams = {}, enabled = true) {
  return useQuery({
    queryKey: taskPageQueryKey(input),
    queryFn: () => getTaskPage(input),
    enabled,
    placeholderData: keepPreviousData,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export const taskSavedViewQueryKey = ["task-saved-views"] as const;

export function useTaskSavedViewsQuery() {
  return useQuery({
    queryKey: taskSavedViewQueryKey,
    queryFn: getTaskSavedViews,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useCreateTaskSavedView() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateTaskSavedViewInput) => createTaskSavedView(input),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: taskSavedViewQueryKey });
    },
  });
}

export function useUpdateTaskSavedView() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      input,
    }: {
      id: string;
      input: UpdateTaskSavedViewInput;
    }) => updateTaskSavedView(id, input),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: taskSavedViewQueryKey });
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await queryClient.invalidateQueries({
          queryKey: taskSavedViewQueryKey,
        });
      }
    },
  });
}

export function useDeleteTaskSavedView() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      expectedVersion,
    }: {
      id: string;
      expectedVersion: number;
    }) => deleteTaskSavedView(id, expectedVersion),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: taskSavedViewQueryKey });
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await queryClient.invalidateQueries({
          queryKey: taskSavedViewQueryKey,
        });
      }
    },
  });
}

export function useTaskQuery(id: string | null) {
  return useQuery({
    queryKey: taskDetailQueryKey(id ?? "closed"),
    queryFn: () => getTask(id!),
    enabled: Boolean(id),
    retry: 1,
  });
}

export const taskAssignmentQueryKey = (taskId: string) =>
  ["task-assignments", taskId] as const;

export const taskEventQueryKey = (taskId: string) =>
  ["task-events", taskId] as const;

export const taskSubmissionQueryKey = (taskId: string) =>
  ["task-submissions", taskId] as const;

export const taskArtifactQueryKey = (taskId: string) =>
  ["task-artifacts", taskId] as const;

export const taskArtifactDetailQueryKey = (artifactId: string) =>
  ["task-artifact", artifactId] as const;

export function useTaskEventsQuery(
  taskId: string | null,
  input: Omit<TaskEventListParams, "page"> = {},
  enabled = true,
) {
  const query = { pageSize: input.pageSize ?? 20 };
  return useInfiniteQuery({
    queryKey: [...taskEventQueryKey(taskId ?? "closed"), "timeline", query],
    queryFn: ({ pageParam }) =>
      getTaskEvents(taskId!, { ...query, page: pageParam }),
    initialPageParam: 1,
    getNextPageParam: (lastPage) =>
      lastPage.meta.page * lastPage.meta.pageSize < lastPage.meta.total
        ? lastPage.meta.page + 1
        : undefined,
    enabled: Boolean(taskId) && enabled,
    retry: 1,
    staleTime: 10_000,
  });
}

export function useTaskSubmissionsQuery(
  taskId: string | null,
  input: Omit<TaskSubmissionListParams, "page"> = {},
  enabled = true,
) {
  const query = { pageSize: input.pageSize ?? 10 };
  return useInfiniteQuery({
    queryKey: [...taskSubmissionQueryKey(taskId ?? "closed"), "history", query],
    queryFn: ({ pageParam }) =>
      getTaskSubmissions(taskId!, { ...query, page: pageParam }),
    initialPageParam: 1,
    getNextPageParam: (lastPage) =>
      lastPage.meta.page * lastPage.meta.pageSize < lastPage.meta.total
        ? lastPage.meta.page + 1
        : undefined,
    enabled: Boolean(taskId) && enabled,
    retry: 1,
    staleTime: 10_000,
  });
}

export function useTaskArtifactsQuery(
  taskId: string | null,
  input: Omit<TaskArtifactListParams, "page"> = {},
  enabled = true,
) {
  const query = {
    pageSize: input.pageSize ?? 20,
    submissionId: input.submissionId,
    includeDeleted: input.includeDeleted ?? true,
  };
  return useInfiniteQuery({
    queryKey: [...taskArtifactQueryKey(taskId ?? "closed"), "history", query],
    queryFn: ({ pageParam }) =>
      getTaskArtifacts(taskId!, { ...query, page: pageParam }),
    initialPageParam: 1,
    getNextPageParam: (lastPage) =>
      lastPage.meta.page * lastPage.meta.pageSize < lastPage.meta.total
        ? lastPage.meta.page + 1
        : undefined,
    enabled: Boolean(taskId) && enabled,
    retry: 1,
    staleTime: 10_000,
  });
}

export function useTaskArtifactQuery(
  artifactId: string | null,
  enabled = true,
) {
  return useQuery({
    queryKey: taskArtifactDetailQueryKey(artifactId ?? "closed"),
    queryFn: () => getTaskArtifact(artifactId!),
    enabled: Boolean(artifactId) && enabled,
    retry: 1,
  });
}

export function useTaskAssignmentsQuery(
  taskId: string | null,
  input: Omit<TaskAssignmentListParams, "page"> = {},
  enabled = true,
) {
  const query = {
    pageSize: input.pageSize ?? 20,
    role: input.role,
    sort: input.sort?.trim() || "-assigned_at",
  };
  return useInfiniteQuery({
    queryKey: [...taskAssignmentQueryKey(taskId ?? "closed"), "history", query],
    queryFn: ({ pageParam }) =>
      getTaskAssignments(taskId!, { ...query, page: pageParam }),
    initialPageParam: 1,
    getNextPageParam: (lastPage) =>
      lastPage.meta.page * lastPage.meta.pageSize < lastPage.meta.total
        ? lastPage.meta.page + 1
        : undefined,
    enabled: Boolean(taskId) && enabled,
    placeholderData: keepPreviousData,
    retry: 1,
    staleTime: 10_000,
  });
}

function setTaskDetailIfNotOlder(queryClient: QueryClient, task: Task): void {
  queryClient.setQueryData<Task>(taskDetailQueryKey(task.id), (current) =>
    current && current.version > task.version ? current : task,
  );
}

async function invalidateTaskAssignments(
  queryClient: QueryClient,
  taskId: string,
): Promise<void> {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: taskQueryKey }),
    queryClient.invalidateQueries({
      queryKey: taskAssignmentQueryKey(taskId),
    }),
    queryClient.invalidateQueries({ queryKey: taskEventQueryKey(taskId) }),
  ]);
}

function assignmentErrorNeedsRefresh(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    (error.status === 404 ||
      error.status === 409 ||
      error.code === "ASSIGNMENT_ACTOR_NOT_ACTIVE")
  );
}

export function useCreateTaskAssignment() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: ({
      taskId,
      input,
    }: {
      taskId: string;
      input: CreateTaskAssignmentInput;
    }) => {
      const fingerprint = JSON.stringify({ taskId, input });
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return createTaskAssignment(taskId, input, attempt.current.key);
    },
    onSuccess: async (result) => {
      attempt.current = null;
      setTaskDetailIfNotOlder(queryClient, result.task);
      await invalidateTaskAssignments(queryClient, result.task.id);
    },
    onError: async (error, variables) => {
      if (assignmentErrorNeedsRefresh(error)) {
        await invalidateTaskAssignments(queryClient, variables.taskId);
      }
    },
  });
}

export function useReassignTaskAssignment() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: ({
      taskId,
      input,
    }: {
      taskId: string;
      input: ReassignTaskAssignmentInput;
    }) => {
      const fingerprint = JSON.stringify({ taskId, input });
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return reassignTaskAssignment(taskId, input, attempt.current.key);
    },
    onSuccess: async (result) => {
      attempt.current = null;
      setTaskDetailIfNotOlder(queryClient, result.task);
      await invalidateTaskAssignments(queryClient, result.task.id);
    },
    onError: async (error, variables) => {
      if (assignmentErrorNeedsRefresh(error)) {
        await invalidateTaskAssignments(queryClient, variables.taskId);
      }
    },
  });
}

export function useEndTaskAssignment() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: ({
      taskId: _taskId,
      assignmentId,
      input,
    }: {
      taskId: string;
      assignmentId: string;
      input: EndTaskAssignmentInput;
    }) => {
      const fingerprint = JSON.stringify({ assignmentId, input });
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return endTaskAssignment(assignmentId, input, attempt.current.key);
    },
    onSuccess: async (result) => {
      attempt.current = null;
      setTaskDetailIfNotOlder(queryClient, result.task);
      await invalidateTaskAssignments(queryClient, result.task.id);
    },
    onError: async (error, variables) => {
      if (assignmentErrorNeedsRefresh(error)) {
        await invalidateTaskAssignments(queryClient, variables.taskId);
      }
    },
  });
}

export function useTaskOptionsQuery(enabled = true) {
  return useQuery({
    queryKey: [...taskQueryKey, "options"],
    queryFn: () => getAllTasks({ sort: "title" }),
    enabled,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export const focusSessionQueryKey = ["focus-sessions", "active"] as const;

const openFocusStatuses = new Set(["active", "paused", "recovery_pending"]);

function activeFocusSnapshot(
  snapshot: FocusSessionSnapshot,
): FocusSessionSnapshot {
  if (snapshot.session && !openFocusStatuses.has(snapshot.session.status)) {
    return { ...snapshot, session: null };
  }
  return snapshot;
}

function cacheFocusSnapshot(
  queryClient: QueryClient,
  snapshot: FocusSessionSnapshot,
) {
  queryClient.setQueryData(focusSessionQueryKey, activeFocusSnapshot(snapshot));
}

async function invalidateFocusDependents(queryClient: QueryClient) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: focusSessionQueryKey }),
    queryClient.invalidateQueries({ queryKey: taskQueryKey }),
    queryClient.invalidateQueries({ queryKey: projectQueryKey }),
    queryClient.invalidateQueries({ queryKey: ["stats", "today"] }),
    queryClient.invalidateQueries({ queryKey: ["stats", "focus"] }),
    queryClient.invalidateQueries({ queryKey: ["focus-sessions", "history"] }),
  ]);
}

function focusErrorNeedsRefresh(error: unknown): boolean {
  return (
    error instanceof ApiError && (error.status === 404 || error.status === 409)
  );
}

function focusCommandCanRetry(failureCount: number, error: unknown): boolean {
  return (
    failureCount < 2 &&
    error instanceof ApiError &&
    (error.code === "NETWORK_ERROR" || error.code === "TIMEOUT")
  );
}

export function useActiveFocusSessionQuery() {
  return useQuery({
    queryKey: focusSessionQueryKey,
    queryFn: getActiveFocusSession,
    refetchInterval: (query) =>
      query.state.data?.session?.status === "active" ? 15_000 : false,
    refetchOnWindowFocus: true,
    retry: 2,
    retryDelay: 500,
    staleTime: 5_000,
  });
}

export function useFocusSessionHistoryQuery(
  input: FocusSessionListParams,
  enabled = true,
) {
  return useQuery({
    queryKey: ["focus-sessions", "history", input],
    queryFn: () => getFocusSessions(input),
    enabled,
    placeholderData: keepPreviousData,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useFocusReportQuery(input: FocusReportParams, enabled = true) {
  return useQuery({
    queryKey: ["stats", "focus", input],
    queryFn: () => getFocusReport(input),
    enabled,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useCreateFocusSession() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: (input: CreateFocusSessionInput) => {
      const fingerprint = JSON.stringify(input);
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return createFocusSession(input, attempt.current.key);
    },
    onSuccess: async (snapshot) => {
      attempt.current = null;
      cacheFocusSnapshot(queryClient, snapshot);
      await queryClient.invalidateQueries({ queryKey: focusSessionQueryKey });
    },
    onError: async (error) => {
      if (focusErrorNeedsRefresh(error)) {
        await queryClient.invalidateQueries({ queryKey: focusSessionQueryKey });
      }
    },
  });
}

function useSimpleFocusCommand(
  command: (
    id: string,
    expectedVersion: number,
  ) => Promise<FocusSessionSnapshot>,
) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, expectedVersion }: FocusSessionCommandInput) =>
      command(id, expectedVersion),
    onSuccess: (snapshot) => cacheFocusSnapshot(queryClient, snapshot),
    onError: async (error) => {
      if (focusErrorNeedsRefresh(error)) {
        await queryClient.invalidateQueries({ queryKey: focusSessionQueryKey });
      }
    },
  });
}

export function usePauseFocusSession() {
  return useSimpleFocusCommand(pauseFocusSession);
}

export function useResumeFocusSession() {
  return useSimpleFocusCommand(resumeFocusSession);
}

export function useRecoverFocusSession() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, action, expectedVersion }: RecoverFocusSessionInput) =>
      recoverFocusSession(id, action, expectedVersion),
    onSuccess: async (snapshot) => {
      cacheFocusSnapshot(queryClient, snapshot);
      if (snapshot.session?.status === "interrupted") {
        await invalidateFocusDependents(queryClient);
      }
    },
    onError: async (error) => {
      if (focusErrorNeedsRefresh(error)) {
        await queryClient.invalidateQueries({ queryKey: focusSessionQueryKey });
      }
    },
  });
}

function useIdempotentFocusEndCommand(
  command: (
    id: string,
    expectedVersion: number,
    idempotencyKey: string,
  ) => Promise<FocusSessionSnapshot>,
) {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: ({ id, expectedVersion }: FocusSessionCommandInput) => {
      const fingerprint = `${id}:${expectedVersion}`;
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return command(id, expectedVersion, attempt.current.key);
    },
    onSuccess: async (snapshot) => {
      attempt.current = null;
      cacheFocusSnapshot(queryClient, snapshot);
      await invalidateFocusDependents(queryClient);
    },
    onError: async (error) => {
      if (focusErrorNeedsRefresh(error)) {
        await queryClient.invalidateQueries({ queryKey: focusSessionQueryKey });
      }
    },
    retry: focusCommandCanRetry,
    retryDelay: 500,
  });
}

export function useStopFocusSession() {
  return useIdempotentFocusEndCommand(stopFocusSession);
}

export function useCancelFocusSession() {
  return useIdempotentFocusEndCommand(cancelFocusSession);
}

export function useTodayStatsQuery(date: string) {
  const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  return useQuery({
    queryKey: ["stats", "today", date, timezone],
    queryFn: () => getTodayStats(date, timezone),
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useCreateTask() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: (input: NewTaskInput) => {
      const fingerprint = JSON.stringify(input);
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return createTask(input, attempt.current.key);
    },
    onSuccess: async () => {
      attempt.current = null;
      await queryClient.invalidateQueries({ queryKey: taskQueryKey });
      await queryClient.invalidateQueries({ queryKey: projectQueryKey });
      await queryClient.invalidateQueries({ queryKey: ["stats", "today"] });
    },
  });
}

function taskFromCachedValue(value: unknown, id: string): Task | undefined {
  if (Array.isArray(value)) {
    return value.find(
      (item): item is Task =>
        typeof item === "object" &&
        item !== null &&
        "id" in item &&
        item.id === id,
    );
  }
  if (typeof value !== "object" || value === null) return undefined;
  if ("id" in value && value.id === id && "version" in value) {
    return value as Task;
  }
  if ("items" in value && Array.isArray(value.items)) {
    return taskFromCachedValue(value.items, id);
  }
  return undefined;
}

function resolveTaskVersion(
  queryClient: QueryClient,
  id: string,
  explicitVersion?: number,
): number {
  if (
    typeof explicitVersion === "number" &&
    Number.isInteger(explicitVersion) &&
    explicitVersion >= 1
  ) {
    return explicitVersion;
  }
  const detail = queryClient.getQueryData<Task>(taskDetailQueryKey(id));
  if (detail && detail.version >= 1) return detail.version;
  for (const [, cached] of queryClient.getQueriesData({
    queryKey: taskQueryKey,
  })) {
    const task = taskFromCachedValue(cached, id);
    if (task && task.version >= 1) return task.version;
  }
  throw new ApiError("任务版本缺失，请刷新后重试", {
    code: "EXPECTED_VERSION_REQUIRED",
  });
}

export function useTaskLifecycleCommand() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: ({
      id,
      input,
    }: {
      id: string;
      input: TaskLifecycleCommandInput;
    }) => {
      const fingerprint = JSON.stringify({ id, input });
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return executeTaskLifecycleCommand(id, input, attempt.current.key);
    },
    onSuccess: async (result) => {
      attempt.current = null;
      setTaskDetailIfNotOlder(queryClient, result.task);
      await invalidateTaskOutputAggregate(queryClient, result.task.id);
    },
    onError: async (error, variables) => {
      if (
        error instanceof ApiError &&
        (error.status === 404 || error.status === 409)
      ) {
        await queryClient.invalidateQueries({ queryKey: taskQueryKey });
        await queryClient.invalidateQueries({
          queryKey: taskAssignmentQueryKey(variables.id),
        });
        await queryClient.invalidateQueries({
          queryKey: taskEventQueryKey(variables.id),
        });
        await queryClient.invalidateQueries({
          queryKey: taskSubmissionQueryKey(variables.id),
        });
        await queryClient.invalidateQueries({
          queryKey: taskArtifactQueryKey(variables.id),
        });
        await queryClient.invalidateQueries({ queryKey: inboxQueryKey });
        if (error.status === 409) {
          await queryClient.invalidateQueries({ queryKey: projectQueryKey });
          await queryClient.invalidateQueries({ queryKey: ["stats", "today"] });
        }
      }
    },
  });
}

function submitOutputFingerprint(
  taskId: string,
  input: SubmitTaskOutputInput,
  fileToken: (file: File) => string,
): string {
  return JSON.stringify({
    taskId,
    expectedVersion: input.expectedVersion,
    summary: input.summary,
    artifacts: input.artifacts.map((artifact) => {
      const common = {
        clientRef: artifact.clientRef,
        storageKind: artifact.storageKind,
        name: artifact.name,
        requiresFollowup: artifact.requiresFollowup,
      };
      switch (artifact.storageKind) {
        case "text":
          return { ...common, contentText: artifact.contentText };
        case "link":
          return { ...common, referenceUrl: artifact.referenceUrl };
        case "structured":
          return { ...common, structuredJson: artifact.structuredJson };
        case "file":
          return {
            ...common,
            file: {
              token: fileToken(artifact.file),
              name: artifact.file.name,
              size: artifact.file.size,
              type: artifact.file.type,
              lastModified: artifact.file.lastModified,
            },
          };
      }
    }),
  });
}

async function invalidateTaskOutputAggregate(
  queryClient: QueryClient,
  taskId: string,
): Promise<void> {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: taskQueryKey }),
    queryClient.invalidateQueries({ queryKey: taskSubmissionQueryKey(taskId) }),
    queryClient.invalidateQueries({ queryKey: taskArtifactQueryKey(taskId) }),
    queryClient.invalidateQueries({ queryKey: taskAssignmentQueryKey(taskId) }),
    queryClient.invalidateQueries({ queryKey: taskEventQueryKey(taskId) }),
    queryClient.invalidateQueries({ queryKey: projectQueryKey }),
    queryClient.invalidateQueries({ queryKey: ["stats", "today"] }),
    queryClient.invalidateQueries({ queryKey: inboxQueryKey }),
  ]);
}

function outputErrorNeedsRefresh(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    (error.status === 404 || error.status === 409 || error.status === 410)
  );
}

export function useSubmitTaskOutput() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  const fileTokens = useRef(new WeakMap<File, string>());
  return useMutation({
    mutationFn: ({
      taskId,
      input,
    }: {
      taskId: string;
      input: SubmitTaskOutputInput;
    }) => {
      const fingerprint = submitOutputFingerprint(taskId, input, (file) => {
        const existing = fileTokens.current.get(file);
        if (existing) return existing;
        const token = crypto.randomUUID();
        fileTokens.current.set(file, token);
        return token;
      });
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return submitTaskOutput(taskId, input, attempt.current.key);
    },
    onSuccess: async (result) => {
      attempt.current = null;
      setTaskDetailIfNotOlder(queryClient, result.task);
      await invalidateTaskOutputAggregate(queryClient, result.task.id);
    },
    onError: async (error, variables) => {
      if (outputErrorNeedsRefresh(error)) {
        await invalidateTaskOutputAggregate(queryClient, variables.taskId);
      }
    },
  });
}

export function useReviewTaskSubmission() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: ({
      taskId,
      input,
    }: {
      taskId: string;
      input: ReviewTaskSubmissionInput;
    }) => {
      const fingerprint = JSON.stringify({ taskId, input });
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return reviewTaskSubmission(taskId, input, attempt.current.key);
    },
    onSuccess: async (result) => {
      attempt.current = null;
      setTaskDetailIfNotOlder(queryClient, result.task);
      await invalidateTaskOutputAggregate(queryClient, result.task.id);
    },
    onError: async (error, variables) => {
      if (outputErrorNeedsRefresh(error)) {
        await invalidateTaskOutputAggregate(queryClient, variables.taskId);
      }
    },
  });
}

export function useDeleteTaskArtifact() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: ({
      taskId,
      artifactId,
      input,
    }: {
      taskId: string;
      artifactId: string;
      input: DeleteTaskArtifactInput;
    }) => {
      const fingerprint = JSON.stringify({ taskId, artifactId, input });
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return deleteTaskArtifact(artifactId, taskId, input, attempt.current.key);
    },
    onSuccess: async (result) => {
      attempt.current = null;
      queryClient.removeQueries({
        queryKey: taskArtifactDetailQueryKey(result.artifact.id),
      });
      setTaskDetailIfNotOlder(queryClient, result.task);
      await invalidateTaskOutputAggregate(queryClient, result.task.id);
    },
    onError: async (error, variables) => {
      if (outputErrorNeedsRefresh(error)) {
        await invalidateTaskOutputAggregate(queryClient, variables.taskId);
      }
    },
  });
}

export function useDownloadTaskArtifact() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, name }: { taskId: string; id: string; name: string }) =>
      downloadTaskArtifact(id, name),
    onSettled: async (_data, _error, variables) => {
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: taskArtifactDetailQueryKey(variables.id),
        }),
        queryClient.invalidateQueries({
          queryKey: taskArtifactQueryKey(variables.taskId),
        }),
        queryClient.invalidateQueries({
          queryKey: taskSubmissionQueryKey(variables.taskId),
        }),
      ]);
    },
  });
}

export function useUpdateTask() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: UpdateTaskInput }) =>
      updateTask(id, {
        ...input,
        expectedVersion: resolveTaskVersion(
          queryClient,
          id,
          input.expectedVersion,
        ),
      }),
    onSuccess: async (task) => {
      queryClient.setQueryData(taskDetailQueryKey(task.id), task);
      await invalidateTaskOutputAggregate(queryClient, task.id);
    },
  });
}

export function useDeleteTask() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (
      variables: string | { id: string; expectedVersion?: number },
    ) => {
      const id = typeof variables === "string" ? variables : variables.id;
      const explicitVersion =
        typeof variables === "string" ? undefined : variables.expectedVersion;
      return deleteTask(
        id,
        resolveTaskVersion(queryClient, id, explicitVersion),
      );
    },
    onSuccess: async (_, variables) => {
      const id = typeof variables === "string" ? variables : variables.id;
      queryClient.removeQueries({ queryKey: taskDetailQueryKey(id) });
      queryClient.removeQueries({ queryKey: taskAssignmentQueryKey(id) });
      queryClient.removeQueries({ queryKey: taskEventQueryKey(id) });
      queryClient.removeQueries({ queryKey: taskSubmissionQueryKey(id) });
      queryClient.removeQueries({ queryKey: taskArtifactQueryKey(id) });
      await queryClient.invalidateQueries({ queryKey: taskQueryKey });
      await queryClient.invalidateQueries({ queryKey: projectQueryKey });
      await queryClient.invalidateQueries({ queryKey: ["stats", "today"] });
      await queryClient.invalidateQueries({ queryKey: inboxQueryKey });
    },
    onError: async (error) => {
      if (isTaskFactsStale(error)) await invalidateTaskFacts(queryClient);
    },
  });
}

async function invalidateTaskFacts(queryClient: QueryClient): Promise<void> {
  await queryClient.invalidateQueries({ queryKey: taskQueryKey });
  await queryClient.invalidateQueries({ queryKey: projectQueryKey });
  await queryClient.invalidateQueries({ queryKey: ["stats", "today"] });
  await queryClient.invalidateQueries({ queryKey: inboxQueryKey });
}

function isTaskFactsStale(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    (error.code === "VERSION_CONFLICT" ||
      error.code === "TASK_BATCH_SET_CHANGED" ||
      error.code === "TASK_REORDER_SET_CHANGED")
  );
}

export function useBatchUpdateTasks() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: BatchUpdateTasksInput) => batchUpdateTasks(input),
    onSuccess: async () => invalidateTaskFacts(queryClient),
    onError: async (error) => {
      if (isTaskFactsStale(error)) {
        await invalidateTaskFacts(queryClient);
      }
    },
  });
}

function isAmbiguousTaskWrite(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    (error.code === "NETWORK_ERROR" || error.code === "TIMEOUT")
  );
}

async function setTaskPlannedDateVerified({
  taskId,
  expectedVersion,
  plannedDate,
}: {
  taskId: string;
  expectedVersion: number;
  plannedDate: string | null;
}) {
  try {
    return await batchUpdateTasks({
      action: "set_planned_date",
      items: [{ id: taskId, expectedVersion }],
      plannedDate,
    });
  } catch (error) {
    if (!isAmbiguousTaskWrite(error)) throw error;
    try {
      const latest = await getTask(taskId);
      if (
        latest.plannedDate === plannedDate &&
        latest.version > expectedVersion
      ) {
        return {
          action: "set_planned_date" as const,
          changed: 1,
          tasks: [latest],
        };
      }
    } catch {
      // Preserve the original ambiguous write error. A failed verification
      // must not be reported as a confirmed plan change.
    }
    throw error;
  }
}

export function useSetTaskPlannedDate() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: {
      taskId: string;
      expectedVersion: number;
      plannedDate: string | null;
    }) => setTaskPlannedDateVerified(input),
    onSuccess: async (result) => {
      for (const task of result.tasks)
        setTaskDetailIfNotOlder(queryClient, task);
      await invalidateTaskFacts(queryClient);
    },
    onError: async (error) => {
      if (isTaskFactsStale(error) || isAmbiguousTaskWrite(error)) {
        await invalidateTaskFacts(queryClient);
      }
    },
  });
}

function isTerminalTask(task: Task): boolean {
  return task.status === "done" || task.status === "cancelled";
}

async function getPlanTasks(plannedDate: string | null): Promise<Task[]> {
  const tasks = await getAllTasks({
    plannedDate: plannedDate ?? undefined,
    sort: "manual_order",
  });
  return plannedDate === null
    ? tasks.filter((task) => task.plannedDate === null)
    : tasks;
}

function insertActiveTaskRelativeToTarget(
  scoped: Task[],
  source: Task,
  targetId: string | null,
  position: "before" | "after" | "end",
): Task[] {
  const active = scoped.filter(
    (task) => !isTerminalTask(task) && task.id !== source.id,
  );
  if (position === "end" || targetId === null) {
    active.push(source);
  } else {
    const targetIndex = active.findIndex((task) => task.id === targetId);
    if (targetIndex < 0) {
      throw new ApiError("目标任务已不在活动计划组，请刷新后重试", {
        code: "TASK_REORDER_SET_CHANGED",
      });
    }
    active.splice(targetIndex + (position === "after" ? 1 : 0), 0, source);
  }
  let activeIndex = 0;
  const ordered: Task[] = [];
  for (const task of scoped) {
    if (isTerminalTask(task)) {
      ordered.push(task);
      continue;
    }
    const next = active[activeIndex++];
    if (next) ordered.push(next);
  }
  ordered.push(...active.slice(activeIndex));
  return ordered;
}

export interface MoveTaskAcrossPlansResult {
  plannedDateChanged: boolean;
  task: Task;
  orderWarnings: string[];
}

export function useMoveTaskAcrossPlans() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({
      source,
      target,
      targetPlannedDate,
      position,
    }: {
      source: Task;
      target: Task | null;
      targetPlannedDate: string | null;
      position: "before" | "after" | "end";
    }) => {
      if (target && source.id === target.id) {
        return {
          plannedDateChanged: false,
          task: source,
          orderWarnings: [],
        } satisfies MoveTaskAcrossPlansResult;
      }
      const sourceDate = source.plannedDate;
      const targetDate = targetPlannedDate;
      const [sourcePlan, targetPlan] =
        sourceDate === targetDate
          ? [await getPlanTasks(sourceDate), null]
          : await Promise.all([
              getPlanTasks(sourceDate),
              getPlanTasks(targetDate),
            ]);
      if (sourcePlan.length > 1_000 || (targetPlan?.length ?? 0) >= 1_000) {
        throw new ApiError("源或目标计划日期达到 1000 项，暂时无法跨组拖拽", {
          code: "TASK_REORDER_LIMIT",
        });
      }
      const latestSource = sourcePlan.find((task) => task.id === source.id);
      const latestTarget = target
        ? (targetPlan ?? sourcePlan).find((task) => task.id === target.id)
        : null;
      if (
        !latestSource ||
        latestSource.version !== source.version ||
        isTerminalTask(latestSource) ||
        (target !== null &&
          (!latestTarget ||
            latestTarget.version !== target.version ||
            isTerminalTask(latestTarget)))
      ) {
        throw new ApiError("任务集合已经变化，请刷新后重试", {
          code: "TASK_REORDER_SET_CHANGED",
        });
      }

      if (sourceDate === targetDate) {
        const ordered = insertActiveTaskRelativeToTarget(
          sourcePlan,
          latestSource,
          target?.id ?? null,
          position,
        );
        const reordered = await reorderTasks({
          plannedDate: sourceDate,
          mode: "manual",
          items: ordered.map((task) => ({
            id: task.id,
            expectedVersion: task.version,
          })),
        });
        return {
          plannedDateChanged: false,
          task:
            reordered.tasks.find((task) => task.id === source.id) ??
            latestSource,
          orderWarnings: [],
        } satisfies MoveTaskAcrossPlansResult;
      }

      const planResult = await setTaskPlannedDateVerified({
        taskId: source.id,
        expectedVersion: source.version,
        plannedDate: targetDate,
      });
      const moved = planResult.tasks.find((task) => task.id === source.id);
      if (!moved) {
        throw new ApiError("改期响应缺少目标任务，请刷新后核对", {
          code: "INVALID_RESPONSE",
        });
      }
      const sourceOrder = sourcePlan.filter((task) => task.id !== source.id);
      const targetOrder = insertActiveTaskRelativeToTarget(
        targetPlan!,
        moved,
        target?.id ?? null,
        position,
      );
      const [sourceOrderResult, targetOrderResult] = await Promise.allSettled([
        reorderTasks({
          plannedDate: sourceDate,
          mode: "manual",
          items: sourceOrder.map((task) => ({
            id: task.id,
            expectedVersion: task.version,
          })),
        }),
        reorderTasks({
          plannedDate: targetDate,
          mode: "manual",
          items: targetOrder.map((task) => ({
            id: task.id,
            expectedVersion: task.version,
          })),
        }),
      ]);
      const orderWarnings: string[] = [];
      if (sourceOrderResult.status === "rejected")
        orderWarnings.push("源日期顺序未能保存");
      if (targetOrderResult.status === "rejected")
        orderWarnings.push("目标日期顺序未能保存");
      const finalMoved =
        targetOrderResult.status === "fulfilled"
          ? (targetOrderResult.value.tasks.find(
              (task) => task.id === source.id,
            ) ?? moved)
          : moved;
      return {
        plannedDateChanged: true,
        task: finalMoved,
        orderWarnings,
      } satisfies MoveTaskAcrossPlansResult;
    },
    onSuccess: async (result) => {
      setTaskDetailIfNotOlder(queryClient, result.task);
      await invalidateTaskFacts(queryClient);
    },
    onError: async (error) => {
      if (isTaskFactsStale(error) || isAmbiguousTaskWrite(error)) {
        await invalidateTaskFacts(queryClient);
      }
    },
  });
}

export function useReorderTasks() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: ReorderTasksInput) => reorderTasks(input),
    onSuccess: async () => invalidateTaskFacts(queryClient),
    onError: async (error) => {
      if (isTaskFactsStale(error)) {
        await invalidateTaskFacts(queryClient);
      }
    },
  });
}

export function useMoveTaskWithinPlan() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({
      taskId,
      plannedDate,
      direction,
      scope = "status",
    }: {
      taskId: string;
      plannedDate: string | null;
      direction: "up" | "down";
      scope?: "status" | "active";
    }) => {
      const tasks = await getAllTasks({
        plannedDate: plannedDate ?? undefined,
        sort: "manual_order",
      });
      const scoped = plannedDate
        ? tasks
        : tasks.filter((task) => task.plannedDate === null);
      if (scoped.length > 1_000) {
        throw new ApiError("同一计划日期超过 1000 项，暂时无法手动排序", {
          code: "TASK_REORDER_LIMIT",
        });
      }
      const currentIndex = scoped.findIndex((task) => task.id === taskId);
      if (currentIndex < 0) {
        throw new ApiError("任务已不在当前计划日期，请刷新后重试", {
          code: "TASK_REORDER_SET_CHANGED",
        });
      }
      const current = scoped[currentIndex];
      const isCandidate =
        scope === "active"
          ? (task: Task) =>
              task.status !== "done" && task.status !== "cancelled"
          : (task: Task) => task.status === current.status;
      if (!isCandidate(current)) {
        throw new ApiError("任务已不在当前活动计划组，请刷新后重试", {
          code: "TASK_REORDER_SET_CHANGED",
        });
      }
      const step = direction === "up" ? -1 : 1;
      let targetIndex = currentIndex + step;
      while (
        targetIndex >= 0 &&
        targetIndex < scoped.length &&
        !isCandidate(scoped[targetIndex])
      ) {
        targetIndex += step;
      }
      if (targetIndex < 0 || targetIndex >= scoped.length) {
        return {
          plannedDate,
          mode: "manual" as const,
          changed: 0,
          tasks: scoped,
        };
      }
      const ordered = [...scoped];
      [ordered[currentIndex], ordered[targetIndex]] = [
        ordered[targetIndex],
        ordered[currentIndex],
      ];
      return reorderTasks({
        plannedDate,
        mode: "manual",
        items: ordered.map((task) => ({
          id: task.id,
          expectedVersion: task.version,
        })),
      });
    },
    onSuccess: async () => invalidateTaskFacts(queryClient),
    onError: async (error) => {
      if (isTaskFactsStale(error)) {
        await invalidateTaskFacts(queryClient);
      }
    },
  });
}

export function useReorderActiveTasksWithinPlan() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({
      plannedDate,
      orderedTaskIds,
    }: {
      plannedDate: string | null;
      orderedTaskIds: string[];
    }) => {
      const tasks = await getAllTasks({
        plannedDate: plannedDate ?? undefined,
        sort: "manual_order",
      });
      const scoped = plannedDate
        ? tasks
        : tasks.filter((task) => task.plannedDate === null);
      if (scoped.length > 1_000) {
        throw new ApiError("同一计划日期超过 1000 项，暂时无法拖拽排序", {
          code: "TASK_REORDER_LIMIT",
        });
      }
      const active = scoped.filter(
        (task) => task.status !== "done" && task.status !== "cancelled",
      );
      const uniqueIds = new Set(orderedTaskIds);
      if (
        uniqueIds.size !== orderedTaskIds.length ||
        orderedTaskIds.length !== active.length ||
        active.some((task) => !uniqueIds.has(task.id))
      ) {
        throw new ApiError("活动任务集合已变化，请刷新后重试", {
          code: "TASK_REORDER_SET_CHANGED",
        });
      }
      const activeById = new Map(active.map((task) => [task.id, task]));
      let activeIndex = 0;
      const ordered = scoped.map((task) =>
        task.status === "done" || task.status === "cancelled"
          ? task
          : activeById.get(orderedTaskIds[activeIndex++])!,
      );
      return reorderTasks({
        plannedDate,
        mode: "manual",
        items: ordered.map((task) => ({
          id: task.id,
          expectedVersion: task.version,
        })),
      });
    },
    onSuccess: async () => invalidateTaskFacts(queryClient),
    onError: async (error) => {
      if (isTaskFactsStale(error)) {
        await invalidateTaskFacts(queryClient);
      }
    },
  });
}

export function useReorderTaskWithinPlanStatus() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({
      source,
      target,
      position,
    }: {
      source: Task;
      target: Task;
      position: "before" | "after";
    }) => {
      if (
        source.id === target.id ||
        source.plannedDate !== target.plannedDate ||
        source.status !== target.status
      ) {
        throw new ApiError("只能在同一计划日期和状态组内拖拽任务", {
          code: "TASK_REORDER_SCOPE_MISMATCH",
        });
      }
      const scoped = await getPlanTasks(source.plannedDate);
      if (scoped.length > 1_000) {
        throw new ApiError("同一计划日期超过 1000 项，暂时无法拖拽排序", {
          code: "TASK_REORDER_LIMIT",
        });
      }
      const latestSource = scoped.find((task) => task.id === source.id);
      const latestTarget = scoped.find((task) => task.id === target.id);
      if (
        !latestSource ||
        !latestTarget ||
        latestSource.version !== source.version ||
        latestTarget.version !== target.version ||
        latestSource.status !== source.status ||
        latestTarget.status !== source.status
      ) {
        throw new ApiError("当前计划状态组已经变化，请刷新后重试", {
          code: "TASK_REORDER_SET_CHANGED",
        });
      }
      const statusOrder = scoped.filter(
        (task) => task.status === source.status && task.id !== source.id,
      );
      const targetIndex = statusOrder.findIndex(
        (task) => task.id === target.id,
      );
      if (targetIndex < 0) {
        throw new ApiError("目标任务已不在当前状态组，请刷新后重试", {
          code: "TASK_REORDER_SET_CHANGED",
        });
      }
      statusOrder.splice(
        targetIndex + (position === "after" ? 1 : 0),
        0,
        latestSource,
      );
      let statusIndex = 0;
      const ordered = scoped.map((task) =>
        task.status === source.status ? statusOrder[statusIndex++]! : task,
      );
      return reorderTasks({
        plannedDate: source.plannedDate,
        mode: "manual",
        items: ordered.map((task) => ({
          id: task.id,
          expectedVersion: task.version,
        })),
      });
    },
    onSuccess: async () => invalidateTaskFacts(queryClient),
    onError: async (error) => {
      if (isTaskFactsStale(error)) await invalidateTaskFacts(queryClient);
    },
  });
}

export function useResetTaskOrder() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (plannedDate: string | null) => {
      const tasks = await getAllTasks({
        plannedDate: plannedDate ?? undefined,
        sort: "manual_order",
      });
      const scoped = plannedDate
        ? tasks
        : tasks.filter((task) => task.plannedDate === null);
      if (scoped.length > 1_000) {
        throw new ApiError("同一计划日期超过 1000 项，暂时无法重置排序", {
          code: "TASK_REORDER_LIMIT",
        });
      }
      return reorderTasks({
        plannedDate,
        mode: "default",
        items: scoped.map((task) => ({
          id: task.id,
          expectedVersion: task.version,
        })),
      });
    },
    onSuccess: async () => invalidateTaskFacts(queryClient),
    onError: async (error) => {
      if (isTaskFactsStale(error)) {
        await invalidateTaskFacts(queryClient);
      }
    },
  });
}

export const tagQueryKey = ["tags"] as const;

export function useTagsQuery(input: TagListParams = {}, enabled = true) {
  return useQuery({
    queryKey: [...tagQueryKey, "list", input],
    queryFn: () => getTags(input),
    enabled,
    placeholderData: keepPreviousData,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useTagOptionsQuery(enabled = true) {
  return useQuery({
    queryKey: [...tagQueryKey, "options"],
    queryFn: () => getAllTags({ sort: "name" }),
    enabled,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useCreateTag() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: (input: TagInput) => {
      const fingerprint = JSON.stringify(input);
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return createTag(input, attempt.current.key);
    },
    onSuccess: async () => {
      attempt.current = null;
      await queryClient.invalidateQueries({ queryKey: tagQueryKey });
    },
  });
}

export function useUpdateTag() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: UpdateTagInput }) =>
      updateTag(id, input),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: tagQueryKey });
      await queryClient.invalidateQueries({ queryKey: taskQueryKey });
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await queryClient.invalidateQueries({ queryKey: tagQueryKey });
        await queryClient.invalidateQueries({ queryKey: taskQueryKey });
      }
    },
  });
}

export function useDeleteTag() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      expectedVersion,
    }: {
      id: string;
      expectedVersion: number;
    }) => deleteTag(id, expectedVersion),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: tagQueryKey });
      await queryClient.invalidateQueries({ queryKey: taskQueryKey });
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await queryClient.invalidateQueries({ queryKey: tagQueryKey });
        await queryClient.invalidateQueries({ queryKey: taskQueryKey });
      }
    },
  });
}

export const projectQueryKey = ["projects"] as const;
export const projectDetailQueryKey = (id: string) =>
  [...projectQueryKey, "detail", id] as const;
export const projectEventQueryKey = (id: string) =>
  [...projectDetailQueryKey(id), "events"] as const;
export const projectArtifactQueryKey = (id: string) =>
  [...projectDetailQueryKey(id), "artifacts"] as const;
export const projectNoteQueryKey = (id: string) =>
  [...projectDetailQueryKey(id), "notes"] as const;
export const projectAttachmentQueryKey = (id: string) =>
  [...projectDetailQueryKey(id), "attachments"] as const;

export function useProjectsQuery(
  input: ProjectListParams = {},
  enabled = true,
) {
  return useQuery({
    queryKey: [...projectQueryKey, "list", input],
    queryFn: () => getProjects(input),
    enabled,
    placeholderData: keepPreviousData,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useProjectOptionsQuery(enabled = true) {
  return useQuery({
    queryKey: [...projectQueryKey, "options"],
    queryFn: () => getAllProjects({ sort: "name" }),
    enabled,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useProjectQuery(id: string | null) {
  return useQuery({
    queryKey: projectDetailQueryKey(id ?? "missing"),
    queryFn: () => getProject(id!),
    enabled: Boolean(id),
    retry: 1,
  });
}

export function useProjectEventsQuery(
  projectId: string | null,
  input: Omit<ProjectEventListParams, "page"> = {},
  enabled = true,
) {
  const query = { pageSize: input.pageSize ?? 20 };
  return useInfiniteQuery({
    queryKey: [
      ...projectEventQueryKey(projectId ?? "missing"),
      "timeline",
      query,
    ],
    queryFn: ({ pageParam }) =>
      getProjectEvents(projectId!, { ...query, page: pageParam }),
    initialPageParam: 1,
    getNextPageParam: (lastPage) =>
      lastPage.meta.page * lastPage.meta.pageSize < lastPage.meta.total
        ? lastPage.meta.page + 1
        : undefined,
    enabled: Boolean(projectId) && enabled,
    retry: 1,
    staleTime: 10_000,
  });
}

export function useProjectNotesQuery(
  projectId: string | null,
  input: ProjectNoteListParams = {},
) {
  return useQuery({
    queryKey: [...projectNoteQueryKey(projectId ?? "missing"), "list", input],
    queryFn: () => getProjectNotes(projectId!, input),
    enabled: Boolean(projectId),
    placeholderData: keepPreviousData,
    retry: 1,
  });
}

export function useProjectArtifactsQuery(
  projectId: string | null,
  input: ProjectArtifactListParams = {},
) {
  return useQuery({
    queryKey: [
      ...projectArtifactQueryKey(projectId ?? "missing"),
      "list",
      input,
    ],
    queryFn: () => getProjectArtifacts(projectId!, input),
    enabled: Boolean(projectId),
    placeholderData: keepPreviousData,
    retry: 1,
  });
}

export function useProjectAttachmentsQuery(
  projectId: string | null,
  input: ProjectAttachmentListParams = {},
) {
  return useQuery({
    queryKey: [
      ...projectAttachmentQueryKey(projectId ?? "missing"),
      "list",
      input,
    ],
    queryFn: () => getProjectAttachments(projectId!, input),
    enabled: Boolean(projectId),
    placeholderData: keepPreviousData,
    retry: 1,
  });
}

export function useCreateProjectAttachment() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: ({
      projectId,
      input,
    }: {
      projectId: string;
      input: CreateProjectAttachmentInput;
    }) => {
      const fingerprint = JSON.stringify({
        projectId,
        name: input.name,
        expectedVersion: input.expectedVersion,
        fileName: input.file.name,
        fileSize: input.file.size,
        fileModified: input.file.lastModified,
      });
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return createProjectAttachment(projectId, input, attempt.current.key);
    },
    onSuccess: async (attachment) => {
      attempt.current = null;
      await queryClient.invalidateQueries({
        queryKey: projectAttachmentQueryKey(attachment.projectId),
      });
      await queryClient.invalidateQueries({ queryKey: projectQueryKey });
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await queryClient.invalidateQueries({ queryKey: projectQueryKey });
      }
    },
  });
}

export function useDeleteProjectAttachment() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: ({
      id,
      projectId,
      input,
    }: {
      id: string;
      projectId: string;
      input: DeleteProjectAttachmentInput;
    }) => {
      const fingerprint = JSON.stringify({ id, projectId, input });
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return deleteProjectAttachment(id, input, attempt.current.key);
    },
    onSuccess: async (attachment) => {
      attempt.current = null;
      await queryClient.invalidateQueries({
        queryKey: projectAttachmentQueryKey(attachment.projectId),
      });
      await queryClient.invalidateQueries({ queryKey: projectQueryKey });
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await queryClient.invalidateQueries({ queryKey: projectQueryKey });
      }
    },
  });
}

export function useDownloadProjectAttachment() {
  return useMutation({
    mutationFn: ({ id, name }: { id: string; name: string }) =>
      downloadProjectAttachment(id, name),
  });
}

export function useCreateProjectNote() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: ({
      projectId,
      input,
    }: {
      projectId: string;
      input: CreateProjectNoteInput;
    }) => {
      const fingerprint = JSON.stringify({ projectId, input });
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return createProjectNote(projectId, input, attempt.current.key);
    },
    onSuccess: async (note) => {
      attempt.current = null;
      await queryClient.invalidateQueries({ queryKey: projectQueryKey });
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.code === "PROJECT_ARCHIVED") {
        await queryClient.invalidateQueries({ queryKey: projectQueryKey });
      }
    },
  });
}

export function useUpdateProjectNote() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      input,
    }: {
      id: string;
      input: UpdateProjectNoteInput;
    }) => updateProjectNote(id, input),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: projectQueryKey });
    },
    onError: async (error) => {
      if (
        error instanceof ApiError &&
        (error.code === "VERSION_CONFLICT" ||
          error.code === "PROJECT_NOTE_DELETED" ||
          error.code === "PROJECT_ARCHIVED")
      ) {
        await queryClient.invalidateQueries({ queryKey: projectQueryKey });
      }
    },
  });
}

export function useDeleteProjectNote() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      input,
    }: {
      id: string;
      input: DeleteProjectNoteInput;
    }) => deleteProjectNote(id, input),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: projectQueryKey });
    },
    onError: async (error) => {
      if (
        error instanceof ApiError &&
        (error.code === "VERSION_CONFLICT" ||
          error.code === "PROJECT_NOTE_DELETED" ||
          error.code === "PROJECT_ARCHIVED")
      ) {
        await queryClient.invalidateQueries({ queryKey: projectQueryKey });
      }
    },
  });
}

export function useCreateProject() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: (input: ProjectInput) => {
      const fingerprint = JSON.stringify(input);
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return createProject(input, attempt.current.key);
    },
    onSuccess: async (project) => {
      attempt.current = null;
      queryClient.setQueryData(projectDetailQueryKey(project.id), project);
      await queryClient.invalidateQueries({ queryKey: projectQueryKey });
      await queryClient.invalidateQueries({ queryKey: clientQueryKey });
    },
  });
}

export function useUpdateProject() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: UpdateProjectInput }) =>
      updateProject(id, input),
    onSuccess: async (project) => {
      queryClient.setQueryData(projectDetailQueryKey(project.id), project);
      await queryClient.invalidateQueries({ queryKey: projectQueryKey });
      await queryClient.invalidateQueries({ queryKey: taskQueryKey });
      await queryClient.invalidateQueries({ queryKey: clientQueryKey });
    },
  });
}

export function useTransitionProject() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      action,
      expectedVersion,
      confirmIncompleteTasks,
    }: {
      id: string;
      action: ProjectTransitionAction;
      expectedVersion: number;
      confirmIncompleteTasks?: boolean;
    }) =>
      transitionProject(id, action, expectedVersion, confirmIncompleteTasks),
    onSuccess: async (project) => {
      queryClient.setQueryData(projectDetailQueryKey(project.id), project);
      await queryClient.invalidateQueries({ queryKey: projectQueryKey });
    },
  });
}

export function useDeleteProject() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      expectedVersion,
    }: {
      id: string;
      expectedVersion: number;
    }) => deleteProject(id, expectedVersion),
    onSuccess: async (_, variables) => {
      queryClient.removeQueries({
        queryKey: projectDetailQueryKey(variables.id),
      });
      await queryClient.invalidateQueries({ queryKey: projectQueryKey });
      await queryClient.invalidateQueries({ queryKey: taskQueryKey });
      await queryClient.invalidateQueries({ queryKey: clientQueryKey });
    },
  });
}

export function resetApiAndRefetch(refetch: () => Promise<unknown>): void {
  resetRuntimeConnection();
  void refetch();
}
