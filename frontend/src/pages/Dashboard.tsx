import { useState, useEffect } from 'react';
import { LogEntry, ProductivityStats, AppConfig, WorkSession } from '../types';
import { apiFetch } from '../api';
import { format } from 'date-fns';
import { CategoryBadge } from '../components/CategoryBadge';
import { ScreenshotThumb, ImageModal } from '../components/Screenshot';
import { Icon, IconName } from '../components/Icon';
import { ResponsiveContainer, BarChart, Bar, XAxis, YAxis, Tooltip, Cell } from 'recharts';

declare const window: Window & {
  go?: {
    main: {
      App: {
        UpdateGeminiAPIKey: (key: string) => Promise<boolean>;
        ProcessPendingLogs: () => Promise<number>;
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
}

function StatCard({
  label,
  value,
  sub,
  iconName,
  iconColor = 'var(--text-secondary)',
}: {
  label: string;
  value: string | number;
  sub?: string;
  iconName: IconName;
  iconColor?: string;
}) {
  return (
    <div className="stat-card">
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 12 }}>
        <span className="stat-label">{label}</span>
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            width: 34,
            height: 34,
            borderRadius: 8,
            backgroundColor: 'var(--bg-elevated)',
            color: iconColor,
            border: '1px solid var(--border-subtle)',
          }}
        >
          <Icon name={iconName} size={18} />
        </div>
      </div>
      <div className="stat-value">{value}</div>
      {sub && <div className="stat-sub">{sub}</div>}
    </div>
  );
}

export default function Dashboard({ logs, stats, config, loading, today, onRefresh }: Props) {
  const [selectedImage, setSelectedImage] = useState<string | null>(null);
  const [processingPending, setProcessingPending] = useState(false);
  const [workSessions, setWorkSessions] = useState<WorkSession[]>([]);

  useEffect(() => {
    if (today) {
      apiFetch(`/api/work-sessions?date=${today}`)
        .then((res) => res.json())
        .then((data) => {
          if (Array.isArray(data)) setWorkSessions(data);
        })
        .catch(() => {});
    }
  }, [today, logs.length]);

  const pendingCount = logs.filter(
    (l) => !l.ai_category || l.ai_category === 'Unknown' || l.ai_reason.includes('No Gemini API key')
  ).length;

  // Filter analyzed logs
  const analyzedLogs = logs.filter(
    (l) => l.ai_category && l.ai_category !== 'Unknown' && !l.ai_reason.includes('No Gemini API key')
  );

  // Calculate Average AI-Returned Productivity Score
  const avgProductivityScore = analyzedLogs.length > 0
    ? Math.round(
        analyzedLogs.reduce(
          (acc, l) => acc + (l.productive_score !== undefined && l.productive_score > 0 ? l.productive_score : l.is_productive ? 100 : 0),
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

  const dateLabel = today === new Date().toISOString().slice(0, 10)
    ? 'Today'
    : format(new Date(today + 'T00:00:00'), 'MMMM d, yyyy');

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

  if (loading) {
    return (
      <div>
        <div className="page-header">
          <div>
            <div className="skeleton" style={{ width: 160, height: 28, marginBottom: 8 }} />
            <div className="skeleton" style={{ width: 220, height: 16 }} />
          </div>
        </div>
        <div className="stats-grid" style={{ marginTop: 24 }}>
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
    <div>
      <ImageModal imagePath={selectedImage} onClose={() => setSelectedImage(null)} />

      {/* Header Bar */}
      <div className="page-header" style={{ marginBottom: 24 }}>
        <div>
          <h1 className="page-title" style={{ fontSize: 24, fontWeight: 700, letterSpacing: '-0.4px', color: 'var(--text-primary)' }}>
            Productivity Intelligence
          </h1>
          <div className="page-subtitle" style={{ display: 'flex', alignItems: 'center', gap: 10, marginTop: 4, fontSize: 13, color: 'var(--text-muted)' }}>
            <span>{dateLabel}</span>
            <span>•</span>
            <span>{logs.length} activity captures analyzed</span>
            {config && (
              <>
                <span>•</span>
                <span>Interval: {config.screenshot_interval_seconds}s</span>
              </>
            )}
          </div>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
          {pendingCount > 0 && (
            <button
              onClick={handleProcessPending}
              disabled={processingPending}
              className="btn btn-secondary"
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 6,
                height: 36,
                padding: '0 14px',
                fontSize: 13,
                fontWeight: 500,
                whiteSpace: 'nowrap',
              }}
            >
              <Icon name="sparkles" size={14} />
              <span>{processingPending ? 'Analyzing...' : `Analyze ${pendingCount} Pending`}</span>
            </button>
          )}

          {onRefresh && (
            <button
              onClick={onRefresh}
              className="btn btn-secondary"
              title="Refresh Dashboard Data"
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 6,
                height: 36,
                padding: '0 14px',
                fontSize: 13,
                fontWeight: 500,
                whiteSpace: 'nowrap',
              }}
            >
              <Icon name="refresh" size={14} />
              <span>Refresh</span>
            </button>
          )}
        </div>
      </div>

      {/* Metric Cards Grid */}
      <div className="stats-grid" style={{ marginBottom: 24 }}>
        <StatCard
          label="AI Focus Rating"
          value={`${avgProductivityScore}%`}
          sub={`Evaluated dynamically by Gemini AI vision`}
          iconName="target"
          iconColor={avgProductivityScore >= 70 ? '#10b981' : avgProductivityScore >= 50 ? '#eab308' : '#ef4444'}
        />
        <StatCard
          label="Total Keystrokes"
          value={totalKeys.toLocaleString()}
          sub="Recorded active keyboard activity"
          iconName="keyboard"
          iconColor="#6366f1"
        />
        <StatCard
          label="Keystroke Variety (Entropy)"
          value={avgEntropy.toFixed(1)}
          sub="Typing complexity score (0 - 100)"
          iconName="activity"
          iconColor="#06b6d4"
        />
        <StatCard
          label="Top Application"
          value={topApps[0]?.appName || stats?.top_category || '—'}
          sub={topApps[0] ? `${topApps[0].percent}% of work time` : `${logs.length} snapshots recorded`}
          iconName="award"
          iconColor="#eab308"
        />
      </div>

      {/* Hourly Trend & App Breakdown Grid */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(360px, 1fr))', gap: 24, marginBottom: 24 }}>
        
        {/* Hourly Focus Score Chart */}
        <div className="card" style={{ padding: '20px' }}>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16 }}>
            <div>
              <h3 style={{ fontSize: 15, fontWeight: 600, color: 'var(--text-primary)', margin: 0 }}>
                Hourly Focus Distribution
              </h3>
              <p style={{ fontSize: 12, color: 'var(--text-muted)', margin: '2px 0 0 0' }}>
                AI-assessed focus percentage (0-100%) across active work hours
              </p>
            </div>
            <span style={{ fontSize: 12, fontWeight: 600, color: 'var(--accent-purple)' }}>
              Avg: {avgProductivityScore}%
            </span>
          </div>

          <div style={{ width: '100%', height: 210 }}>
            <ResponsiveContainer width="100%" height="100%">
              <BarChart data={hourlyChartData} margin={{ top: 10, right: 10, left: -20, bottom: 0 }}>
                <XAxis dataKey="hour" tick={{ fontSize: 11, fill: 'var(--text-muted)' }} axisLine={false} tickLine={false} />
                <YAxis domain={[0, 100]} tick={{ fontSize: 11, fill: 'var(--text-muted)' }} axisLine={false} tickLine={false} />
                <Tooltip
                  contentStyle={{
                    backgroundColor: 'var(--bg-surface)',
                    borderColor: 'var(--border-medium)',
                    borderRadius: 8,
                    fontSize: 12,
                    color: 'var(--text-primary)',
                  }}
                  formatter={(val: any) => [`${val}% AI Focus Rating`, 'Productivity']}
                />
                <Bar dataKey="score" radius={[4, 4, 0, 0]}>
                  {hourlyChartData.map((entry, index) => (
                    <Cell
                      key={`cell-${index}`}
                      fill={
                        entry.score >= 75
                          ? '#10b981'
                          : entry.score >= 50
                          ? '#6366f1'
                          : entry.score > 0
                          ? '#f59e0b'
                          : 'var(--border-subtle)'
                      }
                    />
                  ))}
                </Bar>
              </BarChart>
            </ResponsiveContainer>
          </div>
        </div>

        {/* Top App Usage Share */}
        <div className="card" style={{ padding: '20px' }}>
          <div style={{ marginBottom: 16 }}>
            <h3 style={{ fontSize: 15, fontWeight: 600, color: 'var(--text-primary)', margin: 0 }}>
              Top Applications & Work Distribution
            </h3>
            <p style={{ fontSize: 12, color: 'var(--text-muted)', margin: '2px 0 0 0' }}>
              Time allocation & AI productivity ratings by desktop application
            </p>
          </div>

          <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
            {topApps.length === 0 ? (
              <div style={{ fontSize: 13, color: 'var(--text-muted)', textAlign: 'center', padding: '30px 0' }}>
                No active application data logged yet
              </div>
            ) : (
              topApps.map((app) => (
                <div key={app.appName} style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                  <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', fontSize: 13 }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                      <span style={{ fontWeight: 600, color: 'var(--text-primary)' }}>{app.appName}</span>
                      <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>({app.category})</span>
                    </div>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                      <span style={{ fontSize: 12, fontWeight: 600, color: app.avgScore >= 60 ? 'var(--accent-green)' : 'var(--accent-red)' }}>
                        {app.avgScore}% Focus
                      </span>
                      <span style={{ fontSize: 12, color: 'var(--text-muted)', width: 36, textAlign: 'right' }}>
                        {app.percent}%
                      </span>
                    </div>
                  </div>
                  <div style={{ width: '100%', height: 6, background: 'var(--bg-elevated)', borderRadius: 99, overflow: 'hidden' }}>
                    <div
                      style={{
                        width: `${app.percent}%`,
                        height: '100%',
                        backgroundColor: app.avgScore >= 70 ? '#10b981' : app.avgScore >= 50 ? '#6366f1' : '#ef4444',
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

      {/* Work Sessions Timeline Block */}
      {workSessions.length > 0 && (
        <div className="card" style={{ marginBottom: 24 }}>
          <div className="card-header" style={{ padding: '16px 20px', borderBottom: '1px solid var(--border-subtle)' }}>
            <div>
              <span className="card-title" style={{ fontSize: 14, fontWeight: 600, color: 'var(--text-primary)', textTransform: 'none' }}>
                Work Sessions Summary
              </span>
              <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>
                High-level time blocks grouped continuously by active work activity
              </div>
            </div>
          </div>
          <div className="card-body" style={{ padding: '14px 16px' }}>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
              {workSessions.map((session) => (
                <div
                  key={session.id}
                  style={{
                    padding: '12px 16px',
                    background: 'var(--bg-surface)',
                    border: '1px solid var(--border-subtle)',
                    borderRadius: 'var(--radius-md)',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    gap: 16,
                  }}
                >
                  <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
                    <div
                      style={{
                        width: 36,
                        height: 36,
                        borderRadius: 8,
                        background: 'var(--bg-elevated)',
                        color: 'var(--accent-purple)',
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        border: '1px solid var(--border-subtle)',
                      }}
                    >
                      <Icon name="clock" size={18} />
                    </div>
                    <div>
                      <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-primary)' }}>
                        {session.title}
                      </div>
                      <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>
                        {session.summary}
                      </div>
                    </div>
                  </div>

                  <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                    <span
                      style={{
                        padding: '4px 10px',
                        borderRadius: 99,
                        fontSize: 12,
                        fontWeight: 600,
                        background: session.productive_pct >= 60 ? 'rgba(16, 185, 129, 0.12)' : 'rgba(239, 68, 68, 0.12)',
                        color: session.productive_pct >= 60 ? 'var(--accent-green)' : 'var(--accent-red)',
                        border: `1px solid ${session.productive_pct >= 60 ? 'rgba(16, 185, 129, 0.25)' : 'rgba(239, 68, 68, 0.25)'}`,
                      }}
                    >
                      {Math.round(session.productive_pct)}% AI Focus Score
                    </span>
                    <span style={{ fontSize: 12, color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>
                      {session.log_count} captures
                    </span>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>
      )}

      {/* Activity Stream Feed */}
      <div className="card">
        <div className="card-header" style={{ padding: '16px 20px', borderBottom: '1px solid var(--border-subtle)' }}>
          <div>
            <span className="card-title" style={{ fontSize: 14, fontWeight: 600, color: 'var(--text-primary)', textTransform: 'none', letterSpacing: '0' }}>
              Activity Stream & AI Evaluations
            </span>
            <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>
              Detailed productivity logs with AI percentages, active apps, and screenshot inspection
            </div>
          </div>
          <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
            Click thumbnail to inspect full resolution screenshot
          </span>
        </div>

        <div className="card-body" style={{ padding: '12px 16px' }}>
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
                const scoreVal = log.productive_score !== undefined && log.productive_score > 0
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
                            style={{
                              fontSize: 11,
                              fontWeight: 600,
                              padding: '2px 8px',
                              borderRadius: 4,
                              background: 'var(--bg-elevated)',
                              color: 'var(--text-secondary)',
                              border: '1px solid var(--border-subtle)',
                            }}
                          >
                            💻 {log.app_name}
                          </span>
                        )}
                        {log.app_category && (
                          <span
                            style={{
                              fontSize: 11,
                              color: 'var(--text-muted)',
                              fontStyle: 'italic',
                            }}
                          >
                            ({log.app_category})
                          </span>
                        )}
                      </div>
                      <div className="timeline-reason">{log.ai_reason || '—'}</div>
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
                        >
                          <Icon
                            name={scoreVal >= 50 ? 'check' : 'x'}
                            size={13}
                          />
                          {scoreVal}% Focus Rating
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
