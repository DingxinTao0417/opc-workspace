import { describe, expect, it } from "vitest";
import {
  parseAiActionProposal,
  parseNativeAgentRunStartPreview,
} from "./aiWorkspaceActions";
import { projectFileProposal } from "../test/agentProjectFileFixture";
describe("v6 cross-task file approval", () => {
  it.each([false, true])(
    "accepts complete exact source and protocol anthropic=%s",
    (anthropic) => {
      const p = projectFileProposal(anthropic);
      expect(parseAiActionProposal(p)).toEqual(p);
      expect(
        parseNativeAgentRunStartPreview(p.preview.agent_run_start),
      ).toEqual(p.preview.agent_run_start);
    },
  );
  it.each([
    "missingSource",
    "sameTask",
    "otherProject",
    "oldVersion",
    "missingNewFile",
    "tokens",
    "unacceptedShape",
    "missingHash",
    "sourceAlias",
    "unknownSourceField",
  ])("fails closed: %s", (kind) => {
    const p = projectFileProposal();
    const start = p.preview.agent_run_start!;
    const file = start.input_files![0];
    if (kind === "missingSource") delete file.source_task;
    if (kind === "sameTask") file.source_task!.task_id = start.task.id;
    if (kind === "otherProject") file.source_task!.project_id = start.task.id;
    if (kind === "oldVersion") start.execution_contract_version = 5;
    if (kind === "missingNewFile") {
      file.source_kind = "task_artifact";
      delete file.source_task;
    }
    if (kind === "tokens") start.runtime_limits.max_output_tokens = 8192;
    if (kind === "unacceptedShape") file.source_task!.submission_sequence = 0;
    if (kind === "missingHash") file.sha256 = "";
    if (kind === "sourceAlias") {
      Object.assign(file, { sourceTask: file.source_task });
      delete file.source_task;
    }
    if (kind === "unknownSourceField")
      Object.assign(file.source_task!, { review_reason: "private" });
    expect(() => parseAiActionProposal(p)).toThrow();
  });
});
