import React from "react";
import type { PlayLine } from "./types";
export function formatCategoryNodes(nodes: any[]): any[] {
  if (!nodes || nodes.length === 0) return [];
  return nodes.map((node) => ({
    title: node.name,
    label: node.name,
    value: Number(node.id),
    key: String(node.id),
    ...(node.children?.length ? { children: formatCategoryNodes(node.children) } : {}),
  }));
}

export function flattenCategoryNodes(nodes: any[]): any[] {
  const list: any[] = [];
  const walk = (items: any[]) => {
    for (const item of items) {
      list.push(item);
      if (item.children?.length) walk(item.children);
    }
  };
  walk(nodes || []);
  return list;
}

export function resolveCategoryFields(filmData: any, catList: any[]) {
  const desc = filmData.descriptor || {};
  const cName = desc.cName || filmData.cName || "";
  const rawCid = Number(filmData.rawCid || 0);
  const directCid = Number(filmData.cid || 0);
  const matched =
    (rawCid && catList?.find((c: any) => Number(c.id) === rawCid)) ||
    (cName && catList?.find((c: any) => c.name === cName)) ||
    (directCid && catList?.find((c: any) => Number(c.id) === directCid)) ||
    null;
  return {
    cid: matched ? Number(matched.id) : (rawCid || directCid || undefined),
    pid: matched ? Number(matched.pid <= 0 ? matched.id : matched.pid) : Number(filmData.pid || 0),
    cName: matched ? matched.name : cName,
  };
}

export function renderDrawerRequiredMark(
  label: React.ReactNode,
  { required }: { required: boolean }
) {
  if (required) return label;
  if (
    React.isValidElement(label) &&
    (label.props as Record<string, any>)?.[ "data-no-optional" ]
  ) {
    return label;
  }
  return (
    <>
      {label}
      <span className="ant-form-item-optional">（可选）</span>
    </>
  );
}

export function formatEpisodesToPlayLink(linkList: { episode: string; link: string }[]): string {
  if (!Array.isArray(linkList) || linkList.length === 0) return "";
  return linkList.map((item) => `${item.episode}$${item.link}`).join("#");
}

export function parsePlayLinkToEpisodes(raw: string): { episode: string; link: string }[] {
  if (!raw || !raw.trim()) return [];
  const parts = raw.split("#");
  const list: { episode: string; link: string }[] = [];
  for (let i = 0; i < parts.length; i++) {
    const part = parts[i].trim();
    if (!part) continue;
    const dollarIdx = part.indexOf("$");
    if (dollarIdx >= 0) {
      const episode = part.substring(0, dollarIdx).trim() || `第${String(i + 1).padStart(2, "0")}集`;
      const link = part.substring(dollarIdx + 1).trim();
      if (link) {
        list.push({ episode, link });
      }
    } else {
      list.push({ episode: `第${String(i + 1).padStart(2, "0")}集`, link: part });
    }
  }
  return list;
}

export function mergePlayLinesForSave(
  ownerLines: PlayLine[],
  lineLinksMap: Record<string, string>,
  singlePlayLink?: string,
  singlePlayForm?: string,
): { playLink?: string; playFrom?: string[]; playForm?: string } {
  if (ownerLines.length > 1) {
    const playFromList: string[] = [];
    const playLinkList: string[] = [];
    for (const l of ownerLines) {
      const k = l.id || l.name;
      const link =
        lineLinksMap[k] !== undefined
          ? lineLinksMap[k]
          : formatEpisodesToPlayLink(l.linkList || []);
      if (link && link.trim()) {
        playFromList.push(l.name || l.id);
        playLinkList.push(link.trim());
      }
    }
    if (playLinkList.length > 0) {
      return {
        playLink: playLinkList.join("$$$"),
        playFrom: playFromList,
        playForm: playFromList.join("$$$"),
      };
    }
  } else if (singlePlayLink && singlePlayLink.trim()) {
    const singleForm = (singlePlayForm || ownerLines[0]?.name || "默认播放").trim();
    return {
      playLink: singlePlayLink.trim(),
      playFrom: [singleForm],
      playForm: singleForm,
    };
  }
  return {};
}

export function buildFilmDetailFormValues(
  filmData: Record<string, unknown>,
  cat: { cid?: number; pid?: number; cName?: string },
  isCustom: boolean,
  customPic: string,
  slidePic: string,
  ownerLine?: { id?: string; name?: string },
  formattedPlayLink?: string,
) {
  const desc = (filmData.descriptor as Record<string, unknown> | undefined) || {};
  return {
    id: filmData.id,
    sourceId: filmData.sourceId || "",
    name: filmData.name || "",
    subTitle: desc.subTitle || filmData.subTitle || "",
    cid: cat.cid,
    pid: cat.pid,
    cName: cat.cName,
    followPosterSource: !isCustom,
    picture: customPic,
    pictureSlide: slidePic,
    director: desc.director || filmData.director || "",
    actor: desc.actor || filmData.actor || "",
    writer: desc.writer || filmData.writer || "",
    releaseDate: desc.releaseDate || filmData.releaseDate || "",
    area: desc.area || filmData.area || "",
    language: desc.language || filmData.language || "",
    year: desc.year || (filmData.year ? String(filmData.year) : "") || "",
    initial: desc.initial || filmData.initial || "",
    classTag: desc.classTag || filmData.classTag || "",
    remarks: desc.remarks || filmData.remarks || "",
    state: desc.state || filmData.state || "",
    hits: Number(desc.hits || filmData.hits) || 0,
    playForm: ownerLine?.name || ownerLine?.id || "",
    playLink: formattedPlayLink || "",
    dbId: Number(desc.dbId || filmData.dbId) || 0,
    dbScore: desc.dbScore || (filmData.score ? String(filmData.score) : "") || "",
    content: desc.content || filmData.content || desc.blurb || filmData.blurb || "",
  };
}
