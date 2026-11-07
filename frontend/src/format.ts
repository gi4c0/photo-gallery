// Dates from the API are plain YYYY-MM-DD strings. Parse them at noon local
// time so formatting never shifts them across a day boundary.
function parse(iso: string): Date {
  return new Date(iso + 'T12:00:00');
}

export function todayISO(): string {
  const d = new Date();
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
}

export function fmtDate(iso: string): string {
  return parse(iso).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' });
}

export function fmtLongDate(iso: string): string {
  return parse(iso).toLocaleDateString('en-US', { month: 'long', day: 'numeric', year: 'numeric' });
}

// expiresOn is the first day downloads are closed; the last open day is the one before.
export function lastOpenDay(expiresOn: string): string {
  const d = parse(expiresOn);
  d.setDate(d.getDate() - 1);
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' });
}

// Mirrors the server's sharedOn + months, for the "until" hint before a gallery exists.
export function previewLastOpenDay(months: number): string {
  const d = parse(todayISO());
  d.setMonth(d.getMonth() + months);
  d.setDate(d.getDate() - 1);
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' });
}

// For full timestamps (RFC 3339), shown in the viewer's local time.
export function fmtDateTime(iso: string): string {
  return new Date(iso).toLocaleString('en-US', {
    month: 'short', day: 'numeric', year: 'numeric', hour: 'numeric', minute: '2-digit',
  });
}

// Download statistics, e.g. "2 downloads · last Nov 5, 2026, 2:02 PM".
export function downloadsText(count = 0, last?: string): string {
  return count && last ? `${plural(count, 'download')} · last ${fmtDateTime(last)}` : 'Not downloaded yet';
}

export function plural(n: number, one: string, many = one + 's'): string {
  return `${n} ${n === 1 ? one : many}`;
}
