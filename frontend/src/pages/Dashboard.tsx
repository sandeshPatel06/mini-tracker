import { useState, useEffect } from 'react';
import type { ReactNode } from 'react';
import { LogEntry, ProductivityStats, AppConfig, WorkSession } from '../types';
import { apiFetch } from '../api';
import { format } from 'date-fns';
import { CategoryBadge } from '../components/CategoryBadge';
import { ScreenshotThumb, ImageModal } from '../components/Screenshot';
import { Icon } from '../components/Icon';
import { AppIcon } from '../components/AppIcon';
import { ResponsiveContainer, BarChart, Bar, XAxis, YAxis, Tooltip, Cell } from 'recharts';
import { localISODate, isPendingLog, buildLocalWorkSessions, FOCUS_GOOD, FOCUS_OK } from '../utils';

const prefersReducedMotion =
  typeof window !== 'undefined' &&
  typeof window.matchMedia === 'function' &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches;

function focusColor(score: number): string {
  return score >= FOCUS_GOOD
    ? 'var(--accent-green)'
    : score >= FOCUS_OK
    ? 'var(--accent-amber)'
    : 'var(--accent-red)';
}

function focusBarColor(score: number): string {
  return score >= FOCUS_GOOD
    ? 'var(--accent-green)'
    : score >= FOCUS_OK
    ? 'var(--accent-indigo)'
    : 'var(--accent-red)';
}

declare const window: Window & {
  go?: {
    main: {
      App: {
        UpdateGeminiAPIKey: (key: string) => Promise<boolean>;
        ProcessPendingLogs: () => Promise<number>;
        GetLogsByDate: (date: string) => Promise<LogEntry[] | null>;
      };
    };
  };
};

interface Props {
  logs: LogEntry[];
  stats: ProductivityStats | null;
  config: AppConfig | null;
  loading: boolean;
  today: string;
  onRefresh?: () => void;
  onOpenSettings?: () => void;
  loadError?: string | null;
  onRetry?: () => void;
}

function StatCard({
  label,
  value,
  valueTitle,
  sub,
  valueClass,
  valueColor,
  leading,
}: {
  label: string;
  value: string | number;
  valueTitle?: string;
  sub?: string;
  valueClass?: string;
  valueColor?: string;
  leading?: ReactNode;
}) {
  return (
    <div className="stat-card">
      <span className="stat-label">{label}</span>
      <div
        className={`stat-value${valueClass ? ` ${valueClass}` : ''}`}
        title={valueTitle}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 8,
          fontSize: 24,
          fontVariantNumeric: 'tabular-nums',
          overflow: 'hidden',
          whiteSpace: 'nowrap',
          color: valueColor,
        }}
      >
        {leading}
        <span style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis' }}>{value}</span>
      </div>
      {sub && <div className="stat-sub">{sub}</div>}
    </div>
  );
}

export default function Dashboard({ logs, stats, config, loading, today, onRefresh, onOpenSettings, loadError, onRetry }: Props) {
  const [selectedImage, setSelectedImage] = useState<string | null>(null);
  const [processingPending, setProcessingPending] = useState(false);
  const [workSessions, setWorkSessions] = useState<WorkSession[]>([]);

  useEffect(() => {
    if (!today) return;

    // Desktop runs fully local (no server required): derive the Work Sessions
    // card from the captures we already have in the local database.
    if (window.go?.main?.App?.GetLogsByDate) {
      setWorkSessions(buildLocalWorkSessions(logs));
      return;
    }

    apiFetch(`/api/work-sessions?date=${today}`)
      .then((res) => res.json())
      .then((data) => {
        if (Array.isArray(data) && data.length > 0) {
          setWorkSessions(data);
        } else {
          setWorkSessions(buildLocalWorkSessions(logs));
        }
      })
      .catch(() => setWorkSessions(buildLocalWorkSessions(logs)));
  }, [today, logs]);

  const pendingCount = logs.filter(isPendingLog).length;

  const analyzedLogs = logs.filter((l) => !isPendingLog(l));

  // Calculate Average AI-Returned Productivity Score
  const avgProductivityScore = analyzedLogs.length > 0
    ? Math.round(
        analyzedLogs.reduce(
          (acc, l) => acc + (typeof l.productive_score === 'number' ? l.productive_score : l.is_productive ? 100 : 0),
          0
        ) / analyzedLogs.length
      )
    : 0;

  const totalKeys = logs.reduce((acc, l) => acc + l.total_keys, 0);
  const avgEntropy = logs.length
    ? logs.reduce((acc, l) => acc + l.entropy_score, 0) / logs.length
    : 0;

  // App Usage Breakdown calculation
  const appCounts: Record<string, { count: number; category: string; totalScore: number }> = {};
  logs.forEach((l) => {
    const app = l.app_name || 'Other';
    if (!appCounts[app]) {
      appCounts[app] = { count: 0, category: l.app_category || 'Application', totalScore: 0 };
    }
    appCounts[app].count++;
    appCounts[app].totalScore += l.productive_score !== undefined ? l.productive_score : l.is_productive ? 100 : 0;
  });

  const totalLogCount = logs.length || 1;
  const topApps = Object.entries(appCounts)
    .map(([appName, data]) => ({
      appName,
      category: data.category,
      count: data.count,
      percent: Math.round((data.count / totalLogCount) * 100),
      avgScore: Math.round(data.totalScore / data.count),
    }))
    .sort((a, b) => b.count - a.count)
    .slice(0, 5);

  // Hourly Productivity Trend data calculation
  const hourlyMap: Record<string, { hour: string; totalScore: number; count: number }> = {};
  for (let i = 8; i <= 20; i++) {
    const key = `${i.toString().padStart(2, '0')}:00`;
    hourlyMap[key] = { hour: key, totalScore: 0, count: 0 };
  }

  logs.forEach((l) => {
    try {
      const date = new Date(l.timestamp);
      const h = date.getHours();
      if (h >= 8 && h <= 20) {
        const key = `${h.toString().padStart(2, '0')}:00`;
        const score = l.productive_score !== undefined ? l.productive_score : l.is_productive ? 100 : 0;
        if (hourlyMap[key]) {
          hourlyMap[key].totalScore += score;
          hourlyMap[key].count++;
        }
      }
    } catch {}
  });

  const hourlyChartData = Object.values(hourlyMap).map((d) => ({
    hour: d.hour,
    score: d.count > 0 ? Math.round(d.totalScore / d.count) : 0,
    count: d.count,
  }));

  // Latest 10 entries for Activity Feed
  const recent = [...logs].reverse().slice(0, 10);

  const dateLabel = today === localISODate()
    ? 'Today'
    : format(new Date(today + 'T00:00:00'), 'MMMM d, yyyy');

  const headerMeta = `${dateLabel} · ${logs.length} captures${config ? ` · every ${config.screenshot_interval_seconds}s` : ''}`;

  const handleProcessPending = async () => {
    setProcessingPending(true);
    try {
      if (window.go?.main?.App?.ProcessPendingLogs) {
        await window.go.main.App.ProcessPendingLogs();
      } else {
        await apiFetch('/api/process-pending', { method: 'POST' });
      }
      if (onRefresh) onRefresh();
    } catch (err) {
      console.error('Failed to process pending logs:', err);
    } finally {
      setProcessingPending(false);
    }
  };

  if (loading && logs.length === 0) {
    return (
      <div>
        <div className="page-header">
          <div>
            <div className="skeleton" style={{ width: 160, height: 28, marginBottom: 8 }} />
            <div className="skeleton" style={{ width: 220, height: 16 }} />
          </div>
        </div>
        <div className="stats-grid" style={{ marginTop: 16 }}>
          {[1, 2, 3, 4].map((i) => (
            <div key={i} className="stat-card">
              <div className="skeleton" style={{ width: '60%', height: 14, marginBottom: 12 }} />
              <div className="skeleton" style={{ width: '40%', height: 32 }} />
            </div>
          ))}
        </div>
      </div>
    );
  }

  return (
    <div aria-busy={loading}>
      <ImageModal imagePath={selectedImage} onClose={() => setSelectedImage(null)} />

      {/* Header Bar */}
      <div className="page-header" style={{ marginBottom: 20 }}>
        <div style={{ minWidth: 0 }}>
          <h1 className="page-title">Productivity Intelligence</h1>
          <p
            className="page-subtitle"
            title={headerMeta}
            style={{ fontVariantNumeric: 'tabular-nums', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
          >
            {headerMeta}
          </p>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
          {onRefresh && (
            <button
              onClick={onRefresh}
              className="btn btn-secondary"
              title="Refresh dashboard data"
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 6,
                padding: '10px 18px',
                fontSize: 14,
                fontWeight: 600,
                whiteSpace: 'nowrap',
              }}
            >
              <Icon name="refresh" size={14} />
              <span>Refresh</span>
            </button>
          )}
          {pendingCount > 0 && (
            <button
              onClick={handleProcessPending}
              disabled={processingPending}
              className="btn btn-primary"
              title={`Run AI analysis on ${pendingCount} pending capture${pendingCount === 1 ? '' : 's'}`}
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 6,
                padding: '10px 22px',
                fontSize: 14,
                fontWeight: 700,
                whiteSpace: 'nowrap',
                opacity: processingPending ? 0.6 : undefined,
                cursor: processingPending ? 'not-allowed' : undefined,
              }}
            >
              <Icon name="sparkles" size={14} />
              <span>{processingPending ? 'Analyzing…' : `Analyze ${pendingCount} Pending`}</span>
            </button>
          )}
        </div>
      </div>

      {/* Metric Cards Grid */}
      <div className="stats-grid" style={{ marginBottom: 16 }}>
        <StatCard
          label="AI Focus Rating"
          value={analyzedLogs.length === 0 ? '—' : `${avgProductivityScore}%`}
          sub={analyzedLogs.length === 0 ? 'No captures analyzed yet' : 'Gemini AI vision rating'}
          valueColor={analyzedLogs.length === 0 ? 'var(--text-secondary)' : focusColor(avgProductivityScore)}
        />
        <StatCard
          label="Total Keystrokes"
          value={totalKeys.toLocaleString()}
          sub="Recorded keyboard activity"
          valueClass="text-cyan"
        />
        <StatCard
          label="Keystroke Variety (Entropy)"
          value={avgEntropy.toFixed(1)}
          sub="Typing variety (0-100)"
          valueClass="text-amber"
        />
        <StatCard
          label="Top Application"
          value={topApps[0]?.appName || stats?.top_category || '—'}
          valueTitle={topApps[0]?.appName || stats?.top_category || undefined}
          sub={topApps[0] ? `${topApps[0].percent}% of captures` : `${logs.length} snapshots recorded`}
          valueClass="text-teal"
          leading={topApps[0] ? <AppIcon name={topApps[0].appName} size={22} /> : undefined}
        />
      </div>

      {/* Hourly Trend & App Breakdown Grid */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(360px, 1fr))', gap: 16, marginBottom: 16 }}>

        {/* Hourly Focus Score Chart */}
        <div className="card">
          <div className="card-header">
            <span className="card-title">📈 Hourly Focus</span>
            <span style={{ fontSize: 12, fontWeight: 600, color: 'var(--purple-text)', fontVariantNumeric: 'tabular-nums' }}>
              Avg {analyzedLogs.length === 0 ? '—' : `${avgProductivityScore}%`}
            </span>
          </div>
          <div className="card-body">
            <div style={{ width: '100%', height: 210 }}>
              <ResponsiveContainer width="100%" height="100%">
                <BarChart data={hourlyChartData} margin={{ top: 10, right: 10, left: -20, bottom: 0 }}>
                  <XAxis dataKey="hour" tick={{ fontSize: 11, fill: 'var(--text-muted)' }} axisLine={false} tickLine={false} />
                  <YAxis domain={[0, 100]} tick={{ fontSize: 11, fill: 'var(--text-muted)' }} axisLine={false} tickLine={false} />
                  <Tooltip
                    cursor={{ fill: 'color-mix(in srgb, var(--accent-primary) 8%, transparent)' }}
                    contentStyle={{
                      backgroundColor: 'var(--bg-elevated)',
                      borderColor: 'var(--border-medium)',
                      borderRadius: 8,
                      fontSize: 12,
                      color: 'var(--text-primary)',
                    }}
                    labelStyle={{ color: 'var(--text-primary)', fontWeight: 600 }}
                    itemStyle={{ color: 'var(--text-secondary)', fontVariantNumeric: 'tabular-nums' }}
                    formatter={(val: any, _name: any, item: any) => {
                      const captures = item?.payload?.count ?? 0;
                      return [
                        captures === 0 ? 'No captures' : `${val}% AI Focus Rating`,
                        captures === 0 ? 'Focus rating' : `Focus · ${captures} capture${captures === 1 ? '' : 's'}`,
                      ];
                    }}
                  />
                  <Bar dataKey="score" radius={[4, 4, 0, 0]} isAnimationActive={!prefersReducedMotion}>
                    {hourlyChartData.map((entry, index) => (
                      <Cell
                        key={`cell-${index}`}
                        fill={
                          entry.score >= 75
                            ? 'var(--accent-green)'
                            : entry.score >= 50
                            ? 'var(--accent-indigo)'
                            : entry.score > 0
                            ? 'var(--accent-amber)'
                            : 'var(--border-medium)'
                        }
                      />
                    ))}
                  </Bar>
                </BarChart>
              </ResponsiveContainer>
            </div>
          </div>
        </div>

        {/* Top App Usage Share */}
        <div className="card">
          <div className="card-header">
            <span className="card-title">🏆 Top Apps</span>
            <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>Share of captures</span>
          </div>
          <div className="card-body">
            <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
              {topApps.length === 0 ? (
                <div style={{ fontSize: 13, color: 'var(--text-muted)', textAlign: 'center', padding: '30px 0' }}>
                  No active application data logged yet
                </div>
              ) : (
                topApps.map((app) => (
                  <div key={app.appName} style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, fontSize: 13 }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0, flex: 1 }}>
                        <AppIcon name={app.appName} size={16} />
                        <span title={app.appName} style={{ fontWeight: 600, color: 'var(--text-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{app.appName}</span>
                        <span title={app.category} style={{ fontSize: 11, color: 'var(--text-muted)', maxWidth: 120, flexShrink: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>({app.category})</span>
                      </div>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexShrink: 0 }}>
                        <span style={{ fontSize: 12, fontWeight: 600, fontVariantNumeric: 'tabular-nums', color: app.avgScore >= 60 ? 'var(--badge-productive-text)' : 'var(--badge-unproductive-text)' }}>
                          {app.avgScore}% Focus
                        </span>
                        <span style={{ fontSize: 12, color: 'var(--text-muted)', width: 36, textAlign: 'right', fontVariantNumeric: 'tabular-nums' }}>
                          {app.percent}%
                        </span>
                      </div>
                    </div>
                    <div style={{ width: '100%', height: 6, background: 'var(--bg-elevated)', borderRadius: 99, overflow: 'hidden' }}>
                      <div
                        style={{
                          width: `${app.percent}%`,
                          height: '100%',
                          backgroundColor: focusBarColor(app.avgScore),
                          borderRadius: 99,
                        }}
                      />
                    </div>
                  </div>
                ))
              )}
            </div>
          </div>
        </div>

      </div>

      {/* Work Sessions Timeline Block */}
      {workSessions.length > 0 && (
        <div className="card" style={{ marginBottom: 16 }}>
          <div className="card-header">
            <span className="card-title">⏱️ Work Sessions</span>
            <span style={{ fontSize: 12, color: 'var(--text-muted)', fontVariantNumeric: 'tabular-nums' }}>
              {workSessions.length} session{workSessions.length === 1 ? '' : 's'}
            </span>
          </div>
          <div className="card-body" style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
            {workSessions.map((session) => (
              <div
                key={session.id}
                style={{
                  padding: '10px 14px',
                  background: 'var(--bg-surface)',
                  border: '1px solid var(--border-subtle)',
                  borderRadius: 'var(--radius-sm)',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  gap: 16,
                  minWidth: 0,
                }}
              >
                <div style={{ minWidth: 0, flex: 1 }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 6, minWidth: 0 }}>
                    {session.top_app_name && <AppIcon name={session.top_app_name} size={16} />}
                    <span title={session.title} style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {session.title}
                    </span>
                  </div>
                  <div title={session.summary} style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {session.summary}
                  </div>
                </div>

                <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexShrink: 0 }}>
                  <span
                    className={`badge ${session.productive_pct >= 60 ? 'badge-productive' : 'badge-unproductive'}`}
                    style={{ fontVariantNumeric: 'tabular-nums' }}
                  >
                    {Math.round(session.productive_pct)}% Focus
                  </span>
                  <span style={{ fontSize: 12, color: 'var(--text-muted)', whiteSpace: 'nowrap', fontVariantNumeric: 'tabular-nums' }}>
                    {session.log_count} captures
                  </span>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Activity Stream Feed */}
      <div className="card">
        <div className="card-header">
          <span className="card-title">📸 Activity Stream</span>
          <span style={{ fontSize: 12, color: 'var(--text-muted)', fontVariantNumeric: 'tabular-nums' }}>
            Latest {recent.length} captures
          </span>
        </div>

        <div className="card-body" style={{ padding: '12px 14px' }}>
          {recent.length === 0 ? (
            <div className="empty-state">
              <div className="empty-state-icon" style={{ display: 'flex', justifyContent: 'center', color: 'var(--text-muted)', marginBottom: 12 }}>
                <Icon name="inbox" size={32} />
              </div>
              <div className="empty-state-title">No Activity Captured Yet</div>
              <div className="empty-state-desc">
                The tracking daemon will automatically log desktop screenshots and keystroke activity.
              </div>
            </div>
          ) : (
            <div className="timeline-list">
              {recent.map((log) => {
                const scoreVal = typeof log.productive_score === 'number'
                  ? Math.round(log.productive_score)
                  : log.is_productive ? 100 : 0;
                return (
                  <div key={log.id} className="timeline-item">
                    <span className="timeline-time">
                      {format(new Date(log.timestamp), 'HH:mm')}
                    </span>

                    <ScreenshotThumb
                      imagePath={log.image_path}
                      onClick={() => setSelectedImage(log.image_path)}
                    />

                    <div className="timeline-info">
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
                        <CategoryBadge category={log.ai_category} />
                        {log.app_name && (
                          <span
                            title={log.app_name}
                            style={{
                              display: 'inline-flex',
                              alignItems: 'center',
                              gap: 5,
                              fontSize: 11,
                              fontWeight: 600,
                              padding: '2px 8px',
                              borderRadius: 4,
                              background: 'var(--bg-elevated)',
                              color: 'var(--text-secondary)',
                              border: '1px solid var(--border-subtle)',
                              maxWidth: 180,
                              overflow: 'hidden',
                              whiteSpace: 'nowrap',
                            }}
                          >
                            <AppIcon name={log.app_name} size={14} />
                            <span style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis' }}>
                              {log.app_name}
                            </span>
                          </span>
                        )}
                        {log.app_category && (
                          <span
                            title={log.app_category}
                            style={{
                              fontSize: 11,
                              color: 'var(--text-muted)',
                              fontStyle: 'italic',
                              maxWidth: 160,
                              overflow: 'hidden',
                              textOverflow: 'ellipsis',
                              whiteSpace: 'nowrap',
                            }}
                          >
                            ({log.app_category})
                          </span>
                        )}
                      </div>

                      {log.window_title && (
                        <div
                          title={log.window_title}
                          style={{
                            fontSize: 12,
                            fontWeight: 600,
                            color: 'var(--text-primary)',
                            marginTop: 4,
                            whiteSpace: 'nowrap',
                            overflow: 'hidden',
                            textOverflow: 'ellipsis',
                          }}
                        >
                          🪟 {log.window_title}
                        </div>
                      )}

                      <div className="timeline-reason" style={{ marginTop: 2 }} title={log.ai_reason || undefined}>
                        {log.ai_reason || '—'}
                      </div>
                    </div>

                    <div>
                      {!log.ai_category || log.ai_reason.includes('API key') || log.ai_reason.includes('No key') ? (
                        <span className="badge badge-pending">
                          Pending AI
                        </span>
                      ) : (
                        <span
                          className={`badge ${
                            scoreVal >= 50
                              ? 'badge-productive'
                              : 'badge-unproductive'
                          }`}
                          style={{ fontVariantNumeric: 'tabular-nums' }}
                        >
                          <Icon
                            name={scoreVal >= 50 ? 'check' : 'x'}
                            size={13}
                          />
                          {scoreVal}% Focus
                        </span>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
