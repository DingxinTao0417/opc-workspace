import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { Component, type ComponentType, type ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { DeferredPage } from "./DeferredPage";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

it("loads only the selected page and allows a failed import to be retried", async () => {
  // React logs the import failure that the page boundary catches.
  vi.spyOn(console, "error").mockImplementation(() => undefined);
  const load = vi
    .fn<() => Promise<ComponentType>>()
    .mockRejectedValueOnce(new Error("private module detail"))
    .mockResolvedValueOnce(() => <div>智能体页面</div>);
  render(<DeferredPage load={load} />);
  expect(screen.getByRole("status")).toHaveTextContent("正在加载页面");
  await screen.findByText("页面加载失败");
  expect(screen.queryByText("private module detail")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "重试" }));
  expect(await screen.findByText("智能体页面")).toBeVisible();
  expect(load).toHaveBeenCalledTimes(2);
});

it("does not render an old route when its import finishes late", async () => {
  let finishOld!: (page: ComponentType) => void;
  const oldLoad = () =>
    new Promise<ComponentType>((resolve) => {
      finishOld = resolve;
    });
  const newLoad = async () => () => <div>知识库页面</div>;
  const view = render(<DeferredPage load={oldLoad} />);
  await act(async () => {});
  view.rerender(<DeferredPage load={newLoad} />);
  expect(await screen.findByText("知识库页面")).toBeVisible();
  await act(async () => {
    finishOld(() => <div>旧智能体页面</div>);
  });
  expect(screen.queryByText("旧智能体页面")).toBeNull();
  expect(screen.getByText("知识库页面")).toBeVisible();
});

it("hides a previously loaded page immediately when the route changes", async () => {
  const first = async () => () => <div>原页面</div>;
  const pending = () => new Promise<ComponentType>(() => {});
  const view = render(<DeferredPage load={first} />);
  await screen.findByText("原页面");
  view.rerender(<DeferredPage load={pending} />);
  expect(screen.queryByText("原页面")).toBeNull();
  expect(screen.getByRole("status")).toBeVisible();
});

it("leaves a page that fails while rendering to the application boundary", async () => {
  vi.spyOn(console, "error").mockImplementation(() => undefined);
  class OuterBoundary extends Component<
    { children: ReactNode },
    { failed: boolean }
  > {
    state = { failed: false };
    static getDerivedStateFromError() {
      return { failed: true };
    }
    render() {
      return this.state.failed ? <p>应用级恢复页</p> : this.props.children;
    }
  }
  const load = async () => () => {
    throw new Error("render failure");
  };
  render(
    <OuterBoundary>
      <DeferredPage load={load} />
    </OuterBoundary>,
  );
  expect(await screen.findByText("应用级恢复页")).toBeVisible();
  expect(screen.queryByText("页面加载失败")).toBeNull();
});
