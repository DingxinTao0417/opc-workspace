import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Tag } from "../types/models";
import { TaskTagPicker } from "./TaskTagPicker";

const mocks = vi.hoisted(() => ({ create: vi.fn() }));

const tags: Tag[] = [
  {
    id: "tag-1",
    name: "交付",
    color: "#112233",
    version: 1,
    createdAt: "2026-08-27T00:00:00Z",
  },
  {
    id: "tag-2",
    name: "客户",
    color: "#445566",
    version: 1,
    createdAt: "2026-08-27T00:00:00Z",
  },
];

vi.mock("../api/hooks", () => ({
  useTagOptionsQuery: () => ({
    data: tags,
    isError: false,
    isPending: false,
    isSuccess: true,
    refetch: vi.fn(),
  }),
  useCreateTag: () => ({
    error: null,
    isPending: false,
    mutate: mocks.create,
  }),
}));

function Harness() {
  const [selected, setSelected] = useState(["tag-1"]);
  return (
    <>
      <TaskTagPicker onChange={setSelected} selectedIds={selected} />
      <button onClick={() => setSelected(["tag-1", "tag-2"])} type="button">
        模拟外部选择
      </button>
      <output data-testid="selected-tags">{selected.join(",")}</output>
    </>
  );
}

describe("TaskTagPicker", () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("merges a created tag into the latest selection instead of a stale draft", () => {
    render(<Harness />);
    fireEvent.change(screen.getByLabelText("新标签名称"), {
      target: { value: "复核" },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建并选中标签" }));

    fireEvent.click(screen.getByRole("button", { name: "模拟外部选择" }));
    const callbacks = mocks.create.mock.calls[0][1] as {
      onSuccess: (tag: Tag) => void;
    };
    act(() =>
      callbacks.onSuccess({
        id: "tag-3",
        name: "复核",
        color: "#6E7BF2",
        version: 1,
        createdAt: "2026-08-27T00:00:00Z",
      }),
    );

    expect(screen.getByTestId("selected-tags")).toHaveTextContent(
      "tag-1,tag-2,tag-3",
    );
  });
});
