import type { AiPlanContinuation } from "../api/aiPlanContinuation";
export const continuationSessionId = "018f0000-0000-7000-8000-000000000081";
export function continuationFixture(
  overrides: Partial<AiPlanContinuation> = {},
): AiPlanContinuation {
  return {
    id: "018f0000-0000-7000-8000-000000000082",
    session_id: continuationSessionId,
    version: 1,
    status: "waiting",
    reason: "pending_approval",
    initial_plan_version: 2,
    current_plan_version: 2,
    provider: {
      id: "018f0000-0000-7000-8000-000000000083",
      name: "测试Provider",
      kind: "remote",
      protocol: "openai_chat",
      model: "test",
      version: 2,
      config_version: 3,
    },
    workspace: { provider_version: 2, scopes: ["work", "outputs", "actions"] },
    max_turns: 3,
    turns_started: 0,
    current_generation_id: null,
    last_generation_id: null,
    created_at: "2026-09-21T12:00:00Z",
    updated_at: "2026-09-21T12:00:00Z",
    expires_at: "2026-09-21T12:30:00Z",
    ...overrides,
  };
}
