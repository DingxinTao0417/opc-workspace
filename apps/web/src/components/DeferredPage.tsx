import { useEffect, useState, type ComponentType } from "react";
import { ErrorState, LoadingState } from "./feedback";

// Keep the shell and global generation controls mounted while a page loads.
// A late import from a route the user has left must not replace the new page.
export function DeferredPage({ load }: { load: () => Promise<ComponentType> }) {
  const [attempt, setAttempt] = useState(0);
  const [result, setResult] = useState<{
    load: typeof load;
    attempt: number;
    page?: ComponentType;
    failed?: boolean;
  }>();
  useEffect(() => {
    let active = true;
    void Promise.resolve()
      .then(load)
      .then(
        (page) => {
          if (active) setResult({ load, attempt, page });
        },
        () => {
          if (active) setResult({ load, attempt, failed: true });
        },
      );
    return () => {
      active = false;
    };
  }, [load, attempt]);
  if (result?.load !== load || result.attempt !== attempt)
    return <LoadingState label="正在加载页面…" />;
  if (result.failed)
    return (
      <ErrorState
        title="页面加载失败"
        message="请重试加载，或刷新应用。"
        onRetry={() => setAttempt((value) => value + 1)}
      />
    );
  const Page = result.page;
  return Page ? <Page /> : <LoadingState label="正在加载页面…" />;
}
