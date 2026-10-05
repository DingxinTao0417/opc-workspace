import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import React from "react";
import ReactDOM from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
// Stylesheet order is cascade order. These component stylesheets have always
// been bundled ahead of the global one, which wins when their rules tie; their
// components are now loaded on demand, so the order is pinned here instead of
// being left to chunk loading.
import "./browser.css";
import "./components/ProjectTaskFileSelect.css";
import "@xterm/xterm/css/xterm.css";
import "./components/ai-plan-continuation.css";
import "./workspace-panel.css";
import App from "./App";
import { ServiceRecoveryGate } from "./components/ServiceRecoveryGate";
import { SettingsBootstrap } from "./components/SettingsBootstrap";
import "./styles.css";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
      retry: false,
    },
  },
});

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <ServiceRecoveryGate>
        <SettingsBootstrap>
          <BrowserRouter>
            <App />
          </BrowserRouter>
        </SettingsBootstrap>
      </ServiceRecoveryGate>
    </QueryClientProvider>
  </React.StrictMode>,
);
