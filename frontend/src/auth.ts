import http, { RequestResponse} from "./utils/axios"

const localAuthProvider = {
  isAuthenticated: false,
  signin(email: string, password: string, callback: VoidFunction) {
    http
      .post<RequestResponse>("/users/login", {
        email: email,
        secret: password,
      })
      .then(result => {
        localStorage.setItem("token", result.data.result.token);
        callback();
      })
  },
  signout(callback: VoidFunction) {
    localAuthProvider.isAuthenticated = false;
    localStorage.setItem("token", "");
    callback();
  },
};

export { localAuthProvider };
