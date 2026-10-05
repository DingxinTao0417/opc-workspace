import {
  Component,
  Suspense,
  lazy,
  type ComponentType,
  type ReactNode,
} from "react";
import { ErrorState } from "./feedback";

// A failed import, as opposed to an overlay that loaded and then threw.
class OverlayLoadError extends Error {}

function deferred<Props extends object>(
  load: () => Promise<ComponentType<Props>>,
) {
  return lazy(() =>
    load().then(
      (component) => ({ default: component }),
      () => {
        throw new OverlayLoadError();
      },
    ),
  );
}

const CommandPalette = deferred(() =>
  import("./CommandPalette").then((module) => module.CommandPalette),
);
const NewTaskModal = deferred(() =>
  import("./NewTaskModal").then((module) => module.NewTaskModal),
);
const TaskDetailModal = deferred(() =>
  import("./TaskDetailModal").then((module) => module.TaskDetailModal),
);
const AgentRunDrawer = deferred(() =>
  import("./AgentRunDrawer").then((module) => module.AgentRunDrawer),
);
const SettingsModal = deferred(() =>
  import("./SettingsModal").then((module) => module.SettingsModal),
);

// These overlays sit outside the routed error boundary on purpose: its recovery
// page opens the settings diagnostics. A chunk that fails to load therefore
// must not take the shell down; it leaves a visible notice with a reload
// action. Anything an overlay throws after loading propagates as before.
class OverlayLoadBoundary extends Component<
  { children: ReactNode },
  { error: unknown }
> {
  state: { error: unknown } = { error: null };

  static getDerivedStateFromError(error: unknown) {
    return { error };
  }

  render() {
    const { error } = this.state;
    if (error === null) return this.props.children;
    if (!(error instanceof OverlayLoadError)) throw error;
    return (
      <div className="deferred-overlay-error" role="alert">
        <ErrorState
          compact
          title="部分界面未能加载"
          message="设置、任务详情、命令面板等浮层暂不可用，请重新加载应用。"
          onRetry={() => window.location.reload()}
        />
      </div>
    );
  }
}

// The global overlays are closed on first paint, so their code loads right
// after it instead of inside the entry chunk. Once loaded they stay mounted
// exactly as before, opened and closed through the UI store.
export function DeferredOverlays() {
  return (
    <OverlayLoadBoundary>
      <Suspense fallback={null}>
        <CommandPalette />
        <NewTaskModal />
        <TaskDetailModal />
        <AgentRunDrawer />
        <SettingsModal />
      </Suspense>
    </OverlayLoadBoundary>
  );
}
