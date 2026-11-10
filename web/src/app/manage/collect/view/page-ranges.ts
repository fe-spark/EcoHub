/** 页码去重、按数值排序，连续页压成区间：[5, 1, 2, 3, 7] → "1-3、5、7"。非正整数忽略。 */
export function formatPageRanges(pages?: readonly number[] | null): string {
  if (!pages || pages.length === 0) return "";
  const sorted = Array.from(
    new Set(pages.filter((page) => Number.isInteger(page) && page > 0)),
  ).sort((a, b) => a - b);
  const parts: string[] = [];
  let start = sorted[0];
  let prev = sorted[0];
  for (let i = 1; i <= sorted.length; i += 1) {
    const page = sorted[i];
    if (page === prev + 1) {
      prev = page;
      continue;
    }
    if (start !== undefined) parts.push(start === prev ? `${start}` : `${start}-${prev}`);
    start = page;
    prev = page;
  }
  return parts.join("、");
}

/** 去重后的有效页数。 */
export function countPages(pages?: readonly number[] | null): number {
  if (!pages || pages.length === 0) return 0;
  return new Set(pages.filter((page) => Number.isInteger(page) && page > 0)).size;
}
