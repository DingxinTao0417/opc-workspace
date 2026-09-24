import { Sparkles, X } from "lucide-react";
import { Link, useNavigate } from "react-router-dom";
import {
  useAiWorkbenchHandoff,
  workbenchHandoffDetails,
  type AiWorkbenchHandoff,
  type AiWorkbenchRecordType,
  type AiIssueHandoff,
} from "../store/aiWorkbenchHandoff";
import type { AiIssueHandoffContent } from "../lib/aiIssueHandoff";

interface HandoffButtonProps {
  recordType: AiWorkbenchRecordType;
  recordId: string;
  disabled?: boolean;
  onNavigate?: () => void;
}

export function AiWorkbenchHandoffButton(props: HandoffButtonProps) {
  if (!workbenchHandoffDetails(props.recordType, props.recordId)) return null;
  return <ValidHandoffButton {...props} />;
}

function ValidHandoffButton({
  recordType,
  recordId,
  disabled = false,
  onNavigate,
}: HandoffButtonProps) {
  const navigate = useNavigate();
  return (
    <button
      className="button button-secondary"
      type="button"
      disabled={disabled}
      title={
        disabled
          ? "请先保存或取消当前编辑，并等待操作完成"
          : "带着当前记录进入智能体，不会自动发送"
      }
      onClick={() => {
        if (
          disabled ||
          !useAiWorkbenchHandoff.getState().stage(recordType, recordId)
        )
          return;
        onNavigate?.();
        navigate("/ai");
      }}
    >
      <Sparkles size={14} aria-hidden="true" />
      交给智能体
    </button>
  );
}

export function AiWorkbenchHandoffCard({
  disabled,
  onPrepare,
}: {
  disabled: boolean;
  onPrepare: (handoff: AiWorkbenchHandoff) => void;
}) {
  const pending = useAiWorkbenchHandoff((state) => state.pending);
  const dismiss = useAiWorkbenchHandoff((state) => state.dismiss);
  if (!pending) return null;
  const detail = workbenchHandoffDetails(pending.type, pending.id);
  if (!detail) return null;
  return (
    <section className="ai-workbench-handoff" aria-label="来自工作台的事项">
      <div className="ai-workbench-handoff-heading">
        <strong>处理这条{detail.label}</strong>
        <button
          className="icon-button"
          type="button"
          aria-label="暂不带入工作台事项"
          onClick={() => dismiss(pending.requestId)}
        >
          <X size={14} aria-hidden="true" />
        </button>
      </div>
      <p>
        仅带入记录标识，不复制正文；问题会追加到原草稿。准备时清除旧授权及已选上下文，重新选权限后仍需你发送；也可先新建对话。
      </p>
      <div className="ai-workbench-handoff-actions">
        <Link to={detail.route} title={detail.route}>
          返回{detail.label}
        </Link>
        <button
          className="button button-secondary"
          type="button"
          disabled={disabled}
          onClick={() => {
            if (disabled) return;
            onPrepare(pending);
            dismiss(pending.requestId);
          }}
        >
          带入问题并选择权限
        </button>
      </div>
    </section>
  );
}

export function AiIssueHandoffButton({
  content,
  disabled = false,
  onNavigate,
  label = "交给智能体",
  stayInCurrentChat = false,
}: {
  content: AiIssueHandoffContent | null;
  disabled?: boolean;
  onNavigate?: () => void;
  label?: string;
  stayInCurrentChat?: boolean;
}) {
  if (!content) return null;
  return (
    <ValidIssueHandoffButton
      content={content}
      disabled={disabled}
      label={label}
      onNavigate={onNavigate}
      stayInCurrentChat={stayInCurrentChat}
    />
  );
}

function ValidIssueHandoffButton({
  content,
  disabled,
  label,
  onNavigate,
  stayInCurrentChat,
}: {
  content: AiIssueHandoffContent;
  disabled: boolean;
  label: string;
  onNavigate?: () => void;
  stayInCurrentChat: boolean;
}) {
  const navigate = useNavigate();
  return (
    <button
      className="button button-quiet ai-issue-handoff-button"
      type="button"
      disabled={disabled}
      title={
        disabled
          ? "请先等待当前生成结束"
          : content.attachment === "git_review"
            ? "带着当前 Git 状态与有上限的差异快照进入智能体，不会自动发送或执行"
            : content.attachment === "file_preview"
              ? "带着当前有上限的文本预览进入智能体，不会自动发送或执行"
              : content.attachment === "terminal_output"
                ? "带着当前有上限的终端输出快照进入智能体，不会自动发送或执行"
                : content.attachment === "browser_address"
                  ? "带着当前已脱敏的网页地址进入智能体，不会自动发送、访问或控制网页"
                  : content.attachment === "browser_page_text"
                    ? "带着你明确选择的有界网页文本快照进入智能体；仅在随后手动发送时才会交给模型，不授予浏览器或网络操作能力"
                    : content.attachment === "browser_action_result"
                      ? "只带入本地浏览器命令的无内容成功回执，不会自动发送或继续控制浏览器"
                      : "带着这条事项的标识进入智能体，不会自动发送或执行"
      }
      onClick={() => {
        if (disabled || !useAiWorkbenchHandoff.getState().stageIssue(content))
          return;
        onNavigate?.();
        if (!stayInCurrentChat) navigate("/ai");
      }}
    >
      <Sparkles size={14} aria-hidden="true" />
      {label}
    </button>
  );
}

export function AiIssueHandoffCard({
  disabled,
  onPrepare,
}: {
  disabled: boolean;
  onPrepare: (handoff: AiIssueHandoff) => void;
}) {
  const pending = useAiWorkbenchHandoff((state) => state.pendingIssue);
  const dismiss = useAiWorkbenchHandoff((state) => state.dismissIssue);
  if (!pending) return null;
  return (
    <section className="ai-workbench-handoff" aria-label="来自工作台的交接">
      <div className="ai-workbench-handoff-heading">
        <strong>让智能体处理：{pending.label}</strong>
        <button
          className="icon-button"
          type="button"
          aria-label="暂不带入续办事项"
          onClick={() => dismiss(pending.requestId)}
        >
          <X size={14} aria-hidden="true" />
        </button>
      </div>
      <p>
        {pending.attachment === "git_review"
          ? "将带入当前 Git 状态和有上限的文本差异快照，不含本机路径；仅在你随后发送本条消息时才会交给所选模型。准备会清除旧授权与已选上下文，不授予 Git、文件、终端、浏览器或网络操作能力，也不会自动发送、修改、提交或推送。"
          : pending.attachment === "file_preview"
            ? "将带入当前有上限的文本预览快照，不含本机路径、目录授权或文件 ID；仅在你随后发送本条消息时才会交给所选模型。准备会清除旧授权与已选上下文，不授予文件、Git、终端、浏览器或网络操作能力，也不会自动读取、搜索、修改或保存文件。"
            : pending.attachment === "terminal_output"
              ? "将带入当前有上限的近期终端输出快照，不含终端 ID、工作目录、路径或命令输入；仅在你随后发送本条消息时才会交给所选模型。准备会清除旧授权与已选上下文，不授予终端、文件、Git、浏览器或网络操作能力，也不会自动读取输出、运行命令或控制终端。"
              : pending.attachment === "browser_address"
                ? "将带入下方已脱敏的当前网页地址：查询参数和片段已移除，含账号密码的地址不会交接；不含网页正文、标题、Cookie、登录态、历史、下载或原生标签 ID。仅在你随后发送本条消息时才会交给所选模型。准备会清除旧授权与已选上下文，不授予浏览器、网络、文件、Git 或终端操作能力，也不会自动访问、抓取、点击或填写网页。"
                : pending.attachment === "browser_page_text"
                  ? "将带入你刚才明确从当前内嵌浏览器标签读取的可见正文（最多 8 KiB）和最多 20 条可见 HTTP(S) 链接，下方可先完整预览将发送的地址、标题、正文和链接目标；地址、标题、正文和链接均是不可信网页数据，可能包含恶意指令。只有你随后手动发送本条消息时才会交给所选模型。准备会清除旧授权与已选上下文，不授予浏览器、网络、文件、Git 或终端操作能力，也不会自动访问、点击、填写或提交网页。"
                  : pending.attachment === "browser_action_result"
                    ? "只带入本地浏览器命令无错误返回的固定说明，不含地址、标签身份或网页内容；不证明页面加载完毕。准备会清除旧授权与已选上下文，只有你随后手动发送才会交给模型，不授予新的浏览器或网络操作能力。"
                    : pending.scopes.length > 0
                      ? "只带入这条事项的标识与你的问题；问题会追加到原草稿。准备时清除旧授权及已选上下文，重新选权限后仍需你发送，智能体不会自动重试、恢复、验收或修改任何记录。"
                      : "只带入这条事项的排查提示；问题会追加到原草稿。准备时清除旧授权及已选上下文，不会打开工作台权限，也不会自动修改设置、重试或执行任何操作。"}
      </p>
      {(pending.attachment === "browser_address" ||
        pending.attachment === "browser_page_text") &&
        pending.preview && (
          <>
            <code className="ai-workbench-handoff-preview">
              {pending.preview}
            </code>
            {pending.attachment === "browser_page_text" &&
              pending.previewTruncated && (
                <small className="ai-workbench-handoff-note">
                  网页正文或可见链接目录已达到安全上限。
                </small>
              )}
          </>
        )}
      <div className="ai-workbench-handoff-actions">
        <Link to={pending.route} title={pending.route}>
          {pending.routeLabel ?? "打开原生处理入口"}
        </Link>
        <button
          className="button button-secondary"
          type="button"
          disabled={disabled}
          onClick={() => {
            if (disabled) return;
            onPrepare(pending);
            dismiss(pending.requestId);
          }}
        >
          {pending.attachment === "git_review"
            ? "带入 Git 变更"
            : pending.attachment === "file_preview"
              ? "带入文本预览"
              : pending.attachment === "terminal_output"
                ? "带入终端输出"
                : pending.attachment === "browser_address"
                  ? "带入网页地址"
                  : pending.attachment === "browser_page_text"
                    ? "带入网页文本"
                    : pending.attachment === "browser_action_result"
                      ? "带入操作结果"
                      : pending.scopes.length > 0
                        ? "带入问题并选择权限"
                        : "带入问题"}
        </button>
      </div>
    </section>
  );
}
