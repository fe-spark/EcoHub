"use client";

import React from "react";
import ReactMarkdown, { defaultUrlTransform } from "react-markdown";

interface NoticeMarkdownProps {
  content: string;
}

/** CommonMark 会折叠单个换行；给非代码块的单换行补 hard break，对齐旧 pre-wrap 与 OHOS 逐行展示。 */
function preserveMarkdownLineBreaks(content: string): string {
  const normalized = content.replace(/\r\n/g, "\n");
  return normalized
    .split(/(```[\s\S]*?```)/)
    .map((part, index) =>
      index % 2 === 1 ? part : part.replace(/([^\n])\n(?!\n)/g, "$1  \n")
    )
    .join("");
}

function noticeUrlTransform(url: string, key: string): string {
  const sanitized = defaultUrlTransform(url).trim();
  if (!sanitized) return "";
  if (key === "src") {
    return /^https:\/\//i.test(sanitized) ? sanitized : "";
  }
  return /^(https?:\/\/|mailto:)/i.test(sanitized) ? sanitized : "";
}

export default function NoticeMarkdown({ content }: NoticeMarkdownProps) {
  return (
    <ReactMarkdown
      urlTransform={noticeUrlTransform}
      components={{
        a: ({ href, children }) => {
          if (!href) {
            return <span>{children}</span>;
          }
          return (
            <a href={href} target="_blank" rel="noopener noreferrer">
              {children}
            </a>
          );
        },
        img: ({ src, alt }) => {
          if (!src) return null;
          // 公告图为任意 https 外链，无法预配 next/image remotePatterns
          // eslint-disable-next-line @next/next/no-img-element
          return <img src={src} alt={alt ?? ""} />;
        },
      }}
    >
      {preserveMarkdownLineBreaks(content)}
    </ReactMarkdown>
  );
}
