"use client";

import React from "react";
import styles from "./index.module.less";

type HighlightMatchedTextProps = {
  text?: string;
  query?: string;
  className?: string;
};

function collectTokens(query: string): string[] {
  const raw = query
    .split(/[\s,，·:：\-—_/\\]+/)
    .map((t) => t.trim())
    .filter(Boolean);
  const unique: string[] = [];
  const seen = new Set<string>();
  for (const token of raw) {
    const key = token.toLowerCase();
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    unique.push(token);
  }
  unique.sort((a, b) => b.length - a.length);
  return unique;
}

export default function HighlightMatchedText({
  text,
  query,
  className,
}: HighlightMatchedTextProps) {
  if (!text) {
    return null;
  }
  const trimmed = (query || "").trim();
  if (!trimmed) {
    return <>{text}</>;
  }

  const tokens = collectTokens(trimmed);
  if (tokens.length === 0) {
    return <>{text}</>;
  }

  const escapedTokens = tokens
    .map((t) => t.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"))
    .join("|");

  let splitRe: RegExp;
  try {
    splitRe = new RegExp(`(${escapedTokens})`, "gi");
  } catch {
    return <>{text}</>;
  }

  const tokenSet = new Set(tokens.map((t) => t.toLowerCase()));
  const parts = text.split(splitRe);
  return (
    <>
      {parts.map((part, i) =>
        part && tokenSet.has(part.toLowerCase()) ? (
          <span key={i} className={className || styles.highlightMatched}>
            {part}
          </span>
        ) : (
          <React.Fragment key={i}>{part}</React.Fragment>
        ),
      )}
    </>
  );
}
