import {
  BookOpenText,
  CalendarDays,
  CheckSquare2,
  CircleDollarSign,
  FolderKanban,
  Focus,
  Inbox,
  Map,
  PanelLeftClose,
  PanelLeftOpen,
  ReceiptText,
  Search,
  Settings2,
  Sparkles,
  Sun,
  Users,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { NavLink } from "react-router-dom";
import {
  useInboxStatsQuery,
  useIncomeStatsQuery,
  useRecentClientActivitiesQuery,
  useRoadmapMilestonesQuery,
} from "../api/hooks";
import { FocusMiniCard } from "./FocusMiniCard";
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
    items: [
      { label: "专注", to: "/focus", icon: Focus },
      { label: "AI 助手", to: "/ai", icon: Sparkles },
      { label: "知识库", to: "/knowledge", icon: BookOpenText },
    ],
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

function currentMonthBounds(dateKey: string) {
  const date = localDateFromKey(dateKey);
  const firstDay = new Date(date.getFullYear(), date.getMonth(), 1);
  const lastDay = new Date(date.getFullYear(), date.getMonth() + 1, 0);
  return { dateFrom: localDateKey(firstDay), dateTo: localDateKey(lastDay) };
}

function dueSoonDateKey(dateKey: string, days: number) {
  const date = localDateFromKey(dateKey);
  return localDateKey(
    new Date(date.getFullYear(), date.getMonth(), date.getDate() + days),
  );
}

function compactCny(amountMinor: number) {
  const yuan = amountMinor / 100;
  if (yuan >= 10000) return `¥${(yuan / 10000).toFixed(1)}万`;
  if (yuan >= 1000) return `¥${(yuan / 1000).toFixed(1)}k`;
  return `¥${Math.round(yuan)}`;
}

function formatCny(amountMinor: number) {
  return new Intl.NumberFormat("zh-CN", {
    style: "currency",
    currency: "CNY",
    minimumFractionDigits: 2,
  }).format(amountMinor / 100);
}

export function Sidebar() {
  const { dateKey } = useLocalCalendar();
  const inboxStatsQuery = useInboxStatsQuery();
  const plannedMilestonesQuery = useRoadmapMilestonesQuery({
    page: 1,
    pageSize: 100,
    sort: "target_date",
    status: "planned",
  });
  const activeMilestonesQuery = useRoadmapMilestonesQuery({
    page: 1,
    pageSize: 100,
    sort: "target_date",
    status: "active",
  });
  const recentActivitiesQuery = useRecentClientActivitiesQuery(50);
  const incomeStatsQuery = useIncomeStatsQuery({
    currency: "CNY",
    ...currentMonthBounds(dateKey),
  });
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
  const sidebarCollapsed = useUiStore((state) => state.sidebarCollapsed);
  const toggleSidebarCollapsed = useUiStore(
    (state) => state.toggleSidebarCollapsed,
  );
  const dueSoonKey = dueSoonDateKey(dateKey, 7);
  const roadmapDueCount = [
    ...(plannedMilestonesQuery.data?.items ?? []),
    ...(activeMilestonesQuery.data?.items ?? []),
  ].filter((milestone) => milestone.targetDate <= dueSoonKey).length;
  const weekAgoIso = new Date(Date.now() - 7 * 86_400_000).toISOString();
  const clientRecentCount = (recentActivitiesQuery.data?.items ?? []).filter(
    (activity) => activity.occurredAt >= weekAgoIso,
  ).length;
  const incomeConfirmedMinor = incomeStatsQuery.data?.confirmedIncomeMinor ?? 0;
  const incomeConfirmedCount = incomeStatsQuery.data?.confirmedIncomeCount ?? 0;
  const navBadges: Record<string, { text: string; title: string } | undefined> =
    {
      "/inbox": inboxStatsQuery.data?.pending
        ? {
            text:
              inboxStatsQuery.data.pending > 99
                ? "99+"
                : String(inboxStatsQuery.data.pending),
            title: `${inboxStatsQuery.data.pending} 项待处理`,
          }
        : undefined,
      "/roadmap":
        roadmapDueCount > 0
          ? {
              text: roadmapDueCount > 99 ? "99+" : String(roadmapDueCount),
              title: `${roadmapDueCount} 个临期或逾期节点`,
            }
          : undefined,
      "/clients":
        clientRecentCount > 0
          ? {
              text: clientRecentCount > 99 ? "99+" : String(clientRecentCount),
              title: `${clientRecentCount} 条近 7 天客户动态`,
            }
          : undefined,
      "/income":
        incomeConfirmedCount > 0
          ? {
              text: compactCny(incomeConfirmedMinor),
              title: `本月已确认收入 ${formatCny(incomeConfirmedMinor)}`,
            }
          : undefined,
    };

  return (
    <aside
      aria-label="主导航"
      className={`left-sidebar${sidebarCollapsed ? " is-collapsed" : ""}`}
      id="primary-sidebar"
    >
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
          <span className="local-pill">v0.1.1</span>
        </div>
        <button
          aria-controls="primary-sidebar"
          aria-expanded={!sidebarCollapsed}
          aria-label={sidebarCollapsed ? "展开侧边栏" : "收起侧边栏"}
          className="icon-button sidebar-collapse-button"
          onClick={toggleSidebarCollapsed}
          title={sidebarCollapsed ? "展开侧边栏" : "收起侧边栏"}
          type="button"
        >
          {sidebarCollapsed ? (
            <PanelLeftOpen aria-hidden="true" size={17} />
          ) : (
            <PanelLeftClose aria-hidden="true" size={17} />
          )}
        </button>
      </div>

      <button
        aria-label="搜索或跳转"
        className="sidebar-search"
        onClick={() => setCommandPaletteOpen(true)}
        title={sidebarCollapsed ? "搜索或跳转" : undefined}
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
            {group.items.map(({ label, to, icon: Icon, badge }) => {
              const resolvedBadge =
                navBadges[to] ??
                (badge ? { text: badge, title: badge } : undefined);
              return (
                <NavLink
                  aria-label={sidebarCollapsed ? label : undefined}
                  className={({ isActive }) =>
                    `nav-item${isActive ? " nav-item-active" : ""}`
                  }
                  key={to}
                  title={sidebarCollapsed ? label : undefined}
                  to={to}
                >
                  <Icon className="nav-icon" size={17} />
                  <span className="sidebar-copy nav-text">{label}</span>
                  {resolvedBadge ? (
                    <span
                      aria-label={resolvedBadge.title}
                      className="sidebar-copy nav-badge"
                      title={resolvedBadge.title}
                    >
                      {resolvedBadge.text}
                    </span>
                  ) : null}
                </NavLink>
              );
            })}
          </div>
        ))}
      </nav>

      <FocusMiniCard />

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
