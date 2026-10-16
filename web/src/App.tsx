import { useDesign } from "./design";
import { Home } from "./pages/Home";
import { Editorial } from "./pages/Editorial";
import { Report } from "./pages/Report";
import { Terminal } from "./pages/Terminal";
import { usePath } from "./router";

const DESIGNS = { classic: Report, terminal: Terminal, editorial: Editorial };

export function App() {
  const path = usePath();
  const design = useDesign();
  const match = path.match(/^\/reports\/([0-9a-f-]{36})$/i);
  if (!match) return <Home />;
  const Page = DESIGNS[design];
  return <Page id={match[1]!} key={match[1]} />;
}
