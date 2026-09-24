import { Navigate, Route, Routes } from "react-router-dom";
import { AppShell } from "./components/AppShell";
import { AgentRunDrawer } from "./components/AgentRunDrawer";
import { AiGenerationMonitor } from "./components/AiGenerationMonitor";
import { RoutedAppErrorBoundary } from "./components/AppErrorBoundary";
import { CommandPalette } from "./components/CommandPalette";
import { FocusTicker } from "./components/FocusTicker";
import { FocusRecoveryModal } from "./components/FocusRecoveryModal";
import { GlobalShortcuts } from "./components/GlobalShortcuts";
import { NewTaskModal } from "./components/NewTaskModal";
import { SettingsModal } from "./components/SettingsModal";
import { TaskDetailModal } from "./components/TaskDetailModal";
import { ThemeController } from "./components/ThemeController";
import { DeferredPage } from "./components/DeferredPage";
import { ClientsPage } from "./pages/ClientsPage";
import { ContentCalendarPage } from "./pages/ContentCalendarPage";
import { ClientDetailPage } from "./pages/ClientDetailPage";
import { AutomationLocationPage } from "./pages/AutomationLocationPage";
import { FocusPage } from "./pages/FocusPage";
import { InboxPage } from "./pages/InboxPage";
import { IncomeRoutePage } from "./pages/IncomeRoutePage";
import { FinancialEntryDetailPage } from "./pages/FinancialEntryDetailPage";
import { InvoiceDetailPage } from "./pages/InvoiceDetailPage";
import { InvoicesPage } from "./pages/InvoicesPage";
import { NotFoundPage } from "./pages/NotFoundPage";
import { ProjectsPage } from "./pages/ProjectsPage";
import { RoadmapPage } from "./pages/RoadmapPage";
import { ProjectDetailPage } from "./pages/ProjectDetailPage";
import { TasksPage } from "./pages/TasksPage";
import { TaskSubmissionPage } from "./pages/TaskSubmissionPage";
import { TodayPage } from "./pages/TodayPage";
import { useSettingsStore } from "./store/settings";

const loadAgentPage = () =>
  import("./pages/AiAssistantPage").then((module) => module.AiAssistantPage);
const loadKnowledgePage = () =>
  import("./pages/KnowledgeBasePage").then(
    (module) => module.KnowledgeBasePage,
  );

export default function App() {
  const defaultRoute = useSettingsStore(
    (state) => state.preview?.general.defaultRoute ?? state.defaultRoute,
  );

  return (
    <>
      <GlobalShortcuts />
      <ThemeController />
      <FocusTicker />
      <FocusRecoveryModal />
      <AiGenerationMonitor />
      <RoutedAppErrorBoundary>
        <Routes>
          <Route element={<AppShell />}>
            <Route
              element={<Navigate replace to={`/${defaultRoute}`} />}
              index
            />
            <Route element={<TodayPage />} path="today" />
            <Route element={<TasksPage />} path="tasks" />
            <Route element={<TasksPage />} path="tasks/:taskId" />
            <Route
              element={<TaskSubmissionPage />}
              path="tasks/:taskId/submissions/:submissionId"
            />
            <Route element={<ProjectsPage />} path="projects" />
            <Route element={<ProjectDetailPage />} path="projects/:projectId" />
            <Route element={<ClientsPage />} path="clients" />
            <Route element={<ClientDetailPage />} path="clients/:clientId" />
            <Route element={<IncomeRoutePage />} path="income" />
            <Route
              element={<FinancialEntryDetailPage />}
              path="income/:entryId"
            />
            <Route element={<InvoicesPage />} path="invoices" />
            <Route element={<InvoiceDetailPage />} path="invoices/:invoiceId" />
            <Route element={<InboxPage />} path="inbox" />
            <Route element={<InboxPage />} path="inbox/:inboxItemId" />
            <Route element={<FocusPage />} path="focus" />
            <Route element={<DeferredPage load={loadAgentPage} />} path="ai" />
            <Route
              element={<AutomationLocationPage />}
              path="settings/automation"
            />
            <Route
              element={<DeferredPage load={loadKnowledgePage} />}
              path="knowledge"
            />
            <Route element={<RoadmapPage />} path="roadmap" />
            <Route element={<ContentCalendarPage />} path="content-calendar" />
            <Route element={<NotFoundPage />} path="*" />
          </Route>
        </Routes>
      </RoutedAppErrorBoundary>
      <CommandPalette />
      <NewTaskModal />
      <TaskDetailModal />
      <AgentRunDrawer />
      <SettingsModal />
    </>
  );
}
