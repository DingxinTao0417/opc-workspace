import { cleanup, render, screen } from "@testing-library/react";
import { useState } from "react";
import { afterEach, describe, expect, it } from "vitest";
import type { PageMeta } from "../types/models";
import { useSettledPage } from "./useSettledPage";

interface HarnessProps {
  isBlocked?: boolean;
  isFetching?: boolean;
  isPlaceholderData?: boolean;
  isSuccess?: boolean;
  meta?: PageMeta;
}

function Harness({
  isBlocked = false,
  isFetching = false,
  isPlaceholderData = false,
  isSuccess = true,
  meta,
}: HarnessProps) {
  const [page, setPage] = useState(3);
  useSettledPage({
    page,
    meta,
    isBlocked,
    isFetching,
    isPlaceholderData,
    isSuccess,
    setPage,
  });

  return <output aria-label="当前页">{page}</output>;
}

describe("useSettledPage", () => {
  afterEach(cleanup);

  it("clamps directly to the last valid page after the matching response settles", () => {
    render(<Harness meta={{ page: 3, pageSize: 10, total: 11 }} />);

    expect(screen.getByLabelText("当前页")).toHaveTextContent("2");
  });

  it("ignores loading, placeholder, unsuccessful, and mismatched responses", () => {
    const { rerender } = render(
      <Harness isFetching meta={{ page: 3, pageSize: 10, total: 0 }} />,
    );
    expect(screen.getByLabelText("当前页")).toHaveTextContent("3");

    rerender(
      <Harness isPlaceholderData meta={{ page: 3, pageSize: 10, total: 0 }} />,
    );
    expect(screen.getByLabelText("当前页")).toHaveTextContent("3");

    rerender(
      <Harness isSuccess={false} meta={{ page: 3, pageSize: 10, total: 0 }} />,
    );
    expect(screen.getByLabelText("当前页")).toHaveTextContent("3");

    rerender(<Harness isBlocked meta={{ page: 3, pageSize: 10, total: 0 }} />);
    expect(screen.getByLabelText("当前页")).toHaveTextContent("3");

    rerender(<Harness meta={{ page: 2, pageSize: 10, total: 0 }} />);
    expect(screen.getByLabelText("当前页")).toHaveTextContent("3");

    rerender(<Harness meta={{ page: 3, pageSize: 10, total: 0 }} />);
    expect(screen.getByLabelText("当前页")).toHaveTextContent("1");
  });
});
