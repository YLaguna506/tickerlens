import { Home } from "./pages/Home";
import { Report } from "./pages/Report";
import { usePath } from "./router";

export function App() {
  const path = usePath();
  const match = path.match(/^\/reports\/([0-9a-f-]{36})$/i);
  return match ? <Report id={match[1]!} key={match[1]} /> : <Home />;
}
