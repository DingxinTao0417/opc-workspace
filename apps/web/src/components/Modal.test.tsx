import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Modal } from "./Modal";

function NestedModalHarness({ childDismissible = true }) {
  const [parentOpen, setParentOpen] = useState(true);
  const [childOpen, setChildOpen] = useState(false);
  return (
    <>
      <button type="button">页面按钮</button>
      <Modal
        onClose={() => setParentOpen(false)}
        open={parentOpen}
        title="父弹窗"
      >
        <button onClick={() => setChildOpen(true)} type="button">
          打开子弹窗
        </button>
        {childOpen ? (
          <Modal
            dismissible={childDismissible}
            onClose={() => setChildOpen(false)}
            open
            title="子弹窗"
          >
            <button onClick={() => setChildOpen(false)} type="button">
              完成子操作
            </button>
          </Modal>
        ) : null}
      </Modal>
    </>
  );
}

function SimultaneousCloseHarness() {
  const [parentOpen, setParentOpen] = useState(false);
  const [childOpen, setChildOpen] = useState(false);
  return (
    <>
      <button onClick={() => setParentOpen(true)} type="button">
        打开父弹窗
      </button>
      {parentOpen ? (
        <Modal onClose={() => setParentOpen(false)} open title="批量关闭父弹窗">
          <button onClick={() => setChildOpen(true)} type="button">
            打开批量关闭子弹窗
          </button>
          {childOpen ? (
            <Modal
              onClose={() => setChildOpen(false)}
              open
              title="批量关闭子弹窗"
            >
              <button
                onClick={() => {
                  setChildOpen(false);
                  setParentOpen(false);
                }}
                type="button"
              >
                关闭全部弹窗
              </button>
            </Modal>
          ) : null}
        </Modal>
      ) : null}
    </>
  );
}

function RemovedChildTriggerHarness() {
  const [childOpen, setChildOpen] = useState(false);
  return (
    <Modal onClose={() => undefined} open title="保留父弹窗">
      {!childOpen ? (
        <button onClick={() => setChildOpen(true)} type="button">
          打开并移除触发器
        </button>
      ) : null}
      {childOpen ? (
        <Modal
          onClose={() => setChildOpen(false)}
          open
          title="移除触发器子弹窗"
        >
          <button onClick={() => setChildOpen(false)} type="button">
            关闭无触发器子弹窗
          </button>
        </Modal>
      ) : null}
    </Modal>
  );
}

afterEach(() => {
  document.body.style.overflow = "";
  vi.restoreAllMocks();
});

describe("Modal stack", () => {
  it("lets only the topmost dialog handle Escape and restores focus and body lock", async () => {
    document.body.style.overflow = "auto";
    render(<NestedModalHarness />);
    const trigger = screen.getByRole("button", { name: "打开子弹窗" });
    trigger.focus();
    fireEvent.click(trigger);

    await waitFor(() =>
      expect(screen.getAllByRole("dialog", { hidden: true })).toHaveLength(2),
    );
    const parentDialog = screen.getByRole("dialog", {
      name: "父弹窗",
      hidden: true,
    });
    const childDialog = screen.getByRole("dialog", { name: "子弹窗" });
    expect(parentDialog).not.toHaveAttribute("aria-modal");
    expect(parentDialog.closest(".modal-root")).toHaveAttribute(
      "aria-hidden",
      "true",
    );
    expect(childDialog).toHaveAttribute("aria-modal", "true");
    expect(document.body.style.overflow).toBe("hidden");
    await waitFor(() =>
      expect(childDialog.contains(document.activeElement)).toBe(true),
    );

    fireEvent.keyDown(document, { key: "Escape" });
    await waitFor(() =>
      expect(screen.queryByRole("dialog", { name: "子弹窗" })).toBeNull(),
    );
    expect(screen.getByRole("dialog", { name: "父弹窗" })).toHaveAttribute(
      "aria-modal",
      "true",
    );
    expect(trigger).toHaveFocus();
    expect(document.body.style.overflow).toBe("hidden");

    fireEvent.keyDown(document, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(document.body.style.overflow).toBe("auto");
  });

  it("does not let Escape pass through a non-dismissible topmost dialog", async () => {
    render(<NestedModalHarness childDismissible={false} />);
    fireEvent.click(screen.getByRole("button", { name: "打开子弹窗" }));
    await waitFor(() =>
      expect(screen.getAllByRole("dialog", { hidden: true })).toHaveLength(2),
    );

    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.getByRole("dialog", { name: "子弹窗" })).toBeVisible();
    expect(
      screen.getByRole("dialog", { name: "父弹窗", hidden: true }),
    ).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "完成子操作" }));
    await waitFor(() =>
      expect(screen.queryByRole("dialog", { name: "子弹窗" })).toBeNull(),
    );
    fireEvent.keyDown(document, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("restores the page focus when a nested dialog tree closes in one commit", async () => {
    document.body.style.overflow = "scroll";
    render(<SimultaneousCloseHarness />);
    const pageOpener = screen.getByRole("button", { name: "打开父弹窗" });
    pageOpener.focus();
    fireEvent.click(pageOpener);
    const childOpener = await screen.findByRole("button", {
      name: "打开批量关闭子弹窗",
    });
    childOpener.focus();
    fireEvent.click(childOpener);
    const closeAll = await screen.findByRole("button", {
      name: "关闭全部弹窗",
    });
    closeAll.focus();

    fireEvent.click(closeAll);

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    await waitFor(() => expect(pageOpener).toHaveFocus());
    expect(document.body.style.overflow).toBe("scroll");
  });

  it("falls back to the remaining parent dialog when the child trigger was removed", async () => {
    render(<RemovedChildTriggerHarness />);
    const childOpener = screen.getByRole("button", {
      name: "打开并移除触发器",
    });
    childOpener.focus();
    fireEvent.click(childOpener);
    const closeChild = await screen.findByRole("button", {
      name: "关闭无触发器子弹窗",
    });
    closeChild.focus();

    fireEvent.click(closeChild);

    await waitFor(() =>
      expect(
        screen.queryByRole("dialog", { name: "移除触发器子弹窗" }),
      ).toBeNull(),
    );
    const parentDialog = screen.getByRole("dialog", { name: "保留父弹窗" });
    await waitFor(() => expect(parentDialog).toHaveFocus());
    expect(document.body.style.overflow).toBe("hidden");
  });
});
