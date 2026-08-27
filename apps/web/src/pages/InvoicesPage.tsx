import { Plus } from "lucide-react";
import { EmptyState } from "../components/feedback";
import { PageHeader } from "../components/PageHeader";

export function InvoicesPage() {
  return (
    <div className="page">
      <PageHeader
        actions={
          <button className="button button-primary" type="button">
            <Plus size={15} />
            新建发票
          </button>
        }
        meta={<span className="page-count">0 张</span>}
        title="发票"
      />
      <EmptyState
        message="创建发票后，可以在这里跟踪付款状态。"
        title="暂无发票"
      />
    </div>
  );
}
