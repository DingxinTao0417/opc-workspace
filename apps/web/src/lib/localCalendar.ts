import { useEffect, useRef, useState } from "react";

export interface LocalCalendarSnapshot {
  dateKey: string;
  timeZone: string;
}

const foregroundCalibrationIntervalMs = 60_000;

export function localDateKey(date = new Date()): string {
  const year = String(date.getFullYear()).padStart(4, "0");
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

export function localDateFromKey(dateKey: string): Date {
  const [year, month, day] = dateKey.split("-").map(Number);
  return new Date(year, month - 1, day);
}

export function localTimeZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
}

export function localCalendarSnapshot(now = new Date()): LocalCalendarSnapshot {
  return { dateKey: localDateKey(now), timeZone: localTimeZone() };
}

export function millisecondsUntilNextLocalMidnight(now = new Date()): number {
  const nextMidnight = new Date(
    now.getFullYear(),
    now.getMonth(),
    now.getDate() + 1,
  );
  return Math.max(1, nextMidnight.getTime() - now.getTime() + 1);
}

export function useLocalCalendar(): LocalCalendarSnapshot {
  const [snapshot, setSnapshot] = useState(localCalendarSnapshot);
  const snapshotRef = useRef(snapshot);

  useEffect(() => {
    let disposed = false;
    let timer: number | undefined;

    const schedule = (now: Date) => {
      if (timer !== undefined) window.clearTimeout(timer);
      const midnightDelay = millisecondsUntilNextLocalMidnight(now);
      timer = window.setTimeout(
        sync,
        document.visibilityState === "visible"
          ? Math.min(midnightDelay, foregroundCalibrationIntervalMs)
          : midnightDelay,
      );
    };

    const sync = () => {
      if (disposed) return;
      const now = new Date();
      const nextSnapshot = localCalendarSnapshot(now);
      const current = snapshotRef.current;
      if (
        current.dateKey !== nextSnapshot.dateKey ||
        current.timeZone !== nextSnapshot.timeZone
      ) {
        snapshotRef.current = nextSnapshot;
        setSnapshot(nextSnapshot);
      }
      schedule(now);
    };

    const syncWhenVisible = () => {
      if (document.visibilityState === "visible") sync();
    };

    window.addEventListener("focus", sync);
    window.addEventListener("pageshow", sync);
    document.addEventListener("visibilitychange", syncWhenVisible);
    sync();

    return () => {
      disposed = true;
      if (timer !== undefined) window.clearTimeout(timer);
      window.removeEventListener("focus", sync);
      window.removeEventListener("pageshow", sync);
      document.removeEventListener("visibilitychange", syncWhenVisible);
    };
  }, []);

  return snapshot;
}
