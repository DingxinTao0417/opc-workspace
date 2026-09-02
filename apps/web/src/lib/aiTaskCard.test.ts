import { describe, expect, it } from "vitest";

import { parseAiTaskSuggestion, stripAiTaskBlock } from "./aiTaskCard";

describe("parseAiTaskSuggestion", () => {
  it("parses a well-formed suggestion block", () => {
    const content =
      '好的，建议如下\n[opc:task]{"title":"写周报","description":"汇总本周进展","due":"2026-09-02"}[/opc:task]';
    expect(parseAiTaskSuggestion(content)).toEqual({
      title: "写周报",
      description: "汇总本周进展",
      due: "2026-09-02",
    });
  });

  it("returns null when no block exists", () => {
    expect(parseAiTaskSuggestion("今天没有任务建议。")).toBeNull();
  });

  it("returns null for malformed JSON", () => {
    expect(
      parseAiTaskSuggestion("[opc:task]{title:写周报}[/opc:task]"),
    ).toBeNull();
  });

  it("returns null when the title is missing or blank", () => {
    expect(
      parseAiTaskSuggestion('[opc:task]{"description":"无标题"}[/opc:task]'),
    ).toBeNull();
    expect(
      parseAiTaskSuggestion('[opc:task]{"title":"   "}[/opc:task]'),
    ).toBeNull();
  });

  it("drops fields that do not match their expected shape", () => {
    const suggestion = parseAiTaskSuggestion(
      '[opc:task]{"title":"写周报","due":"下周二","description":42}[/opc:task]',
    );
    expect(suggestion).toEqual({ title: "写周报" });
  });

  it("only reads the first block", () => {
    const content =
      '[opc:task]{"title":"第一个"}[/opc:task] 中间 [opc:task]{"title":"第二个"}[/opc:task]';
    expect(parseAiTaskSuggestion(content)?.title).toBe("第一个");
  });
});

describe("stripAiTaskBlock", () => {
  it("removes the block and trailing whitespace without touching other text", () => {
    const content = '回复正文。\n[opc:task]{"title":"写周报"}[/opc:task]\n';
    expect(stripAiTaskBlock(content)).toBe("回复正文。");
  });

  it("keeps plain replies unchanged", () => {
    expect(stripAiTaskBlock("普通回答")).toBe("普通回答");
  });
});
