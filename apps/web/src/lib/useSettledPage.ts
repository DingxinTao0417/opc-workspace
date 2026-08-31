import { useEffect, type Dispatch, type SetStateAction } from "react";
import type { PageMeta } from "../types/models";

interface UseSettledPageOptions {
  page: number;
  meta: PageMeta | undefined;
  isFetching: boolean;
  isPlaceholderData: boolean;
  isSuccess: boolean;
  setPage: Dispatch<SetStateAction<number>>;
}

export function useSettledPage({
  page,
  meta,
  isFetching,
  isPlaceholderData,
  isSuccess,
  setPage,
}: UseSettledPageOptions): void {
  useEffect(() => {
    if (
      !isSuccess ||
      isFetching ||
      isPlaceholderData ||
      !meta ||
      !Number.isSafeInteger(page) ||
      page < 1 ||
      !Number.isSafeInteger(meta.page) ||
      meta.page !== page ||
      !Number.isSafeInteger(meta.pageSize) ||
      meta.pageSize < 1 ||
      !Number.isSafeInteger(meta.total) ||
      meta.total < 0
    ) {
      return;
    }

    const lastPage = Math.max(1, Math.ceil(meta.total / meta.pageSize));
    if (page <= lastPage) return;

    setPage((currentPage) =>
      currentPage === page && currentPage > lastPage ? lastPage : currentPage,
    );
  }, [isFetching, isPlaceholderData, isSuccess, meta, page, setPage]);
}
