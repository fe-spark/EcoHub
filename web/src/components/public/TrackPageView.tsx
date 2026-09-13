"use client";

import { useEffect } from "react";
import { trackPageView } from "@/lib/track-page-view";

export default function TrackPageView({
  action,
  resource,
  source = "web",
  path,
  resourceTitle,
  resourcePoster,
  resourceCat,
}: {
  action: "browse" | "search" | "play" | "classify";
  resource?: string;
  source?: string;
  path?: string;
  resourceTitle?: string;
  resourcePoster?: string;
  resourceCat?: string;
}) {
  useEffect(() => {
    trackPageView({
      action,
      resource,
      source,
      path,
      resource_title: resourceTitle,
      resource_poster: resourcePoster,
      resource_cat: resourceCat,
    });
  }, [action, resource, source, path, resourceTitle, resourcePoster, resourceCat]);
  return null;
}
