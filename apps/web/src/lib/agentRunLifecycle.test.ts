import { describe, expect, it } from "vitest";
import { agentRunLifecycle } from "./agentRunLifecycle";

const submission = "018f0000-0000-7000-8000-000000000508";

describe("agentRunLifecycle", () => {
  it.each([
    ["queued", "not_ready", "pending", "排队中", false, false],
    ["running", "not_ready", "running", "执行中", false, false],
    ["running", "pending", "recovery_required", "结果待登记", true, false],
    ["succeeded", "retained", "succeeded", "结果已保留 · 未提交", true, true],
    ["failed", "not_ready", "failed", "执行失败", true, true],
    ["interrupted", "not_ready", "failed", "执行中断", true, true],
    ["cancelled", "not_ready", "cancelled", "已取消", false, true],
  ] as const)(
    "maps the legal pair %s + %s to %s",
    (status, delivery, phase, label, attention, terminal) => {
      expect(
        agentRunLifecycle({ status, outputDeliveryStatus: delivery }),
      ).toMatchObject({ phase, label, attention, terminal });
    },
  );

  it("requires a Submission identity before reporting a registered output", () => {
    expect(
      agentRunLifecycle({
        status: "succeeded",
        outputDeliveryStatus: "submitted",
        submissionId: null,
      }).phase,
    ).toBe("unknown");
    expect(
      agentRunLifecycle({
        status: "succeeded",
        outputDeliveryStatus: "submitted",
        submissionId: submission,
      }),
    ).toMatchObject({ phase: "submitted", label: "已提交为任务产出" });
  });

  it("only reports confirmed after an accepted human review", () => {
    const base = {
      status: "succeeded",
      outputDeliveryStatus: "submitted",
      submissionId: submission,
    } as const;
    expect(
      agentRunLifecycle({ ...base, submissionStatus: "pending_review" }),
    ).toMatchObject({ phase: "submitted", label: "已提交 · 待人工验收" });
    expect(
      agentRunLifecycle({ ...base, submissionStatus: "accepted" }),
    ).toMatchObject({ phase: "confirmed", label: "产出已验收" });
    expect(
      agentRunLifecycle({ ...base, submissionStatus: "changes_requested" }),
    ).toMatchObject({ phase: "submitted", attention: true });
    expect(
      agentRunLifecycle({ ...base, submissionStatus: "withdrawn" }),
    ).toMatchObject({ phase: "submitted", attention: false });
  });

  it("distinguishes a gated wait from an ordinary queue", () => {
    expect(
      agentRunLifecycle({
        status: "queued",
        outputDeliveryStatus: "not_ready",
        startGate: { status: "waiting", require: "accepted" },
      }),
    ).toMatchObject({ phase: "pending", label: "等待前置执行" });
    expect(
      agentRunLifecycle({
        status: "queued",
        outputDeliveryStatus: "not_ready",
        startGate: { status: "released", require: "submitted" },
      }).label,
    ).toBe("排队中");
  });

  it.each([
    ["predecessor_failed", "未启动 · 前置条件未满足"],
    ["predecessor_not_accepted", "未启动 · 前置条件未满足"],
    ["expired", "未启动 · 等待超时"],
  ] as const)(
    "explains a Run that never started because of %s",
    (closeReason, label) => {
      const result = agentRunLifecycle({
        status: "cancelled",
        outputDeliveryStatus: "not_ready",
        startGate: { status: "closed", closeReason, require: "accepted" },
      });
      expect(result).toMatchObject({
        phase: "cancelled",
        label,
        attention: true,
      });
      expect(result.detail).toContain("没有启动");
    },
  );

  it.each([
    ["succeeded", "not_ready"],
    ["succeeded", "pending"],
    ["queued", "pending"],
    ["failed", "submitted"],
    ["cancelled", "retained"],
    ["running", "submitted"],
    ["paused", "not_ready"],
    [undefined, undefined],
  ])(
    "never guesses success or failure for %s + %s",
    (status, outputDeliveryStatus) => {
      const result = agentRunLifecycle({ status, outputDeliveryStatus });
      expect(result.phase).toBe("unknown");
      expect(result.label).toBe("状态无法确认");
      expect(result.detail).not.toMatch(/成功|已完成|失败/);
    },
  );
});
