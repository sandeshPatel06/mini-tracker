import { useState, useEffect, useCallback } from 'react';
import { LogEntry, ProductivityStats, AppConfig, Page, User } from './types';
import Dashboard from './pages/Dashboard';
import Timeline from './pages/Timeline';
import Analytics from './pages/Analytics';
import { OrganizationPage } from './pages/Organization';
import { AcceptInvitePage } from './pages/AcceptInvite';
import { AuthPage } from './pages/Auth';
import './style.css';

// Wails runtime bindings
declare const window: Window & {
  go?: {
    main: {
      App: {
        GetTodayLogs: () => Promise<LogEntry[]>;
        GetLogsByDate: (date: string) => Promise<LogEntry[]>;
        GetStats: (date: string) => Promise<ProductivityStats>;
        GetConfig: () => Promise<AppConfig>;
      };
    };
  };
};

function callGo<T>(fn: () => Promise<T>): Promise<T | null> {
  try {
    return fn().catch(() => null);
  } catch {
    return Promise.resolve(null);
  }
}

export default function App() {
  const [page, setPage] = useState<Page>('dashboard');
  const [inviteToken, setInviteToken] = useState<string>('');
  const [today, setToday] = useState(() => new Date().toISOString().slice(0, 10));
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [stats, setStats] = useState<ProductivityStats | null>(null);
  const [config, setConfig] = useState<AppConfig | null>(null);
  const [loading, setLoading] = useState(true);

  // User Auth & Session state
  const [currentUser, setCurrentUser] = useState<User | null>(null);
  const [authChecked, setAuthChecked] = useState<boolean>(false);
  const [isGuestMode, setIsGuestMode] = useState<boolean>(false);

  // Tracker Work Clock State
  const [isTrackingActive, setIsTrackingActive] = useState<boolean>(true);
  const [elapsedSeconds, setElapsedSeconds] = useState<number>(0);

  // Verify active user session on startup
  useEffect(() => {
    fetch('/api/auth/me')
      .then(r => r.ok ? r.json() : null)
      .then(data => {
        if (data && data.authenticated && data.user) {
          setCurrentUser(data.user);
        }
      })
      .catch(() => {})
      .finally(() => {
        setAuthChecked(true);
      });
  }, []);

  const handleLogout = async () => {
    try {
      await fetch('/api/auth/logout', { method: 'POST' });
    } catch {}
    setCurrentUser(null);
    setIsGuestMode(false);
    setPage('dashboard');
  };

  // Fetch initial tracker status
  useEffect(() => {
    fetch('/api/tracker/status')
      .then(r => r.ok ? r.json() : null)
      .then(data => {
        if (data) {
          setIsTrackingActive(!!data.active);
          setElapsedSeconds(data.elapsed_seconds || 0);
        }
      })
      .catch(() => {});
  }, []);

  // Timer ticker
  useEffect(() => {
    if (!isTrackingActive) return;
    const interval = setInterval(() => {
      setElapsedSeconds(prev => prev + 1);
    }, 1000);
    return () => clearInterval(interval);
  }, [isTrackingActive]);

  const handleToggleTracking = async () => {
    try {
      const res = await fetch('/api/tracker/toggle', { method: 'POST' });
      if (res.ok) {
        const data = await res.json();
        setIsTrackingActive(!!data.active);
        setElapsedSeconds(data.elapsed_seconds || 0);
      } else {
        setIsTrackingActive(!isTrackingActive);
      }
    } catch {
      setIsTrackingActive(!isTrackingActive);
    }
  };

  const formatTimer = (totalSeconds: number) => {
    const hrs = Math.floor(totalSeconds / 3600);
    const mins = Math.floor((totalSeconds % 3600) / 60);
    const secs = totalSeconds % 60;
    const pad = (n: number) => n.toString().padStart(2, '0');
    return `${pad(hrs)}:${pad(mins)}:${pad(secs)}`;
  };

  // Hash-based routing handler for invitations
  const checkHashRoute = useCallback(() => {
    const hash = window.location.hash;
    if (hash.includes('/accept-invite')) {
      const match = hash.match(/token=([^&]+)/);
      if (match && match[1]) {
        setInviteToken(match[1]);
        setPage('accept-invite');
      }
    } else if (hash === '#organization') {
      setPage('organization');
    }
  }, []);

  useEffect(() => {
    checkHashRoute();
    window.addEventListener('hashchange', checkHashRoute);
    return () => window.removeEventListener('hashchange', checkHashRoute);
  }, [checkHashRoute]);

  const loadData = useCallback(async (date: string) => {
    setLoading(true);
    let logsData: LogEntry[] | null = null;
    let statsData: ProductivityStats | null = null;
    let configData: AppConfig | null = null;

    if (window.go?.main?.App) {
      [logsData, statsData, configData] = await Promise.all([
        callGo(() => window.go!.main.App.GetLogsByDate(date)),
        callGo(() => window.go!.main.App.GetStats(date)),
        callGo(() => window.go!.main.App.GetConfig()),
      ]);
    } else {
      try {
        const [resLogs, resStats, resConfig] = await Promise.all([
          fetch(`/api/logs?date=${date}`).then(r => r.ok ? r.json() : []),
          fetch(`/api/stats?date=${date}`).then(r => r.ok ? r.json() : null),
          fetch(`/api/config`).then(r => r.ok ? r.json() : null),
        ]);
        logsData = resLogs;
        statsData = resStats;
        configData = resConfig;
      } catch (err) {
        console.error('API fetch error:', err);
      }
    }

    setLogs(logsData ?? []);
    setStats(statsData ?? null);
    setConfig(configData ?? null);
    setLoading(false);
  }, []);

  // Initial load
  useEffect(() => {
    loadData(today);
  }, [today, loadData]);

  // Auto-refresh every 15 seconds
  useEffect(() => {
    const tid = setInterval(() => {
      const todayStr = new Date().toISOString().slice(0, 10);
      if (page === 'dashboard' || page === 'timeline') {
        loadData(todayStr);
      }
    }, 15_000);
    return () => clearInterval(tid);
  }, [page, loadData]);

  const navItems: { id: Page; icon: string; label: string }[] = [
    { id: 'dashboard', icon: '⚡', label: 'Dashboard' },
    { id: 'timeline',  icon: '📋', label: 'Timeline'  },
    { id: 'analytics', icon: '📊', label: 'Analytics' },
    { id: 'organization', icon: '🏢', label: 'Team / Org' },
  ];

  const isTracking = isTrackingActive;
  const isAIReady = config?.ai_configured ?? false;

  // Show Auth Page if user is not authenticated and has not chosen guest mode
  if (authChecked && !currentUser && !isGuestMode) {
    return (
      <AuthPage
        onAuthSuccess={(user) => {
          setCurrentUser(user);
          setPage('dashboard');
        }}
        onSkip={() => setIsGuestMode(true)}
      />
    );
  }

  return (
    <div className="app-shell">
      {/* Sidebar */}
      <aside className="sidebar">
        <div className="sidebar-logo">
          <div className="sidebar-logo-icon">📍</div>
          <div>
            <div className="sidebar-logo-text">Mini Tracker</div>
            <div className="sidebar-logo-sub">Productivity</div>
          </div>
        </div>

        {/* Work Clock & Tracking Control Widget */}
        <div
          style={{
            margin: '12px 16px',
            padding: '12px 14px',
            background: 'var(--bg-card-hover)',
            border: '1px solid var(--border-color)',
            borderRadius: 'var(--radius-md)',
            boxShadow: 'var(--shadow-sm)',
          }}
        >
          <div style={{ fontSize: 10, textTransform: 'uppercase', letterSpacing: 0.8, color: 'var(--text-muted)', fontWeight: 700, marginBottom: 4 }}>
            Work Session
          </div>
          <div style={{ fontFamily: 'monospace', fontSize: 20, fontWeight: 700, color: 'var(--text-primary)', letterSpacing: 1, marginBottom: 8 }}>
            {formatTimer(elapsedSeconds)}
          </div>
          <button
            onClick={handleToggleTracking}
            style={{
              width: '100%',
              padding: '6px 12px',
              border: isTrackingActive ? '1px solid rgba(239, 68, 68, 0.3)' : 'none',
              borderRadius: 'var(--radius-sm)',
              fontWeight: 700,
              fontSize: 12,
              cursor: 'pointer',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              gap: 6,
              transition: 'all 0.2s ease',
              background: isTrackingActive ? 'rgba(239, 68, 68, 0.15)' : 'var(--accent-green)',
              color: isTrackingActive ? 'var(--accent-red)' : '#000',
            }}
          >
            <span>{isTrackingActive ? '⏸️' : '▶️'}</span>
            <span>{isTrackingActive ? 'Pause Tracker' : 'Start Tracker'}</span>
          </button>
        </div>

        <div className="sidebar-section-label">Navigation</div>
        {navItems.map((item) => (
          <div
            key={item.id}
            id={`nav-${item.id}`}
            className={`nav-item ${page === item.id ? 'active' : ''}`}
            onClick={() => {
              window.location.hash = item.id === 'organization' ? 'organization' : '';
              setPage(item.id);
            }}
          >
            <span className="nav-icon">{item.icon}</span>
            <span>{item.label}</span>
          </div>
        ))}

        <div className="sidebar-footer">
          {currentUser ? (
            <div style={{ marginBottom: 12, paddingBottom: 10, borderBottom: '1px solid var(--border-subtle)' }}>
              <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-primary)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                👤 {currentUser.full_name || currentUser.email}
              </div>
              <div style={{ fontSize: 10, color: 'var(--text-muted)', display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginTop: 4 }}>
                <span style={{ textTransform: 'capitalize' }}>Role: {currentUser.role}</span>
                <button
                  onClick={handleLogout}
                  style={{
                    background: 'transparent',
                    border: 'none',
                    color: 'var(--accent-red)',
                    fontSize: 10,
                    fontWeight: 700,
                    cursor: 'pointer',
                  }}
                >
                  Logout 🚪
                </button>
              </div>
            </div>
          ) : (
            <div style={{ marginBottom: 10 }}>
              <button
                onClick={() => setIsGuestMode(false)}
                style={{
                  width: '100%',
                  padding: '4px 8px',
                  background: 'var(--bg-elevated)',
                  border: '1px solid var(--border-medium)',
                  borderRadius: 'var(--radius-sm)',
                  color: 'var(--accent-purple)',
                  fontSize: 11,
                  fontWeight: 600,
                  cursor: 'pointer',
                  marginBottom: 8,
                }}
              >
                🔐 Sign In / Sign Up
              </button>
            </div>
          )}

          <div className="status-badge" style={{ marginBottom: 8 }}>
            <div className={`status-dot ${isTracking ? '' : 'inactive'}`} />
            <span>{isTracking ? 'Tracking active' : 'Tracker paused'}</span>
          </div>
          <div className="status-badge">
            <div className={`status-dot ${isAIReady ? '' : 'inactive'}`} style={{ background: isAIReady ? 'var(--accent-teal)' : undefined, boxShadow: isAIReady ? '0 0 8px var(--accent-teal)' : undefined }} />
            <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>
              {isAIReady ? 'AI Engine Ready' : 'AI Standby'}
            </span>
          </div>
        </div>
      </aside>

      {/* Main content area */}
      <main className="main-content">
        {page === 'dashboard' && (
          <Dashboard
            logs={logs}
            stats={stats}
            config={config}
            loading={loading}
            today={today}
            onRefresh={() => loadData(today)}
          />
        )}
        {page === 'timeline' && (
          <Timeline
            logs={logs}
            loading={loading}
            today={today}
            onDateChange={(d) => { setToday(d); loadData(d); }}
          />
        )}
        {page === 'analytics' && (
          <Analytics
            logs={logs}
            stats={stats}
            loading={loading}
            today={today}
            onDateChange={(d) => { setToday(d); loadData(d); }}
          />
        )}
        {page === 'organization' && (
          <OrganizationPage />
        )}
        {page === 'accept-invite' && (
          <AcceptInvitePage
            token={inviteToken}
            onSuccess={() => {
              window.location.hash = 'organization';
              setPage('organization');
            }}
          />
        )}
      </main>
    </div>
  );
}

