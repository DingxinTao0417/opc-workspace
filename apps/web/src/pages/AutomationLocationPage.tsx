import { useEffect, useRef } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { AutomationSettings } from "../components/AutomationSettings";
import { AutomationRunDetailModal } from "../components/AutomationRunDetailModal";
import { ReturnToAiChat } from "../components/ClientRecordLocation";
import { PageHeader } from "../components/PageHeader";
import { aiWorkspaceHref } from "../lib/aiWorkspaceLinks";
import {
  automationLocationHref,
  parseAutomationLocation,
} from "../lib/automationLocation";
import {
  focusReportReturnSession,
  isWorkspaceIdentity,
} from "../lib/focusReportLocation";

// A routed view of the same human-operated settings and run inspector. Opening
// it never confirms an AI proposal or grants the model access to snapshots.
export function AutomationLocationPage() {
  const [params, setParams] = useSearchParams();
  const navigate = useNavigate();
  const selection = parseAutomationLocation(params);
  const returnSession = focusReportReturnSession(params);
  const invalid =
    ([...params.keys()].some((key) => key !== "return_session") &&
      !selection) ||
    params.getAll("return_session").length > 1;
  const heading = useRef<HTMLDivElement>(null);
  const selectionKey = selection ? `${selection.kind}:${selection.id}` : "list";
  useEffect(() => {
    heading.current?.scrollIntoView?.({ block: "start" });
    heading.current?.focus({ preventScroll: true });
  }, [selectionKey]);
  const clear = () => {
    const next = new URLSearchParams();
    if (returnSession) next.set("return_session", returnSession);
    setParams(next);
  };
  const open = (kind: "rule" | "run", id: string) =>
    navigate(
      aiWorkspaceHref(
        automationLocationHref(kind, id),
        returnSession ?? undefined,
      ),
    );
  const openResult = (kind: "task" | "inbox_item" | "reminder", id: string) => {
    if (!isWorkspaceIdentity(id)) return;
    const route =
      kind === "task"
        ? `/tasks/${id}`
        : kind === "inbox_item"
          ? `/inbox/${id}`
          : `/inbox?reminder=${id}`;
    navigate(aiWorkspaceHref(route, returnSession ?? undefined));
  };
  const actions = {
    onOpenTask: (id: string) => openResult("task", id),
    onOpenInboxItem: (id: string) => openResult("inbox_item", id),
    onOpenReminder: (id: string) => openResult("reminder", id),
  };
  return (
    <div className="page">
      <div ref={heading} tabIndex={-1}>
        <PageHeader
          title={selection?.kind === "run" ? "自动化运行详情" : "自动化设置"}
          actions={
            <>
              <ReturnToAiChat sessionId={returnSession} />
              {(selection || invalid) && (
                <button
                  className="button button-secondary"
                  onClick={clear}
                  type="button"
                >
                  查看全部规则
                </button>
              )}
            </>
          }
        />
      </div>
      {invalid ? (
        <div role="alert" className="settings-state settings-state-error">
          自动化定位链接无效；不会读取或执行其他规则、运行记录。
        </div>
      ) : selection?.kind === "run" ? (
        <AutomationRunDetailModal
          {...actions}
          key={selectionKey}
          runId={selection.id}
          inline
          onSelectRun={(id) => open("run", id)}
          onClose={clear}
        />
      ) : (
        <AutomationSettings
          {...actions}
          key={selectionKey}
          requestedRuleId={selection?.id}
          onSelectRule={(id) => open("rule", id)}
          onSelectRun={(id) => open("run", id)}
        />
      )}
    </div>
  );
}
