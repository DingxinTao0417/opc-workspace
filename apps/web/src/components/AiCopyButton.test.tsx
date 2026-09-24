import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AiCopyButton } from "./AiCopyButton";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("AiCopyButton", () => {
  it("copies the supplied display text and reports success", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("navigator", { clipboard: { writeText } });
    render(<AiCopyButton text="已整理任务建议，尚未创建。" />);
    fireEvent.click(screen.getByRole("button", { name: "复制回复" }));
    await screen.findByRole("button", { name: "已复制回复" });
    expect(writeText).toHaveBeenCalledWith("已整理任务建议，尚未创建。");
  });

  it("reports denied clipboard access without claiming success", async () => {
    vi.stubGlobal("navigator", {
      clipboard: { writeText: vi.fn().mockRejectedValue(new Error("denied")) },
    });
    render(<AiCopyButton text="回复" />);
    fireEvent.click(screen.getByRole("button", { name: "复制回复" }));
    await screen.findByText("复制失败，请选中文字后复制");
    expect(screen.queryByRole("button", { name: "已复制回复" })).toBeNull();
  });

  it("clears the success state when the displayed reply changes", async () => {
    vi.stubGlobal("navigator", {
      clipboard: { writeText: vi.fn().mockResolvedValue(undefined) },
    });
    const view = render(<AiCopyButton text="旧回复" />);
    fireEvent.click(screen.getByRole("button", { name: "复制回复" }));
    await screen.findByRole("button", { name: "已复制回复" });
    view.rerender(<AiCopyButton text="新回复" />);
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "复制回复" }),
      ).toBeInTheDocument(),
    );
  });
});
