import axios, { AxiosInstance } from "axios";

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
    const { message } = await import("antd");

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
