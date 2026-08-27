import { CheckCheck } from "lucide-react";
import { EmptyState } from "../components/feedback";
import { PageHeader } from "../components/PageHeader";

export function InboxPage() {
  return (
    <div className="page">
      <PageHeader
        actions={
          <button className="button button-secondary" type="button">
            <CheckCheck size={15} />
            全部标为已读
          </button>
        }
        meta={<span className="page-count">0 条未读</span>}
        title="收件箱"
      />
      <EmptyState
        message="新的通知和业务动态会显示在这里。"
        title="收件箱为空"
      />
    </div>
  );
}
