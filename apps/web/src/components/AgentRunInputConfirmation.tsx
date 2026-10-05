import { useEffect, useState } from "react";
import { ApiError } from "../api/client";
import {
  useAgentRunStartGateInputsQuery,
  useConfirmAgentRunStartGateInputs,
} from "../api/hooks";
import { agentRunLifecycle } from "../lib/agentRunLifecycle";
import type { AgentRun, AgentRunStartGateInputs } from "../types/models";
import { ProjectFileSourceDetails } from "./ProjectTaskFileSelect";

const fileBytes = (value: number) =>
  value < 1024 ? `${value} B` : `${(value / 1024).toFixed(1)} KiB`;

function readErrorText(error: unknown): string {
  const code = error instanceof ApiError ? error.code : undefined;
  if (code === "AGENT_RUN_INPUT_UNAVAILABLE") {
    return "前置任务已验收的文件已不可用（任务被重新打开、文件被删除或项目变化）。这次执行不会启动，系统会取消它；没有读取或发送任何文件。";
  }
  if (code === "AGENT_RUN_INPUT_NOT_READY") {
    return "启动条件已失效或等待时限已到，这次执行不会启动；没有读取或发送任何文件。";
  }
  return "无法读取前置产出，未展示任何文件，也不能确认。";
}

function confirmErrorText(error: unknown): string {
  const code = error instanceof ApiError ? error.code : undefined;
  if (code === "AGENT_RUN_NOT_EXECUTABLE") {
    return "Agent 适配器当前不可执行，这次执行无法启动；没有发送任何文件。请先恢复适配器后重新核对。";
  }
  if (
    code === "AGENT_RUN_INPUT_CHANGED" ||
    code === "AGENT_RUN_IDENTITY_CHANGED"
  ) {
    return "文件、任务或模型在你核对之后发生了变化，刚才的确认没有生效，也没有发送任何文件。文件清单已重新读取，请重新核对后再确认。";
  }
  if (
    code === "AGENT_RUN_INPUT_UNAVAILABLE" ||
    code === "AGENT_RUN_INPUT_NOT_READY"
  ) {
    return readErrorText(error);
  }
  return "确认没有生效，没有发送任何文件。文件清单已重新读取，请核对后重试。";
}

function InputFileList({ inputs }: { inputs: AgentRunStartGateInputs }) {
  return (
    <ul aria-label="前置产出文件" className="agent-run-input-files">
      {inputs.files.map((file) => (
        <li key={file.id}>
          <strong>{file.name}</strong>
          <span>
            {file.mime} · {fileBytes(file.size_bytes)}
          </span>
          <span>
            SHA-256 <code>{file.sha256}</code>
          </span>
          <ProjectFileSourceDetails source={file.source_task} />
        </li>
      ))}
    </ul>
  );
}

/**
 * The second human confirmation of a Run that takes its predecessor's
 * accepted files (ADR-033). It shows exactly what would be sent and to whom;
 * until both boxes are ticked nothing is started, read or sent.
 */
export function AgentRunInputConfirmation({ run }: { run: AgentRun }) {
  const slot = run.startGate?.inputSlot ?? null;
  // A closed slot holds nothing to read: the Run's own state says why.
  const slotClosed = slot?.status === "closed";
  const inputsQuery = useAgentRunStartGateInputsQuery(
    run.id,
    slot?.status ?? null,
    Boolean(slot) && !slotClosed,
  );
  const confirm = useConfirmAgentRunStartGateInputs();
  const [confirmFiles, setConfirmFiles] = useState(false);
  const [confirmProvider, setConfirmProvider] = useState(false);
  const inputs = inputsQuery.data;
  const fingerprint = inputs?.fingerprint ?? null;
  // Ticks belong to the files on screen. A new reading starts unticked.
  useEffect(() => {
    setConfirmFiles(false);
    setConfirmProvider(false);
  }, [fingerprint]);
  if (!slot) return null;

  const heading = <h3>以前置产出作为输入</h3>;
  const closedSection = (detail: string) => (
    <section aria-label="前置产出输入" className="agent-run-drawer-result">
      {heading}
      <p className="agent-run-drawer-empty" role="status">
        {detail}
      </p>
    </section>
  );
  if (slotClosed) return closedSection(agentRunLifecycle(run).detail);
  if (inputsQuery.isPending) {
    return (
      <section aria-label="前置产出输入" className="agent-run-drawer-result">
        {heading}
        <p className="agent-run-drawer-empty" role="status">
          正在读取前置产出…
        </p>
      </section>
    );
  }
  if (inputsQuery.isError || !inputs) {
    return (
      <section aria-label="前置产出输入" className="agent-run-drawer-result">
        {heading}
        <div className="agent-run-drawer-error" role="alert">
          <p>{readErrorText(inputsQuery.error)}</p>
          <button
            className="button button-secondary"
            disabled={inputsQuery.isFetching}
            onClick={() => void inputsQuery.refetch()}
            type="button"
          >
            重新读取
          </button>
        </div>
      </section>
    );
  }

  const providerLine = `${inputs.provider.name} · ${inputs.provider.model}`;
  const expected = (
    <ul aria-label="约定的产出文件" className="agent-run-input-files">
      {inputs.expectedFiles.map((file) => (
        <li key={file.name}>
          <strong>{file.name}</strong>
          <span>{file.mime}</span>
        </li>
      ))}
    </ul>
  );

  if (inputs.status === "awaiting_predecessor") {
    return (
      <section aria-label="前置产出输入" className="agent-run-drawer-result">
        {heading}
        <p className="agent-run-drawer-empty" role="status">
          前置任务
          {inputs.predecessor.taskTitle
            ? `「${inputs.predecessor.taskTitle}」`
            : ""}
          的产出还没有被验收，现在没有可以确认的文件。验收通过后，这里会列出每个文件供你核对；确认前这次执行不会启动。
        </p>
        {expected}
      </section>
    );
  }
  if (inputs.status === "closed") {
    // The Run on screen may not have caught up yet: explain with the reason
    // this read returned. A closed gate always leaves its Run cancelled.
    return closedSection(
      agentRunLifecycle({
        status: "cancelled",
        outputDeliveryStatus: "not_ready",
        startGate: {
          status: "closed",
          closeReason: inputs.closeReason,
          require: "accepted",
        },
      }).detail,
    );
  }
  if (inputs.status === "bound") {
    return (
      <section aria-label="前置产出输入" className="agent-run-drawer-result">
        {heading}
        <p className="agent-run-input-note" role="status">
          已确认下列 {inputs.files.length} 个文件（共{" "}
          {fileBytes(inputs.totalBytes)}）作为这次执行的输入，发送对象为{" "}
          {providerLine}。
        </p>
        <InputFileList inputs={inputs} />
      </section>
    );
  }

  const submit = () => {
    if (!inputs.fingerprint || !confirmFiles || !confirmProvider) return;
    confirm.mutate(
      {
        runId: run.id,
        taskId: run.taskId,
        fingerprint: inputs.fingerprint,
        provider: {
          version: inputs.provider.version,
          configVersion: inputs.provider.configVersion,
          kind: inputs.provider.kind,
        },
      },
      {
        // A refusal means what was ticked no longer describes the facts.
        onError: () => {
          setConfirmFiles(false);
          setConfirmProvider(false);
        },
      },
    );
  };
  return (
    <section aria-label="前置产出输入" className="agent-run-drawer-result">
      {heading}
      <p className="agent-run-input-note">
        前置任务
        {inputs.predecessor.taskTitle
          ? `「${inputs.predecessor.taskTitle}」`
          : ""}
        的产出已验收。下列 {inputs.files.length} 个文件（共{" "}
        {fileBytes(inputs.totalBytes)}
        ）将作为这次执行的输入；请逐份核对。确认前不会启动、读取或发送。
      </p>
      <InputFileList inputs={inputs} />
      {confirm.isError ? (
        <p className="agent-run-drawer-error" role="alert">
          {confirmErrorText(confirm.error)}
        </p>
      ) : null}
      <label className="agent-run-input-consent">
        <input
          checked={confirmFiles}
          disabled={confirm.isPending || inputsQuery.isFetching}
          onChange={(event) => setConfirmFiles(event.target.checked)}
          type="checkbox"
        />
        我已逐份核对上列文件的名称、大小、SHA-256
        与来源批次，确认它们是前置任务当前已验收的产出，同意作为这次执行的输入
      </label>
      <label className="agent-run-input-consent">
        <input
          checked={confirmProvider}
          disabled={confirm.isPending || inputsQuery.isFetching}
          onChange={(event) => setConfirmProvider(event.target.checked)}
          type="checkbox"
        />
        我同意把这些文件的正文发送给 {providerLine}（
        {inputs.provider.leavesDevice
          ? "远程 Provider：内容将离开本机，发送后无法撤回"
          : "本地 Provider：内容留在本机"}
        ）；确认后这次执行立即进入队列
      </label>
      <button
        className="button button-primary"
        disabled={
          !confirmFiles ||
          !confirmProvider ||
          confirm.isPending ||
          inputsQuery.isFetching
        }
        onClick={submit}
        type="button"
      >
        {confirm.isPending ? "正在确认…" : "确认这些文件并启动执行"}
      </button>
    </section>
  );
}
