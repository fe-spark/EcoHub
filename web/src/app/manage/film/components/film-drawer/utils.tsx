import React from "react";

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
