import CategoryPageView from "./view";
import TrackPageView from "@/components/public/TrackPageView";
import {
  getPublicServerErrorMessage,
  isServerUnauthorizedError,
  redirectToLogin,
  serverGet,
} from "@/lib/server-api";
import { Alert } from "antd";

type CategoryLoadResult =
  | { status: "ok"; data: any }
  | { status: "invalid"; message: string }
  | { status: "unauthorized" }
  | { status: "error"; message: string };

async function getCategoryData(pid: string): Promise<CategoryLoadResult> {
  try {
    const response = await serverGet<any>("/categories/overview", { pid });
    if (response.code === 0) {
      return { status: "ok", data: response.data };
    }
    if (response.msg === "主分类信息获取异常") {
      return { status: "invalid", message: response.msg };
    }
    return {
      status: "error",
      message: response.msg || "分类页面数据获取失败",
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

export default async function CategoryPage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const resolvedSearchParams = await searchParams;
  const pidValue = resolvedSearchParams.pid || resolvedSearchParams.Pid;
  const pid = Array.isArray(pidValue) ? pidValue[0] : pidValue;

  const result = await getCategoryData(pid || "");
  if (result.status === "unauthorized") {
    await redirectToLogin();
  }

  if (result.status === "error") {
    return (
      <Alert
        type="error"
        showIcon
        title="分类页面数据获取失败"
        description={result.message}
      />
    );
  }

  const data = result.status === "ok" ? result.data : null;
  if (!data || !data.title || data.title.id === undefined || data.title.id === null) {
    if (pid) {
      return (
        <Alert
          type="warning"
          showIcon
          title="当前分类已失效，请从最新分类导航重新进入"
        />
      );
    }
    return <Alert type="error" showIcon title="分类页面数据获取失败" />;
  }

  const effectivePid = String(data.title.id);

  return (
    <>
      <TrackPageView
        action="classify"
        resource={effectivePid}
        resourceCat={data.title.name}
        resourceTitle={data.title.name}
      />
      <CategoryPageView data={data} pid={effectivePid} />
    </>
  );
}
