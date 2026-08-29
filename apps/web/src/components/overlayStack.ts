interface OverlayEntry {
  order: number;
  restoreTarget: HTMLElement | null;
  root: HTMLElement;
}

const openOverlays = new Map<symbol, OverlayEntry>();
const overlayStackListeners = new Set<() => void>();
const pendingFocusRestores: OverlayEntry[] = [];
let deferredFocusRestores: OverlayEntry[] = [];
let nextOverlayOrder = 0;
let unlockedBodyOverflow: string | null = null;
let focusRestoreScheduled = false;

function notifyOverlayStack(): void {
  [...overlayStackListeners].forEach((listener) => listener());
}

function topmostOverlayEntry(): [symbol, OverlayEntry] | null {
  let topmost: [symbol, OverlayEntry] | null = null;
  openOverlays.forEach((entry, id) => {
    if (!topmost || entry.order > topmost[1].order) {
      topmost = [id, entry];
    }
  });
  return topmost;
}

function hasMeaningfulFocus(root?: HTMLElement): boolean {
  const activeElement = document.activeElement;
  return (
    activeElement instanceof HTMLElement &&
    activeElement !== document.body &&
    activeElement !== document.documentElement &&
    activeElement.isConnected &&
    !activeElement.closest("[inert]") &&
    (!root || root.contains(activeElement))
  );
}

function scheduleFocusRestore(): void {
  if (focusRestoreScheduled) return;
  focusRestoreScheduled = true;
  queueMicrotask(() => {
    focusRestoreScheduled = false;
    const candidates = [
      ...deferredFocusRestores,
      ...pendingFocusRestores.splice(0),
    ].filter((entry) => entry.restoreTarget?.isConnected);
    deferredFocusRestores = [];

    const topmost = topmostOverlayEntry()?.[1] ?? null;
    if (topmost) {
      const targetsInTopmost = [...candidates]
        .sort((left, right) => right.order - left.order)
        .flatMap((entry) =>
          entry.restoreTarget && topmost.root.contains(entry.restoreTarget)
            ? [entry.restoreTarget]
            : [],
        );
      if (!hasMeaningfulFocus(topmost.root)) {
        for (const target of targetsInTopmost) {
          target.focus();
          if (hasMeaningfulFocus(topmost.root)) break;
        }
      }
      if (!hasMeaningfulFocus(topmost.root)) {
        topmost.root.querySelector<HTMLElement>('[role="dialog"]')?.focus();
      }
      deferredFocusRestores = candidates.filter(
        (entry) =>
          entry.restoreTarget && !topmost.root.contains(entry.restoreTarget),
      );
      return;
    }

    if (hasMeaningfulFocus()) return;
    [...candidates]
      .sort((left, right) => left.order - right.order)
      .find(
        (entry) =>
          entry.restoreTarget &&
          !entry.restoreTarget.closest('[data-overlay-root="true"]'),
      )
      ?.restoreTarget?.focus();
  });
}

export function isTopmostOverlay(id: symbol): boolean {
  return topmostOverlayEntry()?.[0] === id;
}

export function subscribeOverlayStack(listener: () => void): () => void {
  overlayStackListeners.add(listener);
  return () => overlayStackListeners.delete(listener);
}

export function registerOverlay(
  id: symbol,
  root: HTMLElement,
  restoreTarget: HTMLElement | null,
): () => void {
  if (openOverlays.size === 0) {
    unlockedBodyOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
  }
  const entry = {
    order: ++nextOverlayOrder,
    restoreTarget,
    root,
  };
  openOverlays.set(id, entry);
  notifyOverlayStack();

  return () => {
    if (!openOverlays.delete(id)) return;
    pendingFocusRestores.push(entry);
    if (openOverlays.size === 0) {
      document.body.style.overflow = unlockedBodyOverflow ?? "";
      unlockedBodyOverflow = null;
    }
    notifyOverlayStack();
    scheduleFocusRestore();
  };
}
