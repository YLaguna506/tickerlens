import { useDesign } from "./design";
import { Bento } from "./pages/Bento";
import { Decision } from "./pages/Decision";
import { Home } from "./pages/Home";
import { Report } from "./pages/Report";
import { Scorecard } from "./pages/Scorecard";
import { Terminal } from "./pages/Terminal";
import { usePath } from "./router";

const DESIGNS = { classic: Report, scorecard: Scorecard, bento: Bento, decision: Decision, terminal: Terminal };

export function App() {
  const path = usePath();
  const design = useDesign();
  const match = path.match(/^\/reports\/([0-9a-f-]{36})$/i);
  if (!match) return <Home />;
  const Page = DESIGNS[design];
  return <Page id={match[1]!} key={match[1]} />;
}
