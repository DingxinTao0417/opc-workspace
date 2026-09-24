import { useEffect } from "react";
import type { AiProvider } from "../types/models";
import { useAiProjectFiles } from "../store/aiProjectFiles";

export function AiProjectFileContext({
  sessionId,
  provider,
  disabled,
}: {
  sessionId: string;
  provider: AiProvider | null;
  disabled: boolean;
}) {
  const state = useAiProjectFiles();
  const selection = state.selection;
  const files = selection?.files ?? [];
  const approved =
    !!selection &&
    !!provider &&
    state.approval?.selectionId === selection.id &&
    state.approval?.sessionId === sessionId &&
    state.approval?.providerId === provider.id &&
    state.approval?.providerVersion === provider.version;
  useEffect(() => {
    const current = useAiProjectFiles.getState();
    if (
      current.approval &&
      (!provider ||
        current.approval.sessionId !== sessionId ||
        current.approval.providerId !== provider.id ||
        current.approval.providerVersion !== provider.version)
    )
      current.revoke();
  }, [sessionId, provider?.id, provider?.version]);
  useEffect(() => () => useAiProjectFiles.getState().clear(), []);
  useEffect(() => {
    if (!selection) return;
    const timer = window.setTimeout(
      () => {
        if (useAiProjectFiles.getState().selection === selection) {
          useAiProjectFiles.getState().clear();
          useAiProjectFiles.setState({
            error: "文件基线已过期，请回到文件预览重新读取",
          });
        }
      },
      Math.max(0, selection.expiresAt - Date.now()),
    );
    return () => window.clearTimeout(timer);
  }, [selection]);
  if (!selection && !state.loading && !state.error) return null;
  return (
    <section className="ai-workbench-handoff" aria-label="本条消息的项目文件">
      <div className="ai-workbench-handoff-heading">
        <strong>项目文件 · 本条消息授权 {files.length}/4</strong>
        {selection && (
          <button
            type="button"
            className="button button-quiet"
            onClick={state.clear}
          >
            移除全部
          </button>
        )}
      </div>
      {state.loading && <p role="status">正在读取精确文件基线…</p>}
      {state.error && <p role="alert">{state.error}</p>}
      {selection && (
        <>
          <p>
            {selection.rootName} · {files.length} 个完整文件 · 合计{" "}
            {selection.totalBytes}/32768 字节
          </p>
          <div className="ai-project-file-list">
            {files.map(({ snapshot }) => (
              <article className="ai-project-file-item" key={snapshot.path}>
                <div className="ai-project-file-item-heading">
                  <span>
                    {snapshot.path} · {snapshot.size} 字节
                  </span>
                  <button
                    type="button"
                    className="button button-quiet"
                    aria-label={"移除 " + snapshot.path}
                    onClick={() => state.remove(snapshot.path)}
                  >
                    移除
                  </button>
                </div>
                <details>
                  <summary>查看此文件完整外发内容</summary>
                  <pre className="ws-source ai-project-file-source">
                    {snapshot.content}
                  </pre>
                </details>
              </article>
            ))}
          </div>
          <p>
            {provider ? (
              <>
                将把以上 {files.length} 个相对路径和完整内容发送给
                {provider.kind === "local" ? "本机模型 " : "远程供应商 "}
                {provider.name} · {provider.model}。
              </>
            ) : (
              "请先选择模型。"
            )}{" "}
            不额外附带所选目录的绝对路径或原生文件标识。正文中的路径、密钥等敏感内容不会自动脱敏，请先核对。原始附件仅供本条生成使用（可能包含多次模型请求），不写入聊天历史；模型回复可能引用文件内容，并按当前对话保存设置留存和用于后续消息。你要求修改时，模型可为本组文件中的一份提供完整差异建议；写入仍一次只处理一个文件。不会修改文件或执行终端命令。
          </p>
          {!sessionId && <p>请先新建或选择一个对话，再确认文件授权。</p>}
          <div className="ai-workbench-handoff-actions">
            {approved ? (
              <>
                <span role="status">
                  已授权下一条消息，发送前将重新核对文件
                </span>
                <button
                  type="button"
                  className="button button-secondary"
                  onClick={state.revoke}
                >
                  撤销文件授权
                </button>
              </>
            ) : (
              <button
                type="button"
                className="button button-secondary"
                disabled={disabled || !provider || !sessionId}
                onClick={() => {
                  if (provider && !disabled) state.approve(sessionId, provider);
                }}
              >
                仅授权下一条消息使用这些文件
              </button>
            )}
          </div>
        </>
      )}
    </section>
  );
}
