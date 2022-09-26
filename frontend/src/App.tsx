import React from "react";
import Layout from "./components/Layout"
import { Routes, Route } from "react-router-dom";

// Pages
import HostsPage from "./pages/Hosts"

function App() {
  return (
    <>
      
      <Routes>
        <Route path="/" element={<Layout />}>
          <Route index element={<HostsPage />} />
        </Route>
      </Routes>
    </>
  );
}

export default App;
