import { describe, expect, it } from "vitest";

import {
  parseAiMemorySuggestion,
  parseAiTaskSuggestion,
  stripAiSelfCheckBlock,
  stripAiTaskBlock,
} from "./aiTaskCard";

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

describe("parseAiMemorySuggestion", () => {
  it("parses a well-formed memory block", () => {
    const content =
      '好的，我记下了。[opc:memory]{"content":"回答保持简洁"}[/opc:memory]';
    expect(parseAiMemorySuggestion(content)).toEqual({
      content: "回答保持简洁",
    });
  });

  it("rejects malformed, missing-content, and oversized blocks", () => {
    expect(
      parseAiMemorySuggestion("[opc:memory]not-json[/opc:memory]"),
    ).toBeNull();
    expect(
      parseAiMemorySuggestion('[opc:memory]{"title":"x"}[/opc:memory]'),
    ).toBeNull();
    expect(
      parseAiMemorySuggestion(
        `[opc:memory]{"content":"${"长".repeat(501)}"}[/opc:memory]`,
      ),
    ).toBeNull();
    expect(parseAiMemorySuggestion("没有记忆块的普通回答")).toBeNull();
  });

  it("strips both suggestion blocks from the displayed reply", () => {
    const content =
      '正文[opc:task]{"title":"t"}[/opc:task]结尾[opc:memory]{"content":"m"}[/opc:memory]';
    expect(stripAiTaskBlock(content)).toBe("正文结尾");
  });
});

describe("stripAiSelfCheckBlock", () => {
  it("removes the internal self-check verdict from streamed text", () => {
    expect(
      stripAiSelfCheckBlock(
        '草稿正文[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]',
      ),
    ).toBe("草稿正文");
    expect(
      stripAiSelfCheckBlock(
        '草稿[opc:selfcheck]{"sufficient":false,"note":"x"}',
      ),
    ).toBe("草稿");
    expect(stripAiSelfCheckBlock("普通流式文本")).toBe("普通流式文本");
  });
});
