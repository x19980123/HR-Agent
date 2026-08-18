export function fmtTime(v: unknown): string {
  if (!v) return "-";
  const d = new Date(String(v));
  if (Number.isNaN(d.getTime())) return String(v);
  return d.toLocaleString("zh-CN", { hour12: false });
}
