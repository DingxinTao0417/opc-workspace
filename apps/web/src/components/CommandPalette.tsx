import {
  CheckSquare2,
  Clock3,
  FolderKanban,
  Focus,
  Inbox,
  Plus,
  Search,
  Settings2,
  Sun,
  Users,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useTaskPageQuery } from "../api/hooks";
import { useUiStore } from "../store/ui";

interface Command {
  id: string;
  label: string;
  hint: string;
  icon: LucideIcon;
  run: () => void;
}

export function CommandPalette() {
  const open = useUiStore((state) => state.commandPaletteOpen);
  const setOpen = useUiStore((state) => state.setCommandPaletteOpen);
  const setNewTaskOpen = useUiStore((state) => state.setNewTaskOpen);
  const setSettingsOpen = useUiStore((state) => state.setSettingsOpen);
  const setTaskDetailId = useUiStore((state) => state.setTaskDetailId);
  const navigate = useNavigate();
  const [query, setQuery] = useState("");
  const [taskQuery, setTaskQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const normalizedQuery = query.trim();
  const tasksQuery = useTaskPageQuery(
    { q: taskQuery, page: 1, pageSize: 12, sort: "-updated_at" },
    open && taskQuery.length > 0,
  );

  const closeAndNavigate = (to: string) => {
    setOpen(false);
    navigate(to);
  };

  const commands = useMemo<Command[]>(() => {
    const pageCommands: Command[] = [
      {
        id: "today",
        label: "今日",
        hint: "页面",
        icon: Sun,
        run: () => closeAndNavigate("/today"),
      },
      {
        id: "inbox",
        label: "收件箱",
        hint: "页面",
        icon: Inbox,
        run: () => closeAndNavigate("/inbox"),
      },
      {
        id: "tasks",
        label: "任务",
        hint: "页面",
        icon: CheckSquare2,
        run: () => closeAndNavigate("/tasks"),
      },
      {
        id: "projects",
        label: "项目",
        hint: "页面",
        icon: FolderKanban,
        run: () => closeAndNavigate("/projects"),
      },
      {
        id: "clients",
        label: "客户",
        hint: "页面",
        icon: Users,
        run: () => closeAndNavigate("/clients"),
      },
      {
        id: "focus",
        label: "专注",
        hint: "页面",
        icon: Focus,
        run: () => closeAndNavigate("/focus"),
      },
      {
        id: "new-task",
        label: "新建任务",
        hint: "操作 · ⌘N",
        icon: Plus,
        run: () => {
          setOpen(false);
          setNewTaskOpen(true);
        },
      },
      {
        id: "focus-settings",
        label: "打开设置",
        hint: "设置",
        icon: Settings2,
        run: () => {
          setOpen(false);
          setSettingsOpen(true);
        },
      },
    ];
    return pageCommands;
  }, [navigate, setNewTaskOpen, setOpen, setSettingsOpen]);

  const taskCommands = useMemo<Command[]>(() => {
    const taskSource = tasksQuery.data?.items ?? [];
    return taskSource.map((task) => ({
      id: `task-${task.id}`,
      label: task.title,
      hint: `本地任务 · ${task.status}`,
      icon: Clock3,
      run: () => {
        setOpen(false);
        setTaskDetailId(task.id);
      },
    }));
  }, [setOpen, setTaskDetailId, tasksQuery.data]);

  const filtered = useMemo(() => {
    const normalized = query.trim().toLocaleLowerCase();
    if (!normalized) return commands;
    return commands.filter((command) =>
      `${command.label} ${command.hint}`
        .toLocaleLowerCase()
        .includes(normalized),
    );
  }, [commands, query]);

  const results = taskQuery ? [...filtered, ...taskCommands] : filtered;

  useEffect(() => {
    if (!open || !normalizedQuery) {
      setTaskQuery("");
      return;
    }
    const timer = window.setTimeout(() => setTaskQuery(normalizedQuery), 200);
    return () => window.clearTimeout(timer);
  }, [normalizedQuery, open]);

  useEffect(() => {
    if (!open) return;
    setQuery("");
    setTaskQuery("");
    setActiveIndex(0);
    window.setTimeout(() => inputRef.current?.focus(), 0);
  }, [open]);

  useEffect(() => {
    setActiveIndex(0);
  }, [query]);

  if (!open) return null;

  const onKeyDown = (event: React.KeyboardEvent) => {
    if (event.key === "Escape") {
      event.preventDefault();
      setOpen(false);
    } else if (event.key === "ArrowDown") {
      event.preventDefault();
      setActiveIndex((index) =>
        results.length ? Math.min(index + 1, results.length - 1) : 0,
      );
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setActiveIndex((index) => Math.max(index - 1, 0));
    } else if (event.key === "Enter" && results[activeIndex]) {
      event.preventDefault();
      results[activeIndex].run();
    }
  };

  return (
    <div className="command-root" onKeyDown={onKeyDown}>
      <button
        aria-label="关闭命令面板"
        className="modal-backdrop"
        onClick={() => setOpen(false)}
        type="button"
      />
      <section
        aria-label="命令面板"
        aria-modal="true"
        className="command-panel"
        role="dialog"
      >
        <div className="command-search">
          <Search size={18} />
          <input
            aria-label="搜索页面或任务"
            onChange={(event) => setQuery(event.target.value)}
            placeholder="搜索页面、任务或操作…"
            ref={inputRef}
            value={query}
          />
          <kbd>Esc</kbd>
        </div>
        <div className="command-list" role="listbox">
          {results.length ? (
            results.map((command, index) => {
              const Icon = command.icon;
              return (
                <button
                  aria-selected={activeIndex === index}
                  className={`command-item${activeIndex === index ? " command-item-active" : ""}`}
                  key={command.id}
                  onClick={command.run}
                  onMouseEnter={() => setActiveIndex(index)}
                  role="option"
                  type="button"
                >
                  <span className="command-icon">
                    <Icon size={16} />
                  </span>
                  <span className="command-label">{command.label}</span>
                  <span className="command-hint">{command.hint}</span>
                </button>
              );
            })
          ) : (normalizedQuery && !taskQuery) ||
            (tasksQuery.isPending && taskQuery) ? (
            <div aria-live="polite" className="command-empty">
              正在搜索本地任务…
            </div>
          ) : !tasksQuery.isError ? (
            <div className="command-empty">没有匹配结果</div>
          ) : null}
          {tasksQuery.isError && taskQuery ? (
            <div className="command-search-error" role="alert">
              <span>任务搜索暂时不可用。</span>
              <button onClick={() => void tasksQuery.refetch()} type="button">
                重试
              </button>
            </div>
          ) : null}
        </div>
        <footer className="command-footer">
          <span>
            <kbd>↑</kbd>
            <kbd>↓</kbd> 导航
          </span>
          <span>
            <kbd>↵</kbd> 选择
          </span>
        </footer>
      </section>
    </div>
  );
}
