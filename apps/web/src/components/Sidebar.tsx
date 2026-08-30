import {
  CalendarDays,
  CheckSquare2,
  CircleDollarSign,
  Clock3,
  FolderKanban,
  Focus,
  Inbox,
  Map,
  ReceiptText,
  Search,
  Settings2,
  Sun,
  Users,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { NavLink } from "react-router-dom";
import { useInboxStatsQuery, useSidebarWeekTasksQuery } from "../api/hooks";
import {
  localDateFromKey,
  localDateKey,
  useLocalCalendar,
} from "../lib/localCalendar";
import { useSettingsStore } from "../store/settings";
import { useUiStore } from "../store/ui";

interface NavItem {
  label: string;
  to: string;
  icon: LucideIcon;
  badge?: string;
}

const groups: { label: string; items: NavItem[] }[] = [
  {
    label: "工作区",
    items: [
      { label: "今日", to: "/today", icon: Sun },
      { label: "收件箱", to: "/inbox", icon: Inbox },
    ],
  },
  {
    label: "规划",
    items: [
      { label: "任务", to: "/tasks", icon: CheckSquare2 },
      { label: "项目", to: "/projects", icon: FolderKanban },
    ],
  },
  {
    label: "业务",
    items: [
      { label: "客户", to: "/clients", icon: Users },
      { label: "收入", to: "/income", icon: CircleDollarSign },
      { label: "发票", to: "/invoices", icon: ReceiptText },
    ],
  },
  {
    label: "执行",
    items: [{ label: "专注", to: "/focus", icon: Focus }],
  },
  {
    label: "规划与内容",
    items: [
      { label: "路线图", to: "/roadmap", icon: Map },
      {
        label: "内容日历",
        to: "/content-calendar",
        icon: CalendarDays,
      },
    ],
  },
];

function currentLocalWeekRange(dateKey: string): {
  plannedFrom: string;
  plannedTo: string;
} {
  const now = localDateFromKey(dateKey);
  const daysSinceMonday = now.getDay() === 0 ? 6 : now.getDay() - 1;
  const monday = new Date(
    now.getFullYear(),
    now.getMonth(),
    now.getDate() - daysSinceMonday,
  );
  const sunday = new Date(
    monday.getFullYear(),
    monday.getMonth(),
    monday.getDate() + 6,
  );
  return {
    plannedFrom: localDateKey(monday),
    plannedTo: localDateKey(sunday),
  };
}

export function Sidebar() {
  const { dateKey } = useLocalCalendar();
  const weeklyExecutionQuery = useSidebarWeekTasksQuery(
    currentLocalWeekRange(dateKey),
  );
  const inboxStatsQuery = useInboxStatsQuery();
  const displayName = useSettingsStore(
    (state) => state.preview?.profile.displayName ?? state.displayName,
  );
  const avatarDataUrl = useSettingsStore(
    (state) => state.preview?.profile.avatarDataUrl ?? state.avatarDataUrl,
  );
  const setCommandPaletteOpen = useUiStore(
    (state) => state.setCommandPaletteOpen,
  );
  const setSettingsOpen = useUiStore((state) => state.setSettingsOpen);
  const inboxBadge = inboxStatsQuery.data?.pending
    ? inboxStatsQuery.data.pending > 99
      ? "99+"
      : String(inboxStatsQuery.data.pending)
    : undefined;

  return (
    <aside className="left-sidebar" aria-label="主导航">
      <div className="brand-block">
        <div className="brand-mark">
          {avatarDataUrl ? (
            <img alt={`${displayName}的头像`} src={avatarDataUrl} />
          ) : (
            Array.from(displayName.trim())[0]?.toUpperCase() || "O"
          )}
        </div>
        <div className="sidebar-copy min-w-0">
          <div className="brand-name" title={displayName}>
            {displayName}
          </div>
          <span className="local-pill">v0.1.0</span>
        </div>
      </div>

      <button
        className="sidebar-search"
        onClick={() => setCommandPaletteOpen(true)}
        type="button"
      >
        <Search size={15} />
        <span className="sidebar-copy flex-1 text-left">搜索或跳转…</span>
        <kbd className="sidebar-copy">⌘K</kbd>
      </button>

      <nav className="nav-groups">
        {groups.map((group) => (
          <div className="nav-group" key={group.label}>
            <div className="nav-label sidebar-copy">{group.label}</div>
            {group.items.map(({ label, to, icon: Icon, badge }) => (
              <NavLink
                className={({ isActive }) =>
                  `nav-item${isActive ? " nav-item-active" : ""}`
                }
                key={to}
                to={to}
              >
                <Icon className="nav-icon" size={17} />
                <span className="sidebar-copy nav-text">{label}</span>
                {(to === "/inbox" ? inboxBadge : badge) ? (
                  <span
                    aria-label={
                      to === "/inbox"
                        ? `${inboxStatsQuery.data?.pending ?? 0} 项待处理`
                        : undefined
                    }
                    className="sidebar-copy nav-badge"
                  >
                    {to === "/inbox" ? inboxBadge : badge}
                  </span>
                ) : null}
              </NavLink>
            ))}
          </div>
        ))}
      </nav>

      <div className="weekly-card sidebar-copy">
        <div className="flex items-center justify-between">
          <span className="weekly-title">本周执行</span>
          <Clock3 size={14} />
        </div>
        {weeklyExecutionQuery.isPending ? (
          <div className="weekly-copy" role="status">
            正在加载本周任务…
          </div>
        ) : weeklyExecutionQuery.isError ? (
          <div className="weekly-copy" role="alert">
            无法读取本周任务 ·{" "}
            <button
              className="form-inline-action"
              onClick={() => void weeklyExecutionQuery.refetch()}
              type="button"
            >
              重试
            </button>
          </div>
        ) : weeklyExecutionQuery.data?.taskCount ? (
          <>
            <div
              className="progress-track"
              aria-label={`本周完成度 ${weeklyExecutionQuery.data.completedPercent}%`}
            >
              <span
                style={{
                  width: `${weeklyExecutionQuery.data.completedPercent}%`,
                }}
              />
            </div>
            <div className="weekly-copy">
              {weeklyExecutionQuery.data.completedCount} /{" "}
              {weeklyExecutionQuery.data.taskCount} 项
            </div>
          </>
        ) : (
          <div className="weekly-copy" role="status">
            本周暂无已排期任务
          </div>
        )}
      </div>

      <button
        aria-label="打开设置"
        aria-haspopup="dialog"
        className="nav-item sidebar-settings"
        onClick={() => setSettingsOpen(true)}
        type="button"
      >
        <Settings2 className="nav-icon" size={17} />
        <span className="sidebar-copy nav-text">设置</span>
      </button>
    </aside>
  );
}
