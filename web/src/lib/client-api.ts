import axios, { AxiosInstance } from "axios";
import { message } from "@/lib/app-message";

export interface ApiResponse<T = any> {
  code: number;
  msg: string;
  data: T;
}

const instance: AxiosInstance = axios.create({
  baseURL: "/api",
  timeout: 80000,
  withCredentials: true,
});

instance.interceptors.response.use(
  (response) => {
    return response.data;
  },
  async (error) => {
    // 主动取消的请求不提示。
    if (axios.isCancel(error)) {
      return Promise.reject(error);
    }
    // 版本号冲突交给调用方处理，按普通响应返回（code 为 409）。
    if (error.response?.status === 409 && error.response.data) {
      return error.response.data;
    }
    if (error.response?.status === 401) {
      message.error(error.response.data?.msg || "请先登录");
      window.location.href = "/login";
    } else if (error.response?.status === 403) {
      message.error(error.response.data?.msg || "无访问权限");
    } else {
      message.error("服务器繁忙，请稍后再试");
    }

    return Promise.reject(error);
  },
);

export const ApiGet = <T = any>(
  url: string,
  params?: Record<string, any>,
  timeout?: number,
): Promise<ApiResponse<T>> => {
  return instance.get(url, timeout ? { params, timeout } : { params }) as any;
};

export interface ApiRequestOptions {
  signal?: AbortSignal;
}

export const ApiPost = <T = any>(
  url: string,
  data?: any,
  options?: ApiRequestOptions,
): Promise<ApiResponse<T>> => {
  return instance.post(url, data, options) as any;
};

export const ApiPut = <T = any>(url: string, data?: any): Promise<ApiResponse<T>> => {
  return instance.put(url, data) as any;
};

export const ApiPatch = <T = any>(url: string, data?: any): Promise<ApiResponse<T>> => {
  return instance.patch(url, data) as any;
};

export const ApiDelete = <T = any>(url: string): Promise<ApiResponse<T>> => {
  return instance.delete(url) as any;
};

/** 版本号冲突时服务端返回的 code。 */
export const API_CONFLICT = 409;

/** 长耗时请求（如批量站点连通性检测），可单独指定超时时间（ms）。 */
export const ApiPostLong = <T = any>(
  url: string,
  data?: any,
  timeout = 300000,
): Promise<ApiResponse<T>> => {
  return instance.post(url, data, { timeout }) as any;
};

export default instance;
