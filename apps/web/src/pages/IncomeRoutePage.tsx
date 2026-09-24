import { Link, useSearchParams } from "react-router-dom";
import { IncomePage } from "./IncomePage";
import { parseFinanceReportLocation } from "../lib/financeLocation";
import { focusReportReturnSession } from "../lib/focusReportLocation";
import { ReturnToAiChat } from "../components/ClientRecordLocation";

export function IncomeRoutePage() {
  const [params] = useSearchParams();
  const report = parseFinanceReportLocation(params);
  const returnSession = focusReportReturnSession(params);
  const invalid =
    ([...params.keys()].some((key) => key !== "return_session") && !report) ||
    params.getAll("return_session").length > 1;
  const clear = returnSession
    ? `/income?return_session=${returnSession}`
    : "/income";
  if (invalid)
    return (
      <div className="page">
        <p role="alert">财务报告链接无效，不会查询默认范围。</p>
        <ReturnToAiChat sessionId={returnSession} />
        <Link to={clear}>清除报告条件</Link>
      </div>
    );
  return (
    <IncomePage
      key={params.toString()}
      report={report ?? undefined}
      navigation={
        <>
          <ReturnToAiChat sessionId={returnSession} />
          {report && (
            <Link className="button button-secondary" to={clear}>
              清除报告条件
            </Link>
          )}
        </>
      }
    />
  );
}
