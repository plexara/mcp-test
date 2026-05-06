import React from "react";
import ReactDOM from "react-dom/client";
import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import "./index.css";
import App from "./App";
import Login from "./pages/Login";
import Dashboard from "./pages/Dashboard";
import Tools from "./pages/Tools";
import Audit from "./pages/Audit";
import Compare from "./pages/Compare";
import ApiKeys from "./pages/ApiKeys";
import Config from "./pages/Config";
import Wellknown from "./pages/Wellknown";
import About from "./pages/About";
import { ErrorBoundary } from "./components/ErrorBoundary";
// Side-effect import: wires the 401 handler in api.ts.
import "./stores/auth";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { staleTime: 5_000, refetchOnWindowFocus: false, retry: false },
  },
});

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <ErrorBoundary>
      <QueryClientProvider client={queryClient}>
        <BrowserRouter basename="/portal">
          <Routes>
            <Route path="/login" element={<Login />} />
            <Route path="/" element={<App />}>
              <Route index element={<Dashboard />} />
              <Route path="tools" element={<Tools />} />
              <Route path="tools/:name" element={<Tools />} />
              <Route path="audit" element={<Audit />} />
              <Route path="audit/compare" element={<Compare />} />
              <Route path="keys" element={<ApiKeys />} />
              <Route path="config" element={<Config />} />
              <Route path="wellknown" element={<Wellknown />} />
              <Route path="about" element={<About />} />
              <Route path="*" element={<Navigate to="/" replace />} />
            </Route>
          </Routes>
        </BrowserRouter>
      </QueryClientProvider>
    </ErrorBoundary>
  </React.StrictMode>
);
