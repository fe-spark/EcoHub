"use client";

import { useCallback, useEffect, useState } from "react";

export function useContainerWidth(defaultWidth = 600) {
  const [node, setNode] = useState<HTMLDivElement | null>(null);
  const [width, setWidth] = useState(defaultWidth);
  const ref = useCallback((el: HTMLDivElement | null) => {
    setNode(el);
  }, []);

  useEffect(() => {
    if (!node) return;
    const update = () => {
      const w = node.getBoundingClientRect().width;
      if (w > 50) {
        setWidth(Math.floor(w));
      }
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(node);
    return () => observer.disconnect();
  }, [node]);

  return { ref, width };
}
