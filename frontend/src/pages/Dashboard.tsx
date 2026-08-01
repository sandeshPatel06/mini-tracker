import { useState } from 'react';
import { LogEntry, ProductivityStats, AppConfig } from '../types';
import { format } from 'date-fns';
import { CategoryBadge } from '../components/CategoryBadge';
import { ScreenshotThumb, ImageModal } from '../components/Screenshot';

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

function EntropyBar({ value }: { value: number }) {
  return (
    <div className="entropy-bar-wrapper">
      <div className="entropy-bar-track">
        <div className="entropy-bar-fill" style={{ width: `${Math.min(value, 100)}%` }} />
      </div>
      <span className="entropy-bar-label">{value.toFixed(0)}</span>
    </div>
  );
}

function StatCard({
  label, value, sub, icon, color, delay
}: {
  label: string; value: string | number; sub?: string; icon: string; color: string; delay: number;
}) {
  return (
    <div
      className={`stat-card fade-in-up fade-in-up-delay-${delay}`}
      style={{ ['--accent-color' as string]: color }}
    >
      <span className="stat-icon">{icon}</span>
      <div className="stat-label">{label}</div>
      <div className="stat-value">{value}</div>
      {sub && <div className="stat-sub">{sub}</div>}
    </div>
  );
}

export default function Dashboard({ logs, stats, config, loading, today, onRefresh }: Props) {
  const [selectedImage, setSelectedImage] = useState<string | null>(null);
  const [apiKeyInput, setApiKeyInput] = useState('');
  const [savingKey, setSavingKey] = useState(false);

  const productiveCount = logs.filter(l => l.is_productive).length;
  const pendingCount = logs.filter(l => !l.ai_category || l.ai_category === 'Unknown' || l.ai_reason.includes('No Gemini API key')).length;
  const avgEntropy = logs.length
    ? logs.reduce((acc, l) => acc + l.entropy_score, 0) / logs.length
    : 0;
  const totalKeys = logs.reduce((acc, l) => acc + l.total_keys, 0);
  const analyzedCount = logs.length - pendingCount;
  const productivePercent = analyzedCount > 0
    ? Math.round((productiveCount / analyzedCount) * 100)
    : 0;

  // Last 8 entries for recent feed
  const recent = [...logs].reverse().slice(0, 8);

  const dateLabel = today === new Date().toISOString().slice(0, 10)
    ? 'Today'
    : format(new Date(today + 'T00:00:00'), 'MMMM d, yyyy');

  const handleSaveKey = async () => {
    if (!apiKeyInput.trim()) return;
    setSavingKey(true);
    try {
      if (window.go?.main?.App?.UpdateGeminiAPIKey) {
        await window.go.main.App.UpdateGeminiAPIKey(apiKeyInput.trim());
      } else {
        await fetch('/api/config', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ gemini_api_key: apiKeyInput.trim() }),
        });
      }
      setApiKeyInput('');
      if (onRefresh) onRefresh();
    } catch (err) {
      console.error('Failed to save API key:', err);
    } finally {
      setSavingKey(false);
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
          {[1, 2, 3, 4].map(i => (
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

      <div className="page-header fade-in-up">
        <div>
          <h1 className="page-title">Dashboard</h1>
          <div className="page-subtitle">
            {dateLabel} · {logs.length} captures
            {config && (
              <span style={{ marginLeft: 8 }}>
                · interval: {config.screenshot_interval_seconds}s
              </span>
            )}
          </div>
        </div>
      </div>

      {/* AI Key Configuration Alert Card if key is missing */}
      {(!config || !config.ai_configured) && (
        <div
          className="card fade-in-up"
          style={{
            marginTop: 16,
            marginBottom: 20,
            background: 'rgba(245, 158, 11, 0.08)',
            borderColor: 'rgba(245, 158, 11, 0.3)',
          }}
        >
          <div className="card-body" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 16, flexWrap: 'wrap' }}>
            <div style={{ flex: 1, minWidth: 260 }}>
              <div style={{ color: 'var(--accent-amber)', fontWeight: 600, fontSize: 14, display: 'flex', alignItems: 'center', gap: 6 }}>
                🔑 Enable AI Intelligence Engine
              </div>
              <div style={{ color: 'var(--text-secondary)', fontSize: 12, marginTop: 4 }}>
                Enter your AI service key below to enable automated activity insights and productivity classification.
              </div>
            </div>
            <div style={{ display: 'flex', gap: 8, flex: '0 0 auto' }}>
              <input
                type="password"
                placeholder="Paste key here..."
                value={apiKeyInput}
                onChange={(e) => setApiKeyInput(e.target.value)}
                className="date-input"
                style={{ width: 220, fontFamily: 'monospace' }}
              />
              <button
                onClick={handleSaveKey}
                disabled={savingKey || !apiKeyInput.trim()}
                style={{
                  background: 'var(--accent-amber)',
                  color: '#000',
                  fontWeight: 700,
                  border: 'none',
                  borderRadius: 'var(--radius-sm)',
                  padding: '8px 16px',
                  cursor: 'pointer',
                  fontSize: 12,
                }}
              >
                {savingKey ? 'Saving…' : 'Save Key'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Stats Grid */}
      <div className="stats-grid" style={{ marginTop: 0 }}>
        <StatCard
          label="Productive Time"
          value={`${productivePercent}%`}
          sub={`${productiveCount} / ${analyzedCount} analyzed`}
          icon="🎯"
          color="var(--accent-green)"
          delay={1}
        />
        <StatCard
          label="Total Keystrokes"
          value={totalKeys.toLocaleString()}
          sub="since midnight"
          icon="⌨️"
          color="var(--accent-purple)"
          delay={2}
        />
        <StatCard
          label="Avg Entropy"
          value={avgEntropy.toFixed(1)}
          sub="typing variety score"
          icon="📈"
          color="var(--accent-teal)"
          delay={3}
        />
        <StatCard
          label="Top Activity"
          value={stats?.top_category || '—'}
          sub={`${logs.length} total snapshots`}
          icon="🏆"
          color="var(--accent-amber)"
          delay={4}
        />
      </div>

      {/* Recent Activity */}
      <div className="card fade-in-up fade-in-up-delay-4" style={{ marginTop: 20 }}>
        <div className="card-header">
          <span className="card-title">Recent Captures & Screenshots</span>
          <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>
            Click screenshot thumbnail to enlarge
          </span>
        </div>
        <div className="card-body">
          {recent.length === 0 ? (
            <div className="empty-state">
              <div className="empty-state-icon">📭</div>
              <div className="empty-state-title">No captures yet today</div>
              <div className="empty-state-desc">
                The tracker will capture your activity screenshots automatically.
              </div>
            </div>
          ) : (
            <div className="timeline-list">
              {recent.map((log) => (
                <div key={log.id} className="timeline-item">
                  <span className="timeline-time">
                    {format(new Date(log.timestamp), 'HH:mm')}
                  </span>
                  <ScreenshotThumb
                    imagePath={log.image_path}
                    onClick={() => setSelectedImage(log.image_path)}
                  />
                  <div className="timeline-info">
                    <div className="timeline-category">
                      <CategoryBadge category={log.ai_category} />
                    </div>
                    <div className="timeline-reason">{log.ai_reason || '—'}</div>
                    <div className="timeline-keys" style={{ marginTop: 6 }}>
                      <EntropyBar value={log.entropy_score} />
                    </div>
                  </div>
                  <div>
                    {!log.ai_category || log.ai_reason.includes('API key') || log.ai_reason.includes('No key') ? (
                      <span className="badge badge-pending">Pending AI</span>
                    ) : log.is_productive ? (
                      <span className="badge badge-productive">✓ Productive</span>
                    ) : (
                      <span className="badge badge-unproductive">✗ Not</span>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

