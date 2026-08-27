import type { LucideIcon } from "lucide-react";
import { ArrowLeft, CalendarDays, Map } from "lucide-react";
import { Link } from "react-router-dom";
import { PageHeader } from "../components/PageHeader";

export function LaterPage({ type }: { type: "roadmap" | "content-calendar" }) {
  const details: Record<
    typeof type,
    { title: string; icon: LucideIcon; copy: string }
  > = {
    roadmap: {
      title: "路线图",
      icon: Map,
      copy: "里程碑与季度规划将在后续版本实现。",
    },
    "content-calendar": {
      title: "内容日历",
      icon: CalendarDays,
      copy: "内容排期与月历视图将在后续版本实现。",
    },
  };
  const detail = details[type];
  const Icon = detail.icon;
  return (
    <div className="page">
      <PageHeader
        meta={<span className="later-page-badge">后续版本</span>}
        title={detail.title}
      />
      <section className="later-page">
        <span>
          <Icon size={25} />
        </span>
        <h2>暂未纳入 v0.1 基座</h2>
        <p>{detail.copy}</p>
        <Link className="button button-secondary" to="/today">
          <ArrowLeft size={14} />
          返回今日
        </Link>
      </section>
    </div>
  );
}
