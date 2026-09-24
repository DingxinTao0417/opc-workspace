import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
  waitFor,
} from "@testing-library/react";
import {
  Link,
  MemoryRouter,
  Route,
  Routes,
  useLocation,
  useNavigate,
} from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ClientDetailPage } from "../pages/ClientDetailPage";
import { renderAiRichText } from "./AiRichText";
import { useAiChatStore } from "../store/aiChat";
import { useAiWorkbenchHandoff } from "../store/aiWorkbenchHandoff";
import type { ClientActivity, ClientFollowup } from "../types/models";

const clientId = "018f0000-0000-7000-8000-000000005831";
const activityId = "018f0000-0000-7000-8000-000000005832";
const followupId = "018f0000-0000-7000-8000-000000005834";
const sessionId = "018f0000-0000-7000-8000-000000005833";
const actor = {
  id: sessionId,
  type: "owner" as const,
  displayName: "Owner",
  status: "active",
  version: 1,
};
const activity: ClientActivity = {
  id: activityId,
  clientId,
  kind: "note",
  title: "历史会议记录",
  body: "不在第一页的完整正文",
  occurredAt: "2026-09-01T08:00:00Z",
  createdBy: actor,
  sourceType: null,
  sourceId: null,
  version: 7,
  deletedAt: null,
  deletedByActorId: null,
  deleteReason: null,
  createdAt: "2026-09-01T08:00:00Z",
  updatedAt: "2026-09-18T08:00:00Z",
  clientVersion: 12,
};
const followup: ClientFollowup = {
  id: followupId,
  clientId,
  clientName: "真实客户",
  assignedActorId: actor.id,
  assignedActorName: actor.displayName,
  assignedActorType: "owner",
  scheduledAt: "2026-09-20T08:00:00Z",
  timezone: "Asia/Shanghai",
  channel: "电话",
  purpose: "确认交付",
  notes: "约定的细节",
  status: "planned",
  priority: "normal",
  completedAt: null,
  result: null,
  nextStep: null,
  skippedAt: null,
  skipReason: null,
  cancelledAt: null,
  cancelReason: null,
  rescheduledFromId: null,
  version: 8,
  createdAt: "2026-09-01T08:00:00Z",
  updatedAt: "2026-09-18T08:00:00Z",
  clientVersion: 12,
  nextFollowup: null,
};
const mock = vi.hoisted(() => ({
  activity: vi.fn(),
  followup: vi.fn(),
  mutate: vi.fn(),
  reset: vi.fn(),
  missingClient: false,
}));
vi.mock("../api/client", async (original) => ({
  ...(await original<typeof import("../api/client")>()),
  getClientActivity: mock.activity,
  getClientFollowup: mock.followup,
}));
vi.mock("../api/hooks", async (original) => {
  const actual = await original<typeof import("../api/hooks")>();
  const query = () => ({
    data: {
      items: [],
      meta: {
        page: 1,
        pageSize: 6,
        total: 100,
        serverNow: "2026-09-18T00:00:00Z",
      },
    },
    isSuccess: true,
    isPending: false,
    isError: false,
    isFetching: false,
    refetch: vi.fn(),
  });
  const mutation = () => ({
    mutate: mock.mutate,
    reset: mock.reset,
    isPending: false,
    error: null,
  });
  return {
    ...actual,
    useClientQuery: () => ({
      data: mock.missingClient
        ? undefined
        : {
            id: clientId,
            name: "真实客户",
            status: "active",
            notes: null,
            version: 12,
            projectCount: 0,
            createdAt: activity.createdAt,
            updatedAt: activity.updatedAt,
          },
      isPending: false,
      isError: mock.missingClient,
      refetch: vi.fn(),
    }),
    useProjectsQuery: query,
    useClientActivitiesQuery: query,
    useClientFollowupsQuery: query,
    useClientFollowupActorOptionsQuery: () => ({ data: [actor] }),
    useUpdateClient: mutation,
    useDeleteClient: mutation,
    useCreateClientActivity: mutation,
    useUpdateClientActivity: mutation,
    useDeleteClientActivity: mutation,
    useCreateClientFollowup: mutation,
    useUpdateClientFollowup: mutation,
    useCompleteClientFollowup: mutation,
    useSkipClientFollowup: mutation,
    useCancelClientFollowup: mutation,
    useRescheduleClientFollowup: mutation,
  };
});
vi.mock("./ClientFormModal", () => ({ ClientFormModal: () => null }));
vi.mock("./ClientAttachmentsSection", () => ({
  ClientAttachmentsSection: () => null,
}));
vi.mock("./ClientActorLinksSection", () => ({
  ClientActorLinksSection: () => null,
}));

function Navigation() {
  const navigate = useNavigate();
  const location = useLocation();
  return (
    <>
      <output aria-label="当前地址">
        {location.pathname}
        {location.search}
      </output>
      <Link
        to={`/clients/${clientId}?followup=${followupId}&return_session=${sessionId}`}
      >
        另一条回访
      </Link>
      <button onClick={() => navigate(-1)}>后退</button>
    </>
  );
}
const clients: QueryClient[] = [];
function mount(path = "/ai") {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  clients.push(client);
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <Navigation />
        <Routes>
          <Route
            path="/ai"
            element={
              <>
                {renderAiRichText(
                  `[查看该活动](/clients/${clientId}?activity=${activityId})`,
                  sessionId,
                )}
              </>
            }
          />
          <Route path="/clients/:clientId" element={<ClientDetailPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
beforeEach(() => {
  vi.clearAllMocks();
  mock.missingClient = false;
  mock.activity.mockResolvedValue(activity);
  mock.followup.mockResolvedValue(followup);
});
afterEach(() => {
  cleanup();
  clients.splice(0).forEach((client) => client.clear());
  useAiChatStore.setState({ activeSessionId: "" });
  useAiWorkbenchHandoff.setState({ pending: null, pendingIssue: null });
});

describe("AI to exact Client record and back", () => {
  it("opens an off-page activity, edits the actual version only on user action, and returns to the source chat", async () => {
    mount();
    fireEvent.click(screen.getByRole("link", { name: "查看该活动" }));
    expect(await screen.findByText(activity.body!)).toBeVisible();
    expect(screen.getByLabelText("当前地址")).toHaveTextContent(
      `activity=${activityId}&return_session=${sessionId}`,
    );
    expect(mock.mutate).not.toHaveBeenCalled();
    fireEvent.click(
      screen.getByRole("button", { name: `编辑活动 ${activity.title}` }),
    );
    expect(screen.getByLabelText("正文")).toHaveValue(activity.body);
    fireEvent.change(screen.getByLabelText("正文"), {
      target: { value: "人工核实后的正文" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存修改" }));
    expect(mock.mutate).toHaveBeenCalledWith(
      expect.objectContaining({
        id: activityId,
        input: expect.objectContaining({
          expectedVersion: 7,
          body: "人工核实后的正文",
        }),
      }),
      expect.any(Object),
    );
    useAiChatStore.setState({ activeSessionId: "another-chat" });
    fireEvent.click(screen.getByRole("link", { name: "返回原对话" }));
    expect(useAiChatStore.getState().activeSessionId).toBe(sessionId);
    expect(screen.getByRole("link", { name: "查看该活动" })).toBeVisible();
  });

  it("switches record kinds via navigation and back; closing removes selection without losing return context", async () => {
    mount(
      `/clients/${clientId}?activity=${activityId}&return_session=${sessionId}`,
    );
    expect(await screen.findByText(activity.body!)).toBeVisible();
    fireEvent.click(screen.getByRole("link", { name: "另一条回访" }));
    expect(await screen.findByText(followup.purpose)).toBeVisible();
    expect(screen.queryByText(activity.body!)).toBeNull();
    fireEvent.click(
      within(screen.getByLabelText("定位的客户回访")).getByRole("button", {
        name: "完成",
      }),
    );
    expect(screen.getByText(`记录回访结果：${followup.purpose}`)).toBeVisible();
    expect(mock.mutate).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "后退" }));
    expect(await screen.findByText(activity.body!)).toBeVisible();
    expect(screen.queryByText(`记录回访结果：${followup.purpose}`)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "关闭定位" }));
    expect(screen.getByLabelText("当前地址")).toHaveTextContent(
      `/clients/${clientId}?return_session=${sessionId}`,
    );
    expect(screen.queryByLabelText("定位的客户活动")).toBeNull();
    expect(screen.getByRole("link", { name: "返回原对话" })).toBeVisible();
  });

  it("hands the exact client activity and followup to the agent without executing them", async () => {
    mount(
      `/clients/${clientId}?activity=${activityId}&return_session=${sessionId}`,
    );
    expect(await screen.findByText(activity.body!)).toBeVisible();
    const activityPanel = await screen.findByLabelText("定位的客户活动");
    fireEvent.click(
      within(activityPanel).getByRole("button", { name: "交给智能体" }),
    );
    expect(useAiWorkbenchHandoff.getState().pendingIssue).toMatchObject({
      label: "客户活动",
      route: `/clients/${clientId}?activity=${activityId}`,
      scopes: ["work", "clients", "actions"],
    });
    expect(useAiWorkbenchHandoff.getState().pendingIssue?.prompt).toContain(
      `id=${activityId}`,
    );
    expect(mock.mutate).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("link", { name: "另一条回访" }));
    expect(await screen.findByText(followup.purpose)).toBeVisible();
    const followupPanel = await screen.findByLabelText("定位的客户回访");
    fireEvent.click(
      within(followupPanel).getByRole("button", { name: "交给智能体" }),
    );
    expect(useAiWorkbenchHandoff.getState().pendingIssue).toMatchObject({
      label: "客户回访",
      route: `/clients/${clientId}?followup=${followupId}`,
      scopes: ["work", "clients", "actions"],
    });
    expect(useAiWorkbenchHandoff.getState().pendingIssue?.prompt).toContain(
      `id=${followupId}`,
    );
    expect(mock.mutate).not.toHaveBeenCalled();
  });

  it("displays deleted activity metadata without stale body or edit controls", async () => {
    mock.activity.mockResolvedValue({
      ...activity,
      deletedAt: activity.updatedAt,
      deleteReason: "重复记录",
      body: null,
    });
    mount(`/clients/${clientId}?activity=${activityId}`);
    expect(await screen.findByText(/已于.*删除：重复记录/)).toBeVisible();
    expect(screen.queryByText(activity.body!)).toBeNull();
    expect(
      screen.queryByRole("button", { name: `编辑活动 ${activity.title}` }),
    ).toBeNull();
    expect(mock.mutate).not.toHaveBeenCalled();
  });

  it("rejects ambiguous links and keeps a return path even if the customer is unavailable", async () => {
    mount(
      `/clients/${clientId}?activity=${activityId}&followup=${followupId}&return_session=${sessionId}`,
    );
    expect(screen.getByRole("alert")).toHaveTextContent("客户记录链接无效");
    expect(mock.activity).not.toHaveBeenCalled();
    expect(mock.followup).not.toHaveBeenCalled();
    expect(screen.getByRole("link", { name: "返回原对话" })).toBeVisible();
    mock.missingClient = true;
    fireEvent.click(screen.getByRole("link", { name: "另一条回访" }));
    await waitFor(() =>
      expect(screen.getByText("客户详情不可用")).toBeVisible(),
    );
    expect(screen.getByRole("link", { name: "返回原对话" })).toBeVisible();
  });
});
