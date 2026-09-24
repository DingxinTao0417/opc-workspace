import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { ComponentType } from "react";
import { DeferredPage } from "./DeferredPage";

afterEach(cleanup);

it("loads only the selected page and allows a failed import to be retried", async () => {
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
