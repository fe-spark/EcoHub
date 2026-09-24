export interface MarkdownActionResult {
  newContent: string;
  selectionStart: number;
  selectionEnd: number;
}

/**
 * 智能切换包裹类 Markdown 标记（如粗体 **、斜体 *、行内代码 `）
 * 若已包裹则取消（Unwrap），未包裹则添加（Wrap）
 */
export function toggleMarkdownWrap(
  content: string,
  start: number,
  end: number,
  tag: string,
  placeholder: string = "文本"
): MarkdownActionResult {
  const tagLen = tag.length;
  const selected = content.substring(start, end);

  // 1. 选区内部已包含 tag：例如选区就是 "**文字**"
  if (
    selected.length >= tagLen * 2 &&
    selected.startsWith(tag) &&
    selected.endsWith(tag)
  ) {
    // 针对单个 * 的斜体，避免把 ** 当作 * 解包
    const isDoubleAsterisk =
      tag === "*" && (selected.startsWith("**") || selected.endsWith("**"));
    if (!isDoubleAsterisk) {
      const unwrapped = selected.slice(tagLen, -tagLen);
      const newContent =
        content.substring(0, start) + unwrapped + content.substring(end);
      return {
        newContent,
        selectionStart: start,
        selectionEnd: start + unwrapped.length,
      };
    }
  }

  // 2. 选区外部刚好紧贴 tag：例如 "**|文字|**"
  const before = content.substring(start - tagLen, start);
  const after = content.substring(end, end + tagLen);
  if (before === tag && after === tag) {
    const isDoubleAsterisk =
      tag === "*" &&
      (content.substring(start - 2, start) === "**" ||
        content.substring(end, end + 2) === "**");
    if (!isDoubleAsterisk) {
      const newContent =
        content.substring(0, start - tagLen) +
        selected +
        content.substring(end + tagLen);
      return {
        newContent,
        selectionStart: start - tagLen,
        selectionEnd: start - tagLen + selected.length,
      };
    }
  }

  // 3. 未被包裹：添加包裹标记
  const targetText = selected || placeholder;
  const wrapped = `${tag}${targetText}${tag}`;
  const newContent =
    content.substring(0, start) + wrapped + content.substring(end);
  return {
    newContent,
    selectionStart: start + tagLen,
    selectionEnd: start + tagLen + targetText.length,
  };
}

/**
 * 智能切换行首类 Markdown 标记（如标题 ### 、列表 - ）
 * 若所在行已带前缀则移除（Toggle off），否则添加（Toggle on）
 */
export function toggleLinePrefix(
  content: string,
  start: number,
  end: number,
  prefix: string,
  placeholder: string = "内容"
): MarkdownActionResult {
  const lineStart = content.lastIndexOf("\n", start - 1) + 1;
  let lineEnd = content.indexOf("\n", end);
  if (lineEnd === -1) {
    lineEnd = content.length;
  }

  const lineText = content.substring(lineStart, lineEnd);

  // 1. 如果当前行已经有该前缀，则移除
  if (lineText.startsWith(prefix)) {
    const nextLineText = lineText.slice(prefix.length);
    const newContent =
      content.substring(0, lineStart) + nextLineText + content.substring(lineEnd);
    const diff = prefix.length;
    return {
      newContent,
      selectionStart: Math.max(lineStart, start - diff),
      selectionEnd: Math.max(lineStart, end - diff),
    };
  }

  // 2. 如果未包含，则在行首添加前缀
  const nextLineText = lineText
    ? `${prefix}${lineText}`
    : `${prefix}${placeholder}`;
  const newContent =
    content.substring(0, lineStart) + nextLineText + content.substring(lineEnd);
  const diff = prefix.length;

  return {
    newContent,
    selectionStart: start + diff,
    selectionEnd:
      (lineText ? end : lineStart + nextLineText.length) + (lineText ? diff : 0),
  };
}

/**
 * 智能切换超链接标记 [text](url)
 * 若已是链接则解开为纯文本，否则包裹为超链接
 */
export function toggleMarkdownLink(
  content: string,
  start: number,
  end: number,
  defaultUrl: string = "https://example.com",
  defaultText: string = "链接文字"
): MarkdownActionResult {
  const selected = content.substring(start, end);
  const linkRegex = /^\[(.*?)\]\((.*?)\)$/;

  // 1. 如果选中内容已是链接，则解开为纯文本
  const match = selected.match(linkRegex);
  if (match) {
    const rawText = match[1] || match[2];
    const newContent =
      content.substring(0, start) + rawText + content.substring(end);
    return {
      newContent,
      selectionStart: start,
      selectionEnd: start + rawText.length,
    };
  }

  // 2. 否则添加链接
  const linkText = selected || defaultText;
  const replacement = `[${linkText}](${defaultUrl})`;
  const newContent =
    content.substring(0, start) + replacement + content.substring(end);
  return {
    newContent,
    selectionStart: start + 1,
    selectionEnd: start + 1 + linkText.length,
  };
}
