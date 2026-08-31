import {
  AlertCircle,
  Check,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  LoaderCircle,
  Search,
  X,
} from "lucide-react";
import {
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent,
} from "react";
import { createPortal } from "react-dom";
import { useProjectOptionsQuery, useProjectQuery } from "../api/hooks";
import { useSettledPage } from "../lib/useSettledPage";
import type { Project, ProjectStatus } from "../types/models";

const debounceMilliseconds = 250;

const projectStatusLabels: Record<ProjectStatus, string> = {
  planning: "规划中",
  in_progress: "进行中",
  paused: "已暂停",
  completed: "已完成",
  archived: "已归档",
};

type ProjectOption = Pick<Project, "id" | "name" | "status" | "clientName"> & {
  fallback?: boolean;
};

type PopoverPosition = CSSProperties & {
  "--client-select-list-max-height": string;
};

export interface ProjectSelectProps {
  value: string;
  onChange: (id: string) => void;
  ariaLabel: string;
  emptyLabel: string;
  disabled?: boolean;
  selectedName?: string | null;
  includeArchived?: boolean;
  clientId?: string;
  variant: "form" | "toolbar" | "filter";
}

function uniqueProjects(
  items: Project[],
  includeArchived: boolean,
  value: string,
) {
  const seen = new Set<string>();
  return items.filter((project) => {
    if (seen.has(project.id)) return false;
    seen.add(project.id);
    return (
      includeArchived || project.status !== "archived" || project.id === value
    );
  });
}

function projectContext(option: ProjectOption): string {
  return option.clientName ? `客户：${option.clientName}` : "未关联客户";
}

export function ProjectSelect({
  value,
  onChange,
  ariaLabel,
  emptyLabel,
  disabled = false,
  selectedName,
  includeArchived = false,
  clientId,
  variant,
}: ProjectSelectProps) {
  const listboxId = `project-select-${useId().replace(/:/g, "")}`;
  const rootRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const popoverRef = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const [debouncedSearch, setDebouncedSearch] = useState("");
  const [page, setPage] = useState(1);
  const [activeIndex, setActiveIndex] = useState(-1);
  const [rememberedSelection, setRememberedSelection] =
    useState<ProjectOption | null>(null);
  const [popoverPosition, setPopoverPosition] =
    useState<PopoverPosition | null>(null);

  const optionsQuery = useProjectOptionsQuery(
    debouncedSearch,
    page,
    open && !disabled,
    includeArchived,
    clientId,
  );
  const selectedQuery = useProjectQuery(value || null);

  const pageProjects = useMemo(
    () =>
      uniqueProjects(optionsQuery.data?.items ?? [], includeArchived, value),
    [includeArchived, optionsQuery.data?.items, value],
  );

  const selectedOption = useMemo<ProjectOption | null>(() => {
    if (!value) return null;
    if (selectedQuery.data?.id === value) return selectedQuery.data;
    if (rememberedSelection?.id === value) return rememberedSelection;
    const pageProject = pageProjects.find((project) => project.id === value);
    if (pageProject) return pageProject;
    return {
      id: value,
      name: selectedName?.trim() || "已选项目",
      status: "archived",
      clientName: null,
      fallback: true,
    };
  }, [
    pageProjects,
    rememberedSelection,
    selectedName,
    selectedQuery.data,
    value,
  ]);

  const resultOptions = useMemo(
    () => pageProjects.filter((project) => project.id !== value),
    [pageProjects, value],
  );
  const menuOptions = useMemo(
    () => (selectedOption ? [selectedOption, ...resultOptions] : resultOptions),
    [resultOptions, selectedOption],
  );

  const normalizedSearch = search.trim();
  const waitingForInput = open && normalizedSearch !== debouncedSearch;
  const resultsLocked = waitingForInput || optionsQuery.isFetching;
  const meta = optionsQuery.data?.meta;
  const totalPages = Math.max(
    1,
    Math.ceil((meta?.total ?? 0) / Math.max(1, meta?.pageSize ?? 20)),
  );
  const hasPreviousPage = page > 1;
  const hasNextPage = page < totalPages;
  const hasMoreResults = (meta?.total ?? 0) > (meta?.pageSize ?? 20);
  const selectedLabel = selectedOption?.name ?? emptyLabel;
  const activeOption = menuOptions[activeIndex];
  const activeOptionIsRendered = Boolean(
    activeOption &&
    (activeOption.id === selectedOption?.id ||
      (!waitingForInput && !optionsQuery.isPending && !optionsQuery.isError)),
  );

  const closeMenu = () => {
    setOpen(false);
    setSearch("");
    setDebouncedSearch("");
    setPage(1);
    setActiveIndex(-1);
  };

  const openMenu = () => {
    if (disabled || open) return;
    setOpen(true);
    setSearch("");
    setDebouncedSearch("");
    setPage(1);
  };

  useEffect(() => {
    if (!open) return;
    const nextSearch = search.trim();
    if (nextSearch === debouncedSearch) return;
    const timer = window.setTimeout(() => {
      setDebouncedSearch(nextSearch);
      setPage(1);
    }, debounceMilliseconds);
    return () => window.clearTimeout(timer);
  }, [debouncedSearch, open, search]);

  useEffect(() => {
    if (!open) return;
    const onPointerDown = (event: PointerEvent) => {
      const target = event.target as Node;
      if (
        !rootRef.current?.contains(target) &&
        !popoverRef.current?.contains(target)
      ) {
        closeMenu();
      }
    };
    document.addEventListener("pointerdown", onPointerDown, true);
    return () =>
      document.removeEventListener("pointerdown", onPointerDown, true);
  }, [open]);

  useLayoutEffect(() => {
    if (!open) {
      setPopoverPosition(null);
      return;
    }
    const updatePosition = () => {
      const trigger = rootRef.current?.getBoundingClientRect();
      if (!trigger) return;
      const viewportPadding = 12;
      const gap = 5;
      const availableWidth = Math.max(
        220,
        window.innerWidth - viewportPadding * 2,
      );
      const width = Math.min(Math.max(trigger.width, 260), availableWidth);
      const left = Math.min(
        Math.max(viewportPadding, trigger.left),
        Math.max(viewportPadding, window.innerWidth - width - viewportPadding),
      );
      const availableBelow =
        window.innerHeight - trigger.bottom - gap - viewportPadding;
      const availableAbove = trigger.top - gap - viewportPadding;
      const placeBelow =
        availableBelow >= 190 || availableBelow >= availableAbove;
      const availableHeight = Math.max(
        120,
        placeBelow ? availableBelow : availableAbove,
      );
      const shared = {
        left,
        width,
        "--client-select-list-max-height": `${Math.max(80, Math.min(280, availableHeight - 70))}px`,
      };
      setPopoverPosition(
        placeBelow
          ? { ...shared, top: trigger.bottom + gap }
          : { ...shared, bottom: window.innerHeight - trigger.top + gap },
      );
    };
    updatePosition();
    window.addEventListener("resize", updatePosition);
    window.addEventListener("scroll", updatePosition, true);
    return () => {
      window.removeEventListener("resize", updatePosition);
      window.removeEventListener("scroll", updatePosition, true);
    };
  }, [open]);

  useEffect(() => {
    if (!open) {
      setActiveIndex(-1);
      return;
    }
    if (!menuOptions.length) {
      setActiveIndex(-1);
      return;
    }
    setActiveIndex((current) => {
      const activeId = menuOptions[current]?.id;
      if (activeId) {
        const retainedIndex = menuOptions.findIndex(
          (option) => option.id === activeId,
        );
        if (retainedIndex >= 0) return retainedIndex;
      }
      return 0;
    });
  }, [menuOptions, open]);

  useSettledPage({
    page,
    meta,
    isBlocked: !open,
    isFetching: optionsQuery.isFetching,
    isPlaceholderData: optionsQuery.isPlaceholderData,
    isSuccess: optionsQuery.isSuccess,
    setPage,
  });

  useEffect(() => {
    if (!open || disabled) return;
    document
      .getElementById(`${listboxId}-option-${activeOption?.id ?? ""}`)
      ?.scrollIntoView?.({ block: "nearest" });
  }, [activeOption?.id, disabled, listboxId, open]);

  useEffect(() => {
    if (disabled && open) closeMenu();
  }, [disabled, open]);

  const selectProject = (option: ProjectOption) => {
    if (resultsLocked) return;
    setRememberedSelection(option);
    onChange(option.id);
    closeMenu();
  };

  const movePage = (nextPage: number) => {
    if (
      resultsLocked ||
      optionsQuery.isError ||
      nextPage < 1 ||
      nextPage > totalPages
    ) {
      return;
    }
    setPage(nextPage);
    setActiveIndex(selectedOption ? 0 : -1);
    inputRef.current?.focus();
  };

  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.nativeEvent.isComposing || event.nativeEvent.keyCode === 229) {
      return;
    }
    if (event.key === "Escape") {
      if (open) {
        event.preventDefault();
        event.stopPropagation();
        closeMenu();
      }
      return;
    }
    if (event.key === "Tab") {
      closeMenu();
      return;
    }
    if (event.key === "PageUp") {
      event.preventDefault();
      movePage(page - 1);
      return;
    }
    if (event.key === "PageDown") {
      event.preventDefault();
      movePage(page + 1);
      return;
    }
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      if (!open) {
        openMenu();
        return;
      }
      if (!menuOptions.length || resultsLocked) return;
      setActiveIndex((current) => {
        if (event.key === "ArrowDown") {
          return current < 0 ? 0 : (current + 1) % menuOptions.length;
        }
        return current <= 0 ? menuOptions.length - 1 : current - 1;
      });
      return;
    }
    if (event.key === "Home" || event.key === "End") {
      if (!open || !menuOptions.length || resultsLocked) return;
      event.preventDefault();
      setActiveIndex(event.key === "Home" ? 0 : menuOptions.length - 1);
      return;
    }
    if (event.key === "Enter") {
      event.preventDefault();
      if (!open) {
        openMenu();
      } else if (optionsQuery.isError) {
        void optionsQuery.refetch();
      } else if (activeOption && !resultsLocked) {
        selectProject(activeOption);
      }
    }
  };

  return (
    <div
      className={`client-select project-select client-select--${variant}`}
      onBlur={(event) => {
        const relatedTarget = event.relatedTarget as Node | null;
        if (
          !event.currentTarget.contains(relatedTarget) &&
          !popoverRef.current?.contains(relatedTarget)
        ) {
          closeMenu();
        }
      }}
      ref={rootRef}
    >
      <div
        className="client-select__control"
        data-disabled={disabled || undefined}
        data-open={open || undefined}
        onClick={(event) => {
          if (
            disabled ||
            (event.target instanceof Element &&
              event.target.closest(".client-select__clear"))
          ) {
            return;
          }
          inputRef.current?.focus();
          openMenu();
        }}
      >
        {open ? <Search aria-hidden="true" size={14} /> : null}
        <input
          aria-activedescendant={
            open && activeOption && activeOptionIsRendered
              ? `${listboxId}-option-${activeOption.id}`
              : undefined
          }
          aria-autocomplete="list"
          aria-busy={open && (waitingForInput || optionsQuery.isFetching)}
          aria-controls={open ? listboxId : undefined}
          aria-expanded={open}
          aria-label={ariaLabel}
          autoComplete="off"
          disabled={disabled}
          onChange={(event) => {
            if (!open) openMenu();
            setSearch(event.target.value);
            setPage(1);
          }}
          onClick={openMenu}
          onFocus={openMenu}
          onKeyDown={onKeyDown}
          placeholder={open ? "搜索项目…" : emptyLabel}
          ref={inputRef}
          role="combobox"
          spellCheck={false}
          value={open ? search : selectedLabel}
        />
        {!open && value && selectedOption ? (
          <span
            className="client-select__selected-status"
            data-status={
              selectedOption.fallback ? undefined : selectedOption.status
            }
          >
            {selectedOption.fallback
              ? "当前选择"
              : projectStatusLabels[selectedOption.status]}
          </span>
        ) : null}
        {value && !disabled ? (
          <button
            aria-label={`清除${ariaLabel}`}
            className="client-select__clear"
            onClick={(event) => {
              event.stopPropagation();
              setRememberedSelection(null);
              onChange("");
              closeMenu();
            }}
            onMouseDown={(event) => event.preventDefault()}
            type="button"
          >
            <X aria-hidden="true" size={13} />
          </button>
        ) : null}
        <ChevronDown
          aria-hidden="true"
          className="client-select__chevron"
          size={14}
        />
      </div>

      {open
        ? createPortal(
            <div
              className="client-select__popover project-select__popover"
              ref={popoverRef}
              style={popoverPosition ?? { visibility: "hidden" }}
            >
              <div
                aria-label={`${ariaLabel}候选项`}
                className="client-select__listbox"
                id={listboxId}
                role="listbox"
              >
                {selectedOption ? (
                  <>
                    <div className="client-select__section-label">当前选择</div>
                    <ProjectOptionButton
                      active={activeIndex === 0}
                      disabled={resultsLocked}
                      listboxId={listboxId}
                      onMouseEnter={() => {
                        if (!resultsLocked) setActiveIndex(0);
                      }}
                      onSelect={() => selectProject(selectedOption)}
                      option={selectedOption}
                      selected
                    />
                  </>
                ) : null}

                {waitingForInput ? (
                  <div
                    aria-live="polite"
                    className="client-select__state"
                    role="status"
                  >
                    <LoaderCircle
                      aria-hidden="true"
                      className="is-spinning"
                      size={14}
                    />
                    正在等待输入…
                  </div>
                ) : optionsQuery.isPending ? (
                  <div
                    aria-live="polite"
                    className="client-select__state"
                    role="status"
                  >
                    <LoaderCircle
                      aria-hidden="true"
                      className="is-spinning"
                      size={14}
                    />
                    正在读取项目…
                  </div>
                ) : optionsQuery.isError ? (
                  <div
                    className="client-select__state client-select__state--error"
                    role="alert"
                  >
                    <AlertCircle aria-hidden="true" size={14} />
                    <span>项目读取失败，当前选择已保留。</span>
                    <button
                      className="client-select__retry"
                      onClick={() => void optionsQuery.refetch()}
                      onMouseDown={(event) => event.preventDefault()}
                      tabIndex={-1}
                      type="button"
                    >
                      重试
                    </button>
                  </div>
                ) : (
                  <>
                    {resultOptions.length ? (
                      <>
                        {selectedOption ? (
                          <div className="client-select__section-label">
                            搜索结果
                          </div>
                        ) : null}
                        {resultOptions.map((option) => {
                          const index = selectedOption
                            ? menuOptions.findIndex(
                                (item) => item.id === option.id,
                              )
                            : resultOptions.findIndex(
                                (item) => item.id === option.id,
                              );
                          return (
                            <ProjectOptionButton
                              active={activeIndex === index}
                              disabled={resultsLocked}
                              key={option.id}
                              listboxId={listboxId}
                              onMouseEnter={() => {
                                if (!resultsLocked) setActiveIndex(index);
                              }}
                              onSelect={() => selectProject(option)}
                              option={option}
                              selected={false}
                            />
                          );
                        })}
                      </>
                    ) : pageProjects.length === 0 ? (
                      <div aria-live="polite" className="client-select__empty">
                        {debouncedSearch
                          ? `没有匹配“${debouncedSearch}”的项目`
                          : "没有可选择的项目"}
                      </div>
                    ) : null}
                    {optionsQuery.isFetching ? (
                      <div
                        aria-live="polite"
                        className="client-select__loading-cover"
                        role="status"
                      >
                        <LoaderCircle
                          aria-hidden="true"
                          className="is-spinning"
                          size={14}
                        />
                        正在读取第 {page} 页…
                      </div>
                    ) : null}
                  </>
                )}
              </div>

              {!waitingForInput &&
              !optionsQuery.isPending &&
              !optionsQuery.isError ? (
                <footer className="client-select__footer">
                  <button
                    aria-label="上一页项目"
                    disabled={!hasPreviousPage || resultsLocked}
                    onClick={() => movePage(page - 1)}
                    onMouseDown={(event) => event.preventDefault()}
                    tabIndex={-1}
                    type="button"
                  >
                    <ChevronLeft aria-hidden="true" size={13} />
                    上一页
                  </button>
                  <span aria-live="polite">
                    第 {page} / {totalPages} 页
                  </span>
                  <button
                    aria-label="下一页项目"
                    disabled={!hasNextPage || resultsLocked}
                    onClick={() => movePage(page + 1)}
                    onMouseDown={(event) => event.preventDefault()}
                    tabIndex={-1}
                    type="button"
                  >
                    下一页
                    <ChevronRight aria-hidden="true" size={13} />
                  </button>
                </footer>
              ) : null}
              {hasMoreResults && !waitingForInput && !optionsQuery.isError ? (
                <div className="client-select__more-hint">
                  结果较多，可继续输入缩小范围；也可按 PageUp / PageDown 翻页。
                </div>
              ) : null}
            </div>,
            document.body,
          )
        : null}
    </div>
  );
}

function ProjectOptionButton({
  option,
  listboxId,
  active,
  selected,
  disabled,
  onMouseEnter,
  onSelect,
}: {
  option: ProjectOption;
  listboxId: string;
  active: boolean;
  selected: boolean;
  disabled: boolean;
  onMouseEnter: () => void;
  onSelect: () => void;
}) {
  const detail = option.fallback
    ? "当前选择 · 详情暂不可用"
    : `${projectStatusLabels[option.status]} · ${projectContext(option)}`;
  return (
    <button
      aria-disabled={disabled}
      aria-label={`${option.name}${option.fallback ? "，当前选择" : `，${projectStatusLabels[option.status]}，${projectContext(option)}`}`}
      aria-selected={selected}
      className="client-select__option"
      data-active={active || undefined}
      data-selected={selected || undefined}
      disabled={disabled}
      id={`${listboxId}-option-${option.id}`}
      onClick={onSelect}
      onMouseDown={(event) => event.preventDefault()}
      onMouseEnter={onMouseEnter}
      role="option"
      tabIndex={-1}
      type="button"
    >
      <span className="client-select__avatar" aria-hidden="true">
        {option.name.trim().charAt(0).toUpperCase() || "项"}
      </span>
      <span className="client-select__option-copy">
        <strong>{option.name}</strong>
        <small>{detail}</small>
      </span>
      {selected ? <Check aria-hidden="true" size={14} /> : null}
    </button>
  );
}
