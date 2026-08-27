import { ArrowLeft } from "lucide-react";
import { Link } from "react-router-dom";

export function NotFoundPage() {
  return (
    <div className="page">
      <section className="later-page">
        <strong className="not-found-code">404</strong>
        <h1>没有这个页面</h1>
        <p>检查地址，或返回今日工作台。</p>
        <Link className="button button-primary" to="/today">
          <ArrowLeft size={14} />
          返回今日
        </Link>
      </section>
    </div>
  );
}
