import { StrictMode, type PropsWithChildren } from "react";
import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  localDateKey,
  millisecondsUntilNextLocalMidnight,
  useLocalCalendar,
} from "./localCalendar";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.useRealTimers();
  delete (document as unknown as { visibilityState?: DocumentVisibilityState })
    .visibilityState;
});

describe("useLocalCalendar", () => {
  it("rolls over at the next browser-local midnight without high-frequency polling", () => {
    vi.useFakeTimers();
    const beforeMidnight = new Date(2026, 7, 31, 23, 59, 59);
    vi.setSystemTime(beforeMidnight);

    const { result } = renderHook(() => useLocalCalendar());

    expect(result.current.dateKey).toBe("2026-08-31");
    expect(vi.getTimerCount()).toBe(1);

    act(() => vi.advanceTimersByTime(1_002));

    expect(result.current.dateKey).toBe("2026-09-01");
    expect(vi.getTimerCount()).toBe(1);
  });

  it("periodically calibrates a changed foreground clock", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 7, 28, 12));
    const { result } = renderHook(() => useLocalCalendar());

    vi.setSystemTime(new Date(2026, 7, 29, 12));
    act(() => vi.advanceTimersByTime(60_001));

    expect(result.current.dateKey).toBe("2026-08-29");
    expect(vi.getTimerCount()).toBe(1);
  });

  it("resynchronizes after focus, pageshow, and becoming visible", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 7, 28, 12));
    let visibilityState: DocumentVisibilityState = "hidden";
    Object.defineProperty(document, "visibilityState", {
      configurable: true,
      get: () => visibilityState,
    });
    const { result } = renderHook(() => useLocalCalendar());

    vi.setSystemTime(new Date(2026, 7, 29, 12));
    act(() => window.dispatchEvent(new Event("focus")));
    expect(result.current.dateKey).toBe("2026-08-29");

    vi.setSystemTime(new Date(2026, 7, 30, 12));
    act(() => window.dispatchEvent(new Event("pageshow")));
    expect(result.current.dateKey).toBe("2026-08-30");

    vi.setSystemTime(new Date(2026, 7, 31, 12));
    act(() => document.dispatchEvent(new Event("visibilitychange")));
    expect(result.current.dateKey).toBe("2026-08-30");

    visibilityState = "visible";
    act(() => document.dispatchEvent(new Event("visibilitychange")));
    expect(result.current.dateKey).toBe("2026-08-31");
    expect(vi.getTimerCount()).toBe(1);
  });

  it("publishes a changed browser time zone on the next foreground sync", () => {
    let timeZone = "Asia/Shanghai";
    vi.spyOn(
      Intl.DateTimeFormat.prototype,
      "resolvedOptions",
    ).mockImplementation(
      () =>
        ({
          calendar: "gregory",
          locale: "zh-CN",
          numberingSystem: "latn",
          timeZone,
        }) as Intl.ResolvedDateTimeFormatOptions,
    );
    const { result } = renderHook(() => useLocalCalendar());

    expect(result.current.timeZone).toBe("Asia/Shanghai");

    timeZone = "America/Los_Angeles";
    act(() => window.dispatchEvent(new Event("focus")));

    expect(result.current.timeZone).toBe("America/Los_Angeles");
  });

  it("reschedules after the clock moves backwards and cleans up in StrictMode", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 7, 31, 12));
    const addWindowListener = vi.spyOn(window, "addEventListener");
    const removeWindowListener = vi.spyOn(window, "removeEventListener");
    const addDocumentListener = vi.spyOn(document, "addEventListener");
    const removeDocumentListener = vi.spyOn(document, "removeEventListener");
    const wrapper = ({ children }: PropsWithChildren) => (
      <StrictMode>{children}</StrictMode>
    );
    const { result, unmount } = renderHook(() => useLocalCalendar(), {
      wrapper,
    });

    vi.setSystemTime(new Date(2026, 7, 30, 12));
    act(() => window.dispatchEvent(new Event("focus")));

    expect(result.current.dateKey).toBe("2026-08-30");
    expect(vi.getTimerCount()).toBe(1);

    unmount();

    expect(vi.getTimerCount()).toBe(0);
    for (const eventName of ["focus", "pageshow"]) {
      expect(
        addWindowListener.mock.calls.filter(([name]) => name === eventName),
      ).toHaveLength(
        removeWindowListener.mock.calls.filter(([name]) => name === eventName)
          .length,
      );
    }
    expect(
      addDocumentListener.mock.calls.filter(
        ([name]) => name === "visibilitychange",
      ),
    ).toHaveLength(
      removeDocumentListener.mock.calls.filter(
        ([name]) => name === "visibilitychange",
      ).length,
    );
  });
});

describe("local calendar utilities", () => {
  it("uses the actual next local midnight duration across offset changes", () => {
    const now = new Date(2026, 2, 8, 0, 0, 0);
    const nextMidnight = new Date(
      now.getFullYear(),
      now.getMonth(),
      now.getDate() + 1,
    );

    expect(millisecondsUntilNextLocalMidnight(now)).toBe(
      nextMidnight.getTime() - now.getTime() + 1,
    );
    expect(localDateKey(nextMidnight)).toBe("2026-03-09");
  });
});
