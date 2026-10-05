import type {
  CreateProjectAttachmentInput,
  CreateProjectNoteInput,
  DeleteProjectAttachmentInput,
  DeleteProjectNoteInput,
  ProjectArtifactListParams,
  ProjectAttachmentListParams,
  ProjectEventListParams,
  ProjectInput,
  ProjectListParams,
  ProjectNoteListParams,
  ProjectTransitionAction,
  UpdateProjectInput,
  UpdateProjectNoteInput,
} from "../../types/models";
import {
  ApiError,
  createProject,
  createProjectAttachment,
  createProjectNote,
  deleteProject,
  deleteProjectAttachment,
  deleteProjectNote,
  downloadProjectAttachment,
  getProject,
  getProjectArtifacts,
  getProjectAttachments,
  getProjectEvents,
  getProjectNotes,
  getProjects,
  transitionProject,
  updateProject,
  updateProjectNote,
} from "../client";
import {
  keepPreviousData,
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useRef } from "react";
import {
  invalidateProjectCompletionFacts,
  invalidateProjectEditFacts,
  invalidateProjectNoteConflict,
  invalidateProjectQueries,
  invalidateUnifiedSearch,
} from "./invalidation";
import {
  clientQueryKey,
  financialEntryQueryKey,
  focusReportQueryKey,
  inboxQueryKey,
  invoiceQueryKey,
  projectArtifactQueryKey,
  projectAttachmentQueryKey,
  projectDetailQueryKey,
  projectEventQueryKey,
  projectNoteQueryKey,
  projectQueryKey,
  roadmapMilestoneQueryKey,
  taskQueryKey,
} from "./queryKeys";

export function useProjectsQuery(
  input: ProjectListParams = {},
  enabled = true,
) {
  return useQuery({
    queryKey: [...projectQueryKey, "list", input],
    queryFn: ({ signal }) => getProjects(input, signal),
    enabled,
    placeholderData: keepPreviousData,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useProjectOptionsQuery(
  search = "",
  page = 1,
  enabled = true,
  includeArchived = false,
  clientId?: string,
) {
  const normalizedSearch = search.trim();
  return useQuery({
    queryKey: [
      ...projectQueryKey,
      "options",
      normalizedSearch,
      page,
      includeArchived,
      clientId ?? null,
    ],
    queryFn: ({ signal }) =>
      getProjects(
        {
          page,
          pageSize: 20,
          query: normalizedSearch || undefined,
          sort: "name",
          includeArchived,
          ...(clientId ? { clientId } : {}),
        },
        signal,
      ),
    enabled,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useProjectQuery(id: string | null) {
  return useQuery({
    queryKey: projectDetailQueryKey(id ?? "missing"),
    queryFn: ({ signal }) => getProject(id!, signal),
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
    queryFn: ({ signal }) => getProjectArtifacts(projectId!, input, signal),
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
    onSuccess: async () => {
      attempt.current = null;
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
    onSuccess: async () => {
      attempt.current = null;
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
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      name,
    }: {
      projectId: string;
      id: string;
      name: string;
    }) => downloadProjectAttachment(id, name),
    onSettled: async (_data, _error, variables) => {
      await queryClient.invalidateQueries({
        queryKey: projectAttachmentQueryKey(variables.projectId),
      });
    },
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
    onSuccess: async () => {
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
      projectId: _projectId,
      id,
      input,
    }: {
      projectId: string;
      id: string;
      input: UpdateProjectNoteInput;
    }) => updateProjectNote(id, input),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: projectQueryKey });
    },
    onError: async (error, variables) => {
      if (
        error instanceof ApiError &&
        (error.code === "VERSION_CONFLICT" ||
          error.code === "PROJECT_NOTE_DELETED" ||
          error.code === "PROJECT_ARCHIVED")
      ) {
        if (error.code === "VERSION_CONFLICT") {
          await invalidateProjectNoteConflict(queryClient, variables.projectId);
        } else {
          await queryClient.invalidateQueries({ queryKey: projectQueryKey });
        }
      }
    },
  });
}

export function useDeleteProjectNote() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      projectId: _projectId,
      id,
      input,
    }: {
      projectId: string;
      id: string;
      input: DeleteProjectNoteInput;
    }) => deleteProjectNote(id, input),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: projectQueryKey });
    },
    onError: async (error, variables) => {
      if (
        error instanceof ApiError &&
        (error.code === "VERSION_CONFLICT" ||
          error.code === "PROJECT_NOTE_DELETED" ||
          error.code === "PROJECT_ARCHIVED")
      ) {
        if (error.code === "VERSION_CONFLICT") {
          await invalidateProjectNoteConflict(queryClient, variables.projectId);
        } else {
          await queryClient.invalidateQueries({ queryKey: projectQueryKey });
        }
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
      await invalidateUnifiedSearch(queryClient);
    },
  });
}

export function useUpdateProject() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: UpdateProjectInput }) =>
      updateProject(id, input),
    onSuccess: (project) => {
      queryClient.setQueryData(projectDetailQueryKey(project.id), project);
      // Closing an edit modal is a direct acknowledgement of a successful write.
      // Keep the wider derived-read-model refresh asynchronous so a slow refetch
      // cannot leave that modal appearing to be unsaved.
      void invalidateProjectEditFacts(queryClient);
    },
    onError: async (error, variables) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await invalidateProjectEditFacts(queryClient, variables.id);
      }
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
    onSuccess: async (project, variables) => {
      queryClient.setQueryData(projectDetailQueryKey(project.id), project);
      await queryClient.invalidateQueries({ queryKey: projectQueryKey });
      await queryClient.invalidateQueries({ queryKey: clientQueryKey });
      await queryClient.invalidateQueries({
        queryKey: roadmapMilestoneQueryKey,
      });
      if (variables.action === "complete") {
        await invalidateProjectCompletionFacts(queryClient);
      }
      await invalidateUnifiedSearch(queryClient);
    },
    onError: async (error, variables) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await Promise.all([
          ...invalidateProjectQueries(queryClient, variables.id),
          queryClient.invalidateQueries({ queryKey: clientQueryKey }),
          queryClient.invalidateQueries({
            queryKey: roadmapMilestoneQueryKey,
          }),
          invalidateProjectCompletionFacts(queryClient),
          invalidateUnifiedSearch(queryClient),
        ]);
      }
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
      await queryClient.invalidateQueries({ queryKey: invoiceQueryKey });
      await queryClient.invalidateQueries({ queryKey: financialEntryQueryKey });
      await queryClient.invalidateQueries({ queryKey: inboxQueryKey });
      await queryClient.invalidateQueries({
        queryKey: roadmapMilestoneQueryKey,
      });
      await invalidateUnifiedSearch(queryClient);
      // The deleted project's report is still actively observed until the
      // detail page navigates away. Mark all derived reports stale without
      // refetching that now-invalid project id and delaying the navigation.
      await queryClient.invalidateQueries({
        queryKey: focusReportQueryKey,
        refetchType: "none",
      });
    },
  });
}
