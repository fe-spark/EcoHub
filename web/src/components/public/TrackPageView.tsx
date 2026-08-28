"use client";

import { useEffect } from "react";
import { trackPageView } from "@/lib/track-page-view";

export default function TrackPageView({
  action,
  resource,
}: {
  action: "browse" | "search" | "play" | "classify";
  resource?: string;
}) {
  useEffect(() => {
    trackPageView(action, resource);
  }, [action, resource]);
  return null;
}
