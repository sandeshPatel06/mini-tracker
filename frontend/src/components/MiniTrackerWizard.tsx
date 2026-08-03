import { useState, useEffect, useRef, useCallback } from 'react';
import { ProductivityStats } from '../types';

interface MiniTrackerWizardProps {
  isOpen: boolean;
  onClose: () => void;
  isTrackingActive: boolean;
  onToggleTracking: () => void;
  elapsedSeconds: number;
  stats: ProductivityStats | null;
  today: string;
}

const DEFAULT_POS = { x: window.innerWidth - 324, y: window.innerHeight - 460 };

export function MiniTrackerWizard({
  isOpen,
  onClose,
  isTrackingActive,
  onToggleTracking,
  elapsedSeconds,
  stats,
}: MiniTrackerWizardProps) {
  const [pos, setPos] = useState(DEFAULT_POS);
  const [memo, setMemo] = useState('');
  const dragging = useRef(false);
  const dragOffset = useRef({ x: 0, y: 0 });
  const widgetRef = useRef<HTMLDivElement>(null);

  // ── Drag handlers ────────────────────────────────────────────────────
  const onMouseDown = useCallback((e: React.MouseEvent) => {
    // Don't drag from interactive elements
    if ((e.target as HTMLElement).closest('button, textarea, input')) return;
    e.preventDefault();
    dragging.current = true;
    dragOffset.current = {
      x: e.clientX - pos.x,
      y: e.clientY - pos.y,
    };
  }, [pos]);

  useEffect(() => {
    const onMove = (e: MouseEvent) => {
      if (!dragging.current) return;
      const newX = Math.max(0, Math.min(window.innerWidth - 300, e.clientX - dragOffset.current.x));
      const newY = Math.max(0, Math.min(window.innerHeight - 80, e.clientY - dragOffset.current.y));
      setPos({ x: newX, y: newY });
    };
    const onUp = () => { dragging.current = false; };
    window.addEventListener('mousemove', onMove);
    window.addEventListener('mouseup', onUp);
    return () => {
      window.removeEventListener('mousemove', onMove);
      window.removeEventListener('mouseup', onUp);
    };
  }, []);

  // ── ESC to close ────────────────────────────────────────────────────
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape' && isOpen) onClose(); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [isOpen, onClose]);

  if (!isOpen) return null;

  // ── Helpers ──────────────────────────────────────────────────────────
  const pad = (n: number) => String(n).padStart(2, '0');
  const fmtTimer = (s: number) => {
    const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), ss = s % 60;
    return `${pad(h)}:${pad(m)}:${pad(ss)}`;
  };
  const fmtHrsMins = (s: number) => {
    const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60);
    if (h === 0) return `${m}m`;
    return m > 0 ? `${h}h ${m}m` : `${h}h`;
  };
  const fmtMins = (min: number) => fmtHrsMins(min * 60);

  const productiveMin  = stats?.productive_minutes   ?? 0;
  const unproductiveMin = stats?.unproductive_minutes ?? 0;
  const totalMin        = stats?.total_minutes        ?? (productiveMin + unproductiveMin);
  const score = totalMin > 0 ? Math.round((productiveMin / totalMin) * 100) : 0;
  const accentColor = score >= 70 ? '#14a800' : score >= 40 ? '#e8a000' : '#e11900';

  const SEGS = 10;
  const filled = Math.round((score / 100) * SEGS);

  return (
    <div
      ref={widgetRef}
      style={{
        position: 'fixed',
        left: pos.x,
        top: pos.y,
        zIndex: 9999,
        width: 300,
        userSelect: 'none',
        filter: 'drop-shadow(0 8px 32px rgba(0,0,0,0.45))',
      }}
    >
      <style>{`
        .wt2 {
          background: #ffffff;
          border-radius: 8px;
          overflow: hidden;
          border: 1px solid #d4d4d4;
          font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
          color: #1a1a1a;
        }
        /* OS-style title bar */
        .wt2-titlebar {
          background: #f5f5f5;
          border-bottom: 1px solid #d4d4d4;
          padding: 0 10px;
          height: 36px;
          display: flex;
          align-items: center;
          justify-content: space-between;
          cursor: grab;
        }
        .wt2-titlebar:active { cursor: grabbing; }
        .wt2-title { font-size: 12px; font-weight: 600; color: #333; letter-spacing: 0.01em; }
        .wt2-winctrls { display: flex; gap: 6px; }
        .wt2-wc {
          width: 12px; height: 12px; border-radius: 50%; border: none; cursor: pointer;
          display: flex; align-items: center; justify-content: center; font-size: 8px;
          font-weight: 700; color: transparent; transition: color 0.15s;
        }
        .wt2-wc:hover { color: rgba(0,0,0,0.6); }
        .wt2-wc.close  { background: #ff5f57; }
        .wt2-wc.min    { background: #ffbd2e; }
        .wt2-wc.max    { background: #28c840; }

        /* Project bar */
        .wt2-project {
          background: #fff;
          border-bottom: 1px solid #eee;
          padding: 10px 14px;
        }
        .wt2-proj-name { font-size: 14px; font-weight: 700; color: #14a800; margin-bottom: 2px; }
        .wt2-org-name  { font-size: 12px; color: #666; }

        /* Session block */
        .wt2-session {
          padding: 12px 14px;
          border-bottom: 1px solid #eee;
        }
        .wt2-session-top {
          display: flex;
          align-items: center;
          justify-content: space-between;
          margin-bottom: 6px;
        }
        .wt2-session-lbl { font-size: 11px; color: #999; }
        .wt2-online-pill {
          font-size: 11px; font-weight: 700; color: #999;
        }
        .wt2-clock {
          font-size: 28px;
          font-weight: 700;
          color: #14a800;
          font-variant-numeric: tabular-nums;
          font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
          line-height: 1;
          margin-bottom: 10px;
        }
        /* Toggle switch */
        .wt2-toggle-row {
          display: flex;
          align-items: center;
          justify-content: space-between;
        }
        .wt2-toggle-info { font-size: 11px; color: #999; }
        .wt2-switch {
          position: relative;
          width: 48px;
          height: 26px;
          cursor: pointer;
        }
        .wt2-switch input { opacity: 0; width: 0; height: 0; }
        .wt2-slider {
          position: absolute;
          inset: 0;
          border-radius: 13px;
          transition: background 0.2s;
        }
        .wt2-slider:before {
          content: '';
          position: absolute;
          width: 20px; height: 20px;
          left: 3px; bottom: 3px;
          background: #fff;
          border-radius: 50%;
          transition: transform 0.2s;
          box-shadow: 0 1px 4px rgba(0,0,0,0.25);
        }

        /* Day / week stats */
        .wt2-stats {
          padding: 10px 14px;
          border-bottom: 1px solid #eee;
          display: grid;
          grid-template-columns: 1fr 1fr;
          gap: 6px;
        }
        .wt2-stat-lbl { font-size: 10px; color: #999; margin-bottom: 2px; }
        .wt2-stat-val { font-size: 14px; font-weight: 700; color: #1a1a1a; }

        /* Memo */
        .wt2-memo-wrap { padding: 10px 14px; border-bottom: 1px solid #eee; }
        .wt2-memo {
          width: 100%;
          border: 1px solid #d4d4d4;
          border-radius: 4px;
          font-size: 12px;
          padding: 7px 9px;
          resize: none;
          outline: none;
          color: #333;
          font-family: inherit;
          transition: border-color 0.15s;
          box-sizing: border-box;
          background: #fff;
        }
        .wt2-memo:focus { border-color: #14a800; }
        .wt2-memo::placeholder { color: #bbb; }

        /* Activity bars */
        .wt2-activity {
          padding: 10px 14px;
          border-bottom: 1px solid #eee;
        }
        .wt2-act-header {
          display: flex; justify-content: space-between; margin-bottom: 6px;
          font-size: 11px; color: #999;
        }
        .wt2-bars { display: flex; gap: 2px; height: 18px; align-items: flex-end; }
        .wt2-bar { flex: 1; border-radius: 2px 2px 0 0; transition: background 0.3s; }

        /* Footer */
        .wt2-footer {
          padding: 8px 14px;
          display: flex;
          align-items: center;
          justify-content: space-between;
          background: #f9f9f9;
        }
        .wt2-footer-label { font-size: 11px; font-weight: 600; color: #14a800; }
        .wt2-footer-actions { display: flex; gap: 8px; }
        .wt2-footer-btn {
          background: none; border: none; color: #999; cursor: pointer;
          font-size: 16px; padding: 0; transition: color 0.15s;
        }
        .wt2-footer-btn:hover { color: #333; }
      `}</style>

      <div className="wt2" onMouseDown={onMouseDown}>
        {/* OS-style title bar */}
        <div className="wt2-titlebar">
          <div className="wt2-winctrls">
            <button className="wt2-wc close" onClick={onClose} title="Close">✕</button>
            <button className="wt2-wc min" title="Minimize">–</button>
            <button className="wt2-wc max" title="Maximize">+</button>
          </div>
          <span className="wt2-title">⏱ Time Tracker</span>
          <span style={{ width: 52 }} />
        </div>

        {/* Project row */}
        <div className="wt2-project">
          <div className="wt2-proj-name">get-Hike Productivity</div>
          <div className="wt2-org-name">Your Organization</div>
        </div>

        {/* Current session */}
        <div className="wt2-session">
          <div className="wt2-session-top">
            <span className="wt2-session-lbl">Current Session</span>
            <span className="wt2-online-pill" style={{ color: isTrackingActive ? '#14a800' : '#999' }}>
              {isTrackingActive ? '● Online' : '○ Offline'}
            </span>
          </div>
          <div className="wt2-clock" style={{ color: isTrackingActive ? '#14a800' : '#999' }}>
            {fmtTimer(elapsedSeconds)}
          </div>

          {/* ON/OFF toggle */}
          <div className="wt2-toggle-row">
            <span className="wt2-toggle-info">
              {isTrackingActive ? 'Tracking active' : 'Tracker paused'}
            </span>
            <label className="wt2-switch" title={isTrackingActive ? 'Stop tracking' : 'Start tracking'}>
              <input
                type="checkbox"
                checked={isTrackingActive}
                onChange={onToggleTracking}
              />
              <span
                className="wt2-slider"
                style={{ background: isTrackingActive ? '#14a800' : '#ccc' }}
              >
                <span style={{
                  position: 'absolute',
                  width: 20, height: 20,
                  left: 3, bottom: 3,
                  background: '#fff',
                  borderRadius: '50%',
                  transition: 'transform 0.2s',
                  transform: isTrackingActive ? 'translateX(22px)' : 'translateX(0)',
                  boxShadow: '0 1px 4px rgba(0,0,0,0.25)',
                }} />
              </span>
            </label>
          </div>
        </div>

        {/* Today / week stats */}
        <div className="wt2-stats">
          <div>
            <div className="wt2-stat-lbl">Today</div>
            <div className="wt2-stat-val">{fmtMins(totalMin)}</div>
          </div>
          <div>
            <div className="wt2-stat-lbl">Productive</div>
            <div className="wt2-stat-val" style={{ color: accentColor }}>
              {fmtMins(productiveMin)}
            </div>
          </div>
          <div>
            <div className="wt2-stat-lbl">Score</div>
            <div className="wt2-stat-val" style={{ color: accentColor }}>{score}%</div>
          </div>
          <div>
            <div className="wt2-stat-lbl">Unproductive</div>
            <div className="wt2-stat-val" style={{ color: unproductiveMin > 0 ? '#e11900' : '#999' }}>
              {fmtMins(unproductiveMin)}
            </div>
          </div>
        </div>

        {/* Memo */}
        <div className="wt2-memo-wrap">
          <textarea
            className="wt2-memo"
            rows={2}
            placeholder="What are you working on?"
            value={memo}
            onChange={e => setMemo(e.target.value)}
            onMouseDown={e => e.stopPropagation()}
          />
        </div>

        {/* Activity bars */}
        <div className="wt2-activity">
          <div className="wt2-act-header">
            <span>Activity Level</span>
            <span style={{ color: accentColor, fontWeight: 700 }}>{score}%</span>
          </div>
          <div className="wt2-bars">
            {Array.from({ length: SEGS }, (_, i) => {
              const isFilled = i < filled;
              const heightPct = isFilled ? 40 + Math.round((i / SEGS) * 60) : 20;
              return (
                <div
                  key={i}
                  className="wt2-bar"
                  style={{
                    height: `${heightPct}%`,
                    background: isFilled ? accentColor : '#e0e0e0',
                    opacity: isFilled ? 0.9 : 1,
                  }}
                />
              );
            })}
          </div>
        </div>

        {/* Footer */}
        <div className="wt2-footer">
          <span className="wt2-footer-label">🏢 get-Hike</span>
          <div className="wt2-footer-actions">
            <button
              className="wt2-footer-btn"
              title="Reset position"
              onClick={() => setPos(DEFAULT_POS)}
              onMouseDown={e => e.stopPropagation()}
            >
              ⌖
            </button>
            <button
              className="wt2-footer-btn"
              title="Close"
              onClick={onClose}
              onMouseDown={e => e.stopPropagation()}
            >
              ✕
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
