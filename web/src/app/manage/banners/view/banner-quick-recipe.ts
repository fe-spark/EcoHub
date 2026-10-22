import { CategoryQuotaItem, QuickStrategy } from "./banner-quick-config";

export interface BannerQuickRecipe {
  saved: boolean;
  strategy: QuickStrategy;
  autoScrape: boolean;
  quotas: { categoryId: number; count: number }[];
}

export function normalizeQuickStrategy(value?: string): QuickStrategy {
  if (value === "score" || value === "latest") return value;
  return "hot";
}

export function mergeBannerQuickCategories(
  nav: { id: number; name: string }[],
  recipe: BannerQuickRecipe | null,
): CategoryQuotaItem[] {
  const counts = new Map<number, number>();
  if (recipe?.saved) {
    for (const item of recipe.quotas || []) {
      const id = Number(item.categoryId);
      if (id > 0) counts.set(id, Math.max(0, Number(item.count) || 0));
    }
  }
  return nav.map((item) => ({
    id: item.id,
    name: item.name,
    count: recipe?.saved ? counts.get(item.id) ?? 0 : 1,
  }));
}

export function positiveQuotas(categories: CategoryQuotaItem[]) {
  return categories
    .filter((item) => item.id > 0 && (item.count || 0) > 0)
    .map((item) => ({ categoryId: item.id, count: item.count }));
}

export function quickRecipeSnapshot(
  strategy: QuickStrategy,
  categories: CategoryQuotaItem[],
) {
  const quotas = positiveQuotas(categories)
    .map((item) => `${item.categoryId}:${item.count}`)
    .join(",");
  return `${strategy}|${quotas}`;
}
