import { Alert } from "antd";
import HomePageView from "./home-view";
import {
  getPublicServerErrorMessage,
  isServerUnauthorizedError,
  redirectToLogin,
  serverGet,
} from "@/lib/server-api";

type HomeLoadResult =
  | { status: "ok"; data: { banners: any[]; recommends: any[] } }
  | { status: "unauthorized" }
  | { status: "error"; message: string };

async function getHomeData(): Promise<HomeLoadResult> {
  try {
    const response = await serverGet<{
      banners: any[];
      recommends?: any[];
    }>("/home");

    if (response.code === 0 && response.data) {
      return {
        status: "ok",
        data: {
          banners: response.data.banners ?? [],
          recommends: response.data.recommends ?? [],
        },
      };
    }

    return {
      status: "error",
      message: response.msg || "首页数据获取失败，请稍后重试",
    };
  } catch (error) {
    if (isServerUnauthorizedError(error)) {
      return { status: "unauthorized" };
    }
    return {
      status: "error",
      message: getPublicServerErrorMessage(error),
    };
  }
}

export default async function HomePage() {
  const result = await getHomeData();

  if (result.status === "unauthorized") {
    return await redirectToLogin();
  }

  if (result.status === "error") {
    return (
      <Alert
        type="error"
        showIcon
        title="首页数据加载失败"
        description={result.message}
      />
    );
  }

  return <HomePageView data={result.data} />;
}
