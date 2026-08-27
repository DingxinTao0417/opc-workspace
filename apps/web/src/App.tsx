import { useEffect } from "react";
import { Navigate, Route, Routes } from "react-router-dom";
import { AppShell } from "./components/AppShell";
import { CommandPalette } from "./components/CommandPalette";
import { FocusTicker } from "./components/FocusTicker";
import { NewTaskModal } from "./components/NewTaskModal";
import { SettingsModal } from "./components/SettingsModal";
import { TaskDetailModal } from "./components/TaskDetailModal";
import { ThemeController } from "./components/ThemeController";
import { ClientsPage } from "./pages/ClientsPage";
import { FocusPage } from "./pages/FocusPage";
import { InboxPage } from "./pages/InboxPage";
import { IncomePage } from "./pages/IncomePage";
import { InvoicesPage } from "./pages/InvoicesPage";
import { LaterPage } from "./pages/LaterPage";
import { NotFoundPage } from "./pages/NotFoundPage";
import { ProjectsPage } from "./pages/ProjectsPage";
import { TasksPage } from "./pages/TasksPage";
import { TodayPage } from "./pages/TodayPage";
import { useSettingsStore } from "./store/settings";
import { useUiStore } from "./store/ui";

function GlobalShortcuts() {
  const setCommandPaletteOpen = useUiStore(
    (state) => state.setCommandPaletteOpen,
  );
  const setNewTaskOpen = useUiStore((state) => state.setNewTaskOpen);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (!(event.metaKey || event.ctrlKey) || event.altKey) return;
      if (event.key.toLowerCase() === "k") {
        event.preventDefault();
        setCommandPaletteOpen(true);
      } else if (event.key.toLowerCase() === "n") {
        event.preventDefault();
        setNewTaskOpen(true);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [setCommandPaletteOpen, setNewTaskOpen]);

  return null;
}

export default function App() {
  const defaultRoute = useSettingsStore(
    (state) => state.preview?.general.defaultRoute ?? state.defaultRoute,
  );

  return (
    <>
      <GlobalShortcuts />
      <ThemeController />
      <FocusTicker />
      <Routes>
        <Route element={<AppShell />}>
          <Route element={<Navigate replace to={`/${defaultRoute}`} />} index />
          <Route element={<TodayPage />} path="today" />
          <Route element={<TasksPage />} path="tasks" />
          <Route element={<ProjectsPage />} path="projects" />
          <Route element={<ClientsPage />} path="clients" />
          <Route element={<IncomePage />} path="income" />
          <Route element={<InvoicesPage />} path="invoices" />
          <Route element={<InboxPage />} path="inbox" />
          <Route element={<FocusPage />} path="focus" />
          <Route element={<LaterPage type="roadmap" />} path="roadmap" />
          <Route
            element={<LaterPage type="content-calendar" />}
            path="content-calendar"
          />
          <Route element={<NotFoundPage />} path="*" />
        </Route>
      </Routes>
      <CommandPalette />
      <NewTaskModal />
      <TaskDetailModal />
      <SettingsModal />
    </>
  );
}
