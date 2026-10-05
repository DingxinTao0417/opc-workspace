import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
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

// Two sibling dialogs, as the Run drawer and its Task detail are: the detail
// is asked for later and must not cover the drawer.
function PlacementHarness({ named = true }: { named?: boolean }) {
  const [drawerOpen, setDrawerOpen] = useState(named);
  const [detailOpen, setDetailOpen] = useState(false);
  return (
    <>
      <button onClick={() => setDetailOpen(true)} type="button">
        稍后打开详情
      </button>
      <Modal
        onClose={() => setDrawerOpen(false)}
        open={drawerOpen}
        overlayName="drawer"
        title="抽屉"
      >
        <button type="button">抽屉操作</button>
      </Modal>
      <Modal
        beneathOverlay="drawer"
        onClose={() => setDetailOpen(false)}
        open={detailOpen}
        title="详情"
      >
        <button type="button">详情操作</button>
      </Modal>
    </>
  );
}

const overlayRoot = (dialog: HTMLElement) =>
  dialog.closest<HTMLElement>('[data-overlay-root="true"]')!;
const layer = (dialog: HTMLElement) => Number(overlayRoot(dialog).style.zIndex);

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
  cleanup();
  document.body.style.overflow = "";
  vi.restoreAllMocks();
});

describe("Modal stack", () => {
  it("supports a named close control and presentation classes for a side drawer", () => {
    const { unmount } = render(
      <Modal
        bodyClassName="drawer-body"
        closeLabel="收起执行过程"
        onClose={() => undefined}
        open
        panelClassName="drawer-panel"
        title="执行过程"
      >
        内容
      </Modal>,
    );

    expect(screen.getByRole("dialog", { name: "执行过程" })).toHaveClass(
      "drawer-panel",
    );
    expect(
      screen
        .getByRole("dialog", { name: "执行过程" })
        .querySelector(".modal-body"),
    ).toHaveClass("drawer-body");
    expect(screen.getByRole("button", { name: "收起执行过程" })).toBeVisible();
    unmount();
  });

  it("opens a dialog beneath the named one that is already open", async () => {
    render(<PlacementHarness />);
    const drawer = screen.getByRole("dialog", { name: "抽屉" });
    fireEvent.click(screen.getByRole("button", { name: "稍后打开详情" }));

    // The later dialog is in the document but neither topmost nor painted
    // above the drawer.
    const detail = screen.getByText("详情操作").closest("section")!;
    await waitFor(() => expect(overlayRoot(detail)).toHaveAttribute("inert"));
    expect(overlayRoot(detail)).toHaveAttribute("aria-hidden", "true");
    expect(detail).not.toHaveAttribute("aria-modal");
    expect(drawer).toHaveAttribute("aria-modal", "true");
    expect(overlayRoot(drawer)).not.toHaveAttribute("inert");
    expect(layer(drawer)).toBeGreaterThan(layer(detail));

    // Escape reaches the drawer only; the dialog beneath then takes over.
    fireEvent.keyDown(document, { key: "Escape" });
    await waitFor(() =>
      expect(screen.queryByRole("dialog", { name: "抽屉" })).toBeNull(),
    );
    const remaining = await screen.findByRole("dialog", { name: "详情" });
    await waitFor(() =>
      expect(remaining).toHaveAttribute("aria-modal", "true"),
    );
    expect(overlayRoot(remaining)).not.toHaveAttribute("inert");
    await waitFor(() => expect(remaining).toHaveFocus());
  });

  it("opens on top as usual when the named dialog is not open", async () => {
    render(<PlacementHarness named={false} />);
    fireEvent.click(screen.getByRole("button", { name: "稍后打开详情" }));
    const detail = await screen.findByRole("dialog", { name: "详情" });
    await waitFor(() => expect(detail).toHaveAttribute("aria-modal", "true"));
    expect(overlayRoot(detail)).not.toHaveAttribute("inert");
  });

  it("paints a dialog opened later above the one beneath it", async () => {
    render(<NestedModalHarness />);
    const parent = screen.getByRole("dialog", { name: "父弹窗" });
    fireEvent.click(screen.getByRole("button", { name: "打开子弹窗" }));
    const child = await screen.findByRole("dialog", { name: "子弹窗" });
    expect(layer(child)).toBeGreaterThan(layer(parent));
    expect(layer(parent)).toBeGreaterThanOrEqual(100);
  });

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
