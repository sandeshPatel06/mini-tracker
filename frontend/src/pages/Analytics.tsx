import { useState, useEffect, useMemo } from 'react';
import { LogEntry, ProductivityStats, User, AppConfig } from '../types';
import { apiFetch } from '../api';
import { format } from 'date-fns';
import {
  BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer,
  LineChart, Line, PieChart, Pie, Cell, Legend,
} from 'recharts';
import { localISODate, isPendingLog, FOCUS_GOOD, FOCUS_OK } from '../utils';

interface Props {
  logs: LogEntry[];
  stats: ProductivityStats | null;
  loading: boolean;
  today: string;
  onDateChange: (date: string) => void;
  currentUser?: User | null;
  selectedUserId?: number | null;
  startDate?: string;
  endDate?: string;
  onUserChange?: (userId: number | null) => void;
  onDateRangeChange?: (start: string, end: string) => void;
  loadError?: string | null;
  onRetry?: () => void;
  config?: AppConfig | null;
}

// Custom Tooltip Component
const CustomTooltip = ({ active, payload, label }: { active?: boolean; payload?: any[]; label?: string }) => {
  if (!active || !payload?.length) return null;
  return (
    <div style={{
      background: 'var(--bg-elevated)',
      border: '1px solid var(--border-medium)',
      borderRadius: 'var(--radius-md)',
      padding: '10px 14px',
      fontSize: 12,
      boxShadow: 'var(--shadow-md)',
      color: 'var(--text-primary)',
    }}>
      <div style={{ color: 'var(--text-secondary)', marginBottom: 6, fontWeight: 600 }}>{label}</div>
      {payload.map((p, i) => {
        const noCaptures = p.name === 'Focus Rating' && p.payload?.Captures === 0;
        const display = noCaptures
          ? 'No captures'
          : typeof p.value === 'number'
            ? (Number.isInteger(p.value) ? `${p.value}${p.unit || ''}` : p.value.toFixed(1))
            : p.value;
        return (
          <div key={i} style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 4, fontVariantNumeric: 'tabular-nums' }}>
            <div style={{ width: 8, height: 8, borderRadius: '50%', background: p.color || p.fill }} />
            <span>{p.name}: <strong>{display}</strong></span>
          </div>
        );
      })}
    </div>
  );
};

export default function Analytics({
  logs,
  stats,
  loading,
  today,
  onDateChange,
  currentUser,
  selectedUserId,
  startDate,
  endDate,
  onUserChange,
  onDateRangeChange,
  loadError,
  onRetry,
  config,
}: Props) {
  const [members, setMembers] = useState<{ id: number; full_name: string; email: string; role: string }[]>([]);

  useEffect(() => {
    if (currentUser?.role === 'admin' || currentUser?.role === 'owner') {
      apiFetch('/api/org/members')
        .then(r => r.ok ? r.json() : null)
        .then(data => {
          if (data?.members) {
            setMembers(data.members);
          }
        })
        .catch(() => {});
    }
  }, [currentUser]);

  // Analyzed logs for focus percentage
  const analyzedLogs = useMemo(() => {
    return logs.filter(l => !isPendingLog(l));
  }, [logs]);

  // Overall Average Focus Score
  const avgFocusScore = useMemo(() => {
    if (analyzedLogs.length === 0) return 0;
    return Math.round(
      analyzedLogs.reduce((acc, l) => acc + (typeof l.productive_score === 'number' ? l.productive_score : l.is_productive ? 100 : 0), 0) / analyzedLogs.length
    );
  }, [analyzedLogs]);

  // Build hourly focus score trend chart data
  const hourlyData = useMemo(() => {
    return Array.from({ length: 24 }, (_, h) => {
      const hourLogs = logs.filter(l => new Date(l.timestamp).getHours() === h);
      const totalScore = hourLogs.reduce((acc, l) => acc + (typeof l.productive_score === 'number' ? l.productive_score : l.is_productive ? 100 : 0), 0);
      const avgScore = hourLogs.length > 0 ? Math.round(totalScore / hourLogs.length) : 0;
      return {
        hour: `${String(h).padStart(2, '0')}:00`,
        'Focus Rating': avgScore,
        Captures: hourLogs.length,
      };
    });
  }, [logs]);

  // App Usage Breakdown calculation
  const appData = useMemo(() => {
    const appMap: Record<string, { count: number; category: string; totalScore: number }> = {};
    logs.forEach(l => {
      const app = l.app_name || 'Other';
      if (!appMap[app]) {
        appMap[app] = { count: 0, category: l.app_category || 'Application', totalScore: 0 };
      }
      appMap[app].count++;
      appMap[app].totalScore += typeof l.productive_score === 'number' ? l.productive_score : l.is_productive ? 100 : 0;
    });

    const total = logs.length || 1;
    return Object.entries(appMap)
      .map(([name, d]) => ({
        name,
        category: d.category,
        count: d.count,
        percent: Math.round((d.count / total) * 100),
        avgScore: Math.round(d.totalScore / d.count),
      }))
      .sort((a, b) => b.count - a.count)
      .slice(0, 6);
  }, [logs]);

  // Entropy & Typing Line Chart Data
  const multiDay = !!(startDate && endDate && startDate !== endDate);
  const entropyData = useMemo(() => {
    return [...logs]
      .sort((a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime())
      .map(l => ({
        time: format(new Date(l.timestamp), multiDay ? 'MMM d HH:mm' : 'HH:mm'),
        Entropy: parseFloat(l.entropy_score.toFixed(1)),
        FocusScore: l.productive_score !== undefined ? Math.round(l.productive_score) : l.is_productive ? 100 : 0,
      }));
  }, [logs, multiDay]);

  // Category breakdown for pie chart
  const categoryData = useMemo(() => {
    const catMap = new Map<string, number>();
    logs.filter(l => l.ai_category && l.ai_category !== 'Unknown').forEach(l => {
      catMap.set(l.ai_category, (catMap.get(l.ai_category) ?? 0) + 1);
    });
    return Array.from(catMap.entries())
      .sort((a, b) => b[1] - a[1])
      .map(([name, value]) => ({ name, value }));
  }, [logs]);

  const PIE_COLORS = [
    'var(--accent-green)',
    'var(--accent-indigo)',
    'var(--accent-teal)',
    'var(--accent-amber)',
    'var(--accent-purple)',
    'var(--accent-cyan)',
    'var(--accent-red)',
  ];

  const dateLabel = startDate && endDate && startDate !== endDate
    ? `${format(new Date(startDate + 'T00:00:00'), 'MMM d, yyyy')} - ${format(new Date(endDate + 'T00:00:00'), 'MMM d, yyyy')}`
    : today === localISODate()
    ? 'Today'
    : format(new Date(today + 'T00:00:00'), 'MMMM d, yyyy');

  const intervalSec = config?.screenshot_interval_seconds ?? 30;
  const activeMinutes = logs.length > 0 ? (logs.length * intervalSec) / 60 : 0;

  return (
    <div aria-busy={loading}>
      <div className="page-header" style={{ flexWrap: 'wrap', gap: 16, marginBottom: 24 }}>
        <div>
          <h1 className="page-title" style={{ fontSize: 24, fontWeight: 700, letterSpacing: '-0.4px', color: 'var(--text-primary)' }}>
            Productivity Analytics & Deep Dive
          </h1>
          <div className="page-subtitle" style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: 4 }}>
            {dateLabel} · {selectedUserId ? 'Single User Deep Dive' : 'Team / Organization Overview'}
          </div>
        </div>

        <div style={{ display: 'flex', gap: 10, alignItems: 'center', flexWrap: 'wrap' }}>
          {(currentUser?.role === 'admin' || currentUser?.role === 'owner') && members.length > 0 && (
            <select
              className="date-input"
              aria-label="Filter by team member"
              value={selectedUserId || ''}
              onChange={(e) => onUserChange?.(e.target.value ? Number(e.target.value) : null)}
              style={{
                padding: '6px 12px',
                borderRadius: 'var(--radius-md)',
                background: 'var(--bg-surface)',
                color: 'var(--text-primary)',
                border: '1px solid var(--border-medium)',
                fontSize: 13,
              }}
            >
              <option value="">👥 All Organization Users</option>
              {members.map(m => (
                <option key={m.id} value={m.id}>
                  👤 {m.full_name || m.email} ({m.role})
                </option>
              ))}
            </select>
          )}

          <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
            <input
              type="date"
              className="date-input"
              aria-label="Range start date"
              value={startDate || today}
              max={endDate || localISODate()}
              onChange={(e) => {
                const s = e.target.value;
                onDateRangeChange?.(s, endDate && endDate < s ? s : endDate || s);
              }}
              style={{
                padding: '6px 12px',
                borderRadius: 'var(--radius-md)',
                background: 'var(--bg-surface)',
                color: 'var(--text-primary)',
                border: '1px solid var(--border-medium)',
                fontSize: 13,
              }}
            />
            <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>to</span>
            <input
              type="date"
              className="date-input"
              aria-label="Range end date"
              value={endDate || today}
              min={startDate || undefined}
              max={localISODate()}
              onChange={(e) => {
                const ed = e.target.value;
                onDateRangeChange?.(startDate || today, ed);
              }}
              style={{
                padding: '6px 12px',
                borderRadius: 'var(--radius-md)',
                background: 'var(--bg-surface)',
                color: 'var(--text-primary)',
                border: '1px solid var(--border-medium)',
                fontSize: 13,
              }}
            />
          </div>
        </div>
      </div>

      {loadError && logs.length > 0 && (
        <div className="alert alert-error" role="alert" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, marginBottom: 16 }}>
          <span>{loadError}</span>
          {onRetry && <button className="btn btn-sm" onClick={onRetry}>Retry</button>}
        </div>
      )}
      {loading && logs.length === 0 ? (
        <div className="card" style={{ padding: 32 }}>
          <div className="skeleton" style={{ width: '100%', height: 240 }} />
        </div>
      ) : loadError && logs.length === 0 ? (
        <div className="card">
          <div className="alert alert-error" role="alert" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, margin: 16 }}>
            <span>{loadError}</span>
            {onRetry && <button className="btn btn-sm" onClick={onRetry}>Retry</button>}
          </div>
        </div>
      ) : logs.length === 0 ? (
        <div className="card">
          <div className="empty-state">
            <div className="empty-state-icon" style={{ fontSize: 32, marginBottom: 12 }}>📊</div>
            <div className="empty-state-title">No analytics data recorded yet</div>
            <div className="empty-state-desc">Analytics will populate automatically as background captures are analyzed.</div>
          </div>
        </div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 24 }}>

          {/* Top Summary Banner */}
          <div className="stats-grid">
            <div className="stat-card">
              <span className="stat-label">AI Focus Rating</span>
              <div className="stat-value" style={{ color: analyzedLogs.length === 0 ? 'var(--text-muted)' : avgFocusScore >= FOCUS_GOOD ? 'var(--accent-green)' : avgFocusScore >= FOCUS_OK ? 'var(--accent-amber)' : 'var(--accent-red)', fontVariantNumeric: 'tabular-nums' }}>
                {analyzedLogs.length === 0 ? '—' : `${avgFocusScore}%`}
              </div>
              <div className="stat-sub">{analyzedLogs.length} of {logs.length} captures evaluated</div>
            </div>

            <div className="stat-card">
              <span className="stat-label">Active Work Duration</span>
              <div className="stat-value" style={{ fontVariantNumeric: 'tabular-nums' }}>
                {activeMinutes >= 60 ? `${(activeMinutes / 60).toFixed(1)} hrs` : `${Math.round(activeMinutes)} mins`}
              </div>
              <div className="stat-sub">Based on {logs.length} background telemetry points</div>
            </div>

            <div className="stat-card">
              <span className="stat-label">Avg Input Telemetry</span>
              <div className="stat-value" style={{ fontVariantNumeric: 'tabular-nums' }}>
                {logs.length > 0 ? Math.round(logs.reduce((acc, l) => acc + l.total_keys, 0) / logs.length) : 0} <span style={{ fontSize: 14, fontWeight: 500, color: 'var(--text-muted)' }}>keys / cap</span>
              </div>
              <div className="stat-sub">Average keystrokes per {intervalSec}s capture interval</div>
            </div>
          </div>

          {/* Hourly Focus Score Chart */}
          <div className="card" style={{ padding: 20 }}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16 }}>
              <div>
                <h3 style={{ fontSize: 15, fontWeight: 600, color: 'var(--text-primary)', margin: 0 }}>
                  Hourly AI Focus Rating (0-100%)
                </h3>
                <p style={{ fontSize: 12, color: 'var(--text-muted)', margin: '2px 0 0 0' }}>
                  Average AI productivity percentage score for each active hour
                </p>
              </div>
              <span style={{ fontSize: 12, fontWeight: 600, color: 'var(--purple-text)', fontVariantNumeric: 'tabular-nums' }}>
                Overall Focus: {avgFocusScore}%
              </span>
            </div>

            <div style={{ width: '100%', height: 200 }}>
              <ResponsiveContainer width="100%" height="100%">
                <BarChart data={hourlyData} margin={{ top: 10, right: 10, left: -20, bottom: 0 }} accessibilityLayer>
                  <CartesianGrid strokeDasharray="3 3" stroke="var(--border-subtle)" vertical={false} />
                  <XAxis dataKey="hour" tick={{ fontSize: 11, fill: 'var(--text-muted)' }} axisLine={false} tickLine={false} />
                  <YAxis domain={[0, 100]} tick={{ fontSize: 11, fill: 'var(--text-muted)' }} axisLine={false} tickLine={false} />
                  <Tooltip content={<CustomTooltip />} cursor={{ fill: 'color-mix(in srgb, var(--accent-primary) 8%, transparent)' }} />
                  <Bar dataKey="Focus Rating" fill="var(--accent-purple)" radius={[4, 4, 0, 0]} unit="%" />
                </BarChart>
              </ResponsiveContainer>
            </div>
          </div>

          {/* Grid: App Breakdown & Category Pie */}
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(360px, 1fr))', gap: 24 }}>
            
            {/* Top Applications Share */}
            <div className="card" style={{ padding: 20 }}>
              <div style={{ marginBottom: 16 }}>
                <h3 style={{ fontSize: 15, fontWeight: 600, color: 'var(--text-primary)', margin: 0 }}>
                  Desktop App Time & Focus Ratings
                </h3>
                <p style={{ fontSize: 12, color: 'var(--text-muted)', margin: '2px 0 0 0' }}>
                  Breakdown of time share & AI evaluation per application
                </p>
              </div>

              <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
                {appData.map((app) => (
                  <div key={app.name} style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, fontSize: 13 }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0, flex: 1 }}>
                        <span title={app.name} style={{ fontWeight: 600, color: 'var(--text-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>💻 {app.name}</span>
                        <span title={app.category} style={{ fontSize: 11, color: 'var(--text-muted)', maxWidth: 120, flexShrink: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>({app.category})</span>
                      </div>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexShrink: 0 }}>
                        <span style={{ fontSize: 12, fontWeight: 600, fontVariantNumeric: 'tabular-nums', color: app.avgScore >= FOCUS_OK ? 'var(--badge-productive-text)' : 'var(--badge-unproductive-text)' }}>
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
                          backgroundColor: app.avgScore >= FOCUS_GOOD ? 'var(--accent-green)' : app.avgScore >= FOCUS_OK ? 'var(--accent-indigo)' : 'var(--accent-red)',
                          borderRadius: 99,
                        }}
                      />
                    </div>
                  </div>
                ))}
              </div>
            </div>

            {/* Category Pie Chart */}
            <div className="card" style={{ padding: 20 }}>
              <div style={{ marginBottom: 16 }}>
                <h3 style={{ fontSize: 15, fontWeight: 600, color: 'var(--text-primary)', margin: 0 }}>
                  Work Categories Breakdown
                </h3>
                <p style={{ fontSize: 12, color: 'var(--text-muted)', margin: '2px 0 0 0' }}>
                  Distribution across AI categories
                </p>
              </div>

              <div style={{ width: '100%', height: 220 }}>
                {categoryData.length > 0 ? (
                  <ResponsiveContainer width="100%" height="100%">
                    <PieChart accessibilityLayer>
                      <Pie
                        data={categoryData}
                        cx="50%"
                        cy="50%"
                        outerRadius={70}
                        paddingAngle={3}
                        dataKey="value"
                        label={false}
                        labelLine={false}
                      >
                        {categoryData.map((_, i) => (
                          <Cell key={i} fill={PIE_COLORS[i % PIE_COLORS.length]} />
                        ))}
                      </Pie>
                      <Tooltip content={<CustomTooltip />} />
                      <Legend
                        verticalAlign="bottom"
                        iconType="circle"
                        wrapperStyle={{ fontSize: 11 }}
                        formatter={(value: any) => <span style={{ color: 'var(--text-secondary)' }}>{value}</span>}
                      />
                    </PieChart>
                  </ResponsiveContainer>
                ) : (
                  <div style={{ fontSize: 13, color: 'var(--text-muted)', textAlign: 'center', padding: '40px 0' }}>
                    No categories logged yet
                  </div>
                )}
              </div>
            </div>

          </div>

          {/* Keystroke Entropy & Focus Trend Line Chart */}
          {(
            <div className="card" style={{ padding: 20 }}>
              <div style={{ marginBottom: 16 }}>
                <h3 style={{ fontSize: 15, fontWeight: 600, color: 'var(--text-primary)', margin: 0 }}>
                  Continuous Keystroke Variety & Focus Score Timeline
                </h3>
                <p style={{ fontSize: 12, color: 'var(--text-muted)', margin: '2px 0 0 0' }}>
                  Keystroke entropy vs AI-evaluated focus percentage over time
                </p>
              </div>

              <div style={{ width: '100%', height: 180 }}>
                {entropyData.length < 2 ? (
                  <div style={{ fontSize: 13, color: 'var(--text-muted)', textAlign: 'center', padding: '60px 0' }}>
                    Need at least 2 captures to show a trend
                  </div>
                ) : (
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={entropyData} margin={{ top: 10, right: 10, left: -20, bottom: 0 }} accessibilityLayer>
                    <CartesianGrid strokeDasharray="3 3" stroke="var(--border-subtle)" vertical={false} />
                    <XAxis dataKey="time" tick={{ fontSize: 10, fill: 'var(--text-muted)' }} axisLine={false} tickLine={false} />
                    <YAxis domain={[0, 100]} tick={{ fontSize: 10, fill: 'var(--text-muted)' }} axisLine={false} tickLine={false} />
                    <Tooltip content={<CustomTooltip />} cursor={{ stroke: 'var(--border-medium)', strokeDasharray: '3 3' }} />
                    <Line
                      type="monotone"
                      dataKey="FocusScore"
                      name="Focus Rating (%)"
                      stroke="var(--accent-purple)"
                      strokeWidth={2}
                      dot={false}
                      activeDot={{ r: 4, fill: 'var(--accent-purple)' }}
                    />
                    <Line
                      type="monotone"
                      dataKey="Entropy"
                      name="Keystroke Entropy"
                      stroke="var(--accent-cyan)"
                      strokeWidth={1.5}
                      strokeDasharray="4 4"
                      dot={false}
                    />
                    <Legend
                      wrapperStyle={{ fontSize: 11 }}
                      formatter={(value: any) => <span style={{ color: 'var(--text-secondary)' }}>{value}</span>}
                    />
                  </LineChart>
                </ResponsiveContainer>
                )}
              </div>
            </div>
          )}

        </div>
      )}
    </div>
  );
}
