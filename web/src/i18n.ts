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
  // Banks, insurers and REITs.
  "Banks lend out their depositors' money, so interest is their revenue and deposits their main debt. They are read on what they earn on their shareholders' equity (ROE), how cheaply they run (efficiency ratio) and how much equity backs their assets, and valued on book value (P/B).": "Los bancos prestan el dinero de sus depositantes: los intereses son sus ingresos y los depósitos su principal deuda. Se analizan por lo que ganan sobre el patrimonio de sus accionistas (ROE), cuánto les cuesta operar (ratio de eficiencia) y cuánto patrimonio respalda sus activos, y se valúan por su valor en libros (P/B).",
  "Broker-dealers fund trading and client accounts with borrowed money, so debt ratios do not apply. They are read on what they earn on their shareholders' equity (ROE) and how much equity backs their assets, and valued on book value (P/B).": "Las casas de bolsa financian sus operaciones y las cuentas de clientes con dinero prestado, así que los ratios de deuda no aplican. Se analizan por lo que ganan sobre el patrimonio de sus accionistas (ROE) y cuánto patrimonio respalda sus activos, y se valúan por su valor en libros (P/B).",
  "Insurers collect premiums now and pay claims later, investing the money in between. They are read on what they earn on their shareholders' equity (ROE), how much of their premiums go to claims and how their book value grows, and valued on book value (P/B).": "Las aseguradoras cobran primas hoy y pagan siniestros después, e invierten el dinero mientras tanto. Se analizan por lo que ganan sobre el patrimonio de sus accionistas (ROE), qué parte de las primas se va en siniestros y cómo crece su valor en libros, y se valúan por su valor en libros (P/B).",
  "REITs own property and must pay out most of their income. Depreciation of their buildings makes earnings look low, so they are read on funds from operations (FFO: net income + real estate depreciation and impairments − gains on property sales) and valued on price to FFO (P/FFO).": "Los REITs son dueños de inmuebles y deben repartir la mayor parte de sus ganancias. La depreciación de sus edificios hace que la utilidad parezca baja, así que se analizan por sus fondos de operaciones (FFO: utilidad neta + depreciación y deterioros inmobiliarios − ganancias por venta de inmuebles) y se valúan por precio sobre FFO (P/FFO).",
  "Return on equity": "Rentabilidad sobre el patrimonio (ROE)",
  "Return on assets": "Rentabilidad sobre los activos (ROA)",
  "Equity ÷ assets": "Patrimonio ÷ activos",
  "Book value per share": "Valor en libros por acción",
  "Book value per share CAGR 5y": "CAGR del valor en libros por acción 5 años",
  "Net interest income ÷ assets": "Ingreso neto por intereses ÷ activos",
  "Efficiency ratio": "Ratio de eficiencia",
  "Fee income share": "Peso de las comisiones",
  "Loans ÷ deposits": "Préstamos ÷ depósitos",
  "Claims ÷ premiums": "Siniestros ÷ primas",
  "Investment income ÷ revenue": "Ingreso por inversiones ÷ ingresos",
  "Premium growth 1y": "Crecimiento de primas 1 año",
  "Premium CAGR 5y": "CAGR de primas 5 años",
  "Funds from operations (FFO)": "Fondos de operaciones (FFO)",
  "FFO per share": "FFO por acción",
  "Dividends ÷ FFO": "Dividendos ÷ FFO",
  "Interest coverage (FFO)": "Cobertura de intereses (FFO)",
  "FFO per share CAGR 3y": "CAGR del FFO por acción 3 años",
  "FFO per share CAGR 5y": "CAGR del FFO por acción 5 años",
  "Debt ÷ assets": "Deuda ÷ activos",
  "shareholders' equity ÷ total assets": "patrimonio ÷ activos totales",
  "common equity (shareholders' equity − preferred stock) ÷ diluted shares": "patrimonio común (patrimonio − acciones preferentes) ÷ acciones diluidas",
  "non-interest expense ÷ (net interest income + non-interest income); lower is better": "gastos no financieros ÷ (ingreso neto por intereses + ingresos no financieros); menos es mejor",
  "non-interest income ÷ (net interest income + non-interest income)": "ingresos no financieros ÷ (ingreso neto por intereses + ingresos no financieros)",
  "loans ÷ deposits": "préstamos ÷ depósitos",
  "claims and policyholder benefits ÷ premiums earned (the loss ratio); lower is better": "siniestros y beneficios a asegurados ÷ primas devengadas (índice de siniestralidad); menos es mejor",
  "net investment income ÷ revenue": "ingreso neto por inversiones ÷ ingresos",
  "latest fiscal year ÷ the year before − 1": "último año fiscal ÷ el año anterior − 1",
  "net income to common shareholders + depreciation + real estate impairments − gains on property sales (approximate)": "utilidad neta para accionistas comunes + depreciación + deterioros inmobiliarios − ganancias por venta de inmuebles (aproximado)",
  "FFO ÷ diluted shares": "FFO ÷ acciones diluidas",
  "price ÷ FFO per share": "precio ÷ FFO por acción",
  "dividends paid ÷ FFO; above 100% means paying out more than the properties earn": "dividendos pagados ÷ FFO; más de 100% es repartir más de lo que generan los inmuebles",
  "(FFO + interest expense) ÷ interest expense": "(FFO + gasto por intereses) ÷ gasto por intereses",
  "total debt ÷ total assets": "deuda total ÷ activos totales",
  "net income to common shareholders": "utilidad neta para accionistas comunes",
  "average common equity": "patrimonio común promedio",
  "average total assets": "activos totales promedio",
  "total assets": "activos totales",
  "common equity": "patrimonio común",
  "net interest income": "ingreso neto por intereses",
  "non-interest expense": "gastos no financieros",
  "non-interest income": "ingresos no financieros",
  "net interest income + non-interest income": "ingreso neto por intereses + ingresos no financieros",
  "loans": "préstamos",
  "deposits": "depósitos",
  "claims and benefits": "siniestros y beneficios",
  "premiums earned": "primas devengadas",
  "net investment income": "ingreso neto por inversiones",
  "depreciation": "depreciación",
  "real estate impairments": "deterioros inmobiliarios",
  "gains on property sales": "ganancias por venta de inmuebles",
  "FFO": "FFO",
  "FFO + interest expense": "FFO + gasto por intereses",
  "a value is zero or negative, so a growth rate is not meaningful": "un valor es cero o negativo, así que la tasa de crecimiento no tiene sentido",
  "net income or depreciation not reported for four straight quarters": "utilidad neta o depreciación no reportadas en cuatro trimestres seguidos",
  "FFO per share not available": "FFO por acción no disponible",
  "return on equity not available": "rentabilidad sobre el patrimonio no disponible",
  "efficiency ratio not available": "ratio de eficiencia no disponible",
  "total assets not reported": "activos totales no reportados",
  "book value per share history not available": "historial del valor en libros por acción no disponible",
  "premiums earned not reported": "primas devengadas no reportadas",
  "debt or total assets not reported": "deuda o activos totales no reportados",
  "interest expense not reported": "gasto por intereses no reportado",
  "dividends or FFO not reported": "dividendos o FFO no reportados",
  "net income missing for a recent year": "falta la utilidad neta de un año reciente",
  "FFO missing for a recent year": "falta el FFO de un año reciente",
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
  [/^not meaningful for (banks|brokers|insurers|reits): free cash flow means something different for them$/,
    (m) => `no aplica a ${KIND_ES[m[1]!]}: el flujo de caja libre significa otra cosa para ellos`],
  [/^the price is below what today's cash flow is worth even if it shrank (\d+)% a year$/,
    (m) => `el precio está por debajo de lo que vale el flujo de caja de hoy aunque se redujera un ${m[1]}% al año`],
  [/^the price needs over (\d+)% yearly growth$/, (m) => `el precio necesita más de ${m[1]}% de crecimiento anual`],
  [/^reverse DCF: the price implies ([\d.-]+)% yearly growth against ([\d.-]+)%, expectations (low|in line|high|very high) → ([+-]\d+) \((\S+) → (\S+)\)$/,
    (m) => `DCF inverso: el precio implica ${m[1]}% de crecimiento anual frente a ${m[2]}%, expectativas ${ES[m[3]!] ?? m[3]} → ${m[4]} (${m[5]} → ${m[6]})`],
  [/^average of FY(\d+)–FY(\d+)$/, (m) => `promedio del FY${m[1]} al FY${m[2]}`],
  [/^insiders bought (\S+) on the open market, more than they sold$/, (m) => `los directivos compraron ${m[1]} en el mercado abierto, más de lo que vendieron`],
  [/^insiders sold (\S+) outside pre-arranged plans \(([\d.]+)% of the company\) and bought none$/,
    (m) => `los directivos vendieron ${m[1]} fuera de planes programados (${m[2]}% de la empresa) y no compraron`],
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
  // Banks, insurers and REITs (metrics/sector.go, scoring/strength.go).
  [/^\(end ÷ start\)\^\(1\/(\d+)\) − 1, book value per share at fiscal year ends$/, (m) => `(final ÷ inicial)^(1/${m[1]}) − 1, valor en libros por acción al cierre de cada año fiscal`],
  [/^\(end ÷ start\)\^\(1\/(\d+)\) − 1, FFO per share over trailing twelve months$/, (m) => `(final ÷ inicial)^(1/${m[1]}) − 1, FFO por acción de los últimos doce meses`],
  [/^(net income to common shareholders|net income) ÷ average common equity \((.+)\)$/, (m) => `${tx(m[1]!)} ÷ patrimonio común promedio (${tx(m[2]!)})`],
  [/^net income ÷ average total assets \((.+)\)$/, (m) => `utilidad neta ÷ activos totales promedio (${tx(m[1]!)})`],
  [/^net interest income ÷ average total assets \((.+)\); close to the net interest margin, which divides by interest-earning assets only$/,
    (m) => `ingreso neto por intereses ÷ activos totales promedio (${tx(m[1]!)}); cercano al margen financiero, que divide solo entre los activos que generan intereses`],
  [/^average of (\S+) and (\S+)$/, (m) => `promedio del ${m[1]} y el ${m[2]}`],
  [/^at (\d{4}-\d{2}-\d{2})$/, (m) => `al ${m[1]}`],
  [/^no FFO for the twelve months to (\S+)$/, (m) => `sin FFO para los doce meses al ${m[1]}`],
  [/^FFO per share CAGR (\d+)y: (.+)$/, (m) => `CAGR del FFO por acción ${m[1]} años: ${m[2] === "not available" ? "no disponible" : m[2]}`],
  [/^(✓|✗) return on equity (\S+) \(([≥<]) (\S+)\)$/, (m) => `${m[1]} rentabilidad sobre el patrimonio ${m[2]} (${m[3]} ${m[4]})`],
  [/^(✓|✗) efficiency ratio (\S+) \(([≤>]) (\S+)\)$/, (m) => `${m[1]} ratio de eficiencia ${m[2]} (${m[3]} ${m[4]})`],
  [/^(✓|✗) equity is (\S+) of assets \(([≥<]) (\S+)\)$/, (m) => `${m[1]} el patrimonio es el ${m[2]} de los activos (${m[3]} ${m[4]})`],
  [/^(✓|✗) debt is (\S+) of assets \(([≤>]) (\S+)\)$/, (m) => `${m[1]} la deuda es el ${m[2]} de los activos (${m[3]} ${m[4]})`],
  [/^(✓|✗) dividends are (\S+) of FFO \(([≤>]) (\S+)\)$/, (m) => `${m[1]} los dividendos son el ${m[2]} del FFO (${m[3]} ${m[4]})`],
  [/^(✓|✗) FFO before interest covers interest (only )?(\S+)× \(([≥<]) (\S+)×\)$/,
    (m) => `${m[1]} el FFO antes de intereses cubre los intereses ${m[2] ? "solo " : ""}${m[3]}× (${m[4]} ${m[5]}×)`],
  [/^(✓|✗) book value per share (grew|shrank) (\S+) a year over (\d+) years$/,
    (m) => `${m[1]} el valor en libros por acción ${m[2] === "grew" ? "creció" : "cayó"} ${m[3]} al año en ${m[4]} años`],
  [/^(✓|✗) premiums earned (grew|fell) (\S+) in the latest fiscal year$/,
    (m) => `${m[1]} las primas devengadas ${m[2] === "grew" ? "crecieron" : "cayeron"} ${m[3]} en el último año fiscal`],
  [/^(✓|✗) a profit in each of the last (\d+) years$/, (m) => `${m[1]} ganancias en cada uno de los últimos ${m[2]} años`],
  [/^(✓|✗) a loss in at least one of the last (\d+) years$/, (m) => `${m[1]} pérdidas en al menos uno de los últimos ${m[2]} años`],
  [/^(✓|✗) positive FFO in each of the last (\d+) years$/, (m) => `${m[1]} FFO positivo en cada uno de los últimos ${m[2]} años`],
  [/^(✓|✗) FFO was not positive in each of the last (\d+) years$/, (m) => `${m[1]} el FFO no fue positivo en cada uno de los últimos ${m[2]} años`],
  [/^fewer than (\d+) years of (net income|FFO)$/, (m) => `menos de ${m[1]} años de ${m[2] === "FFO" ? "FFO" : "utilidad neta"}`],
  // Notes from any ratio: "<input> not reported", "<input> not available".
  [/^(.+) not reported$/, (m) => `${tx(m[1]!)}: no reportado`],
  [/^(.+) not available$/, (m) => `${tx(m[1]!)}: no disponible`],
  [/^(.+) is zero or negative$/, (m) => `${tx(m[1]!)} es cero o negativo`],
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
