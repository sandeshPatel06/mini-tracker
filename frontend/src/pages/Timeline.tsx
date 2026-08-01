import { useState } from 'react';
import { LogEntry } from '../types';
import { format } from 'date-fns';
import { CategoryBadge } from '../components/CategoryBadge';
import { ScreenshotThumb, ImageModal } from '../components/Screenshot';

interface Props {
  logs: LogEntry[];
  loading: boolean;
  today: string;
  onDateChange: (date: string) => void;
}

function EntropyBar({ value }: { value: number }) {
  const color = value > 60
    ? 'var(--accent-green)'
    : value > 30
    ? 'var(--accent-purple)'
    : 'var(--text-muted)';
  return (
    <div className="entropy-bar-wrapper">
      <div className="entropy-bar-track">
        <div className="entropy-bar-fill" style={{ width: `${Math.min(value, 100)}%`, background: color }} />
      </div>
      <span className="entropy-bar-label">{value.toFixed(0)}</span>
    </div>
  );
}

export default function Timeline({ logs, loading, today, onDateChange }: Props) {
  const [selectedImage, setSelectedImage] = useState<string | null>(null);

  const sortedLogs = [...logs].sort((a, b) =>
    new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime()
  );

  return (
    <div>
      <ImageModal imagePath={selectedImage} onClose={() => setSelectedImage(null)} />

      <div className="page-header fade-in-up">
        <div>
          <h1 className="page-title">Timeline</h1>
          <div className="page-subtitle">
            Minute-by-minute capture history
          </div>
        </div>
        <div>
          <input
            id="timeline-date-picker"
            type="date"
            className="date-input"
            value={today}
            max={new Date().toISOString().slice(0, 10)}
            onChange={(e) => onDateChange(e.target.value)}
          />
        </div>
      </div>

      <div className="card fade-in-up fade-in-up-delay-1" style={{ marginTop: 8 }}>
        <div className="card-header">
          <span className="card-title">
            {logs.length} captures
          </span>
          <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>
            {today === new Date().toISOString().slice(0, 10)
              ? 'Today'
              : format(new Date(today + 'T00:00:00'), 'MMMM d, yyyy')}
          </span>
        </div>
        <div className="card-body">
          {loading ? (
            <div className="timeline-list">
              {[1, 2, 3, 4, 5].map(i => (
                <div key={i} className="timeline-item">
                  <div className="skeleton" style={{ width: 40, height: 16 }} />
                  <div className="skeleton timeline-thumb-placeholder" style={{ border: 'none' }} />
                  <div style={{ flex: 1 }}>
                    <div className="skeleton" style={{ width: '50%', height: 16, marginBottom: 8 }} />
                    <div className="skeleton" style={{ width: '80%', height: 12 }} />
                  </div>
                  <div className="skeleton" style={{ width: 70, height: 22, borderRadius: 99 }} />
                </div>
              ))}
            </div>
          ) : sortedLogs.length === 0 ? (
            <div className="empty-state">
              <div className="empty-state-icon">📭</div>
              <div className="empty-state-title">No data for this date</div>
              <div className="empty-state-desc">
                No captures found. Try selecting a different date.
              </div>
            </div>
          ) : (
            <div className="timeline-list">
              {sortedLogs.map((log) => (
                <div key={log.id} className="timeline-item">
                  <span className="timeline-time">
                    {format(new Date(log.timestamp), 'HH:mm:ss')}
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
                    <div style={{ marginTop: 6, display: 'flex', gap: 16, alignItems: 'center' }}>
                      <span className="timeline-keys">
                        ⌨️ {log.total_keys} keys · {log.unique_keys} unique
                      </span>
                    </div>
                    <div style={{ marginTop: 4 }}>
                      <EntropyBar value={log.entropy_score} />
                    </div>
                  </div>

                  <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end', gap: 4 }}>
                    {!log.ai_category || log.ai_reason.includes('No Gemini API key') ? (
                      <span className="badge badge-pending">Pending AI</span>
                    ) : log.is_productive ? (
                      <span className="badge badge-productive">✓ Productive</span>
                    ) : (
                      <span className="badge badge-unproductive">✗ Not</span>
                    )}
                    {log.ai_confidence > 0 && (
                      <span style={{ fontSize: 10, color: 'var(--text-muted)' }}>
                        {(log.ai_confidence * 100).toFixed(0)}% conf.
                      </span>
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

