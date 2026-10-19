import type { WorkSession } from './types';

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

// sessionGapMs splits consecutive captures into separate work sessions when
// this much inactivity passes between them.
const SESSION_GAP_MS = 15 * 60 * 1000;

interface SessionSourceLog {
  id: number;
  timestamp: string;
  app_name?: string;
  app_category?: string;
  ai_category: string;
  is_productive: boolean;
}

// buildLocalWorkSessions derives work sessions from locally stored captures so
// the desktop app can show the Work Sessions card without a server backend.
// Sessions are dropped below 3 captures to avoid micromanaging idle noise.
export function buildLocalWorkSessions(logs: SessionSourceLog[]): WorkSession[] {
  const ascending = [...logs].sort(
    (a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime()
  );

  type Draft = {
    start: string;
    end: string;
    count: number;
    productive: number;
    apps: Map<string, number>;
    last: number;
  };
  const drafts: Draft[] = [];

  for (const log of ascending) {
    const ts = new Date(log.timestamp).getTime();
    if (Number.isNaN(ts)) continue;
    const last = drafts[drafts.length - 1];
    if (!last || ts - last.last > SESSION_GAP_MS) {
      drafts.push({
        start: log.timestamp,
        end: log.timestamp,
        count: 1,
        productive: log.is_productive ? 1 : 0,
        apps: new Map<string, number>(),
        last: ts,
      });
    } else {
      last.count += 1;
      if (log.is_productive) last.productive += 1;
      last.end = log.timestamp;
      last.last = ts;
    }
    const current = drafts[drafts.length - 1];
    const appKey = log.app_name || log.ai_category || 'Unknown';
    current.apps.set(appKey, (current.apps.get(appKey) || 0) + 1);
  }

  const sessions: WorkSession[] = [];
  drafts.forEach((d, i) => {
    if (d.count < 3) return;
    const topApp = [...d.apps.entries()].sort((a, b) => b[1] - a[1])[0]?.[0] || 'Desktop Session';
    sessions.push({
      id: -(i + 1),
      title: topApp,
      summary: `${d.productive}/${d.count} captures rated productive${d.productive < d.count ? ' · pending AI analysis included' : ''}`,
      start_time: d.start,
      end_time: d.end,
      log_count: d.count,
      productive_pct: Math.round((d.productive / d.count) * 100),
      top_app_name: topApp,
    });
  });
  return sessions;
}

export function formatCompactNumber(n: number): string {
  if (!Number.isFinite(n)) return '0';
  return new Intl.NumberFormat(undefined, { notation: 'compact', maximumFractionDigits: 1 }).format(n);
}

export const FOCUS_GOOD = 70;
export const FOCUS_OK = 50;
