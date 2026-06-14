import axios from "axios";

// Same-origin in production (served by the Go binary); the Vite dev server
// proxies /api to the backend.
export const api = axios.create({
  baseURL: "/api",
});

api.interceptors.request.use((config) => {
  const token = localStorage.getItem("token");
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

api.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      localStorage.removeItem("token");
      if (window.location.pathname !== "/login") {
        window.location.href = "/login";
      }
    }
    return Promise.reject(error);
  }
);

// The backend wraps payloads as { result, error }.
export interface ApiEnvelope<T> {
  result: T;
  error?: { code: number; message: string };
}

export function unwrap<T>(data: ApiEnvelope<T>): T {
  return data.result;
}
