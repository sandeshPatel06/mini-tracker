import { LogEntry, ProductivityStats } from '../types';
import { format } from 'date-fns';
import {
  BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer,
  LineChart, Line, PieChart, Pie, Cell, Legend,
} from 'recharts';

interface Props {
  logs: LogEntry[];
  stats: ProductivityStats | null;
  loading: boolean;
  today: string;
  onDateChange: (date: string) => void;
}

const COLORS = {
  productive:   '#10b981',
  unproductive: '#ef4444',
  entropy:      '#6366f1',
  teal:         '#2dd4bf',
  amber:        '#f59e0b',
};

// Custom tooltip for recharts
const CustomTooltip = ({ active, payload, label }: { active?: boolean; payload?: {value:number;name:string}[]; label?: string }) => {
  if (!active || !payload?.length) return null;
  return (
    <div style={{
      background: 'var(--bg-elevated)',
      border: '1px solid var(--border-medium)',
      borderRadius: 'var(--radius-sm)',
      padding: '8px 12px',
      fontSize: 12,
    }}>
      <div style={{ color: 'var(--text-muted)', marginBottom: 4 }}>{label}</div>
      {payload.map((p, i) => (
        <div key={i} style={{ color: 'var(--text-primary)' }}>
          {p.name}: <strong>{typeof p.value === 'number' ? p.value.toFixed(1) : p.value}</strong>
        </div>
      ))}
    </div>
  );
};

export default function Analytics({ logs, stats, loading, today, onDateChange }: Props) {
  // Build hourly bar chart data
  const hourlyData = Array.from({ length: 24 }, (_, h) => {
    const hourLogs = logs.filter(l => new Date(l.timestamp).getHours() === h);
    const prod = hourLogs.filter(l => l.is_productive && l.ai_category).length;
    const unprod = hourLogs.filter(l => !l.is_productive && l.ai_category).length;
    return { hour: `${String(h).padStart(2, '0')}:00`, Productive: prod, Unproductive: unprod };
  }).filter(d => d.Productive > 0 || d.Unproductive > 0);

  // Build entropy line chart data (every capture)
  const entropyData = [...logs]
    .sort((a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime())
    .map(l => ({
      time: format(new Date(l.timestamp), 'HH:mm'),
      Entropy: parseFloat(l.entropy_score.toFixed(1)),
      Keys: l.total_keys,
    }));

  // Category breakdown for pie
  const categoryMap = new Map<string, number>();
  logs.filter(l => l.ai_category).forEach(l => {
    categoryMap.set(l.ai_category, (categoryMap.get(l.ai_category) ?? 0) + 1);
  });
  const categoryData = Array.from(categoryMap.entries())
    .sort((a, b) => b[1] - a[1])
    .map(([name, value]) => ({ name, value }));

  const PIE_COLORS = [
    COLORS.entropy, COLORS.teal, COLORS.amber, COLORS.productive,
    '#8b5cf6', '#ec4899', '#f97316', '#06b6d4',
  ];

  const productiveMin = stats?.productive_minutes ?? 0;
  const unproductiveMin = stats?.unproductive_minutes ?? 0;
  const totalAnalyzed = productiveMin + unproductiveMin;
  const donutData = totalAnalyzed > 0
    ? [
        { name: 'Productive', value: productiveMin },
        { name: 'Unproductive', value: unproductiveMin },
      ]
    : null;

  const dateLabel = today === new Date().toISOString().slice(0, 10)
    ? 'Today'
    : format(new Date(today + 'T00:00:00'), 'MMMM d, yyyy');

  return (
    <div>
      <div className="page-header fade-in-up">
        <div>
          <h1 className="page-title">Analytics</h1>
          <div className="page-subtitle">{dateLabel} · deep dive</div>
        </div>
        <input
          id="analytics-date-picker"
          type="date"
          className="date-input"
          value={today}
          max={new Date().toISOString().slice(0, 10)}
          onChange={(e) => onDateChange(e.target.value)}
        />
      </div>

      {loading ? (
        <div className="card" style={{ padding: 32 }}>
          <div className="skeleton" style={{ width: '100%', height: 200 }} />
        </div>
      ) : logs.length === 0 ? (
        <div className="card">
          <div className="empty-state">
            <div className="empty-state-icon">📊</div>
            <div className="empty-state-title">No data yet</div>
            <div className="empty-state-desc">Analytics will appear once you have captures for this date.</div>
          </div>
        </div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>

          {/* Productive vs Unproductive donut + summary */}
          <div className="charts-grid-2col">
            <div className="card fade-in-up fade-in-up-delay-1">
              <div className="card-header">
                <span className="card-title">Productivity Split</span>
              </div>
              <div className="chart-container">
                {donutData ? (
                  <div className="donut-row">
                    <ResponsiveContainer width={140} height={140}>
                      <PieChart>
                        <Pie
                          data={donutData}
                          cx="50%"
                          cy="50%"
                          innerRadius={40}
                          outerRadius={60}
                          paddingAngle={3}
                          dataKey="value"
                        >
                          <Cell fill={COLORS.productive} />
                          <Cell fill={COLORS.unproductive} />
                        </Pie>
                      </PieChart>
                    </ResponsiveContainer>
                    <div className="donut-legend">
                      <div className="donut-legend-item">
                        <div className="legend-dot" style={{ background: COLORS.productive }} />
                        <span>Productive</span>
                        <span className="legend-value">{productiveMin}</span>
                      </div>
                      <div className="donut-legend-item">
                        <div className="legend-dot" style={{ background: COLORS.unproductive }} />
                        <span>Not Productive</span>
                        <span className="legend-value">{unproductiveMin}</span>
                      </div>
                      <div className="donut-legend-item" style={{ marginTop: 4 }}>
                        <div className="legend-dot" style={{ background: 'var(--accent-teal)' }} />
                        <span>Avg Entropy</span>
                        <span className="legend-value">{stats?.avg_entropy_score?.toFixed(1) ?? '—'}</span>
                      </div>
                    </div>
                  </div>
                ) : (
                  <div className="empty-state" style={{ padding: '24px 0' }}>
                    <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>
                      AI analysis pending…
                    </div>
                  </div>
                )}
              </div>
            </div>

            {/* Category pie */}
            <div className="card fade-in-up fade-in-up-delay-2">
              <div className="card-header">
                <span className="card-title">Activity Categories</span>
              </div>
              <div className="chart-container">
                {categoryData.length > 0 ? (
                  <ResponsiveContainer width="100%" height={140}>
                    <PieChart>
                      <Pie
                        data={categoryData}
                        cx="50%"
                        cy="50%"
                        outerRadius={55}
                        paddingAngle={2}
                        dataKey="value"
                        label={({ name, percent }) =>
                          `${name} ${((percent ?? 0) * 100).toFixed(0)}%`
                        }
                        labelLine={false}
                      >
                        {categoryData.map((_, i) => (
                          <Cell key={i} fill={PIE_COLORS[i % PIE_COLORS.length]} />
                        ))}
                      </Pie>
                      <Tooltip content={<CustomTooltip />} />
                    </PieChart>
                  </ResponsiveContainer>
                ) : (
                  <div className="empty-state" style={{ padding: '24px 0' }}>
                    <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>No categories yet</div>
                  </div>
                )}
              </div>
            </div>
          </div>

          {/* Hourly bar chart */}
          {hourlyData.length > 0 && (
            <div className="card fade-in-up fade-in-up-delay-3">
              <div className="card-header">
                <span className="card-title">Hourly Activity</span>
              </div>
              <div className="chart-container">
                <ResponsiveContainer width="100%" height={180}>
                  <BarChart data={hourlyData} barCategoryGap="30%">
                    <CartesianGrid strokeDasharray="3 3" stroke="rgba(99,102,241,0.08)" />
                    <XAxis
                      dataKey="hour"
                      tick={{ fontSize: 10, fill: 'var(--text-muted)' }}
                      axisLine={false}
                      tickLine={false}
                    />
                    <YAxis
                      tick={{ fontSize: 10, fill: 'var(--text-muted)' }}
                      axisLine={false}
                      tickLine={false}
                      width={24}
                    />
                    <Tooltip content={<CustomTooltip />} />
                    <Bar dataKey="Productive" fill={COLORS.productive} radius={[3, 3, 0, 0]} />
                    <Bar dataKey="Unproductive" fill={COLORS.unproductive} radius={[3, 3, 0, 0]} />
                  </BarChart>
                </ResponsiveContainer>
              </div>
            </div>
          )}

          {/* Entropy line chart */}
          {entropyData.length > 1 && (
            <div className="card fade-in-up fade-in-up-delay-4">
              <div className="card-header">
                <span className="card-title">Keystroke Entropy Over Time</span>
              </div>
              <div className="chart-container">
                <ResponsiveContainer width="100%" height={160}>
                  <LineChart data={entropyData}>
                    <CartesianGrid strokeDasharray="3 3" stroke="rgba(99,102,241,0.08)" />
                    <XAxis
                      dataKey="time"
                      tick={{ fontSize: 10, fill: 'var(--text-muted)' }}
                      axisLine={false}
                      tickLine={false}
                      interval="preserveStartEnd"
                    />
                    <YAxis
                      tick={{ fontSize: 10, fill: 'var(--text-muted)' }}
                      axisLine={false}
                      tickLine={false}
                      domain={[0, 100]}
                      width={28}
                    />
                    <Tooltip content={<CustomTooltip />} />
                    <Line
                      type="monotone"
                      dataKey="Entropy"
                      stroke={COLORS.entropy}
                      strokeWidth={2}
                      dot={false}
                      activeDot={{ r: 4, fill: COLORS.entropy }}
                    />
                  </LineChart>
                </ResponsiveContainer>
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
