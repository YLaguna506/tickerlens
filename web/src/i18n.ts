// English / Spanish. The page's own text goes through t("English text"),
// which looks it up in the Spanish dictionary (es.ts). Text the API computes
// (score reasons, metric names, rules) goes through tx(), which knows the
// backend's sentence patterns. Anything without a translation stays English.

import { useEffect, useState } from "react";
import { ES } from "./es";

export type Lang = "en" | "es";

const KEY = "tickerlens.lang";
const EVENT = "tickerlens:lang";

function readLang(): Lang {
  try {
    const saved = localStorage.getItem(KEY);
    if (saved === "en" || saved === "es") return saved;
  } catch {
    // Storage can be blocked; fall back to the browser's language.
  }
  return navigator.language.toLowerCase().startsWith("es") ? "es" : "en";
}

let current: Lang = readLang();
document.documentElement.lang = current;

export const getLang = () => current;

export function setLang(l: Lang) {
  current = l;
  document.documentElement.lang = l;
  try {
    localStorage.setItem(KEY, l);
  } catch {
    // Not remembered; the choice still applies until the page reloads.
  }
  window.dispatchEvent(new Event(EVENT));
}

/** Re-renders the caller when the language changes. */
export function useLang(): Lang {
  const [lang, set] = useState(current);
  useEffect(() => {
    const onChange = () => set(current);
    window.addEventListener(EVENT, onChange);
    return () => window.removeEventListener(EVENT, onChange);
  }, []);
  return lang;
}

/** Locale for dates in the current language. */
export const dateLocale = () => (current === "es" ? "es" : "en-US");

/** The page's own text: t("Built in {time}", { time: "2.1 s" }). */
export function t(en: string, vars?: Record<string, string | number>): string {
  let s = current === "es" ? (ES[en] ?? en) : en;
  if (vars) {
    for (const [k, v] of Object.entries(vars)) s = s.split(`{${k}}`).join(String(v));
  }
  return s;
}

// --- Text computed by the API ------------------------------------------------------

/** Exact backend strings. */
const EXACT: Record<string, string> = {
  // Metric names.
  "Gross margin": "Margen bruto",
  "Operating margin": "Margen operativo",
  "Net margin": "Margen neto",
  "FCF margin": "Margen de FCF",
  "Cash & short-term investments": "Efectivo e inversiones a corto plazo",
  "Total debt": "Deuda total",
  "Debt / equity": "Deuda / patrimonio",
  "EBIT / interest": "EBIT / intereses",
  "FCF / total debt": "FCF / deuda total",
  "Market cap": "Capitalización de mercado",
  "Dividend yield": "Rendimiento por dividendos",
  "Buyback yield": "Rendimiento por recompras",
  "Debt paydown yield": "Rendimiento por pago de deuda",
  "Total shareholder yield": "Rendimiento total al accionista",
  // Formulas.
  "buybacks ÷ market cap": "recompras ÷ capitalización",
  "cash + short-term investments": "efectivo + inversiones a corto plazo",
  "dividend yield + buyback yield + debt paydown yield": "rend. por dividendos + rend. por recompras + rend. por pago de deuda",
  "dividends paid ÷ market cap": "dividendos pagados ÷ capitalización",
  "free cash flow ÷ revenue": "flujo de caja libre ÷ ingresos",
  "free cash flow ÷ total debt": "flujo de caja libre ÷ deuda total",
  "gross profit ÷ revenue": "utilidad bruta ÷ ingresos",
  "market cap ÷ free cash flow": "capitalización ÷ flujo de caja libre",
  "market cap ÷ revenue": "capitalización ÷ ingresos",
  "market cap ÷ shareholders' equity": "capitalización ÷ patrimonio",
  "net debt repaid ÷ market cap": "deuda neta pagada ÷ capitalización",
  "net income ÷ revenue": "utilidad neta ÷ ingresos",
  "operating income ÷ interest expense": "utilidad operativa ÷ gasto por intereses",
  "operating income ÷ revenue": "utilidad operativa ÷ ingresos",
  "price × diluted shares": "precio × acciones diluidas",
  "price ÷ diluted EPS": "precio ÷ EPS diluido",
  "price ÷ diluted EPS from continuing operations": "precio ÷ EPS diluido de operaciones continuas",
  "diluted EPS from continuing operations": "EPS diluido de operaciones continuas",
  "total debt ÷ shareholders' equity": "deuda total ÷ patrimonio",
  // Capital allocation.
  "Capital expenditure": "Inversión en capital (capex)",
  Acquisitions: "Adquisiciones",
  Dividends: "Dividendos",
  Buybacks: "Recompras",
  "Net debt repaid": "Deuda neta pagada",
  "Net new borrowing": "Deuda neta nueva",
  // Limited support (banks, broker-dealers, insurers, REITs).
  "Banks lend out their depositors' money: deposits are their main debt and interest is their revenue, so debt ratios, margins and free cash flow mean something different here. The financial health score is left out; book value (P/B) and return on equity say more.":
    "Los bancos prestan el dinero de sus depositantes: los depósitos son su principal deuda y los intereses son sus ingresos, así que las razones de deuda, los márgenes y el flujo de caja libre significan otra cosa aquí. Se omite el puntaje de salud financiera; el valor en libros (P/B) y el retorno sobre el patrimonio dicen más.",
  "Broker-dealers fund their trading and client accounts with borrowed money, so debt ratios and free cash flow mean something different here. The financial health score is left out.":
    "Las casas de bolsa financian sus operaciones y las cuentas de sus clientes con dinero prestado, así que las razones de deuda y el flujo de caja libre significan otra cosa aquí. Se omite el puntaje de salud financiera.",
  "Insurers invest the premiums they hold for future claims (the float) and report reserves for those claims as liabilities, so debt ratios and margins read differently. The financial health score is left out.":
    "Las aseguradoras invierten las primas que guardan para reclamos futuros (el float) y reportan las reservas para esos reclamos como pasivos, así que las razones de deuda y los márgenes se leen distinto. Se omite el puntaje de salud financiera.",
  "REITs must pay out most of their income and are judged on funds from operations (FFO), which TickerLens does not compute; depreciation of their properties makes P/E look high.":
    "Los REITs deben repartir la mayor parte de sus ingresos y se evalúan por fondos de operaciones (FFO), que TickerLens no calcula; la depreciación de sus propiedades hace que el P/E se vea alto.",
  "proxy statements are not available": "las declaraciones para accionistas no están disponibles",
  // Input names.
  buybacks: "recompras",
  cash: "efectivo",
  "diluted EPS": "EPS diluido",
  "diluted shares": "acciones diluidas",
  "dividends paid": "dividendos pagados",
  end: "final",
  equity: "patrimonio",
  "free cash flow": "flujo de caja libre",
  "gross profit": "utilidad bruta",
  "interest expense": "gasto por intereses",
  "market cap": "capitalización",
  "net debt repaid": "deuda neta pagada",
  "net income": "utilidad neta",
  "operating income": "utilidad operativa",
  price: "precio",
  revenue: "ingresos",
  "short-term investments": "inversiones a corto plazo",
  start: "inicial",
  "total debt": "deuda total",
  // Per-share bases.
  EPS: "EPS",
  "free cash flow per share": "flujo de caja libre por acción",
  "revenue per share": "ingresos por acción",
  // Notes on metrics.
  "buybacks not reported": "recompras no reportadas",
  "cash not reported": "efectivo no reportado",
  "debt or equity not reported": "deuda o patrimonio no reportados",
  "diluted EPS is negative or zero, so the multiple is not meaningful": "el EPS diluido es negativo o cero, así que el múltiplo no tiene sentido",
  "diluted EPS not available": "EPS diluido no disponible",
  "diluted share count not reported": "acciones diluidas no reportadas",
  "dividends paid not reported": "dividendos pagados no reportados",
  "free cash flow not reported": "flujo de caja libre no reportado",
  "gross profit not reported": "utilidad bruta no reportada",
  "interest expense not reported separately": "el gasto por intereses no se reporta por separado",
  "market cap not available": "capitalización no disponible",
  "market cap not reported": "capitalización no reportada",
  "market cap unknown": "capitalización desconocida",
  "negative: the company borrowed more than it repaid": "negativo: la empresa pidió prestado más de lo que pagó",
  "net debt repaid not reported": "deuda neta pagada no reportada",
  "net income not reported": "utilidad neta no reportada",
  "no debt": "sin deuda",
  "no debt reported": "sin deuda reportada",
  "no interest expense": "sin gasto por intereses",
  "no short-term investments reported; cash only": "sin inversiones a corto plazo reportadas; solo efectivo",
  "not reported": "no reportado",
  "operating income not reported": "utilidad operativa no reportada",
  "price not reported": "precio no reportado",
  "shareholders' equity is zero or negative": "el patrimonio es cero o negativo",
  "starting value is zero or negative, so a growth rate is not meaningful": "el valor inicial es cero o negativo, así que la tasa de crecimiento no tiene sentido",
  "ending value is zero or negative, so a growth rate is not meaningful": "el valor final es cero o negativo, así que la tasa de crecimiento no tiene sentido",
  "sum of the components that are reported": "suma de los componentes reportados",
  "no dividends, buybacks or debt paydown reported": "no se reportan dividendos, recompras ni pago de deuda",
  // Score reasons.
  "no growth rates available": "no hay tasas de crecimiento disponibles",
  "no price data": "sin datos de precio",
  "no valuation metric had enough data": "ninguna métrica de valuación tenía suficientes datos",
  "not enough data to score": "no hay suficientes datos para puntuar",
  "not enough price history": "no hay suficiente historial de precios",
  "✗ operating loss: earnings do not cover interest": "✗ pérdida operativa: las ganancias no cubren los intereses",
  "caution: every metric's history is distorted, so this comparison is weak": "cuidado: el historial de todas las métricas está distorsionado, así que esta comparación es débil",
  "lacked enough history": "no tenía suficiente historial",
  "no TTM data": "sin datos TTM",
  // Phase.
  Startup: "Arranque",
  Hypergrowth: "Hipercrecimiento",
  "Operating leverage": "Apalancamiento operativo",
  "Capital return": "Retorno de capital",
  Decline: "Declive",
  "Is revenue growing?": "¿Crecen los ingresos?",
  "Is it profitable?": "¿Es rentable?",
  "Is it returning capital to shareholders?": "¿Devuelve capital a los accionistas?",
  "dividends and buybacks not reported": "dividendos y recompras no reportados",
  "not enough quarterly data": "no hay suficientes datos trimestrales",
  "not profitable and not growing fast enough for hypergrowth": "no es rentable y no crece lo suficiente para ser hipercrecimiento",
  "profitable, but not yet returning much capital (or revenue not growing)": "rentable, pero aún no devuelve mucho capital (o los ingresos no crecen)",
  // Verdict.
  "Strong business at an attractive price": "Negocio fuerte a un precio atractivo",
  "Great business; wait for a better price": "Gran negocio; espera un mejor precio",
  "Solid business at a fair price": "Negocio sólido a un precio justo",
  "Not compelling at this price": "No es atractivo a este precio",
  "Weak business, or a mediocre one that is expensive": "Negocio débil, o uno mediocre que está caro",
  "no business scores available, so no verdict": "no hay puntajes del negocio, así que no hay veredicto",
  "all scores and data available": "todos los puntajes y datos disponibles",
  "no business quality score (AI analysis unavailable)": "sin puntaje de calidad del negocio (análisis de IA no disponible)",
  "no growth score": "sin puntaje de crecimiento",
  "no financial health score": "sin puntaje de salud financiera",
  "no valuation score": "sin puntaje de valuación",
  "valued with a fallback metric": "valorada con una métrica alternativa",
  "lifecycle phase is a weak fit": "la fase del ciclo de vida no encaja bien",
  "Business quality": "Calidad del negocio",
  Growth: "Crecimiento",
  "Financial health": "Salud financiera",
  // AI and data errors.
  "no revenue breakdown disclosed in the documents": "los documentos no desglosan los ingresos",
  "the AI agent did not finish in time": "el agente de IA no terminó a tiempo",
};

const NOUN: Record<string, string> = { Revenue: "ingresos", revenue: "ingresos", EPS: "EPS", FCF: "FCF" };

type Rule = [RegExp, (m: RegExpMatchArray) => string];

/** Backend sentence patterns, tried in order. Groups are numbers or names. */
const KIND_ES: Record<string, string> = { banks: "bancos", brokers: "casas de bolsa", insurers: "aseguradoras", reits: "REITs" };

const PATTERNS: Rule[] = [
  [/^(Revenue|EPS|FCF) CAGR (\d+)y$/, (m) => `CAGR de ${NOUN[m[1]!]} ${m[2]} años`],
  [/^(EPS|revenue) CAGR (\d+)y: (.+)$/, (m) => `CAGR de ${NOUN[m[1]!]} ${m[2]} años: ${m[3] === "not available" ? "no disponible" : m[3]}`],
  [/^\(end ÷ start\)\^\(1\/(\d+)\) − 1$/, (m) => `(final ÷ inicial)^(1/${m[1]}) − 1`],
  [/^derived: (.+)$/, (m) => `derivado: ${m[1]}`],
  [/^average (.+) → (.+)$/, (m) => `promedio ${m[1]} → ${m[2]}`],
  [/^(\d+) of (\d+) criteria met → (.+)$/, (m) => `${m[1]} de ${m[2]} criterios cumplidos → ${m[3]}`],
  [/^skipped: (.+)$/, (m) => `omitido: ${tx(m[1]!)}`],
  [/^fewer than (\d+) years of free cash flow$/, (m) => `menos de ${m[1]} años de flujo de caja libre`],
  [/^(✓|✗) EBIT covers interest (only )?(.+)× \(([≥<]) (.+)×\)$/, (m) => `${m[1]} el EBIT cubre los intereses ${m[2] ? "solo " : ""}${m[3]}× (${m[4]} ${m[5]}×)`],
  [/^(✓|✗) debt\/equity (.+) \(([≤>]) (.+)\)$/, (m) => `${m[1]} deuda/patrimonio ${m[2]} (${m[3]} ${m[4]})`],
  [/^(✓|✗) yearly FCF is (.+)% of total debt \(([≥<]) (.+)%\)$/, (m) => `${m[1]} el FCF anual es el ${m[2]}% de la deuda total (${m[3]} ${m[4]}%)`],
  [/^(✓|✗) positive FCF in each of the last (\d+) years$/, (m) => `${m[1]} FCF positivo en cada uno de los últimos ${m[2]} años`],
  [/^(✓|✗) FCF was not positive in each of the last (\d+) years$/, (m) => `${m[1]} el FCF no fue positivo en cada uno de los últimos ${m[2]} años`],
  [/^(\S+) today (.+?)× vs (\d+)-year range: cheap below (.+?)×, expensive above (.+?)×$/,
    (m) => `${m[1]} hoy ${m[2]}× vs su rango de ${m[3]} años: barato bajo ${m[4]}×, caro sobre ${m[5]}×`],
  [/^today is higher than (.+?)% of the last (\d+) trading days → (.+)$/, (m) => `hoy está por encima del ${m[1]}% de los últimos ${m[2]} días de mercado → ${m[3]}`],
  [/^used (\S+) because (\S+) (.+)$/, (m) => `se usó ${m[1]} porque ${m[2]} ${tx(m[3]!)}`],
  [/^has a distorted history: (.+)$/, (m) => `tiene un historial distorsionado: ${tx(m[1]!)}`],
  [/^is not meaningful today \((.+)\)$/, (m) => `no tiene sentido hoy (${tx(m[1]!)})`],
  [/^(.+) fell as low as (\S+) against a typical (\S+)$/, (m) => `${tx(m[1]!)} llegó a ${m[2]} frente a un valor típico de ${m[3]}`],
  [/^(.+) was zero or negative during the last (\d+) years$/, (m) => `${tx(m[1]!)} fue cero o negativo en los últimos ${m[2]} años`],
  [/^(.+) is negative or zero$/, (m) => `${tx(m[1]!)} es negativo o cero`],
  [/^a spin-off on (\S+) took about (\d+)% off the share price, and the last twelve months of filings \(to (\S+)\) still include the business spun off$/,
    (m) => `una escisión (spin-off) el ${m[1]} le quitó cerca del ${m[2]}% al precio de la acción, y los últimos doce meses de reportes (hasta ${m[3]}) aún incluyen el negocio escindido`],
  [/^no valuation: (.+)$/, (m) => `sin valuación: ${tx(m[1]!)}`],
  [/^valuation: (.+)$/, (m) => `valuación: ${tx(m[1]!)}`],
  [/^not comparable with history: (.+)$/, (m) => `no comparable con su historial: ${tx(m[1]!)}`],
  [/^leaves out discontinued operations; with them, diluted EPS is (\S+)$/,
    (m) => `excluye las operaciones discontinuadas; con ellas, el EPS diluido es ${m[1]}`],
  [/^(.+) reported for (\d+) of (\d+) years$/, (m) => `${tx(m[1]!)} reportado en ${m[2]} de ${m[3]} años`],
  [/^not scored: debt ratios do not apply to (banks|brokers|insurers|reits)$/, (m) => `sin puntaje: las razones de deuda no aplican a ${KIND_ES[m[1]!]}`],
  [/^limited support: the metrics are built for operating companies, not (banks|brokers|insurers|reits)$/,
    (m) => `soporte limitado: las métricas están hechas para empresas operativas, no para ${KIND_ES[m[1]!]}`],
  [/^sec: no proxy statement \(DEF 14A\) found for CIK (\d+)$/, (m) => `SEC: no se encontró una declaración para accionistas (DEF 14A) para el CIK ${m[1]}`],
  [/^the proxy statement filed (\S+) has no ownership table$/, (m) => `la declaración para accionistas presentada el ${m[1]} no tiene tabla de propiedad`],
  [/^(\S+): history distorted, (.+)$/, (m) => `${m[1]}: historial distorsionado, ${tx(m[2]!)}`],
  [/^(\S+): not meaningful today \((.+)\)$/, (m) => `${m[1]}: no tiene sentido hoy (${tx(m[2]!)})`],
  [/^(\S+): only (\d+) days of history \(need (\d+)\)$/, (m) => `${m[1]}: solo ${m[2]} días de historial (se necesitan ${m[3]})`],
  [/^interest expense from FY(\d+) \(not reported quarterly\)$/, (m) => `gasto por intereses del FY${m[1]} (no se reporta por trimestre)`],
  [/^no data for FY(\d+)$/, (m) => `sin datos para el FY${m[1]}`],
  // Phase.
  [/^profitable, growing, and returning capital \(shareholder yield (.+), (.+) of FCF\)$/,
    (m) => `rentable, en crecimiento y devolviendo capital (rendimiento al accionista ${m[1]}, ${m[2]} del FCF)`],
  [/^revenue up (.+)% in a year while net margin is (.+)$/, (m) => `ingresos +${m[1]}% en un año con un margen neto de ${m[2]}`],
  [/^revenue shrank (.+)%\/yr over 3 years and operating margin fell from (.+)% to (.+)%$/,
    (m) => `los ingresos cayeron ${m[1]}% al año durante 3 años y el margen operativo bajó de ${m[2]}% a ${m[3]}%`],
  [/^losing money with revenue under \$100M \((.+)\)$/, (m) => `pierde dinero con ingresos menores de $100M (${m[1]})`],
  [/^losses widened from (.+) to (.+) over the past year$/, (m) => `las pérdidas crecieron de ${m[1]} a ${m[2]} en el último año`],
  [/^TTM revenue (.+) vs a year earlier$/, (m) => `ingresos TTM ${m[1]} vs hace un año`],
  [/^TTM net income (.+) \(net margin (.+)\)$/, (m) => `utilidad neta TTM ${m[1]} (margen neto ${m[2]})`],
  [/^dividends \+ buybacks = (.+) of market cap, (.+) of FCF$/, (m) => `dividendos + recompras = ${m[1]} de la capitalización, ${m[2]} del FCF`],
  // Verdict rules.
  [/^Buy: B (\S+) is at least (\S+) and V (\S+) is at least (\S+)( \(very cheap\))?$/,
    (m) => `Comprar: B ${m[1]} es al menos ${m[2]} y V ${m[3]} es al menos ${m[4]}${m[5] ? " (muy barato)" : ""}`],
  [/^Watch: B (\S+) is at least (\S+), but V (\S+) is below (\S+)$/, (m) => `Vigilar: B ${m[1]} es al menos ${m[2]}, pero V ${m[3]} está por debajo de ${m[4]}`],
  [/^Hold: B (\S+) is at least (\S+) and V (\S+) is at least (\S+)$/, (m) => `Mantener: B ${m[1]} es al menos ${m[2]} y V ${m[3]} es al menos ${m[4]}`],
  [/^Pass: B (\S+) and V (\S+) match no Buy, Watch or Hold rule$/, (m) => `Pasar: B ${m[1]} y V ${m[2]} no cumplen ninguna regla de Comprar, Vigilar o Mantener`],
  [/^Sell: business score B (\S+) is below (\S+)$/, (m) => `Vender: el puntaje del negocio B ${m[1]} está por debajo de ${m[2]}`],
  [/^Sell: B (\S+) is below (\S+) and valuation V (\S+) is (\S+) or lower \(expensive\)$/,
    (m) => `Vender: B ${m[1]} está por debajo de ${m[2]} y la valuación V ${m[3]} es ${m[4]} o menos (caro)`],
  [/^Watch: would be (Buy: .+), but there is no business quality score$/,
    (m) => `Vigilar: sería ${tx(m[1]!)}, pero no hay puntaje de calidad del negocio`],
  [/^−(\d+): (.+)$/, (m) => `−${m[1]}: ${tx(m[2]!)}`],
  [/^AI analysis failed: (.+)$/, (m) => `falló el análisis de IA: ${m[1]}`],
  [/^missing data: (.+)$/, (m) => `faltan datos: ${m[1]}`],
  [/^could not read the latest 10-K: (.+)$/, (m) => `no se pudo leer el último 10-K: ${m[1]}`],
  // Periods.
  [/TTM to /, (m) => m.input!.replace(/TTM to /g, "TTM al ")],
  [/^balance sheet at (.+)$/, (m) => `balance al ${m[1]}`],
  [/^price on (.+)$/, (m) => `precio del ${m[1]}`],
];

/** Text computed by the API, in the current language. */
export function tx(text: string | undefined | null): string {
  if (!text) return "";
  if (current === "en") return text;
  const exact = EXACT[text] ?? ES[text];
  if (exact) return exact;
  for (const [re, fn] of PATTERNS) {
    const m = text.match(re);
    if (m) return fn(m);
  }
  return text;
}
