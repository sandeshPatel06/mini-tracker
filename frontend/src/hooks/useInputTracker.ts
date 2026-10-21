import { useEffect } from 'react';
import { apiFetch } from '../api';
import { callGo } from '../services/wailsBridge';

export function useInputTracker(isTrackingActive: boolean) {
  useEffect(() => {
    if (!isTrackingActive) return;

    let totalCount = 0;
    const uniqueKeys = new Set<string>();
    let mouseClicks = 0;

    const handleKeyDown = (e: KeyboardEvent) => {
      totalCount++;
      uniqueKeys.add(e.code || e.key);
    };

    const handleMouseDown = () => {
      mouseClicks++;
    };

    window.addEventListener('keydown', handleKeyDown, { passive: true });
    window.addEventListener('mousedown', handleMouseDown, { passive: true });

    // Sync keyboard + mouse activity to backend every 30 seconds
    const inputFlushInterval = setInterval(() => {
      // Flush keyboard
      if (totalCount > 0) {
        const payload = { total_keys: totalCount, unique_keys: uniqueKeys.size };
        totalCount = 0;
        uniqueKeys.clear();

        const w = window as any;
        if (w.go?.main?.App) {
          callGo(() => w.go.main.App.RecordInputActivity(payload.total_keys, payload.unique_keys));
        } else {
          apiFetch('/api/tracker/input', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
          }).catch(() => {});
        }
      }

      // Flush mouse clicks
      const clicksSnapshot = mouseClicks;
      mouseClicks = 0;

      if (clicksSnapshot > 0) {
        const w = window as any;
        if (w.go?.main?.App) {
          callGo(() => w.go.main.App.RecordMouseActivity(clicksSnapshot));
        } else {
          apiFetch('/api/tracker/input', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ total_clicks: clicksSnapshot }),
          }).catch(() => {});
        }
      }
    }, 30000);

    return () => {
      window.removeEventListener('keydown', handleKeyDown);
      window.removeEventListener('mousedown', handleMouseDown);
      clearInterval(inputFlushInterval);
    };
  }, [isTrackingActive]);
}
