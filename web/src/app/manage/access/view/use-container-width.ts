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

export function useContainerSize(defaultWidth = 400, defaultHeight = 240) {
  const [node, setNode] = useState<HTMLDivElement | null>(null);
  const [size, setSize] = useState({ width: defaultWidth, height: defaultHeight });
  const ref = useCallback((el: HTMLDivElement | null) => {
    if (el) {
      const rect = el.getBoundingClientRect();
      const w = Math.floor(rect.width);
      const h = Math.floor(rect.height);
      if (w > 0 && h > 0) {
        setSize((prev) => (prev.width === w && prev.height === h ? prev : { width: w, height: h }));
      }
    }
    setNode(el);
  }, []);

  useEffect(() => {
    if (!node) return;
    const update = () => {
      const rect = node.getBoundingClientRect();
      const w = Math.floor(rect.width);
      const h = Math.floor(rect.height);
      if (w > 0 && h > 0) {
        setSize((prev) => (prev.width === w && prev.height === h ? prev : { width: w, height: h }));
      }
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(node);
    return () => observer.disconnect();
  }, [node]);

  return { ref, width: size.width, height: size.height };
}

