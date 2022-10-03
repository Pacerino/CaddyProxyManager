import axios, { AxiosRequestConfig } from "axios";

export interface RequestResponse {
    result: any;
    error: Error;
  }
  
  interface Error {
    code: number;
    message: string;
  }


axios.interceptors.request.use((config: AxiosRequestConfig) => {
  if (!config) {
    config = {};
  }
  if (!config.headers) {
    config.headers = {};
  }
  const token = localStorage.getItem("token");
  if (token) {
    config.headers.authorization = `Bearer ${token}`;
  }
  config.baseURL = "http://localhost:3001/api/";
  return config;
});

const methods =  {
    get: axios.get,
    post: axios.post,
    put: axios.put,
    delete: axios.delete,
    patch: axios.patch,
  };

export default methods;