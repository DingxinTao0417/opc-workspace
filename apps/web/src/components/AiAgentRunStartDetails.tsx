import type {
  AiActionProposal,
  AiAgentRunStartPreview,
} from "../api/aiWorkspaceActions";
import { AgentRunReworkDetails } from "./AgentRunReworkDetails";
import { ProjectFileSourceDetails } from "./ProjectTaskFileSelect";

const taskStatus: Record<string, string> = {
  todo: "待办",
  in_progress: "进行中",
  blocked: "已阻塞",
};
const taskKind: Record<string, string> = {
  work: "工作",
  review: "审核",
  followup: "跟进",
  reminder: "提醒",
};
const fileBytes = (value: number) =>
  value < 1024 ? `${value} B` : `${(value / 1024).toFixed(1)} KiB`;

export function AiAgentRunStartDetails({
  proposal,
  preview: suppliedPreview,
}: {
  proposal?: AiActionProposal;
  preview?: AiAgentRunStartPreview;
}) {
  const preview = suppliedPreview ?? proposal?.preview.agent_run_start;
  if (!preview) return null;
  const controlledFiles = preview.input_files ?? [];
  const fileContract =
    controlledFiles.length > 0 ||
    preview.output_contract?.type === "file" ||
    preview.output_contract?.type === "files";
  const fileOutput =
    fileContract &&
    (preview.output_contract?.type === "file" ||
      preview.output_contract?.type === "files");
  const providerLeavesDevice =
    preview.provider.leaves_device ?? preview.provider.kind === "remote";
  return (
    <section aria-label="Agent 执行完整快照" className="ai-split-preview">
      {preview.start_gate ? (
        <section aria-label="启动条件">
          <h4>满足条件后由系统启动 · 不是现在启动</h4>
          <p>
            等待任务「{preview.start_gate.predecessor_task_title}」第{" "}
            {preview.start_gate.predecessor_attempt} 次执行（
            <code>{preview.start_gate.predecessor_run_id}</code>）的产出
            {preview.start_gate.require === "accepted"
              ? "被人工验收接受"
              : "登记为任务提交"}
            后，才让下方这一次执行进入队列；确认后{" "}
            {preview.start_gate.within_hours} 小时内未满足即自动取消。
          </p>
          <p>
            前置执行失败、取消、只保留结果
            {preview.start_gate.require === "accepted"
              ? "、被要求返工或撤回"
              : ""}
            时也会取消，不会改用其他执行或自动重试。等待期间可随时取消；启动前仍会复核下方冻结的任务、Agent
            与 Provider，任一变化即失败关闭。前置产出不会自动成为本次输入。
          </p>
        </section>
      ) : null}
      {preview.restart ? (
        <section aria-label="新执行关联的原运行">
          <h4>按当前事实重新执行 · 不是原样重试</h4>
          <p>
            原执行 <code>{preview.restart.run_id}</code> · 第{" "}
            {preview.restart.attempt} 次 · {preview.restart.status} /{" "}
            {preview.restart.output_delivery_status}
          </p>
          <p>
            原任务版本 {preview.restart.task_version} · Agent{" "}
            <code>{preview.restart.actor_id}</code> · Provider{" "}
            <code>{preview.restart.provider_id}</code> · 模型{" "}
            {preview.restart.model} · 结束 {preview.restart.completed_at}
          </p>
          <p>
            下方是本次重新冻结的当前事实，可能与原任务、分派及模型不同。旧运行、结果与审批不覆盖；不会继承原文件或返工资料。确认后会重新调用所选
            Provider，可能产生费用；新产出仍需人工验收。
          </p>
        </section>
      ) : null}
      <p className="ai-action-note">
        确认后才会启动一次隔离的 Agent Run。执行成功后，服务端会尝试把
        {fileOutput ? "一个受控 UTF-8 文件结果" : "文本结果"}
        登记为待验收产出；登记失败时结果仍保留在 Run
        中。无论哪种情况都不会自动完成任务或代替你验收。
      </p>
      {preview.rework_context ? (
        <AgentRunReworkDetails
          context={preview.rework_context}
          retry={proposal?.action.action === "agent_run.retry"}
        />
      ) : null}
      <h4>任务冻结快照</h4>
      <dl>
        <div>
          <dt>任务</dt>
          <dd>{preview.task.title}</dd>
        </div>
        <div>
          <dt>任务 ID</dt>
          <dd>
            <code>{preview.task.id}</code>
          </dd>
        </div>
        <div>
          <dt>说明</dt>
          <dd>{preview.task.description || "未设置"}</dd>
        </div>
        <div>
          <dt>完成条件</dt>
          <dd>{preview.task.completion_criteria || "未设置"}</dd>
        </div>
        <div>
          <dt>状态 / 类型</dt>
          <dd>
            {taskStatus[preview.task.status]} / {taskKind[preview.task.kind]}
          </dd>
        </div>
        <div>
          <dt>优先级</dt>
          <dd>{preview.task.priority}</dd>
        </div>
        <div>
          <dt>项目 / 父任务</dt>
          <dd>
            {preview.task.project_id ?? "未归项目"} /{" "}
            {preview.task.parent_task_id ?? "无父任务"}
          </dd>
        </div>
        <div>
          <dt>计划 / 截止</dt>
          <dd>
            {preview.task.planned_date ?? "未安排"} /{" "}
            {preview.task.due_date ?? "无截止时间"}
          </dd>
        </div>
        <div>
          <dt>预计 / 已记录</dt>
          <dd>
            {preview.task.estimated_minutes ?? 0} /{" "}
            {preview.task.actual_minutes} 分钟
          </dd>
        </div>
        <div>
          <dt>手动顺序</dt>
          <dd>{preview.task.manual_order ?? "未设置"}</dd>
        </div>
        <div>
          <dt>创建 / 更新时间</dt>
          <dd>
            {preview.task.created_at} / {preview.task.updated_at}
          </dd>
        </div>
        <div>
          <dt>验收方式</dt>
          <dd>
            {preview.task.review_policy === "manual" ? "人工验收" : "无需验收"}
          </dd>
        </div>
        <div>
          <dt>任务版本</dt>
          <dd>{preview.task.version}</dd>
        </div>
      </dl>
      <h4>执行身份</h4>
      <dl>
        <div>
          <dt>Agent</dt>
          <dd>
            {preview.agent.display_name} · 已启用 · 版本 {preview.agent.version}
          </dd>
        </div>
        <div>
          <dt>Agent ID</dt>
          <dd>
            <code>{preview.agent.id}</code>
          </dd>
        </div>
        <div>
          <dt>责任分派</dt>
          <dd>
            <code>{preview.assignment.id}</code> ·{" "}
            {preview.assignment.assigned_at}
          </dd>
        </div>
        <div>
          <dt>Adapter</dt>
          <dd>
            {preview.adapter.display_name} · {preview.adapter.protocol_version}{" "}
            · 健康且隔离已验证 · 版本 {preview.adapter.version}
          </dd>
        </div>
        <div>
          <dt>Adapter ID</dt>
          <dd>
            <code>{preview.adapter.id}</code>
          </dd>
        </div>
      </dl>
      <h4>Provider 与运行边界</h4>
      <dl>
        <div>
          <dt>Provider</dt>
          <dd>
            {preview.provider.name}（
            {preview.provider.kind === "local" ? "本地端点" : "远程服务"}） ·
            健康可用 ·{providerLeavesDevice ? " 数据离开本机" : " 数据留在本机"}
          </dd>
        </div>
        <div>
          <dt>Provider ID</dt>
          <dd>
            <code>{preview.provider.id}</code>
          </dd>
        </div>
        <div>
          <dt>协议 / 模型</dt>
          <dd>
            {preview.provider.protocol} / {preview.provider.model}
          </dd>
        </div>
        <div>
          <dt>Provider 版本</dt>
          <dd>
            记录 {preview.provider.version} · 配置{" "}
            {preview.provider.config_version}
          </dd>
        </div>
        <div>
          <dt>执行次数</dt>
          <dd>第 {preview.attempt} 次</dd>
        </div>
        <div>
          <dt>运行上限</dt>
          <dd>
            {preview.runtime_limits.timeout_seconds / 60} 分钟 · 文本结果最多{" "}
            {preview.runtime_limits.max_result_bytes / 1024} KiB
            {preview.runtime_limits.max_output_tokens !== undefined
              ? ` · 最多 ${preview.runtime_limits.max_output_tokens} 输出 tokens`
              : ""}
          </dd>
        </div>
        <div>
          <dt>执行契约</dt>
          <dd>版本 {preview.execution_contract_version}</dd>
        </div>
      </dl>
      {fileContract && preview.output_contract && preview.file_limits ? (
        <>
          <h4>受控文件与产出契约</h4>
          {controlledFiles.length > 0 ? (
            <dl aria-label="受控输入文件元数据">
              {controlledFiles.map((file) => (
                <div key={file.id}>
                  <dt>{file.name}</dt>
                  <dd>
                    {file.source_kind === "task_artifact"
                      ? "任务产出文件（task_artifact）"
                      : file.source_kind === "project_task_artifact"
                        ? "同项目跨任务已验收文件（project_task_artifact）"
                        : "项目附件（project_attachment）"}
                    ；{file.mime}；{fileBytes(file.size_bytes)}；ID{" "}
                    <code>{file.id}</code>；SHA-256 <code>{file.sha256}</code>
                    <ProjectFileSourceDetails source={file.source_task} />
                  </dd>
                </div>
              ))}
            </dl>
          ) : (
            <p className="ai-action-note">本次没有选择受控输入文件。</p>
          )}
          <dl>
            <div>
              <dt>输入合计</dt>
              <dd>
                {controlledFiles.length} / {preview.file_limits.max_files} 个 ·{" "}
                {fileBytes(preview.input_file_total_bytes ?? 0)} /{" "}
                {fileBytes(preview.file_limits.max_total_bytes)}；单个最多{" "}
                {fileBytes(preview.file_limits.max_file_bytes)}
              </dd>
            </div>
            <div>
              <dt>产出类型</dt>
              <dd>
                {preview.output_contract.type === "file"
                  ? `文件 ${preview.output_contract.name}（${preview.output_contract.mime}）`
                  : preview.output_contract.type === "files"
                    ? `多个受控文件：${preview.output_contract.files
                        .map((file) => `${file.name}（${file.mime}）`)
                        .join("；")}`
                    : "行内文本"}
              </dd>
            </div>
            <div>
              <dt>产出上限</dt>
              <dd>{fileBytes(preview.file_limits.max_result_bytes)}</dd>
            </div>
          </dl>
          {controlledFiles.length > 0 ? (
            <p className="ai-action-note">
              卡片只展示服务端冻结的文件元数据，不含路径或正文。正文尚未读取；你还需在下方单独同意，执行器才会读取并校验这些精确文件。
              {preview.input_files_leave_device
                ? `所选文件字节将发送给远程 Provider「${preview.provider.name}」并离开本机。`
                : `所选文件字节只交给本地 Provider「${preview.provider.name}」，不会离开本机。`}
            </p>
          ) : null}
        </>
      ) : null}
      <p className="ai-action-note">
        只使用这里冻结的任务、执行身份
        {preview.rework_context ? "、上述返工意见和所选旧产出" : ""}
        {controlledFiles.length > 0 ? "和上述受控输入文件" : ""}。不会读取
        {controlledFiles.length > 0 ? "其他" : "任意"}
        文件、终端、浏览器或工作区内容，也不会把执行成功当作任务完成或人工验收通过；登记后的产出仍需人工检查和验收。
      </p>
    </section>
  );
}
