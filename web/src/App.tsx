import { useDesign } from "./design";
import { useLang } from "./i18n";
import { Bento } from "./pages/Bento";
import { Compare } from "./pages/Compare";
import { Decision } from "./pages/Decision";
import { Evidence } from "./pages/Evidence";
import { Home } from "./pages/Home";
import { PrintReport } from "./pages/PrintReport";
import { Report } from "./pages/Report";
import { Terminal } from "./pages/Terminal";
import { Watchlist } from "./pages/Watchlist";
import { usePath } from "./router";

const DESIGNS = { classic: Report, evidence: Evidence, bento: Bento, decision: Decision, terminal: Terminal };

export function App() {
  const path = usePath();
  const design = useDesign();
  // Re-render everything when the language changes; t() reads it directly.
  const lang = useLang();
  if (path === "/watchlist") return <Watchlist key={lang} />;
  if (path === "/compare") return <Compare key={`${lang}:${window.location.search}`} />;
  const print = path.match(/^\/reports\/([0-9a-f-]{36})\/print$/i);
  if (print) return <PrintReport id={print[1]!} key={`${print[1]}:${lang}`} />;
  const match = path.match(/^\/reports\/([0-9a-f-]{36})$/i);
  if (!match) return <Home key={lang} />;
  const Page = DESIGNS[design];
  return <Page id={match[1]!} key={`${match[1]}:${lang}`} />;
}
