import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../api/client";
import { projectNoteQueryKey } from "../api/hooks";
import { useAiChatStore } from "../store/aiChat";
import type { ProjectNote } from "../types/models";
import { ProjectNoteLocation } from "./ProjectNoteLocation";

const getProjectNote = vi.hoisted(() => vi.fn());
vi.mock("../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../api/client")>("../api/client");
  return { ...actual, getProjectNote };
});

const projectId = "018f0000-0000-7000-8000-000000005841";
const noteId = "018f0000-0000-7000-8000-000000005842";
const sessionId = "018f0000-0000-7000-8000-000000005843";
const note: ProjectNote = {
  id: noteId,
  projectId,
  title: "精确项目笔记",
  body: "只展示按 ID 重新读取的最新正文",
  occurredAt: "2026-09-18T08:00:00Z",
  createdBy: { id: sessionId, type: "owner", displayName: "我" },
  version: 5,
  deletedAt: null,
  deletedByActorId: null,
  deleteReason: null,
  createdAt: "2026-09-18T08:00:00Z",
  updatedAt: "2026-09-18T08:05:00Z",
  projectVersion: 8,
};
const clients: QueryClient[] = [];

function Location() {
  const location = useLocation();
  return (
    <output aria-label="当前位置">
      {location.pathname}
      {location.search}
    </output>
  );
}

function setup(load = getProjectNote) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  clients.push(client);
  const close = vi.fn();
  const view = render(
    <QueryClientProvider client={client}>
      <MemoryRouter
        initialEntries={[
          `/projects/${projectId}?note=${noteId}&return_session=${sessionId}`,
        ]}
      >
        <ProjectNoteLocation
          noteId={noteId}
          onClose={close}
          projectId={projectId}
          returnSession={sessionId}
        >
          {(value) => <p>{value.body}</p>}
        </ProjectNoteLocation>
        <Location />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return { client, close, load, ...view };
}

afterEach(() => {
  cleanup();
  getProjectNote.mockReset();
  clients.splice(0).forEach((client) => client.clear());
  useAiChatStore.setState({ activeSessionId: "" });
});

describe("direct project note location", () => {
  it("loads the exact note, focuses it and returns to the source session", async () => {
    getProjectNote.mockResolvedValue(note);
    const { close, load } = setup();
    expect(await screen.findByText(note.body!)).toBeVisible();
    expect(load).toHaveBeenCalledWith(noteId, expect.any(AbortSignal));
    expect(screen.getByLabelText("定位的项目笔记")).toHaveFocus();

    fireEvent.click(screen.getByRole("button", { name: "关闭定位" }));
    expect(close).toHaveBeenCalledOnce();
    fireEvent.click(screen.getByRole("link", { name: "返回原对话" }));
    expect(useAiChatStore.getState().activeSessionId).toBe(sessionId);
    expect(screen.getByLabelText("当前位置")).toHaveTextContent("/ai");
  });

  it.each([
    { ...note, id: sessionId },
    { ...note, projectId: sessionId },
  ])("rejects a mismatched note or project identity", async (value) => {
    getProjectNote.mockResolvedValue(value);
    setup(getProjectNote);
    expect(
      await screen.findByText("链接中的笔记不属于当前项目，未展示其内容。"),
    ).toBeVisible();
    expect(screen.queryByText(note.body!)).toBeNull();
  });

  it("supports an explicit fresh retry without substituting a list item", async () => {
    getProjectNote
      .mockRejectedValueOnce(new ApiError("missing", { status: 404 }))
      .mockResolvedValue(note);
    setup(getProjectNote);
    expect(await screen.findByText("项目笔记不可用")).toBeVisible();
    expect(screen.queryByText(note.body!)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(await screen.findByText(note.body!)).toBeVisible();
    expect(getProjectNote).toHaveBeenCalledTimes(2);
  });

  it("hides cached content while a shared invalidation fails", async () => {
    getProjectNote.mockResolvedValue(note);
    const { client } = setup();
    expect(await screen.findByText(note.body!)).toBeVisible();
    getProjectNote.mockRejectedValue(new Error("offline"));
    await act(async () => {
      await client.invalidateQueries({
        queryKey: projectNoteQueryKey(projectId),
      });
    });
    expect(await screen.findByText("项目笔记不可用")).toBeVisible();
    expect(screen.queryByText(note.body!)).toBeNull();
  });

  it("aborts an in-flight exact read when the location is unmounted", async () => {
    let signal: AbortSignal | undefined;
    getProjectNote.mockImplementation((_id: string, input: AbortSignal) => {
      signal = input;
      return new Promise<ProjectNote>(() => {});
    });
    const view = setup(getProjectNote);
    await waitFor(() => expect(getProjectNote).toHaveBeenCalledOnce());
    expect(screen.getByText("正在读取项目笔记…")).toBeVisible();
    view.unmount();
    expect(signal?.aborted).toBe(true);
  });
});
