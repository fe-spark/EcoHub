"use client";

import { useEffect, useRef, useState } from "react";

export function useContainerWidth(defaultWidth = 600) {
  const ref = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(defaultWidth);

  useEffect(() => {
    if (!ref.current) return;
    const update = () => {
      if (ref.current) {
        const w = ref.current.getBoundingClientRect().width;
        if (w > 50) {
          setWidth(Math.floor(w));
        }
      }
    };
    update();
    const observer = new ResizeObserver(() => update());
    observer.observe(ref.current);
    return () => observer.disconnect();
  }, []);

  return { ref, width };
}
