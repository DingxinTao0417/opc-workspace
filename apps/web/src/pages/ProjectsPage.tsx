import { Plus } from "lucide-react";
import { EmptyState } from "../components/feedback";
import { PageHeader } from "../components/PageHeader";

export function ProjectsPage() {
  return (
    <div className="page">
      <PageHeader
        actions={
          <button className="button button-primary" type="button">
            <Plus size={15} />
            新建项目
          </button>
        }
        meta={<span className="page-count">0 个</span>}
        title="项目"
      />
      <EmptyState
        message="创建项目后，可以在这里跟踪进度和关联任务。"
        title="暂无项目"
      />
    </div>
  );
}
