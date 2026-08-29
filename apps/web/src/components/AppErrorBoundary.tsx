import { AlertTriangle, RefreshCw, Stethoscope } from "lucide-react";
import { Component, type ErrorInfo, type ReactNode } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { useUiStore } from "../store/ui";

interface AppErrorBoundaryProps {
  children: ReactNode;
  resetKey: string;
  onGoToday: () => void;
  onOpenDiagnostics: () => void;
}

interface AppErrorBoundaryState {
  failed: boolean;
}

export class AppErrorBoundary extends Component<
  AppErrorBoundaryProps,
  AppErrorBoundaryState
> {
  state: AppErrorBoundaryState = { failed: false };

  static getDerivedStateFromError(): AppErrorBoundaryState {
    return { failed: true };
  }

  componentDidCatch(_error: Error, _info: ErrorInfo): void {
    // React captures the render failure. The v0.1 fallback intentionally does
    // not persist or display raw errors because they may contain business data.
  }

  componentDidUpdate(previous: AppErrorBoundaryProps): void {
    if (this.state.failed && previous.resetKey !== this.props.resetKey) {
      this.setState({ failed: false });
    }
  }

  private retry = () => this.setState({ failed: false });

  render() {
    if (!this.state.failed) return this.props.children;

    return (
      <main className="app-error-boundary" role="alert">
        <div className="app-error-card">
          <span aria-hidden="true" className="app-error-icon">
            <AlertTriangle size={22} />
          </span>
          <p className="eyebrow">本地界面恢复</p>
          <h1>当前页面未能正常显示</h1>
          <p>
            本地数据没有因此被自动修改。你可以重新渲染当前页面，或先打开运行诊断核对服务与版本状态。
          </p>
          <div className="app-error-actions">
            <button
              autoFocus
              className="button button-primary"
              onClick={this.retry}
              type="button"
            >
              <RefreshCw size={14} />
              重新渲染
            </button>
            <button
              className="button button-secondary"
              onClick={this.props.onOpenDiagnostics}
              type="button"
            >
              <Stethoscope size={14} />
              打开运行诊断
            </button>
            <button
              className="button button-quiet"
              onClick={this.props.onGoToday}
              type="button"
            >
              返回今日
            </button>
          </div>
          <small>
            为避免泄露任务、客户或文件内容，这里不会显示或复制原始错误详情。
          </small>
        </div>
      </main>
    );
  }
}

export function RoutedAppErrorBoundary({ children }: { children: ReactNode }) {
  const location = useLocation();
  const navigate = useNavigate();
  const setSettingsOpen = useUiStore((state) => state.setSettingsOpen);

  return (
    <AppErrorBoundary
      onGoToday={() => navigate("/today")}
      onOpenDiagnostics={() => setSettingsOpen(true, "diagnostics")}
      resetKey={`${location.pathname}${location.search}${location.hash}`}
    >
      {children}
    </AppErrorBoundary>
  );
}
