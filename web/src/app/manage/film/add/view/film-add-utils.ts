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
