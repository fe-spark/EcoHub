import { CategoryQuotaItem } from "./banner-quick-config";

export function positiveQuotas(categories: CategoryQuotaItem[]) {
  return categories
    .filter((item) => item.id > 0 && (item.count || 0) > 0)
    .map((item) => ({ categoryId: item.id, count: item.count }));
}
