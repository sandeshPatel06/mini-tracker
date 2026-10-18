export function localISODate(d: Date = new Date()): string {
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, '0');
  const day = String(d.getDate()).padStart(2, '0');
  return `${y}-${m}-${day}`;
}

export function isPendingLog(log: { ai_category?: string | null; ai_reason?: string | null }): boolean {
  const reason = log.ai_reason || '';
  return !log.ai_category || log.ai_category === 'Unknown' ||
    reason.includes('No Gemini API key') || reason.includes('Offline Mode') ||
    reason.includes('API key') || reason.includes('No key');
}

export function formatCompactNumber(n: number): string {
  if (!Number.isFinite(n)) return '0';
  return new Intl.NumberFormat(undefined, { notation: 'compact', maximumFractionDigits: 1 }).format(n);
}

export const FOCUS_GOOD = 70;
export const FOCUS_OK = 50;
