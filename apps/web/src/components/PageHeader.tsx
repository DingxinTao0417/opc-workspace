import type { ReactNode } from "react";

export function PageHeader({
  title,
  eyebrow,
  meta,
  actions,
}: {
  title: string;
  eyebrow?: ReactNode;
  meta?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <header className="page-header">
      <div className="min-w-0">
        {eyebrow ? <div className="page-eyebrow">{eyebrow}</div> : null}
        <div className="flex items-center gap-3">
          <h1 className="page-title">{title}</h1>
          {meta}
        </div>
      </div>
      {actions ? <div className="page-actions">{actions}</div> : null}
    </header>
  );
}
