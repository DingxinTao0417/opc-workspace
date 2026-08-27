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
  Sun,
  Users,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { NavLink } from "react-router-dom";
import { useTasksQuery } from "../api/hooks";
import { useUiStore } from "../store/ui";

interface NavItem {
  label: string;
  to: string;
  icon: LucideIcon;
  badge?: string;
  later?: boolean;
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
    label: "后续版本",
    items: [
      { label: "路线图", to: "/roadmap", icon: Map, later: true },
      {
        label: "内容日历",
        to: "/content-calendar",
        icon: CalendarDays,
        later: true,
      },
    ],
  },
];

export function Sidebar() {
  const tasksQuery = useTasksQuery();
  const setCommandPaletteOpen = useUiStore(
    (state) => state.setCommandPaletteOpen,
  );
  const taskCount = tasksQuery.data?.length ?? 0;
  const completedCount =
    tasksQuery.data?.filter((task) => task.status === "done").length ?? 0;
  const completedPercent = taskCount
    ? Math.round((completedCount / taskCount) * 100)
    : 0;

  return (
    <aside className="left-sidebar" aria-label="主导航">
      <div className="brand-block">
        <div className="brand-mark">O</div>
        <div className="sidebar-copy min-w-0">
          <div className="brand-name">opc-workspace</div>
          <span className="local-pill">本地优先 · v0.1</span>
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
            {group.items.map(({ label, to, icon: Icon, badge, later }) => (
              <NavLink
                className={({ isActive }) =>
                  `nav-item${isActive ? " nav-item-active" : ""}`
                }
                key={to}
                to={to}
              >
                <Icon className="nav-icon" size={17} />
                <span className="sidebar-copy nav-text">{label}</span>
                {badge ? (
                  <span className="sidebar-copy nav-badge">{badge}</span>
                ) : null}
                {later ? (
                  <span className="sidebar-copy later-badge">后续</span>
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
        <div
          className="progress-track"
          aria-label={`本周完成度 ${completedPercent}%`}
        >
          <span style={{ width: `${completedPercent}%` }} />
        </div>
        <div className="weekly-copy">
          {taskCount ? `${completedCount} / ${taskCount} 项` : "暂无任务"}
        </div>
      </div>
    </aside>
  );
}
