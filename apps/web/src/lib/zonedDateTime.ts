export type ZonedDateTimeConversion =
  | { iso: string; ambiguous: boolean; kind: "valid" }
  | { kind: "invalid" | "nonexistent" };

interface LocalDateTimeParts {
  year: number;
  month: number;
  day: number;
  hour: number;
  minute: number;
}

function parseLocalDateTime(value: string): LocalDateTimeParts | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/.exec(value);
  if (!match) return null;
  const [year, month, day, hour, minute] = match.slice(1).map(Number);
  const probe = new Date(Date.UTC(year, month - 1, day, hour, minute));
  if (
    probe.getUTCFullYear() !== year ||
    probe.getUTCMonth() !== month - 1 ||
    probe.getUTCDate() !== day ||
    probe.getUTCHours() !== hour ||
    probe.getUTCMinutes() !== minute
  ) {
    return null;
  }
  return { year, month, day, hour, minute };
}

function timeZoneFormatter(timeZone: string): Intl.DateTimeFormat {
  return new Intl.DateTimeFormat("en-CA", {
    timeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
  });
}

function partsInTimeZone(
  date: Date,
  timeZone: string,
): LocalDateTimeParts | null {
  try {
    const parts = timeZoneFormatter(timeZone).formatToParts(date);
    const values = Object.fromEntries(
      parts
        .filter((part) => part.type !== "literal")
        .map((part) => [part.type, Number(part.value)]),
    );
    if (
      !Number.isInteger(values.year) ||
      !Number.isInteger(values.month) ||
      !Number.isInteger(values.day) ||
      !Number.isInteger(values.hour) ||
      !Number.isInteger(values.minute)
    ) {
      return null;
    }
    return {
      year: values.year,
      month: values.month,
      day: values.day,
      hour: values.hour,
      minute: values.minute,
    };
  } catch {
    return null;
  }
}

function isSameLocalTime(
  left: LocalDateTimeParts,
  right: LocalDateTimeParts,
): boolean {
  return (
    left.year === right.year &&
    left.month === right.month &&
    left.day === right.day &&
    left.hour === right.hour &&
    left.minute === right.minute
  );
}

function pad(value: number): string {
  return String(value).padStart(2, "0");
}

export function formatDateTimeLocalInTimeZone(
  value: string | Date,
  timeZone: string,
): string {
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const parts = partsInTimeZone(date, timeZone);
  return parts
    ? `${parts.year}-${pad(parts.month)}-${pad(parts.day)}T${pad(parts.hour)}:${pad(parts.minute)}`
    : "";
}

// HTML datetime-local values are wall-clock values. Resolve them against the
// explicitly selected IANA zone instead of the browser's zone. When DST falls
// back, the earlier physical instant is used consistently; spring-forward gaps
// are rejected so a plan is never silently shifted to another wall time.
export function localDateTimeToZonedISOString(
  value: string,
  timeZone: string,
): ZonedDateTimeConversion {
  const local = parseLocalDateTime(value);
  if (!local || !timeZone.trim()) return { kind: "invalid" };
  const nominalUtc = Date.UTC(
    local.year,
    local.month - 1,
    local.day,
    local.hour,
    local.minute,
  );
  const candidates: number[] = [];
  // IANA offsets are within this range. Checking each minute keeps uncommon
  // 30/45-minute offsets correct and only runs when a plan is saved.
  for (
    let offsetMinutes = -16 * 60;
    offsetMinutes <= 16 * 60;
    offsetMinutes++
  ) {
    const candidate = new Date(nominalUtc + offsetMinutes * 60_000);
    const parts = partsInTimeZone(candidate, timeZone);
    if (parts && isSameLocalTime(parts, local))
      candidates.push(candidate.getTime());
  }
  if (candidates.length === 0) return { kind: "nonexistent" };
  candidates.sort((left, right) => left - right);
  return {
    kind: "valid",
    iso: new Date(candidates[0]).toISOString(),
    ambiguous: candidates.length > 1,
  };
}
