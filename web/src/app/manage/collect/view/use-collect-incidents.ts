"use client";

import { useCallback, useState } from "react";
import { ApiGet } from "@/lib/client-api";
import type { SourceIncident } from "./types";

export function useCollectIncidents() {
  const [incidentMap, setIncidentMap] = useState<Map<string, SourceIncident>>(new Map());

  const refreshIncidents = useCallback(async () => {
    try {
      const resp = await ApiGet<{ list: SourceIncident[] }>("/manage/collect/history/incidents");
      if (resp.code === 0 && Array.isArray(resp.data?.list)) {
        const nextMap = new Map<string, SourceIncident>();
        for (const item of resp.data.list) {
          if (item.sourceId) {
            nextMap.set(item.sourceId, item);
          }
        }
        setIncidentMap(nextMap);
      }
    } catch {
      // 网络错误已由请求拦截器提示
    }
  }, []);

  return { incidentMap, refreshIncidents };
}
