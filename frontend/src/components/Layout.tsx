import { Navbar, Button } from "flowbite-react";
import { Outlet, useNavigate } from "react-router-dom";
import { useAuth } from "../App";
function Layout() {
  const auth = useAuth();
  const navigate = useNavigate();

  return (
    <div>
      <Navbar fluid={true} rounded={false}>
        <Navbar.Brand href="https://flowbite.com/">
          <img
            src="https://flowbite.com/docs/images/logo.svg"
            className="mr-3 h-6 sm:h-9"
            alt="Flowbite Logo"
          />
          <span className="self-center whitespace-nowrap text-xl font-semibold dark:text-white">
            Caddy Proxy Manager
          </span>
        </Navbar.Brand>

        {auth.user && (
          <div className="flex md:order-2">
            <Button onClick={() => auth.signout(() => navigate("/"))}>
              Logout
            </Button>
            <Navbar.Toggle />
          </div>
        )}

        <Navbar.Collapse>
          {/*           <Navbar.Link href="/home">Home</Navbar.Link>
          <Navbar.Link href="/hosts">Hosts</Navbar.Link> */}
        </Navbar.Collapse>
      </Navbar>
      <div className="container mx-auto py-4">
        <Outlet />
      </div>
    </div>
  );
}

export default Layout;
