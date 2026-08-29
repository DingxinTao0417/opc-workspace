import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import React from "react";
import ReactDOM from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
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
