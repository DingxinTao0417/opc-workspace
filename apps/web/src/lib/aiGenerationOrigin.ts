import type { AiGenerationOrigin } from "../types/models";
import { isWorkspaceIdentity } from "./focusReportLocation";

/** Missing is legacy/manual; malformed present metadata must never become manual. */
export function parseAiGenerationOrigin(
  value: unknown,
): AiGenerationOrigin | undefined {
  if (value === undefined) return undefined;
  const row = value as Partial<AiGenerationOrigin> | null;
  if (
    !row ||
    typeof row !== "object" ||
    Array.isArray(row) ||
    Object.keys(row).sort().join(",") !==
      "continuation_id,kind,max_turns,turn_index" ||
    row.kind !== "plan_continuation" ||
    !isWorkspaceIdentity(row.continuation_id ?? "") ||
    !Number.isSafeInteger(row.max_turns) ||
    row.max_turns! < 1 ||
    row.max_turns! > 8 ||
    !Number.isSafeInteger(row.turn_index) ||
    row.turn_index! < 1 ||
    row.turn_index! > row.max_turns!
  ) {
    throw Object.assign(new Error("自动续办来源响应无效"), {
      code: "INVALID_RESPONSE",
    });
  }
  return row as AiGenerationOrigin;
}
