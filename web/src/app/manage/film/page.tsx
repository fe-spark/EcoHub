import { Suspense } from "react";
import FilmListPageView from "./view";

export default function FilmListPage() {
  return (
    <Suspense fallback={null}>
      <FilmListPageView />
    </Suspense>
  );
}
