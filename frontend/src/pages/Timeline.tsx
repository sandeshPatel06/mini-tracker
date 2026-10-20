import { useState, useMemo, useEffect } from 'react';
import { LogEntry } from '../types';
import { format } from 'date-fns';
import { CategoryBadge } from '../components/CategoryBadge';
import { ScreenshotThumb, ImageModal } from '../components/Screenshot';
import { Icon } from '../components/Icon';
import { AppIcon } from '../components/AppIcon';
import { AppFilterSelect } from '../components/AppFilterSelect';
import { CustomSelect } from '../components/CustomSelect';
import { localISODate, isPendingLog, FOCUS_OK } from '../utils';

interface Props {
  logs: LogEntry[];
  loading: boolean;
  today: string;
  onDateChange: (date: string) => void;
  loadError?: string | null;
  onRetry?: () => void;
}

export default function Timeline({ logs, loading, today, onDateChange, loadError, onRetry }: Props) {
  const [selectedImage, setSelectedImage] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState('');
  const [selectedApp, setSelectedApp] = useState('ALL');
  const [selectedCategory, setSelectedCategory] = useState('ALL');

  const [currentPage, setCurrentPage] = useState(1);
  const [pageSize, setPageSize] = useState(25);

  // Extract unique app names & categories for dropdown filters
  const uniqueApps = useMemo(() => {
    const apps = new Set<string>();
    logs.forEach((l) => {
      if (l.app_name) apps.add(l.app_name);
    });
    return Array.from(apps).sort();
  }, [logs]);

  const uniqueCategories = useMemo(() => {
    const cats = new Set<string>();
    logs.forEach((l) => {
      if (l.ai_category && l.ai_category !== 'Unknown') cats.add(l.ai_category);
    });
    return Array.from(cats).sort();
  }, [logs]);

  // Filter logs based on search query, app, and category
  const filteredLogs = useMemo(() => {
    return logs
      .filter((log) => {
        if (selectedApp !== 'ALL' && log.app_name !== selectedApp) return false;
        if (selectedCategory !== 'ALL' && log.ai_category !== selectedCategory) return false;
        if (searchQuery.trim()) {
          const q = searchQuery.toLowerCase();
          const matchApp = log.app_name?.toLowerCase().includes(q);
          const matchReason = log.ai_reason?.toLowerCase().includes(q);
          const matchCat = log.ai_category?.toLowerCase().includes(q);
          const matchWindow = log.window_title?.toLowerCase().includes(q);
          return matchApp || matchReason || matchCat || matchWindow;
        }
        return true;
      })
      .sort((a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime());
  }, [logs, selectedApp, selectedCategory, searchQuery]);

  const totalPages = Math.ceil(filteredLogs.length / pageSize) || 1;

  // Reset to page 1 when the selected day changes
  useEffect(() => {
    setCurrentPage(1);
  }, [today]);

  // Never leave the paginator on a page that no longer exists
  useEffect(() => {
    if (currentPage > totalPages) {
      setCurrentPage(totalPages);
    }
  }, [currentPage, totalPages]);

  const paginatedLogs = useMemo(() => {
    const start = (currentPage - 1) * pageSize;
    return filteredLogs.slice(start, start + pageSize);
  }, [filteredLogs, currentPage, pageSize]);

  // Calculate day summary metrics from analyzed captures only
  const analyzedLogs = useMemo(() => logs.filter((l) => !isPendingLog(l)), [logs]);

  const avgDayScore = analyzedLogs.length > 0
    ? Math.round(
        analyzedLogs.reduce((acc, l) => acc + (typeof l.productive_score === 'number' ? l.productive_score : l.is_productive ? 100 : 0), 0) / analyzedLogs.length
      )
    : 0;

  const hasActiveFilters = Boolean(searchQuery.trim()) || selectedApp !== 'ALL' || selectedCategory !== 'ALL';

  return (
    <div aria-busy={loading} className="fade-in-up">
      <ImageModal imagePath={selectedImage} onClose={() => setSelectedImage(null)} />

      {/* Header Bar */}
      <div className="page-header" style={{ marginBottom: 16 }}>
        <div>
          <h1 className="page-title">Activity Timeline</h1>
          <p className="page-subtitle">Detailed minute-by-minute activity captures &amp; AI focus ratings.</p>
        </div>
      </div>

      {/* Compact filter toolbar */}
      <div className="card" style={{ marginBottom: 16 }}>
        <div className="card-body" style={{ padding: '12px 16px' }}>
          <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 10 }}>
            <label htmlFor="timeline-date-picker" style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.6px', color: 'var(--text-secondary)' }}>
              Date
            </label>
            <input
              id="timeline-date-picker"
              type="date"
              className="date-input"
              value={today}
              max={localISODate()}
              onChange={(e) => onDateChange(e.target.value)}
              style={{ height: 34, padding: '0 10px', fontSize: 13 }}
            />

            <div style={{ position: 'relative', flex: '1 1 200px', minWidth: 170 }}>
              <input
                type="text"
                placeholder="Search apps, windows, reason..."
                className="form-input"
                aria-label="Search captures"
                value={searchQuery}
                onChange={(e) => {
                  setSearchQuery(e.target.value);
                  setCurrentPage(1);
                }}
                style={{ height: 34, paddingLeft: 32, fontSize: 13, width: '100%' }}
              />
              <div style={{ position: 'absolute', left: 10, top: 10, color: 'var(--text-muted)' }}>
                <Icon name="search" size={14} />
              </div>
            </div>

            <AppFilterSelect
              apps={uniqueApps}
              value={selectedApp}
              onChange={(value) => {
                setSelectedApp(value);
                setCurrentPage(1);
              }}
            />

            <CustomSelect
              value={selectedCategory}
              onChange={(value) => {
                setSelectedCategory(value);
                setCurrentPage(1);
              }}
              ariaLabel="Filter by category"
              triggerStyle={{ minWidth: 140 }}
              options={[
                {
                  value: 'ALL',
                  label: 'All Categories',
                  leading: <Icon name="filter" size={14} />,
                },
                ...uniqueCategories.map((cat) => ({ value: cat, label: cat })),
              ]}
            />
          </div>

          <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 10, marginTop: 10, fontSize: 12, color: 'var(--text-muted)', fontVariantNumeric: 'tabular-nums' }}>
            <span>
              Showing <strong style={{ color: 'var(--text-primary)' }}>{paginatedLogs.length}</strong> of {filteredLogs.length}
            </span>
            <span style={{ color: 'var(--border-medium)' }}>•</span>
            <span style={{ fontWeight: 600, color: avgDayScore >= FOCUS_OK ? 'var(--badge-productive-text)' : 'var(--accent-primary-text)' }}>
              Avg Day Focus: {analyzedLogs.length === 0 ? '—' : `${avgDayScore}%`}
            </span>
          </div>
        </div>
      </div>

      {/* Main Activity Timeline List */}
      <div className="card">
        <div className="card-header">
          <span className="card-title" style={{ textTransform: 'none', fontSize: 14, color: 'var(--text-primary)' }}>
            📅 {today === localISODate() ? 'Today\'s Log Feed' : format(new Date(today + 'T00:00:00'), 'MMMM d, yyyy')}
          </span>
        </div>

        <div className="card-body" style={{ padding: '12px 16px' }}>
          {loadError && logs.length > 0 && (
            <div className="alert alert-error" role="alert" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, marginBottom: 12 }}>
              <span>{loadError}</span>
              {onRetry && <button className="btn btn-secondary btn-sm" onClick={onRetry}>Retry</button>}
            </div>
          )}
          {loading && logs.length === 0 ? (
            <div className="timeline-list">
              {[1, 2, 3, 4, 5].map((i) => (
                <div key={i} className="timeline-item" style={{ padding: '8px 12px', gap: 10 }}>
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
          ) : loadError && logs.length === 0 ? (
            <div className="alert alert-error" role="alert" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}>
              <span>{loadError}</span>
              {onRetry && <button className="btn btn-secondary btn-sm" onClick={onRetry}>Retry</button>}
            </div>
          ) : paginatedLogs.length === 0 ? (
            <div className="empty-state">
              <div className="empty-state-icon" style={{ display: 'flex', justifyContent: 'center', color: 'var(--text-muted)', marginBottom: 12 }}>
                <Icon name="inbox" size={32} />
              </div>
              <div className="empty-state-title">
                {hasActiveFilters ? 'No matching captures found' : 'No captures on this date'}
              </div>
              <div className="empty-state-desc">
                {hasActiveFilters
                  ? 'Try adjusting or clearing your filters.'
                  : 'The tracker may not have been running, or the backend is unreachable.'}
              </div>
              <div style={{ marginTop: 12 }}>
                {hasActiveFilters ? (
                  <button
                    className="btn btn-secondary btn-sm"
                    onClick={() => { setSearchQuery(''); setSelectedApp('ALL'); setSelectedCategory('ALL'); setCurrentPage(1); }}
                  >
                    Clear filters
                  </button>
                ) : today !== localISODate() ? (
                  <button className="btn btn-secondary btn-sm" onClick={() => onDateChange(localISODate())}>Go to today</button>
                ) : null}
              </div>
            </div>
          ) : (
            <div className="timeline-list">
              {paginatedLogs.map((log) => {
                const scoreVal = typeof log.productive_score === 'number'
                  ? Math.round(log.productive_score)
                  : log.is_productive ? 100 : 0;

                return (
                  <div key={log.id} className="timeline-item" style={{ padding: '8px 12px', gap: 10 }}>
                    <span className="timeline-time" style={{ fontVariantNumeric: 'tabular-nums' }}>
                      {format(new Date(log.timestamp), 'HH:mm:ss')}
                    </span>

                    <ScreenshotThumb
                      imagePath={log.image_path}
                      onClick={() => setSelectedImage(log.image_path)}
                    />

                    <div className="timeline-info">
                      <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexWrap: 'wrap' }}>
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
                              padding: '1px 7px',
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
                            style={{ fontSize: 11, color: 'var(--text-muted)', fontStyle: 'italic', maxWidth: 160, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
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
                            whiteSpace: 'nowrap',
                            overflow: 'hidden',
                            textOverflow: 'ellipsis',
                          }}
                        >
                          🪟 {log.window_title}
                        </div>
                      )}

                      <div className="timeline-reason" title={log.ai_reason || undefined}>
                        {log.ai_reason || '—'}
                      </div>

                      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, alignItems: 'center', fontSize: 11, color: 'var(--text-muted)', fontVariantNumeric: 'tabular-nums' }}>
                        <span>⌨️ {log.total_keys} keys · {log.unique_keys} unique</span>
                        <span style={{ color: 'var(--border-medium)' }}>•</span>
                        <span>Entropy: {log.entropy_score.toFixed(1)}</span>
                      </div>
                    </div>

                    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end', gap: 3, flexShrink: 0 }}>
                      {isPendingLog(log) ? (
                        <span className="badge badge-pending" style={{ fontVariantNumeric: 'tabular-nums' }}>Pending AI</span>
                      ) : (
                        <span className={`badge ${scoreVal >= 50 ? 'badge-productive' : 'badge-unproductive'}`} style={{ fontVariantNumeric: 'tabular-nums' }}>
                          <Icon name={scoreVal >= 50 ? 'check' : 'x'} size={13} />
                          {scoreVal}% Focus
                        </span>
                      )}
                      {log.ai_confidence > 0 && (
                        <span style={{ fontSize: 10, color: 'var(--text-muted)', fontVariantNumeric: 'tabular-nums' }}>
                          {(log.ai_confidence * 100).toFixed(0)}% AI confidence
                        </span>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>

        {/* Pagination Footer */}
        {filteredLogs.length > 0 && (
          <div style={{ padding: '10px 16px', borderTop: '1px solid var(--border-subtle)', display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, flexWrap: 'wrap' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <label htmlFor="timeline-page-size" style={{ fontSize: 12, color: 'var(--text-muted)' }}>Per page</label>
              <CustomSelect
                id="timeline-page-size"
                ariaLabel="Rows per page"
                compact
                value={String(pageSize)}
                onChange={(value) => {
                  setPageSize(Number(value));
                  setCurrentPage(1);
                }}
                triggerStyle={{ minWidth: 64 }}
                options={[15, 25, 50, 100].map((size) => ({
                  value: String(size),
                  label: String(size),
                }))}
              />
            </div>

            <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
              <button
                disabled={currentPage <= 1}
                onClick={() => setCurrentPage((p) => Math.max(1, p - 1))}
                className="btn btn-secondary btn-sm"
              >
                ← Previous
              </button>
              <span style={{ fontSize: 12, color: 'var(--text-primary)', fontWeight: 600, fontVariantNumeric: 'tabular-nums' }}>
                {currentPage} / {totalPages}
              </span>
              <button
                disabled={currentPage >= totalPages}
                onClick={() => setCurrentPage((p) => Math.min(totalPages, p + 1))}
                className="btn btn-secondary btn-sm"
              >
                Next →
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
