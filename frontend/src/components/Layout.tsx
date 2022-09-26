import { Navbar } from "flowbite-react";
import { Outlet } from "react-router-dom";
function Layout() {
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
