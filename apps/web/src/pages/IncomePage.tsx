import { CalendarRange } from "lucide-react";
import { EmptyState } from "../components/feedback";
import { PageHeader } from "../components/PageHeader";

export function IncomePage() {
  return (
    <div className="page">
      <PageHeader
        actions={
          <button className="button button-secondary" type="button">
            <CalendarRange size={15} />近 6 个月
          </button>
        }
        meta={<span className="page-count">0 条记录</span>}
        title="收入"
      />
      <EmptyState
        message="有收入记录后，这里会显示收入概览和趋势。"
        title="暂无收入数据"
      />
    </div>
  );
}
