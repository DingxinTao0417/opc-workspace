import { describe, expect, it } from "vitest";
import {
  agentRunHref,
  aiWorkspaceHref,
  isAiWorkspaceRoute,
  parseAiWorkspaceToolRoute,
  parseAgentRunLocation,
  parseTaskSavedViewLocation,
  parseTaskWorkspaceLocation,
} from "./aiWorkspaceLinks";

describe("workspace navigation boundary", () => {
  const id = "018f0000-0000-7000-8000-000000005833";
  it.each([
    "/invoices",
    "/inbox",
    "/focus",
    `/tasks/${id}`,
    `/tasks?task_view=${id}`,
    `/tasks/${id}?agent_run=${id}`,
    `/projects/${id}`,
    `/projects/${id}?note=${id}`,
    `/clients/${id}`,
    `/inbox/${id}`,
    `/inbox?reminder=${id}`,
    `/roadmap?milestone=${id}`,
    `/content-calendar?item=${id}`,
    "/knowledge",
    `/knowledge?source=${id}&job=${id}`,
    "/ai?workspace=overview",
    "/ai?workspace=agents",
    "/ai?workspace=files",
    "/ai?workspace=review",
    "/ai?workspace=terminal",
    "/ai?workspace=browser",
    "/ai?workspace=managed",
    `/ai?session=${id}`,
  ])("accepts %s", (route) => expect(isAiWorkspaceRoute(route)).toBe(true));
  it.each([
    "/invoices?redirect=evil",
    "/invoices/../settings",
    "javascript:alert(1)",
    "/focus?redirect=evil",
    "/focus/../settings",
    "https://example.com",
    "//example.com/tasks",
    "/tasks/not-an-id",
    "/tasks?task_view=not-an-id",
    `/tasks?task_view=${id}&task_view=${id}`,
    `/tasks?task_view=${id}&redirect=evil`,
    `/tasks/${id}?redirect=evil`,
    `/tasks/${id}?agent_run=${id}&agent_run=${id}`,
    `/tasks/${id}?agent_run=not-an-id`,
    `/tasks/${id}?agent_run=${id}&return_session=${id}`,
    `/tasks/${id}?agent_run=${id}&unknown=value`,
    `/projects/${id}?note=${id}&return_session=${id}`,
    `/projects/${id}?note=${id}&note=${id}`,
    `/projects/${id}?note=not-an-id`,
    `/tasks/${id}/../settings`,
    `/roadmap?milestone=${id}&foo=bar`,
    `/inbox?reminder=${id}&redirect=evil`,
    `/inbox?return_session=${id}`,
    `/inbox?return_session=${id}&return_session=${id}`,
    `/inbox?redirect=https://example.com`,
    `/knowledge?source=${id}`,
    `/knowledge?source=${id}&job=not-an-id`,
    `/knowledge?source=${id}&job=${id}&redirect=evil`,
    "/knowledge?return_session=018f0000-0000-7000-8000-000000005833",
    "/ai?workspace=chat",
    "/ai?workspace=file",
    "/ai?workspace=files&path=secret.txt",
    "/ai?workspace=browser&url=https://example.com",
    "/ai?workspace=files&workspace=review",
    "/ai?workspace=files#fragment",
    "/ai?session=not-an-id",
    `/ai?session=${id}&workspace=agents`,
    `/ai?session=${id}&session=${id}`,
  ])("rejects %s", (route) => expect(isAiWorkspaceRoute(route)).toBe(false));

  it("adds the displaying main or side conversation only in the UI", () => {
    expect(aiWorkspaceHref(`/tasks?task_view=${id}`, id)).toBe(
      `/tasks?task_view=${id}&return_session=${id}`,
    );
    expect(aiWorkspaceHref(`/projects/${id}?note=${id}`, id)).toBe(
      `/projects/${id}?note=${id}&return_session=${id}`,
    );
    expect(
      aiWorkspaceHref(`/projects/${id}?note=${id}`, "untrusted-session"),
    ).toBe(`/projects/${id}?note=${id}`);
    expect(aiWorkspaceHref("/inbox", id)).toBe(`/inbox?return_session=${id}`);
    expect(aiWorkspaceHref(`/tasks/${id}?agent_run=${id}`, id)).toBe(
      `/tasks/${id}?agent_run=${id}&return_session=${id}`,
    );
    expect(aiWorkspaceHref(`/tasks/${id}`, id)).toBe(
      `/tasks/${id}?return_session=${id}`,
    );
    expect(aiWorkspaceHref(`/inbox?return_session=${id}`, id)).toBe(
      `/inbox?return_session=${id}`,
    );
    expect(aiWorkspaceHref("/inbox", "javascript:alert(1)")).toBe("/inbox");
  });

  it("parses only the exact saved-view identity and optional return session", () => {
    expect(
      parseTaskSavedViewLocation(
        "/tasks",
        `?task_view=${id}&return_session=${id}`,
      ),
    ).toEqual({ viewId: id, returnSession: id });
    expect(
      parseTaskSavedViewLocation("/tasks", `?task_view=${id}&unknown=1`),
    ).toBeNull();
    expect(
      parseTaskSavedViewLocation("/tasks", `?task_view=${id}&task_view=${id}`),
    ).toBeNull();
  });

  it.each([
    `/projects/${id}`,
    `/clients/${id}`,
    `/inbox/${id}`,
    `/inbox?reminder=${id}`,
    `/roadmap?milestone=${id}`,
    `/content-calendar?item=${id}`,
  ])("preserves the source conversation for %s", (route) => {
    for (const sessionId of [
      "018f0000-0000-7000-8000-000000000021",
      "018f0000-0000-7000-8000-000000000022",
    ]) {
      expect(aiWorkspaceHref(route, sessionId)).toBe(
        `${route}${route.includes("?") ? "&" : "?"}return_session=${sessionId}`,
      );
      expect(isAiWorkspaceRoute(aiWorkspaceHref(route, sessionId))).toBe(false);
    }
    expect(aiWorkspaceHref(route)).toBe(route);
    expect(aiWorkspaceHref(route, "invalid-session")).toBe(route);
  });

  it("parses only exact local workspace tool launchers", () => {
    expect(parseAiWorkspaceToolRoute("/ai?workspace=review")).toBe("review");
    expect(parseAiWorkspaceToolRoute("/ai?workspace=terminal")).toBe(
      "terminal",
    );
    expect(
      parseAiWorkspaceToolRoute(
        "/ai?workspace=browser&url=https://example.com",
      ),
    ).toBeNull();
    expect(parseAiWorkspaceToolRoute("https://example.com")).toBeNull();
  });

  it("parses only one exact task, run and optional return identity", () => {
    expect(parseAgentRunLocation(`/tasks/${id}`, `?agent_run=${id}`)).toEqual({
      taskId: id,
      runId: id,
      returnSession: null,
    });
    expect(
      parseAgentRunLocation(
        `/tasks/${id}`,
        `?agent_run=${id}&return_session=${id}`,
      ),
    ).toEqual({ taskId: id, runId: id, returnSession: id });
    expect(
      parseAgentRunLocation(
        `/tasks/${id}`,
        `?agent_run=${id}&return_session=${id}&return_session=${id}`,
      ),
    ).toBeNull();
    expect(agentRunHref(id, id)).toBe(`/tasks/${id}?agent_run=${id}`);
    expect(agentRunHref(id.toUpperCase(), id)).toBe("");
    expect(parseTaskWorkspaceLocation(`/tasks/${id}`, "")).toEqual({
      taskId: id,
      returnSession: null,
    });
    expect(
      parseTaskWorkspaceLocation(
        `/tasks/${id}`,
        `?return_session=${id}&return_session=${id}`,
      ),
    ).toBeNull();
  });
});
