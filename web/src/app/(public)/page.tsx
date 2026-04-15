import HomePageClient from "./HomePageClient";
import { serverGet } from "@/lib/server-api";

async function getHomeData() {
  try {
    const response = await serverGet<{
      banners: any[];
      content: any[];
    }>("/index");

    if (response.code === 0 && response.data) {
      return response.data;
    }
  } catch (error) {
    console.error("fetch home data error:", error);
  }

  return {
    banners: [],
    content: [],
  };
}

export default async function HomePage() {
  const data = await getHomeData();

  return <HomePageClient data={data} />;
}
