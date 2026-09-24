import { describe, expect, it } from "vitest";
import { agentRunFailureReason } from "./agentRunFailureSource";

describe("fixed Agent failure explanations", () => {
  it.each([
    "AGENT_EXECUTOR_UNAVAILABLE",
    "AGENT_EXECUTOR_FAILED",
    "AGENT_PROTOCOL_INVALID",
    "AGENT_RESULT_TOO_LARGE",
    "AGENT_RUN_TIMED_OUT",
    "AGENT_EXECUTOR_INVALID_INPUT",
    "AGENT_MODEL_ENDPOINT_INVALID",
    "AGENT_MODEL_UNAVAILABLE",
    "AGENT_MODEL_FAILED",
    "AGENT_MODEL_TRUNCATED",
    "AGENT_MODEL_FILTERED",
    "AGENT_MODEL_RESPONSE_INVALID",
    "AGENT_RESULT_EMPTY",
    "AGENT_RESULT_INVALID",
    "AGENT_RUN_IDENTITY_CHANGED",
    "AGENT_MODEL_KEY_UNAVAILABLE",
    "AGENT_RUN_FAILED",
  ])("explains %s without claiming delivery or suggesting bypass", (code) => {
    const message = agentRunFailureReason(code);
    expect(message).toContain("未登记产出");
    expect(message).not.toMatch(/自动重试|绕过|已提交|已验收|https?:|C:\\/);
  });
  it.each([
    "",
    "MODEL_UNAVAILABLE",
    "AGENT_MODEL_RESPONSE_INVALID private output",
    "https://secret.example/token",
    "UNKNOWN_NEW_CODE",
  ])("does not interpret arbitrary message %s as a safe diagnosis", (code) => {
    expect(agentRunFailureReason(code)).toBeNull();
  });
});
