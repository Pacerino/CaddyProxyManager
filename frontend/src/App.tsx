import Layout from "./components/Layout";
import {
  Routes,
  Route,
  useNavigate,
  useLocation,
  Navigate,
} from "react-router-dom";
import React from "react";
import { localAuthProvider } from "./auth";
import { TextInput, Button, /* Alert */ } from "flowbite-react";
import { useForm, SubmitHandler } from "react-hook-form";


// Pages
import HostsPage from "./pages/Hosts";

function App() {
  return (
    <AuthProvider>
      <Routes>
        <Route element={<Layout />}>
          <Route path="/login" element={<LoginPage />} />
          <Route
            path="/"
            element={
              <RequireAuth>
                <HostsPage />
              </RequireAuth>
            }
          />
        </Route>
      </Routes>
    </AuthProvider>
  );
}

interface AuthContextType {
  user: any;
  signin: (mail: string, password: string,  callback: VoidFunction) => void;
  signout: (callback: VoidFunction) => void;
}

let AuthContext = React.createContext<AuthContextType>(null!);

function AuthProvider({ children }: { children: React.ReactNode }) {
  let [user, setUser] = React.useState<any>(null);

  let signin = (email: string, password: string, callback: VoidFunction) => {
    return localAuthProvider.signin(email, password, () => {
      setUser(email);
      callback();
    });
  };

  let signout = (callback: VoidFunction) => {
    return localAuthProvider.signout(() => {
      setUser(null);
      callback();
    });
  };

  let value = { user, signin, signout };

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  return React.useContext(AuthContext);
}

function RequireAuth({ children }: { children: JSX.Element }) {
  let auth = useAuth();
  let location = useLocation();

  if (!auth.user) {
    // Redirect them to the /login page, but save the current location they were
    // trying to go to when they were redirected. This allows us to send them
    // along to that page after they login, which is a nicer user experience
    // than dropping them off on the home page.
    return <Navigate to="/login" state={{ from: location }} replace />;
  }

  return children;
}
interface FormValues {
  email: string;
  password: string;
}


function LoginPage() {
  let navigate = useNavigate();
  let location = useLocation();
  let from = location.state?.from?.pathname || "/";
  let auth = useAuth();
  const { register, handleSubmit } = useForm<FormValues>();
  const performLogin: SubmitHandler<FormValues> = async (data) => {
   auth.signin(data.email, data.password, () => {
    navigate(from, { replace: true });
  })
  };

  return (
    <form onSubmit={handleSubmit(performLogin)}>
      <div className="flex justify-center">
        <div className="px-8 pt-6 pb-8 mb-4 flex flex-col w-1/4">
          <h1 className="text-white text-4xl pb-8">Please login</h1>
          {/* {error && (
            <div className="pb-8">
              <Alert color="failure">{error}</Alert>
            </div>
          )} */}
          <div className="mb-4">
            <TextInput
              type="email"
              placeholder="name@caddyproxymanager.com"
              required={true}
              {...register("email", { required: true })}
            />
          </div>
          <div className="mb-6">
            <TextInput
              type="password"
              required={true}
              {...register("password", { required: true })}
            />
          </div>
          <div className="flex items-center justify-between">
            <Button type="submit">Login</Button>
          </div>
        </div>
      </div>
    </form>
  );
}

export default App;
