import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import {
  aiWorkspaceHref,
  isAiWorkspaceRoute,
  parseAiWorkspaceToolRoute,
} from "../lib/aiWorkspaceLinks";
import { useUiStore } from "../store/ui";

// 极简只读 markdown 渲染：段落、有序/无序列表、**加粗**、`code`。
// 只生成 React 节点（无 dangerouslySetInnerHTML），模型输出天然被转义。
function inlineMarkdown(
  text: string,
  keyPrefix: string,
  sessionId?: string,
): ReactNode[] {
  return text
    .split(/(\*\*[^*]+\*\*|`[^`]+`|\[[^\]\n]+\]\([^\s)]+\))/g)
    .filter((part) => part !== "")
    .map((part, index) => {
      const key = `${keyPrefix}-${index}`;
      const link = /^\[([^\]]+)\]\(([^)]+)\)$/.exec(part);
      if (link && isAiWorkspaceRoute(link[2])) {
        const workspaceTool = parseAiWorkspaceToolRoute(link[2]);
        return (
          <Link
            key={key}
            to={workspaceTool ? "/ai" : aiWorkspaceHref(link[2], sessionId)}
            className={workspaceTool ? "ai-workspace-tool-link" : undefined}
            title={workspaceTool ? "打开右侧工作区" : undefined}
            onClick={() => {
              if (!workspaceTool) return;
              useUiStore.setState({ rightOverviewCollapsed: false });
              useUiStore.getState().setRightPanelTab(workspaceTool);
            }}
          >
            {link[1]}
          </Link>
        );
      }
      if (part.startsWith("**") && part.endsWith("**") && part.length > 4) {
        return <strong key={key}>{part.slice(2, -2)}</strong>;
      }
      if (part.startsWith("`") && part.endsWith("`") && part.length > 2) {
        return <code key={key}>{part.slice(1, -1)}</code>;
      }
      return <span key={key}>{part}</span>;
    });
}

export function renderAiRichText(
  content: string,
  sessionId?: string,
): ReactNode[] {
  const lines = content.split(/\r?\n/);
  const blocks: ReactNode[] = [];
  let paragraph: string[] = [];
  let list: { ordered: boolean; items: string[] } | null = null;
  let blockKey = 0;

  const flushParagraph = () => {
    if (paragraph.length > 0) {
      const key = `p-${blockKey++}`;
      blocks.push(
        <p key={key}>{inlineMarkdown(paragraph.join(" "), key, sessionId)}</p>,
      );
      paragraph = [];
    }
  };
  const flushList = () => {
    if (list) {
      const key = `l-${blockKey++}`;
      const items = list.items.map((item, index) => (
        <li key={`${key}-${index}`}>
          {inlineMarkdown(item, `${key}-${index}`, sessionId)}
        </li>
      ));
      blocks.push(
        list.ordered ? <ol key={key}>{items}</ol> : <ul key={key}>{items}</ul>,
      );
      list = null;
    }
  };

  for (const raw of lines) {
    const line = raw.trimEnd();
    const ordered = /^(\d+)[.、)]\s+/.exec(line);
    const unordered = /^[-*•]\s+/.exec(line);
    if (!line.trim()) {
      flushParagraph();
      flushList();
      continue;
    }
    if (ordered) {
      flushParagraph();
      if (!list || !list.ordered) {
        flushList();
        list = { ordered: true, items: [] };
      }
      list.items.push(line.slice(ordered[0].length));
      continue;
    }
    if (unordered) {
      flushParagraph();
      if (list && list.ordered) {
        flushList();
      }
      if (!list) {
        list = { ordered: false, items: [] };
      }
      list.items.push(line.slice(unordered[0].length));
      continue;
    }
    flushList();
    paragraph.push(line.trim());
  }
  flushParagraph();
  flushList();
  return blocks;
}
