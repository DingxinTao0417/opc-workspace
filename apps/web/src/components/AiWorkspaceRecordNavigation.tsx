import { useState } from "react";
import { useNavigate } from "react-router-dom";
import {
  aiWorkspaceRecordNavigationLabels,
  aiWorkspaceRecordNavigationRoute,
} from "../lib/aiWorkspaceNavigation";
import { useUiStore } from "../store/ui";
import { useSettingsStore } from "../store/settings";
import { useWorkspacePanels } from "../store/workspacePanels";

const canonicalSessionId =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

function routeWithReturnSession(
  route: string,
  sessionId: string | null,
): string {
  if (!sessionId || !canonicalSessionId.test(sessionId)) return route;
  const target = new URL(route, window.location.origin);
  target.searchParams.set("return_session", sessionId);
  return `${target.pathname}${target.search}`;
}

// The model cannot supply a route. The Sidecar authorizes a closed record
// identity, the client derives the route, and the human must still choose to
// leave the chat. This preserves the user's conversation return path without
// copying a record snapshot into the UI store.
export function AiWorkspaceRecordNavigation({
  sessionId,
}: {
  sessionId: string | null;
}) {
  const navigate = useNavigate();
  const [previewError, setPreviewError] = useState<{
    requestId: number;
    message: string;
  } | null>(null);
  const request = useUiStore((state) => state.workspaceRecordNavigationRequest);
  const dismiss = useUiStore((state) => state.dismissWorkspaceRecordNavigation);
  const toggleRightOverviewCollapsed = useUiStore(
    (state) => state.toggleRightOverviewCollapsed,
  );
  const configuredRightOverview = useSettingsStore(
    (state) =>
      state.preview?.general.showRightOverview ?? state.showRightOverview,
  );
  if (!request) return null;
  const label = aiWorkspaceRecordNavigationLabels[request.recordType];
  const route = aiWorkspaceRecordNavigationRoute(
    request.recordType,
    request.recordId,
    request.taskId,
    request.parentId,
    request.submissionId,
  );
  if (!route) return null;
  const open = () => {
    dismiss(request.id);
    navigate(routeWithReturnSession(route, sessionId));
  };
  const preview = () => {
    if (request.recordType !== "task_artifact") return;
    setPreviewError(null);
    const panelId = `task-artifact:${request.recordId}`;
    try {
      const panels = useWorkspacePanels.getState();
      const existing = panels.tabs.find((tab) => tab.id === panelId);
      if (
        existing &&
        (existing.kind !== "artifact" ||
          existing.artifactTaskId !== request.taskId ||
          existing.artifactSubmissionId !== request.submissionId)
      ) {
        throw new Error("已有同名预览标签与当前产出归属不一致，请关闭后重试。");
      }
      if (existing) {
        panels.update(panelId, {
          returnSession:
            sessionId && canonicalSessionId.test(sessionId)
              ? sessionId
              : undefined,
        });
        panels.select(panelId);
      } else
        panels.open(
          {
            kind: "artifact",
            title: "任务产出",
            artifactId: request.recordId,
            artifactTaskId: request.taskId,
            artifactSubmissionId: request.submissionId,
            ...(sessionId && canonicalSessionId.test(sessionId)
              ? { returnSession: sessionId }
              : {}),
          },
          panelId,
        );
      // A previously queued generic panel request must not steal focus when
      // expanding the right workspace for this newer, explicit preview.
      const ui = useUiStore.getState();
      useWorkspacePanels.setState({
        handledRequest: `${ui.rightPanelTab}:${ui.rightPanelRequestMode}:${ui.rightPanelRequest}`,
      });
      if (ui.rightOverviewCollapsed) toggleRightOverviewCollapsed();
      dismiss(request.id);
    } catch (error) {
      setPreviewError({
        requestId: request.id,
        message:
          error instanceof Error ? error.message : "右侧预览无法打开，请重试。",
      });
    }
  };
  return (
    <section className="ai-workspace-record-navigation" role="alert">
      <div>
        <strong>智能体建议打开{label}</strong>
        <p>
          {request.recordType === "task_artifact"
            ? "可留在对话并在右侧只读预览，或打开完整详情；都不会把产出内容回传给智能体。"
            : request.recordType === "task_saved_view"
              ? "确认后会在任务页重新读取并应用当前保存的筛选条件；不会修改任务，也不会把页面结果回传给智能体。"
              : "仅会在本地工作台定位该记录；不会自动离开对话，也不会把记录内容回传给智能体。"}
        </p>
        {previewError?.requestId === request.id && (
          <p role="alert">{previewError.message}</p>
        )}
      </div>
      <div className="ai-workspace-record-navigation-actions">
        <button
          className="button button-secondary"
          onClick={() => dismiss(request.id)}
          type="button"
        >
          留在对话
        </button>
        {request.recordType === "task_artifact" && configuredRightOverview && (
          <button
            className="button button-primary"
            onClick={preview}
            type="button"
          >
            右侧预览
          </button>
        )}
        <button
          className={
            request.recordType === "task_artifact" && configuredRightOverview
              ? "button button-secondary"
              : "button button-primary"
          }
          onClick={open}
          type="button"
        >
          {request.recordType === "task_artifact"
            ? "打开完整详情"
            : `打开${label}`}
        </button>
      </div>
    </section>
  );
}
