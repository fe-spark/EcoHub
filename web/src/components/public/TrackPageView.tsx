"use client";

import { useEffect } from "react";
import { trackPageView } from "@/lib/track-page-view";

export default function TrackPageView({
  action,
  resource,
  source = "web",
  path,
}: {
  action: "browse" | "search" | "play" | "classify";
  resource?: string;
  source?: string;
  path?: string;
}) {
  useEffect(() => {
    trackPageView(action, resource, source, path);
  }, [action, resource, source, path]);
  return null;
}
