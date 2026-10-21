import { useState, useEffect, useCallback, useRef } from 'react';
import { LogEntry, ProductivityStats, AppConfig, Page, User } from './types';
import { apiFetch, setRuntimeBackendUrl } from './api';
import { useToast } from './context/ToastContext';
import Dashboard from './pages/Dashboard';
import Timeline from './pages/Timeline';
import Analytics from './pages/Analytics';
import { OrganizationPage } from './pages/Organization';
import { AcceptInvitePage } from './pages/AcceptInvite';
import type { AcceptInviteResult } from './pages/AcceptInvite';
import { AuthPage } from './pages/Auth';
import { SettingsPage } from './pages/Settings';
import { Icon, IconName } from './components/Icon';
import { Sidebar } from './components/Sidebar';
import { TaskGoalModal } from './components/TaskGoalModal';
import { EmptyState } from './components/EmptyState';
import { Alert } from './components/Alert';
import { DevMenu } from './components/DevMenu';
import { useDevMode } from './hooks/useDevMode';
import { useInputTracker } from './hooks/useInputTracker';
import { runClientVisionAnalysis } from './services/aiVision';
import { callGo, syncBackendAuthSession } from './services/wailsBridge';
import { localISODate, isPendingLog, bumpUsageCount } from './utils';
import { EventsOn } from '../wailsjs/runtime/runtime';
import './style.css';

const AUTH_STORAGE_KEYS = ['mini_auth_user', 'mini_guest_mode', 'mini_jwt_token', 'mini_auth_org'];

function clearStoredAuth() {
  AUTH_STORAGE_KEYS.forEach((key) => localStorage.removeItem(key));
}

export default function App() {
  const toast = useToast();
  const [page, setPage] = useState<Page>('dashboard');
  const [inviteToken, setInviteToken] = useState<string>('');
  const [today, setToday] = useState(() => localISODate());
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [stats, setStats] = useState<ProductivityStats | null>(null);
  const [config, setConfig] = useState<AppConfig | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [aiKeyPresent, setAiKeyPresent] = useState(() => !!localStorage.getItem('mini_gemini_api_key'));
  const [trackerError, setTrackerError] = useState<string | null>(null);

  // User Auth & Session state
  const [currentUser, setCurrentUser] = useState<User | null>(() => {
    try {
      const stored = localStorage.getItem('mini_auth_user');
      return stored ? JSON.parse(stored) : null;
    } catch {
      return null;
    }
  });
  const [authChecked, setAuthChecked] = useState<boolean>(false);
  const [isGuestMode, setIsGuestMode] = useState<boolean>(() => {
    return localStorage.getItem('mini_guest_mode') === 'true';
  });
  const [authNotice, setAuthNotice] = useState<string | null>(null);
  const [backendUnavailable, setBackendUnavailable] = useState<boolean>(false);

  // Theme Management (Dark, Light, Auto System Preference)
  const [theme, setTheme] = useState<'dark' | 'light' | 'auto'>(() => {
    return (localStorage.getItem('mini_theme') as 'dark' | 'light' | 'auto') || 'auto';
  });

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme);
    localStorage.setItem('mini_theme', theme);
  }, [theme]);

  // Developer menu (unlocked by 7 quick taps on a logo, session-only)
  const dev = useDevMode();
  const [devToastVisible, setDevToastVisible] = useState(false);

  useEffect(() => {
    if (dev.tapProgress < 4 || dev.tapProgress >= 7) {
      setDevToastVisible(false);
      return;
    }
    setDevToastVisible(true);
    const timer = window.setTimeout(() => setDevToastVisible(false), 1600);
    return () => window.clearTimeout(timer);
  }, [dev.tapProgress]);

  // Tracker Work Clock State
  const [isTrackingActive, setIsTrackingActive] = useState<boolean>(true);
  const [elapsedSeconds, setElapsedSeconds] = useState<number>(0);

  // Active Task Goal State (Max 20 words limit)
  const [userTask, setUserTask] = useState<string>(() => localStorage.getItem('mini_user_task') || '');
  const [showTaskModal, setShowTaskModal] = useState<boolean>(false);

  const saveTask = (text: string) => {
    const words = text.trim().split(/\s+/).filter(Boolean);
    const limited = words.slice(0, 20).join(' ');
    setUserTask(limited);
    localStorage.setItem('mini_user_task', limited);
    const w = window as any;
    if (w.go?.main?.App?.SetActiveTask) {
      callGo(() => w.go.main.App.SetActiveTask(limited));
    }
    setShowTaskModal(false);
  };

  // Zero-sudo mode: auto-track keypresses and mouse clicks via extracted hook
  useInputTracker(isTrackingActive);

  // Verify active user session on startup
  useEffect(() => {
    const checkSession = async () => {
      const w = window as any;
      if (w.go?.main?.App?.GetConfig) {
        try {
          const appConfig = await w.go.main.App.GetConfig();
          if (appConfig?.backend_endpoint) {
            setRuntimeBackendUrl(appConfig.backend_endpoint);
          }
        } catch {}
      }

      try {
        const res = await apiFetch('/api/auth/me');
        const data = res.ok ? await res.json() : null;
        setBackendUnavailable(false);
        if (data && data.authenticated && data.user) {
          setCurrentUser(data.user);
          localStorage.setItem('mini_auth_user', JSON.stringify(data.user));
        } else {
          localStorage.removeItem('mini_auth_user');
          setCurrentUser(null);
          if (localStorage.getItem('mini_guest_mode') === 'true') {
            setIsGuestMode(true);
          }
        }
      } catch {
        if ((window as any).go?.main?.App) {
          setBackendUnavailable(true);
        }
        if (localStorage.getItem('mini_guest_mode') === 'true') {
          setIsGuestMode(true);
          return;
        }
        try {
          const cached = localStorage.getItem('mini_auth_user');
          if (cached) {
            const parsed = JSON.parse(cached);
            if (parsed?.id) {
              setCurrentUser(parsed);
            }
          }
        } catch {}
      } finally {
        setAuthChecked(true);
      }
    };

    checkSession();
  }, []);

  const retryBackendConnection = useCallback(async (): Promise<boolean> => {
    const w = window as any;
    if (w.go?.main?.App?.GetConfig) {
      try {
        const appConfig = await w.go.main.App.GetConfig();
        if (appConfig?.backend_endpoint) {
          setRuntimeBackendUrl(appConfig.backend_endpoint);
        }
      } catch {}
    }
    try {
      await apiFetch('/api/auth/me');
      setBackendUnavailable(false);
      return true;
    } catch {
      setBackendUnavailable(true);
      return false;
    }
  }, []);

  // Keep Wails backend session in sync with auth state
  useEffect(() => {
    const token = localStorage.getItem('mini_jwt_token') || '';
    if (isGuestMode) {
      syncBackendAuthSession('', true);
    } else if (token) {
      syncBackendAuthSession(token, false);
    }
  }, [currentUser, isGuestMode]);

  // Handle session expiration
  useEffect(() => {
    const handleAuthExpired = () => {
      const hadSession = Boolean(localStorage.getItem('mini_jwt_token') || localStorage.getItem('mini_auth_user'));
      if (!hadSession) return;
      clearStoredAuth();
      syncBackendAuthSession('', true);
      setCurrentUser(null);
      setIsGuestMode(false);
      setAuthNotice('Your session expired. Please sign in again.');
    };
    window.addEventListener('mini-auth-expired', handleAuthExpired);
    return () => window.removeEventListener('mini-auth-expired', handleAuthExpired);
  }, []);

  const handleLogout = async () => {
    try {
      await apiFetch('/api/auth/logout', { method: 'POST' });
    } catch {}
    clearStoredAuth();
    syncBackendAuthSession('', true);
    setCurrentUser(null);
    setIsGuestMode(false);
    setPage('dashboard');
  };

  // Desktop AI usage event listener
  useEffect(() => {
    if (!(window as any).go?.main?.App) return;
    const off = EventsOn('ai_usage_updated', (payload: any) => {
      const requests = typeof payload?.requests === 'number' && payload.requests > 0 ? payload.requests : 1;
      bumpUsageCount(requests);
    });
    return () => {
      try { (off as unknown as () => void)?.(); } catch {}
    };
  }, []);

  // Fetch initial tracker status
  useEffect(() => {
    const app = (window as any).go?.main?.App;
    if (app?.GetTrackingStatus) {
      callGo(() => app.GetTrackingStatus())
        .then((status: any) => {
          if (status) {
            setIsTrackingActive(!!status.active);
            if (typeof status.elapsed_seconds === 'number') {
              setElapsedSeconds(status.elapsed_seconds);
            }
          }
        })
        .catch(() => {});
      return;
    }

    apiFetch('/api/tracker/status')
      .then(r => r.ok ? r.json() : null)
      .then(data => {
        if (data) {
          setIsTrackingActive(!!data.active);
          if (data.elapsed_seconds && data.elapsed_seconds > 0) {
            setElapsedSeconds(data.elapsed_seconds);
          }
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
    const refreshStatus = async () => {
      try {
        const app = (window as any).go?.main?.App;
        if (app?.GetTrackingStatus) {
          const status: any = await callGo(() => app.GetTrackingStatus());
          if (status) {
            setIsTrackingActive(!!status.active);
            setElapsedSeconds(status.elapsed_seconds || 0);
            return;
          }
        }
        const res = await apiFetch('/api/tracker/status');
        if (res.ok) {
          const data = await res.json();
          setIsTrackingActive(!!data.active);
          setElapsedSeconds(data.elapsed_seconds || 0);
        }
      } catch {}
    };
    try {
      const app = (window as any).go?.main?.App;
      if (app?.ToggleTracking) {
        const res: any = await callGo(() => app.ToggleTracking());
        if (res) {
          setIsTrackingActive(!!res.active);
          setElapsedSeconds(res.elapsed_seconds || 0);
          setTrackerError(null);
          toast.success(res.active ? 'Work tracker started' : 'Work tracker paused');
          return;
        }
      }
      const res = await apiFetch('/api/tracker/toggle', { method: 'POST' });
      if (res.ok) {
        const data = await res.json();
        setIsTrackingActive(!!data.active);
        setElapsedSeconds(data.elapsed_seconds || 0);
        setTrackerError(null);
        toast.success(data.active ? 'Work tracker started' : 'Work tracker paused');
      } else {
        const msg = 'Failed to update tracker status.';
        setTrackerError(msg);
        toast.error(msg);
        await refreshStatus();
      }
    } catch {
      const msg = 'Failed to update tracker status. Server might be offline.';
      setTrackerError(msg);
      toast.error(msg);
      await refreshStatus();
    }
  };

  const formatTimer = (totalSeconds: number) => {
    const hrs = Math.floor(totalSeconds / 3600);
    const mins = Math.floor((totalSeconds % 3600) / 60);
    const secs = totalSeconds % 60;
    const pad = (n: number) => n.toString().padStart(2, '0');
    return `${pad(hrs)}:${pad(mins)}:${pad(secs)}`;
  };

  // Hash-based routing
  const checkHashRoute = useCallback(() => {
    const urlParams = new URLSearchParams(window.location.search);
    const queryToken = urlParams.get('token') || urlParams.get('invite');
    if (queryToken) {
      setInviteToken(queryToken);
      setPage('accept-invite');
      return;
    }

    const hash = window.location.hash;
    if (hash.includes('accept-invite')) {
      const match = hash.match(/token=([^&]+)/);
      if (match && match[1]) {
        setInviteToken(match[1]);
        setPage('accept-invite');
      }
      return;
    }
    const cleanHash = hash.split('?')[0];
    const route = cleanHash.replace(/^#\/?/, '');
    if (route === 'dashboard' || route === 'timeline' || route === 'analytics' || route === 'settings' || route === 'organization') {
      setPage(route as Page);
    } else if (route === '') {
      setPage('dashboard');
    }
  }, []);

  useEffect(() => {
    checkHashRoute();
    window.addEventListener('hashchange', checkHashRoute);
    return () => window.removeEventListener('hashchange', checkHashRoute);
  }, [checkHashRoute]);

  const [selectedUserId, setSelectedUserId] = useState<number | null>(null);
  const [startDate, setStartDate] = useState<string>(today);
  const [endDate, setEndDate] = useState<string>(today);
  const [isSyncing, setIsSyncing] = useState<boolean>(false);
  const [syncMessage, setSyncMessage] = useState<string>('');
  const syncMessageTimerRef = useRef<number | null>(null);

  useEffect(() => {
    return () => {
      if (syncMessageTimerRef.current) clearTimeout(syncMessageTimerRef.current);
    };
  }, []);

  const handleSyncNow = async () => {
    if (syncMessageTimerRef.current) {
      clearTimeout(syncMessageTimerRef.current);
      syncMessageTimerRef.current = null;
    }
    setIsSyncing(true);
    setSyncMessage('Syncing...');
    try {
      const w = window as any;
      if (w.go?.main?.App?.TriggerSyncNow) {
        await w.go.main.App.TriggerSyncNow();
      } else {
        await apiFetch('/api/telemetry/pull');
      }
      await loadData();
      setSyncMessage('Synced just now');
    } catch {
      setSyncMessage('Sync failed');
    } finally {
      setIsSyncing(false);
      syncMessageTimerRef.current = window.setTimeout(() => setSyncMessage(''), 4000);
    }
  };

  const stateRef = useRef({} as any);
  stateRef.current = { selectedUserId, startDate, endDate, userTask, today, page };

  const requestIdRef = useRef(0);
  const aiAnalysisInFlightRef = useRef(false);
  const aiSubmittedLogIdsRef = useRef<Set<number>>(new Set());
  const aiResultCacheRef = useRef<Map<number, Partial<LogEntry>>>(new Map());

  const loadData = useCallback(async (date?: string, filterUserId?: number | null, sDate?: string, eDate?: string, opts?: { background?: boolean }) => {
    const requestId = ++requestIdRef.current;
    if (!opts?.background) {
      setLoading(true);
    }
    let logsData: LogEntry[] | null = null;
    let statsData: ProductivityStats | null = null;
    let configData: AppConfig | null = null;

    const targetUser = filterUserId !== undefined ? filterUserId : stateRef.current.selectedUserId;

    let start: string;
    let end: string;
    if (sDate || eDate) {
      start = sDate || eDate!;
      end = eDate || sDate!;
    } else if (date) {
      start = date;
      end = date;
    } else if (stateRef.current.page === 'analytics') {
      start = stateRef.current.startDate || stateRef.current.today || localISODate();
      end = stateRef.current.endDate || stateRef.current.today || localISODate();
    } else {
      start = stateRef.current.today || localISODate();
      end = start;
    }

    const userParam = targetUser ? `&user_id=${targetUser}` : '';
    const dateParam = `start_date=${start}&end_date=${end}`;

    const w = window as any;
    if (w.go?.main?.App) {
      const dates: string[] = [];
      if (start !== end) {
        const rangeStart = new Date(`${start}T00:00:00`);
        const rangeEnd = new Date(`${end}T00:00:00`);
        for (let d = new Date(rangeStart); d <= rangeEnd && dates.length < 31; d.setDate(d.getDate() + 1)) {
          dates.push(localISODate(d));
        }
      }
      if (dates.length === 0) dates.push(start);

      const statsDate = start !== end ? end : (date || end);
      const [logsLists, statsResult, configResult] = await Promise.all([
        Promise.all(dates.map(d => callGo<LogEntry[]>(() => w.go!.main.App.GetLogsByDate(d)))),
        callGo<ProductivityStats>(() => w.go!.main.App.GetStats(statsDate)),
        callGo<AppConfig>(() => w.go!.main.App.GetConfig()),
      ]);
      const fetchedLists = logsLists.filter((list): list is LogEntry[] => Array.isArray(list));
      if (fetchedLists.length === 0) {
        logsData = null;
      } else {
        logsData = fetchedLists
          .flat()
          .sort((a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime());
      }
      statsData = statsResult;
      configData = configResult;
    } else {
      try {
        const [resLogs, resStats, resConfig] = await Promise.all([
          apiFetch(`/api/logs?${dateParam}${userParam}`).then(r => {
            if (!r.ok) throw new Error(`Logs failed with status ${r.status}`);
            return r.json();
          }),
          apiFetch(`/api/stats?${dateParam}${userParam}`).then(r => {
            if (!r.ok) throw new Error(`Stats failed with status ${r.status}`);
            return r.json();
          }),
          apiFetch(`/api/config`).then(r => {
            if (!r.ok) throw new Error(`Config failed with status ${r.status}`);
            return r.json();
          }),
        ]);
        logsData = resLogs;
        statsData = resStats;
        configData = resConfig;
      } catch (err: any) {
        console.warn('Backend server request failed:', err);
        setLoadError(err?.message || "Couldn't reach the tracker backend.");
      }
    }

    if (requestId !== requestIdRef.current) return;

    if (logsData === null) {
      setLoadError("Couldn't reach the tracker backend.");
    } else {
      setLoadError(null);
    }

    if (configData?.backend_endpoint) {
      setRuntimeBackendUrl(configData.backend_endpoint);
    }

    const isDesktop = Boolean(w.go?.main?.App);
    const localApiKey = isDesktop ? localStorage.getItem('mini_gemini_api_key') : null;
    const serverAiConfigured = Boolean(configData?.ai_configured);
    setAiKeyPresent(Boolean(localApiKey || serverAiConfigured));
    const rawModel = localStorage.getItem('mini_ai_model') || 'models/gemini-3.5-flash-lite';
    const localModel = rawModel.startsWith('models/') ? rawModel : `models/${rawModel}`;
    const intervalSec = configData?.screenshot_interval_seconds || 30;

    if (logsData && logsData.length > 0 && (localApiKey || (!isDesktop && serverAiConfigured))) {
      const candidateLogs = logsData
        .filter(isPendingLog)
        .filter((log) => !aiSubmittedLogIdsRef.current.has(log.id));

      if (candidateLogs.length >= 3 && !aiAnalysisInFlightRef.current) {
        aiAnalysisInFlightRef.current = true;
        if (!isDesktop) {
          const candidateBatch = candidateLogs.slice(0, 6);
          const candidateIds = candidateBatch.map(c => c.id);
          candidateIds.forEach(id => aiSubmittedLogIdsRef.current.add(id));

          apiFetch('/api/ai/analyze-batch', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
              log_ids: candidateIds,
              user_task: stateRef.current.userTask as string,
            }),
          })
            .then(async (res) => {
              if (res.ok) {
                const data = await res.json();
                if (data.results && Array.isArray(data.results)) {
                  if (requestId === requestIdRef.current) {
                    setLogs((prev) => prev.map((log) => {
                      const match = data.results.find((r: any) => r.LogID === log.id);
                      if (match) {
                        return {
                          ...log,
                          ai_category: match.category,
                          app_name: match.app_name || log.app_name,
                          app_category: match.app_category || log.app_category,
                          window_title: match.window_title || log.window_title,
                          is_productive: match.productive,
                          productive_score: match.productivity_score,
                          ai_confidence: match.confidence,
                          ai_reason: match.reason,
                        };
                      }
                      return log;
                    }));
                  }
                }
              } else {
                candidateIds.forEach(id => aiSubmittedLogIdsRef.current.delete(id));
              }
            })
            .catch(() => {
              candidateIds.forEach(id => aiSubmittedLogIdsRef.current.delete(id));
            })
            .finally(() => {
              aiAnalysisInFlightRef.current = false;
            });
        } else if (localApiKey) {
          void runClientVisionAnalysis({
            candidateLogs,
            apiKey: localApiKey,
            model: localModel,
            intervalSec,
            userTask: stateRef.current.userTask as string,
            submittedIds: aiSubmittedLogIdsRef.current,
            resultCache: aiResultCacheRef.current,
            setLogs,
          }).finally(() => {
            aiAnalysisInFlightRef.current = false;
          });
        }
      }
    }

    const mergedLogs = (logsData ?? []).map((log) => {
      if (!isPendingLog(log)) return log;
      const patch = aiResultCacheRef.current.get(log.id);
      return patch ? { ...log, ...patch } : log;
    });

    setLogs(mergedLogs);
    setStats(statsData ?? null);
    setConfig(configData ?? null);
    setLoading(false);
  }, []);

  const loadDataRef = useRef(loadData);
  loadDataRef.current = loadData;

  const retryLoad = () => {
    if (page === 'analytics') {
      loadData(today, selectedUserId, startDate, endDate);
    } else {
      loadData(today);
    }
  };

  useEffect(() => {
    loadData(today);
  }, [today, loadData]);

  // Periodic background refresh & 7-day retention cleanup
  useEffect(() => {
    const syncInterval = setInterval(() => {
      loadDataRef.current(undefined, undefined, undefined, undefined, { background: true });

      try {
        const retentionCutoff = new Date();
        retentionCutoff.setDate(retentionCutoff.getDate() - 7);
        const cutoffStr = localISODate(retentionCutoff);
        
        Object.keys(localStorage).forEach(key => {
          if (key.startsWith('mini_logs_') || key.startsWith('mini_stats_')) {
            const datePart = key.split('_').pop();
            if (datePart && datePart < cutoffStr) {
              localStorage.removeItem(key);
            }
          }
        });
      } catch {}
    }, 30000);

    return () => clearInterval(syncInterval);
  }, []);

  const [isMobileMenuOpen, setIsMobileMenuOpen] = useState<boolean>(false);

  useEffect(() => {
    if (!isMobileMenuOpen) return;
    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setIsMobileMenuOpen(false);
    };
    window.addEventListener('keydown', handleEscape);
    return () => window.removeEventListener('keydown', handleEscape);
  }, [isMobileMenuOpen]);

  const navigateTo = (targetPage: Page) => {
    if (targetPage !== 'accept-invite') {
      window.location.hash = `#${targetPage}`;
    }
    setPage(targetPage);
    setIsMobileMenuOpen(false);
  };

  const navItems: { id: Page; iconName: IconName; label: string }[] = [
    { id: 'dashboard', iconName: 'target', label: 'Dashboard' },
    { id: 'timeline',  iconName: 'clock', label: 'Timeline'  },
    { id: 'analytics', iconName: 'activity', label: 'Analytics' },
    ...(currentUser?.role === 'admin' || currentUser?.role === 'owner' ? [
      { id: 'organization' as Page, iconName: 'building' as IconName, label: 'Team / Org' },
      { id: 'settings' as Page, iconName: 'settings' as IconName, label: 'AI Settings' },
    ] : isGuestMode ? [
      { id: 'settings' as Page, iconName: 'settings' as IconName, label: 'Preferences' },
    ] : []),
  ];

  const mainRef = useRef<HTMLElement | null>(null);

  useEffect(() => {
    const titles: Record<Page, string> = {
      dashboard: 'Dashboard',
      timeline: 'Timeline',
      analytics: 'Analytics',
      organization: 'Team / Org',
      settings: 'Settings',
      'accept-invite': 'Accept Invitation',
      auth: 'Sign In',
    };
    document.title = `${titles[page]} — get-Hike`;
    const main = mainRef.current;
    if (main) {
      main.scrollTop = 0;
      main.focus();
    }
  }, [page]);

  const completeInviteSignIn = (result: AcceptInviteResult) => {
    if (result.user) {
      localStorage.setItem('mini_auth_user', JSON.stringify(result.user));
    }
    if (result.org) {
      localStorage.setItem('mini_auth_org', JSON.stringify(result.org));
    }
    if (result.token) {
      localStorage.setItem('mini_jwt_token', result.token);
      syncBackendAuthSession(result.token, false);
    }
    localStorage.removeItem('mini_guest_mode');
    setIsGuestMode(false);
    setCurrentUser(result.user || null);
    setAuthNotice(null);
    navigateTo('dashboard');
  };

  if (authChecked && !currentUser && !isGuestMode) {
    if (window.location.hash.includes('accept-invite')) {
      return (
        <main className="main-content" id="main-content" tabIndex={-1}>
          <div className="page-container">
            <AcceptInvitePage
              token={inviteToken}
              onSuccess={completeInviteSignIn}
            />
          </div>
        </main>
      );
    }
    return (
      <>
        {authNotice && (
          <div
            className="alert alert-error"
            role="alert"
            style={{ position: 'fixed', top: 16, left: '50%', transform: 'translateX(-50%)', zIndex: 2000, maxWidth: 440, boxShadow: 'var(--shadow-md)' }}
          >
            {authNotice}
          </div>
        )}
        <AuthPage
          backendUnavailable={backendUnavailable}
          onRetryConnection={retryBackendConnection}
          onAuthSuccess={(user, org, token) => {
            if (user) {
              localStorage.setItem('mini_auth_user', JSON.stringify(user));
            }
            if (org) {
              localStorage.setItem('mini_auth_org', JSON.stringify(org));
            }
            if (token) {
              localStorage.setItem('mini_jwt_token', token);
              syncBackendAuthSession(token, false);
            }
            setAuthNotice(null);
            setCurrentUser(user);
          }}
          onSkip={() => {
            localStorage.setItem('mini_guest_mode', 'true');
            syncBackendAuthSession('', true);
            setAuthNotice(null);
            setIsGuestMode(true);
          }}
        />
      </>
    );
  }

  return (
    <div className={`app-shell ${isMobileMenuOpen ? 'mobile-nav-active' : ''}`}>
      <a href="#main-content" className="skip-link" style={{ position: 'absolute' }}>
        Skip to content
      </a>

      {/* Modular Sidebar Component */}
      <Sidebar
        page={page}
        isMobileMenuOpen={isMobileMenuOpen}
        setIsMobileMenuOpen={setIsMobileMenuOpen}
        currentUser={currentUser}
        isGuestMode={isGuestMode}
        setIsGuestMode={setIsGuestMode}
        elapsedSeconds={elapsedSeconds}
        formatTimer={formatTimer}
        isTrackingActive={isTrackingActive}
        handleToggleTracking={handleToggleTracking}
        trackerError={trackerError}
        userTask={userTask}
        onOpenTaskModal={() => setShowTaskModal(true)}
        isSyncing={isSyncing}
        syncMessage={syncMessage}
        handleSyncNow={handleSyncNow}
        navItems={navItems}
        navigateTo={navigateTo}
        handleLogout={handleLogout}
        dev={dev}
        aiKeyPresent={aiKeyPresent}
        aiConfigured={config?.ai_configured}
      />

      {/* Main content area */}
      <main className="main-content" id="main-content" tabIndex={-1} ref={mainRef}>
        <div className="page-container">
          {loadError && (
            <Alert
              type="error"
              message={loadError}
              onRetry={retryLoad}
              style={{ marginBottom: 16 }}
            />
          )}
          {page === 'dashboard' && (
            <Dashboard
              logs={logs}
              stats={stats}
              config={config}
              loading={loading}
              today={today}
              onRefresh={() => loadData(today)}
              loadError={loadError}
              onRetry={retryLoad}
            />
          )}
          {page === 'timeline' && (
            <Timeline
              logs={logs}
              loading={loading}
              today={today}
              onDateChange={(d) => {
                setToday(d);
                setStartDate(d);
                setEndDate(d);
                loadData(d);
              }}
              loadError={loadError}
              onRetry={retryLoad}
            />
          )}
          {page === 'analytics' && (
            <Analytics
              logs={logs}
              stats={stats}
              config={config}
              loading={loading}
              today={today}
              onDateChange={(d) => { setToday(d); loadData(d, selectedUserId, d, d); }}
              currentUser={currentUser}
              selectedUserId={selectedUserId}
              startDate={startDate}
              endDate={endDate}
              onUserChange={(uid) => {
                setSelectedUserId(uid);
                loadData(today, uid, startDate, endDate);
              }}
              onDateRangeChange={(s, e) => {
                setStartDate(s);
                setEndDate(e);
                loadData(today, selectedUserId, s, e);
              }}
              loadError={loadError}
              onRetry={retryLoad}
            />
          )}
          {page === 'organization' && (currentUser?.role === 'admin' || currentUser?.role === 'owner') ? (
            <OrganizationPage />
          ) : page === 'organization' ? (
            <EmptyState
              title="Organization settings unavailable"
              description={
                isGuestMode
                  ? 'Organization and team management are available when logged in with an organization account.'
                  : "Your account doesn't have permission to view this page. Contact your organization admin if you believe this is a mistake."
              }
              action={
                <button type="button" className="btn btn-primary btn-sm" onClick={() => navigateTo('dashboard')}>
                  Back to Dashboard
                </button>
              }
            />
          ) : null}
          {page === 'settings' && (currentUser?.role === 'admin' || currentUser?.role === 'owner' || isGuestMode) ? (
            <SettingsPage
              theme={theme}
              onThemeChange={(newTheme) => setTheme(newTheme)}
              syncIntervalSeconds={config?.screenshot_interval_seconds}
            />
          ) : page === 'settings' ? (
            <EmptyState
              title="You don't have access"
              description="Your account doesn't have permission to view this page. Contact your organization admin if you believe this is a mistake."
              action={
                <button type="button" className="btn btn-primary btn-sm" onClick={() => navigateTo('dashboard')}>
                  Back to Dashboard
                </button>
              }
            />
          ) : null}
          {page === 'accept-invite' && (
            <AcceptInvitePage
              token={inviteToken}
              onSuccess={completeInviteSignIn}
            />
          )}
        </div>
      </main>

      {/* Target Task Goal Modal Dialog */}
      <TaskGoalModal
        isOpen={showTaskModal}
        onClose={() => setShowTaskModal(false)}
        currentTask={userTask}
        onSave={(task) => saveTask(task)}
      />

      {/* Dev mode tap-progress toast */}
      {devToastVisible && dev.tapProgress >= 4 && dev.tapProgress < 7 && (
        <div className="dev-toast" role="status" aria-live="polite">
          Dev mode: {dev.tapProgress}/7
        </div>
      )}

      {/* Developer Menu */}
      {dev.enabled && (
        <DevMenu
          isOpen={dev.open}
          onClose={() => dev.setOpen(false)}
          theme={theme}
          onThemeChange={setTheme}
          onDisable={dev.disable}
        />
      )}
    </div>
  );
}
