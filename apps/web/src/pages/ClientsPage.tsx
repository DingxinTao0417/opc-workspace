import { Plus } from "lucide-react";
import { EmptyState } from "../components/feedback";
import { PageHeader } from "../components/PageHeader";

export function ClientsPage() {
  return (
    <div className="page">
      <PageHeader
        actions={
          <button className="button button-primary" type="button">
            <Plus size={15} />
            新建客户
          </button>
        }
        meta={<span className="page-count">0 个</span>}
        title="客户"
      />
      <EmptyState
        message="添加客户后，可以在这里查看联系人和合作记录。"
        title="暂无客户"
      />
    </div>
  );
}
