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
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import {
  ApiError,
  getClient,
  getInboxItem,
  getProject,
  getTask,
} from "../api/client";
import { useSearchQuery } from "../api/hooks";
import {
  loadCommandRecents,
  recordCommandRecent,
  removeCommandRecent,
  type CommandRecent,
} from "../store/commandRecents";
import { useUiStore, type SettingsModule } from "../store/ui";
import type { SearchResourceType } from "../types/models";

interface Command {
  id: string;
  label: string;
  hint: string;
  icon: LucideIcon;
  run: () => void;
}

interface RecentResourceCommand {
  resourceType: SearchResourceType;
  resourceId: string;
  label: string;
  hint: string;
  icon: LucideIcon;
  route: string;
}

const focusableSelector = [
  "button:not([disabled])",
  "[href]",
  "input:not([disabled])",
  "select:not([disabled])",
  "textarea:not([disabled])",
  '[tabindex]:not([tabindex="-1"])',
].join(",");

const settingsCommands: {
  id: string;
  label: string;
  module: SettingsModule;
}[] = [
  { id: "settings", label: "打开设置", module: "general" },
  { id: "settings-profile", label: "个人资料设置", module: "profile" },
  { id: "settings-appearance", label: "外观设置", module: "appearance" },
  { id: "settings-focus", label: "专注设置", module: "focus" },
  { id: "settings-actors", label: "人员与责任设置", module: "actors" },
  { id: "settings-data", label: "数据与备份", module: "data" },
  { id: "settings-about", label: "关于", module: "about" },
];

const searchResourcePresentation: Record<
  SearchResourceType,
  { label: string; icon: LucideIcon }
> = {
  task: { label: "任务", icon: Clock3 },
  project: { label: "项目", icon: FolderKanban },
  client: { label: "客户", icon: Users },
  inbox_item: { label: "收件箱", icon: Inbox },
};

async function resolveRecentResource(
  recent: Extract<CommandRecent, { kind: "resource" }>,
): Promise<RecentResourceCommand> {
  const presentation = searchResourcePresentation[recent.resourceType];
  switch (recent.resourceType) {
    case "task": {
      const task = await getTask(recent.resourceId);
      return {
        resourceType: recent.resourceType,
        resourceId: recent.resourceId,
        label: task.title,
        hint: `本地${presentation.label} · ${task.status}`,
        icon: presentation.icon,
        route: `/tasks/${task.id}`,
      };
    }
    case "project": {
      const project = await getProject(recent.resourceId);
      return {
        resourceType: recent.resourceType,
        resourceId: recent.resourceId,
        label: project.name,
        hint: `本地${presentation.label} · ${project.status}`,
        icon: presentation.icon,
        route: `/projects/${project.id}`,
      };
    }
    case "client": {
      const client = await getClient(recent.resourceId);
      return {
        resourceType: recent.resourceType,
        resourceId: recent.resourceId,
        label: client.name,
        hint: `本地${presentation.label} · ${client.status}`,
        icon: presentation.icon,
        route: `/clients/${client.id}`,
      };
    }
    case "inbox_item": {
      const item = await getInboxItem(recent.resourceId);
      return {
        resourceType: recent.resourceType,
        resourceId: recent.resourceId,
        label: item.title,
        hint: `本地${presentation.label} · ${item.status}`,
        icon: presentation.icon,
        route: `/inbox/${item.id}`,
      };
    }
  }
}

export function CommandPalette() {
  const open = useUiStore((state) => state.commandPaletteOpen);
  const setOpen = useUiStore((state) => state.setCommandPaletteOpen);
  const setNewTaskOpen = useUiStore((state) => state.setNewTaskOpen);
  const setSettingsOpen = useUiStore((state) => state.setSettingsOpen);
  const navigate = useNavigate();
  const [query, setQuery] = useState("");
  const [searchQuery, setSearchQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);
  const [recents, setRecents] = useState<CommandRecent[]>(() =>
    loadCommandRecents(),
  );
  const [recentResources, setRecentResources] = useState<
    RecentResourceCommand[]
  >([]);
  const inputRef = useRef<HTMLInputElement>(null);
  const panelRef = useRef<HTMLElement>(null);
  const normalizedQuery = query.trim();
  const resourcesQuery = useSearchQuery(
    { q: searchQuery, page: 1, pageSize: 12 },
    open && searchQuery.length > 0,
  );

  const recordRecentCommand = useCallback((commandId: string) => {
    setRecents((current) =>
      recordCommandRecent(current, { kind: "command", commandId }),
    );
  }, []);

  const recordRecentResource = useCallback(
    (resourceType: SearchResourceType, resourceId: string) => {
      setRecents((current) =>
        recordCommandRecent(current, {
          kind: "resource",
          resourceType,
          resourceId,
        }),
      );
    },
    [],
  );

  const closeAndNavigate = (to: string, commandId?: string) => {
    if (commandId) recordRecentCommand(commandId);
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
        run: () => closeAndNavigate("/today", "today"),
      },
      {
        id: "inbox",
        label: "收件箱",
        hint: "页面",
        icon: Inbox,
        run: () => closeAndNavigate("/inbox", "inbox"),
      },
      {
        id: "tasks",
        label: "任务",
        hint: "页面",
        icon: CheckSquare2,
        run: () => closeAndNavigate("/tasks", "tasks"),
      },
      {
        id: "projects",
        label: "项目",
        hint: "页面",
        icon: FolderKanban,
        run: () => closeAndNavigate("/projects", "projects"),
      },
      {
        id: "clients",
        label: "客户",
        hint: "页面",
        icon: Users,
        run: () => closeAndNavigate("/clients", "clients"),
      },
      {
        id: "focus",
        label: "专注",
        hint: "页面",
        icon: Focus,
        run: () => closeAndNavigate("/focus", "focus"),
      },
      {
        id: "new-task",
        label: "新建任务",
        hint: "操作 · ⌘N",
        icon: Plus,
        run: () => {
          recordRecentCommand("new-task");
          setOpen(false);
          setNewTaskOpen(true);
        },
      },
      ...settingsCommands.map(({ id, label, module }) => ({
        id,
        label,
        hint: `设置 · ${label === "打开设置" ? "通用" : label.replace(/设置$/, "")}`,
        icon: Settings2,
        run: () => {
          recordRecentCommand(id);
          setOpen(false);
          setSettingsOpen(true, module);
        },
      })),
    ];
    return pageCommands;
  }, [navigate, recordRecentCommand, setNewTaskOpen, setOpen, setSettingsOpen]);

  const resourceCommands = useMemo<Command[]>(() => {
    const resourceSource = resourcesQuery.data?.items ?? [];
    return resourceSource.map((resource) => ({
      id: `${resource.resourceType}-${resource.resourceId}`,
      label: resource.title,
      hint: `本地${searchResourcePresentation[resource.resourceType].label} · ${resource.subtitle || resource.status}`,
      icon: searchResourcePresentation[resource.resourceType].icon,
      run: () => {
        recordRecentResource(resource.resourceType, resource.resourceId);
        setOpen(false);
        navigate(resource.route);
      },
    }));
  }, [navigate, recordRecentResource, resourcesQuery.data, setOpen]);

  const recentResourceRecords = useMemo(
    () =>
      recents.filter(
        (recent): recent is Extract<CommandRecent, { kind: "resource" }> =>
          recent.kind === "resource",
      ),
    [recents],
  );

  useEffect(() => {
    if (!open || recentResourceRecords.length === 0) {
      if (!open) setRecentResources([]);
      return;
    }
    let current = true;
    void Promise.all(
      recentResourceRecords.map(async (recent) => {
        try {
          return await resolveRecentResource(recent);
        } catch (error) {
          return error instanceof ApiError && error.status === 404
            ? { stale: recent }
            : null;
        }
      }),
    ).then((resolved) => {
      if (!current) return;
      const stale = resolved.flatMap((entry) =>
        entry && "stale" in entry ? [entry.stale] : [],
      );
      if (stale.length) {
        setRecents((stored) =>
          stale.reduce(
            (currentRecents, recent) =>
              removeCommandRecent(currentRecents, recent),
            stored,
          ),
        );
      }
      setRecentResources(
        resolved.filter((entry): entry is RecentResourceCommand =>
          Boolean(entry && !("stale" in entry)),
        ),
      );
    });
    return () => {
      current = false;
    };
  }, [open, recentResourceRecords]);

  const filtered = useMemo(() => {
    const normalized = query.trim().toLocaleLowerCase();
    if (!normalized) return commands;
    return commands.filter((command) =>
      `${command.label} ${command.hint}`
        .toLocaleLowerCase()
        .includes(normalized),
    );
  }, [commands, query]);

  const recentCommands = useMemo<Command[]>(() => {
    const commandsByID = new Map(
      commands.map((command) => [command.id, command]),
    );
    const resourcesByKey = new Map(
      recentResources.map((resource) => [
        `${resource.resourceType}:${resource.resourceId}`,
        resource,
      ]),
    );
    return recents.flatMap((recent) => {
      if (recent.kind === "command") {
        const command = commandsByID.get(recent.commandId);
        return command
          ? [
              {
                ...command,
                id: `recent-command-${command.id}`,
                hint: `最近使用 · ${command.hint}`,
              },
            ]
          : [];
      }
      const resource = resourcesByKey.get(
        `${recent.resourceType}:${recent.resourceId}`,
      );
      if (!resource) return [];
      return [
        {
          id: `recent-resource-${resource.resourceType}-${resource.resourceId}`,
          label: resource.label,
          hint: `最近使用 · ${resource.hint}`,
          icon: resource.icon,
          run: () => {
            recordRecentResource(resource.resourceType, resource.resourceId);
            setOpen(false);
            navigate(resource.route);
          },
        },
      ];
    });
  }, [
    commands,
    navigate,
    recentResources,
    recents,
    recordRecentResource,
    setOpen,
  ]);

  const fixedCommandsWithoutRecents = useMemo(() => {
    const recentCommandIDs = new Set(
      recents.flatMap((recent) =>
        recent.kind === "command" ? [recent.commandId] : [],
      ),
    );
    return commands.filter((command) => !recentCommandIDs.has(command.id));
  }, [commands, recents]);

  const results = useMemo(
    () =>
      searchQuery
        ? [...filtered, ...resourceCommands]
        : query.trim()
          ? filtered
          : [...recentCommands, ...fixedCommandsWithoutRecents],
    [
      filtered,
      fixedCommandsWithoutRecents,
      query,
      recentCommands,
      resourceCommands,
      searchQuery,
    ],
  );

  useEffect(() => {
    if (!open || !normalizedQuery) {
      setSearchQuery("");
      return;
    }
    setSearchQuery("");
    const timer = window.setTimeout(() => setSearchQuery(normalizedQuery), 200);
    return () => window.clearTimeout(timer);
  }, [normalizedQuery, open]);

  useEffect(() => {
    if (!open) return;
    const previouslyFocused =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    setQuery("");
    setSearchQuery("");
    setActiveIndex(0);
    const frame = window.requestAnimationFrame(() => inputRef.current?.focus());
    return () => {
      window.cancelAnimationFrame(frame);
      document.body.style.overflow = previousOverflow;
      if (previouslyFocused?.isConnected) previouslyFocused.focus();
    };
  }, [open]);

  useEffect(() => {
    setActiveIndex(0);
  }, [query]);

  useEffect(() => {
    setActiveIndex((index) =>
      results.length ? Math.min(index, results.length - 1) : 0,
    );
  }, [results.length]);

  useEffect(() => {
    const activeResult = results[activeIndex];
    if (!activeResult) return;
    document
      .getElementById(`command-result-${activeResult.id}`)
      ?.scrollIntoView?.({ block: "nearest" });
  }, [activeIndex, results]);

  if (!open) return null;

  const onKeyDown = (event: React.KeyboardEvent) => {
    if (event.nativeEvent.isComposing || event.nativeEvent.keyCode === 229) {
      return;
    }
    if (event.key === "Escape") {
      event.preventDefault();
      setOpen(false);
    } else if (event.key === "Tab") {
      const focusable = panelRef.current
        ? Array.from(
            panelRef.current.querySelectorAll<HTMLElement>(focusableSelector),
          ).filter((element) => element.tabIndex >= 0)
        : [];
      if (!focusable.length) {
        event.preventDefault();
        panelRef.current?.focus();
        return;
      }
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
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
        ref={panelRef}
        role="dialog"
        tabIndex={-1}
      >
        <div className="command-search">
          <Search size={18} />
          <input
            aria-activedescendant={
              results[activeIndex]
                ? `command-result-${results[activeIndex].id}`
                : undefined
            }
            aria-autocomplete="list"
            aria-controls="command-results"
            aria-expanded="true"
            aria-label="搜索页面、业务或操作"
            onChange={(event) => setQuery(event.target.value)}
            placeholder="搜索任务、项目、客户、收件箱或操作…"
            ref={inputRef}
            role="combobox"
            value={query}
          />
          <kbd>Esc</kbd>
        </div>
        <div className="command-list" id="command-results" role="listbox">
          {!normalizedQuery && recentCommands.length ? (
            <div className="command-section-label">最近使用</div>
          ) : null}
          {results.length ? (
            results.map((command, index) => {
              const Icon = command.icon;
              return (
                <button
                  aria-selected={activeIndex === index}
                  className={`command-item${activeIndex === index ? " command-item-active" : ""}`}
                  id={`command-result-${command.id}`}
                  key={command.id}
                  onClick={command.run}
                  onMouseEnter={() => setActiveIndex(index)}
                  role="option"
                  tabIndex={-1}
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
          ) : (normalizedQuery && !searchQuery) ||
            (resourcesQuery.isPending && searchQuery) ? (
            <div aria-live="polite" className="command-empty">
              正在搜索本地业务…
            </div>
          ) : !resourcesQuery.isError ? (
            <div className="command-empty">没有匹配结果</div>
          ) : null}
          {resourcesQuery.isError && searchQuery ? (
            <div className="command-search-error" role="alert">
              <span>本地业务搜索暂时不可用。</span>
              <button
                onClick={() => void resourcesQuery.refetch()}
                type="button"
              >
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
