"use client";

import { useState, useEffect } from "react";
import { ApiGet } from "./client-api";

export function useTmdbEnabled(): boolean {
  const [enabled, setEnabled] = useState(false);

  useEffect(() => {
    let mounted = true;
    ApiGet("/manage/tmdb/config")
      .then((resp: any) => {
        if (mounted && resp?.code === 0) {
          const cfg = resp.data;
          setEnabled(Boolean(cfg?.enabled && cfg?.apiKey?.trim()));
        }
      })
      .catch(() => {
        if (mounted) {
          setEnabled(false);
        }
      });
    return () => {
      mounted = false;
    };
  }, []);

  return enabled;
}
