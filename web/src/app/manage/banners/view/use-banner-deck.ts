"use client";

import { useCallback, useMemo, useRef, useState } from "react";
import { BannerRecord } from "./types";

function orderKey(list: BannerRecord[]) {
  return list.map((item) => String(item.id)).join("\0");
}

/** 服务端字段按本地顺序套回；已删除的丢掉，新增的接到末尾。 */
export function rebaseBannerDraft(draft: BannerRecord[], serverList: BannerRecord[]) {
  const byId = new Map(serverList.map((item) => [String(item.id), item]));
  const used = new Set<string>();
  const next: BannerRecord[] = [];
  for (const item of draft) {
    const fresh = byId.get(String(item.id));
    if (!fresh) continue;
    next.push(fresh);
    used.add(String(item.id));
  }
  for (const item of serverList) {
    const id = String(item.id);
    if (!used.has(id)) next.push(item);
  }
  return next;
}

export function useBannerDeck() {
  const [banners, setBannersState] = useState<BannerRecord[]>([]);
  const [savedBanners, setSavedBannersState] = useState<BannerRecord[]>([]);
  const bannersRef = useRef(banners);
  const savedRef = useRef(savedBanners);

  const setBanners = useCallback((updater: BannerRecord[] | ((prev: BannerRecord[]) => BannerRecord[])) => {
    const prev = bannersRef.current;
    const next = typeof updater === "function" ? updater(prev) : updater;
    bannersRef.current = next;
    setBannersState(next);
  }, []);

  const replaceList = useCallback((list: BannerRecord[]) => {
    bannersRef.current = list;
    savedRef.current = list;
    setBannersState(list);
    setSavedBannersState(list);
  }, []);

  const absorbServerList = useCallback((serverList: BannerRecord[]) => {
    const dirty = orderKey(bannersRef.current) !== orderKey(savedRef.current);
    const nextDraft = dirty ? rebaseBannerDraft(bannersRef.current, serverList) : serverList;
    bannersRef.current = nextDraft;
    savedRef.current = serverList;
    setBannersState(nextDraft);
    setSavedBannersState(serverList);
  }, []);

  const discardOrder = useCallback(() => {
    const saved = savedRef.current;
    bannersRef.current = saved;
    setBannersState(saved);
  }, []);

  const patchByMid = useCallback((mid: number, patch: Partial<BannerRecord>) => {
    const apply = (list: BannerRecord[]) =>
      list.map((item) => (Number(item.mid) === Number(mid) ? { ...item, ...patch } : item));
    const nextDraft = apply(bannersRef.current);
    const nextSaved = apply(savedRef.current);
    bannersRef.current = nextDraft;
    savedRef.current = nextSaved;
    setBannersState(nextDraft);
    setSavedBannersState(nextSaved);
  }, []);

  const orderDirty = useMemo(
    () => orderKey(banners) !== orderKey(savedBanners),
    [banners, savedBanners],
  );

  return {
    banners,
    bannersRef,
    orderDirty,
    setBanners,
    replaceList,
    absorbServerList,
    discardOrder,
    patchByMid,
  };
}
