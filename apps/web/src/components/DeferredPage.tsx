import {
  Component,
  Suspense,
  lazy,
  useMemo,
  useState,
  type ComponentType,
  type ReactNode,
} from "react";
import { ErrorState, LoadingState } from "./feedback";

type PageLoader = () => Promise<ComponentType>;

// A failed import, as opposed to a page that loaded and then threw while
// rendering. The message is never shown: it may carry a module path.
class PageLoadError extends Error {}

class PageLoadBoundary extends Component<
  { children: ReactNode; onRetry: () => void },
  { error: unknown }
> {
  state: { error: unknown } = { error: null };

  static getDerivedStateFromError(error: unknown) {
    return { error };
  }

  render() {
    const { error } = this.state;
    if (error === null) return this.props.children;
    // Render failures belong to the application error boundary.
    if (!(error instanceof PageLoadError)) throw error;
    return (
      <ErrorState
        title="页面加载失败"
        message="请重试加载，或刷新应用。"
        onRetry={this.props.onRetry}
      />
    );
  }
}

const loaderIds = new WeakMap<PageLoader, number>();
let nextLoaderId = 0;
function loaderId(load: PageLoader) {
  let id = loaderIds.get(load);
  if (id === undefined) {
    id = nextLoaderId++;
    loaderIds.set(load, id);
  }
  return id;
}

// Keep the shell and global generation controls mounted while a page loads.
// The boundary and Suspense are keyed by route loader and attempt: a new route
// hides the previous page at once, a late import from a route the user has left
// cannot replace the new page, and a retry starts from a fresh lazy component
// (React.lazy keeps a rejected import forever).
export function DeferredPage({ load }: { load: PageLoader }) {
  const [attempt, setAttempt] = useState(0);
  const Page = useMemo(
    () =>
      lazy(() =>
        load().then(
          (page) => ({ default: page }),
          () => {
            throw new PageLoadError();
          },
        ),
      ),
    // A retry must build a new lazy component for the same loader.
    // eslint-disable-next-line react-hooks/exhaustive-deps -- attempt is a retry key, not a value the factory reads
    [load, attempt],
  );
  return (
    <PageLoadBoundary
      key={`${loaderId(load)}:${attempt}`}
      onRetry={() => setAttempt((value) => value + 1)}
    >
      <Suspense fallback={<LoadingState label="正在加载页面…" />}>
        <Page />
      </Suspense>
    </PageLoadBoundary>
  );
}
