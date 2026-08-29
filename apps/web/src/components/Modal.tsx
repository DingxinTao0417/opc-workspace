import { X } from "lucide-react";
import {
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import {
  isTopmostOverlay,
  registerOverlay,
  subscribeOverlayStack,
} from "./overlayStack";

export function Modal({
  open,
  onClose,
  title,
  children,
  footer,
  width = "520px",
  dismissible = true,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  children: ReactNode;
  footer?: ReactNode;
  width?: string;
  dismissible?: boolean;
}) {
  const panelRef = useRef<HTMLElement>(null);
  const rootRef = useRef<HTMLDivElement>(null);
  const onCloseRef = useRef(onClose);
  const modalId = useRef(Symbol("modal")).current;
  const [isTopmost, setIsTopmost] = useState(false);
  const titleId = useId();

  useEffect(() => {
    onCloseRef.current = onClose;
  }, [onClose]);

  useLayoutEffect(() => {
    const root = rootRef.current;
    if (!open || !root) return undefined;
    const previouslyFocused =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null;
    const updateTopmost = () => {
      const next = isTopmostOverlay(modalId);
      root.toggleAttribute("inert", !next);
      setIsTopmost(next);
    };
    const unsubscribe = subscribeOverlayStack(updateTopmost);
    const unregister = registerOverlay(modalId, root, previouslyFocused);
    updateTopmost();
    return () => {
      unsubscribe();
      unregister();
    };
  }, [modalId, open]);

  useEffect(() => {
    if (!open) return undefined;
    const focusableSelector = [
      "button:not([disabled])",
      "[href]",
      "input:not([disabled])",
      "select:not([disabled])",
      "textarea:not([disabled])",
      '[tabindex]:not([tabindex="-1"])',
    ].join(",");
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.defaultPrevented || !isTopmostOverlay(modalId)) return;
      if (
        event.key === "Escape" &&
        (event.isComposing || event.keyCode === 229)
      ) {
        return;
      }
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopImmediatePropagation();
        if (dismissible) onCloseRef.current();
        return;
      }
      if (event.key !== "Tab" || !panelRef.current) return;
      const focusable = Array.from(
        panelRef.current.querySelectorAll<HTMLElement>(focusableSelector),
      ).filter((element) => element.offsetParent !== null);
      if (focusable.length === 0) {
        event.preventDefault();
        panelRef.current.focus();
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
    };
    document.addEventListener("keydown", onKeyDown);
    const frame = window.requestAnimationFrame(() => {
      if (
        isTopmostOverlay(modalId) &&
        !panelRef.current?.contains(document.activeElement)
      ) {
        const target = panelRef.current?.querySelector<HTMLElement>(
          `[autofocus], ${focusableSelector}`,
        );
        (target ?? panelRef.current)?.focus();
      }
    });
    return () => {
      window.cancelAnimationFrame(frame);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [dismissible, modalId, open]);

  if (!open) return null;

  return createPortal(
    <div
      aria-hidden={isTopmost ? undefined : true}
      className="modal-root"
      data-overlay-root="true"
      ref={rootRef}
    >
      {dismissible ? (
        <button
          aria-label="关闭弹窗"
          className="modal-backdrop"
          onClick={onClose}
          type="button"
        />
      ) : (
        <div className="modal-backdrop" />
      )}
      <section
        aria-labelledby={titleId}
        aria-modal={isTopmost ? "true" : undefined}
        className="modal-panel"
        ref={panelRef}
        role="dialog"
        style={{ width }}
        tabIndex={-1}
      >
        <header className="modal-header">
          <h2 id={titleId}>{title}</h2>
          {dismissible ? (
            <button
              aria-label="关闭"
              className="icon-button"
              onClick={onClose}
              type="button"
            >
              <X size={16} />
            </button>
          ) : null}
        </header>
        <div className="modal-body">{children}</div>
        {footer ? <footer className="modal-footer">{footer}</footer> : null}
      </section>
    </div>,
    document.body,
  );
}
