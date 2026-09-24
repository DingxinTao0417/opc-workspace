// A bounded linear full-context diff. The changed middle is a replacement
// block (not necessarily minimal). No text is omitted, normalized or executed.
export function fileReviewDiff(before: string, after: string) {
  const lines = (text: string) => text.match(/[^\n]*\n|[^\n]+$/g) ?? [];
  const oldLines = lines(before);
  const newLines = lines(after);
  let start = 0;
  while (
    start < oldLines.length &&
    start < newLines.length &&
    oldLines[start] === newLines[start]
  )
    start++;
  let end = 0;
  while (
    end < oldLines.length - start &&
    end < newLines.length - start &&
    oldLines[oldLines.length - 1 - end] === newLines[newLines.length - 1 - end]
  )
    end++;
  return [
    { kind: "same", lines: oldLines.slice(0, start) },
    { kind: "removed", lines: oldLines.slice(start, oldLines.length - end) },
    { kind: "added", lines: newLines.slice(start, newLines.length - end) },
    { kind: "same", lines: oldLines.slice(oldLines.length - end) },
  ] as const;
}

export function visibleFileLine(line: string) {
  return JSON.stringify(line)
    .slice(1, -1)
    .replace(
      /[\u007f-\u009f\u061c\u200b-\u200f\u2028-\u202e\u2060-\u206f\ufeff]/g,
      (char) => `\\u${char.charCodeAt(0).toString(16).padStart(4, "0")}`,
    );
}
