import type { ProjectStatus } from "../types/models";

export interface ProjectPortfolioView {
  query: string;
  status: ProjectStatus | "";
  clientId: string;
  page: number;
}

const statuses = new Set<ProjectStatus>([
  "planning",
  "in_progress",
  "paused",
  "completed",
  "archived",
]);
const canonicalId =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const controls = /[\u0000-\u001f\u007f]/;

export function validProjectPortfolioView(view: ProjectPortfolioView): boolean {
  return (
    typeof view.query === "string" &&
    Array.from(view.query).length <= 200 &&
    !controls.test(view.query) &&
    (view.status === "" || statuses.has(view.status)) &&
    (view.clientId === "" || canonicalId.test(view.clientId)) &&
    Number.isSafeInteger(view.page) &&
    view.page >= 1 &&
    view.page <= 1_000_000
  );
}

export function projectPortfolioHref(
  view: ProjectPortfolioView,
): string | null {
  if (!validProjectPortfolioView(view)) return null;
  const params = new URLSearchParams();
  if (view.query) params.set("q", view.query);
  if (view.status) params.set("status", view.status);
  if (view.clientId) params.set("client_id", view.clientId);
  if (view.page > 1) params.set("page", String(view.page));
  const search = params.toString();
  return search ? `/projects?${search}` : "/projects";
}

export function readProjectPortfolioLocation(
  search: string | URLSearchParams,
): ProjectPortfolioView {
  const params = new URLSearchParams(search);
  const single = (key: string) => {
    const values = params.getAll(key);
    return values.length === 1 ? values[0] : "";
  };
  const query = single("q");
  const status = single("status");
  const clientId = single("client_id");
  const rawPage = single("page");
  return {
    query:
      Array.from(query).length <= 200 && !controls.test(query) ? query : "",
    status: statuses.has(status as ProjectStatus)
      ? (status as ProjectStatus)
      : "",
    clientId: canonicalId.test(clientId) ? clientId : "",
    page:
      /^[1-9]\d{0,6}$/.test(rawPage) && Number(rawPage) <= 1_000_000
        ? Number(rawPage)
        : 1,
  };
}
