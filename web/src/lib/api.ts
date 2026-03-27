import axios, { AxiosInstance } from "axios";

const isClient = typeof window !== "undefined";

function getServerApiBase(): string {
  const apiUrl = process.env.API_URL?.trim();
  if (!apiUrl) {
    throw new Error(
      "缺少环境变量 API_URL，请在 web/.env.local 中配置，例如 API_URL=http://127.0.0.1:3601",
    );
  }
  return `${apiUrl.replace(/\/+$/, "")}/api`;
}

const instance: AxiosInstance = axios.create({
  baseURL: isClient ? "/api" : getServerApiBase(),
  timeout: 80000,
});

// 响应拦截器
instance.interceptors.response.use(
  (response) => {
    return response.data;
  },
  async (error) => {
    if (isClient) {
      // 动态导入 message 以避免在服务端报错
      const { message } = await import("antd");
      if (error.response?.status === 401) {
        message.error(error.response.data?.msg || "请先登录");
        window.location.href = "/login";
      } else if (error.response?.status === 403) {
        message.error(error.response.data?.msg || "无访问权限");
      } else {
        message.error("服务器繁忙，请稍后再试");
      }
    }
    return Promise.reject(error);
  },
);

// 通用响应类型
export interface ApiResponse<T = any> {
  code: number;
  msg: string;
  data: T;
}

export const ApiGet = <T = any>(
  url: string,
  params?: Record<string, any>,
): Promise<ApiResponse<T>> => {
  return instance.get(url, { params }) as any;
};

export const ApiPost = <T = any>(
  url: string,
  data?: any,
): Promise<ApiResponse<T>> => {
  return instance.post(url, data) as any;
};

export default instance;
