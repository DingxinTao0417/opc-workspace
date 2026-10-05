import { Navigate, Route, Routes } from "react-router-dom";
import { AppShell } from "./components/AppShell";
import { AiGenerationMonitor } from "./components/AiGenerationMonitor";
import { RoutedAppErrorBoundary } from "./components/AppErrorBoundary";
import { DeferredOverlays } from "./components/DeferredOverlays";
import { FocusTicker } from "./components/FocusTicker";
import { FocusRecoveryModal } from "./components/FocusRecoveryModal";
import { GlobalShortcuts } from "./components/GlobalShortcuts";
import { ThemeController } from "./components/ThemeController";
import { DeferredPage } from "./components/DeferredPage";
import { NotFoundPage } from "./pages/NotFoundPage";
import { TodayPage } from "./pages/TodayPage";
import { useSettingsStore } from "./store/settings";

// Every page except the default first screen (Today) and the tiny not-found
// page is its own chunk; DeferredPage shows the loading and failure states.
const loadAgentPage = () =>
  import("./pages/AiAssistantPage").then((module) => module.AiAssistantPage);
const loadKnowledgePage = () =>
  import("./pages/KnowledgeBasePage").then(
    (module) => module.KnowledgeBasePage,
  );
const loadClientsPage = () =>
  import("./pages/ClientsPage").then((module) => module.ClientsPage);
const loadContentCalendarPage = () =>
  import("./pages/ContentCalendarPage").then(
    (module) => module.ContentCalendarPage,
  );
const loadClientDetailPage = () =>
  import("./pages/ClientDetailPage").then((module) => module.ClientDetailPage);
const loadAutomationLocationPage = () =>
  import("./pages/AutomationLocationPage").then(
    (module) => module.AutomationLocationPage,
  );
const loadFocusPage = () =>
  import("./pages/FocusPage").then((module) => module.FocusPage);
const loadInboxPage = () =>
  import("./pages/InboxPage").then((module) => module.InboxPage);
const loadIncomeRoutePage = () =>
  import("./pages/IncomeRoutePage").then((module) => module.IncomeRoutePage);
const loadFinancialEntryDetailPage = () =>
  import("./pages/FinancialEntryDetailPage").then(
    (module) => module.FinancialEntryDetailPage,
  );
const loadInvoiceDetailPage = () =>
  import("./pages/InvoiceDetailPage").then(
    (module) => module.InvoiceDetailPage,
  );
const loadInvoicesPage = () =>
  import("./pages/InvoicesPage").then((module) => module.InvoicesPage);
const loadProjectsPage = () =>
  import("./pages/ProjectsPage").then((module) => module.ProjectsPage);
const loadRoadmapPage = () =>
  import("./pages/RoadmapPage").then((module) => module.RoadmapPage);
const loadProjectDetailPage = () =>
  import("./pages/ProjectDetailPage").then(
    (module) => module.ProjectDetailPage,
  );
const loadTasksPage = () =>
  import("./pages/TasksPage").then((module) => module.TasksPage);
const loadTaskSubmissionPage = () =>
  import("./pages/TaskSubmissionPage").then(
    (module) => module.TaskSubmissionPage,
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
            <Route
              element={<DeferredPage load={loadTasksPage} />}
              path="tasks"
            />
            <Route
              element={<DeferredPage load={loadTasksPage} />}
              path="tasks/:taskId"
            />
            <Route
              element={<DeferredPage load={loadTaskSubmissionPage} />}
              path="tasks/:taskId/submissions/:submissionId"
            />
            <Route
              element={<DeferredPage load={loadProjectsPage} />}
              path="projects"
            />
            <Route
              element={<DeferredPage load={loadProjectDetailPage} />}
              path="projects/:projectId"
            />
            <Route
              element={<DeferredPage load={loadClientsPage} />}
              path="clients"
            />
            <Route
              element={<DeferredPage load={loadClientDetailPage} />}
              path="clients/:clientId"
            />
            <Route
              element={<DeferredPage load={loadIncomeRoutePage} />}
              path="income"
            />
            <Route
              element={<DeferredPage load={loadFinancialEntryDetailPage} />}
              path="income/:entryId"
            />
            <Route
              element={<DeferredPage load={loadInvoicesPage} />}
              path="invoices"
            />
            <Route
              element={<DeferredPage load={loadInvoiceDetailPage} />}
              path="invoices/:invoiceId"
            />
            <Route
              element={<DeferredPage load={loadInboxPage} />}
              path="inbox"
            />
            <Route
              element={<DeferredPage load={loadInboxPage} />}
              path="inbox/:inboxItemId"
            />
            <Route
              element={<DeferredPage load={loadFocusPage} />}
              path="focus"
            />
            <Route element={<DeferredPage load={loadAgentPage} />} path="ai" />
            <Route
              element={<DeferredPage load={loadAutomationLocationPage} />}
              path="settings/automation"
            />
            <Route
              element={<DeferredPage load={loadKnowledgePage} />}
              path="knowledge"
            />
            <Route
              element={<DeferredPage load={loadRoadmapPage} />}
              path="roadmap"
            />
            <Route
              element={<DeferredPage load={loadContentCalendarPage} />}
              path="content-calendar"
            />
            <Route element={<NotFoundPage />} path="*" />
          </Route>
        </Routes>
      </RoutedAppErrorBoundary>
      <DeferredOverlays />
    </>
  );
}
