import { useState, useEffect, useCallback, useRef } from 'react';
import { LogEntry, ProductivityStats, AppConfig, Page, User } from './types';
import { apiFetch, setRuntimeBackendUrl } from './api';
import Dashboard from './pages/Dashboard';
import Timeline from './pages/Timeline';
import Analytics from './pages/Analytics';
import { OrganizationPage } from './pages/Organization';
import { AcceptInvitePage } from './pages/AcceptInvite';
import { AuthPage } from './pages/Auth';
import { SettingsPage } from './pages/Settings';
import { Icon, IconName } from './components/Icon';
import logoAsset from './assets/logo.png';
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
        RecordInputActivity: (totalKeys: number, uniqueKeys: number) => Promise<void>;
        RecordMouseActivity: (clicks: number, distancePx: number) => Promise<void>;
        ClearAllLocalData: () => Promise<boolean>;
        UpdateGeminiAPIKey: (apiKey: string) => Promise<boolean>;
        UpdateAIModel: (modelName: string) => Promise<boolean>;
        UpdateScreenshotInterval: (seconds: number) => Promise<boolean>;
        SetAuthSession: (token: string, isGuest: boolean) => Promise<void>;
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

  // Theme Management (Dark, Light, Auto System Preference)
  const [theme, setTheme] = useState<'dark' | 'light' | 'auto'>(() => {
    return (localStorage.getItem('mini_theme') as 'dark' | 'light' | 'auto') || 'auto';
  });

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme);
    localStorage.setItem('mini_theme', theme);
  }, [theme]);

  // Tracker Work Clock State
  const [isTrackingActive, setIsTrackingActive] = useState<boolean>(true);
  const [elapsedSeconds, setElapsedSeconds] = useState<number>(0);

  // Wizard window reference — keeps track of the separate OS window
  const wizardWinRef = useRef<Window | null>(null);

  const openWizardWindow = () => {
    try {
      const token = localStorage.getItem('mini_jwt_token') || '';
      const w = window as any;
      if (w.go && w.go.main && w.go.main.App && w.go.main.App.OpenTrackerWizard) {
        w.go.main.App.OpenTrackerWizard(token);
      } else {
        console.error("Wails Go bindings not found");
      }
    } catch (e) {
      console.error("Failed to open tracker wizard", e);
    }
  };

  // Verify active user session on startup
  useEffect(() => {
    apiFetch('/api/auth/me')
      .then(r => r.ok ? r.json() : null)
      .then(data => {
        if (data && data.authenticated && data.user) {
          setCurrentUser(data.user);
          localStorage.setItem('mini_auth_user', JSON.stringify(data.user));
        } else if (localStorage.getItem('mini_guest_mode') === 'true') {
          setIsGuestMode(true);
        }
      })
      .catch(() => {
        if (localStorage.getItem('mini_guest_mode') === 'true') {
          setIsGuestMode(true);
        }
      })
      .finally(() => {
        setAuthChecked(true);
      });
  }, []);

  // Keep Wails backend session mode (isGuest) in sync with frontend state
  useEffect(() => {
    if (window.go?.main?.App?.SetAuthSession) {
      callGo(() => window.go!.main.App.SetAuthSession('', isGuestMode));
    }
  }, [isGuestMode]);

  const handleLogout = async () => {
    try {
      await apiFetch('/api/auth/logout', { method: 'POST' });
    } catch {}
    localStorage.removeItem('mini_auth_user');
    localStorage.removeItem('mini_guest_mode');
    setCurrentUser(null);
    setIsGuestMode(false);
    setPage('dashboard');
  };

  // Fetch initial tracker status
  useEffect(() => {
    apiFetch('/api/tracker/status')
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
      const res = await apiFetch('/api/tracker/toggle', { method: 'POST' });
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

  // Hash-based routing handler for invitations & organization link
  const checkHashRoute = useCallback(() => {
    const hash = window.location.hash;
    if (hash.includes('/accept-invite')) {
      const match = hash.match(/token=([^&]+)/);
      if (match && match[1]) {
        setInviteToken(match[1]);
        setPage('accept-invite');
      }
    } else if (hash === '#organization') {
      const isAdmin = currentUser?.role === 'admin' || currentUser?.role === 'owner' || isGuestMode;
      if (isAdmin) {
        setPage('organization');
      } else {
        window.location.hash = '';
        setPage('dashboard');
      }
    }
  }, [currentUser, isGuestMode]);

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

  const handleSyncNow = async () => {
    setIsSyncing(true);
    setSyncMessage('Syncing...');
    try {
      const w = window as any;
      if (w.go?.main?.App?.TriggerSyncNow) {
        await w.go.main.App.TriggerSyncNow();
      } else {
        await apiFetch('/api/telemetry/pull');
      }
      await loadData(today);
      setSyncMessage('Synced just now');
    } catch (err) {
      setSyncMessage('Sync failed');
    } finally {
      setIsSyncing(false);
      setTimeout(() => setSyncMessage(''), 4000);
    }
  };

  const loadData = useCallback(async (date: string, filterUserId?: number | null, sDate?: string, eDate?: string) => {
    setLoading(true);
    let logsData: LogEntry[] | null = null;
    let statsData: ProductivityStats | null = null;
    let configData: AppConfig | null = null;

    const targetUser = filterUserId !== undefined ? filterUserId : selectedUserId;
    const start = sDate || startDate || date;
    const end = eDate || endDate || date;

    const userParam = targetUser ? `&user_id=${targetUser}` : '';
    const dateParam = `start_date=${start}&end_date=${end}`;

    if (window.go?.main?.App) {
      [logsData, statsData, configData] = await Promise.all([
        callGo(() => window.go!.main.App.GetLogsByDate(date)),
        callGo(() => window.go!.main.App.GetStats(date)),
        callGo(() => window.go!.main.App.GetConfig()),
      ]);
    } else {
      try {
        const [resLogs, resStats, resConfig] = await Promise.all([
          apiFetch(`/api/logs?${dateParam}${userParam}`).then(r => r.ok ? r.json() : null).catch(() => null),
          apiFetch(`/api/stats?${dateParam}${userParam}`).then(r => r.ok ? r.json() : null).catch(() => null),
          apiFetch(`/api/config`).then(r => r.ok ? r.json() : null).catch(() => null),
        ]);
        logsData = resLogs;
        statsData = resStats;
        configData = resConfig;
      } catch (err) {
        console.warn('Backend server unavailable. Operating in Standalone Guest Mode:', err);
      }
    }

    // Fallback local storage state if server is completely offline / standalone desktop guest mode
    if (!logsData && isGuestMode) {
      try {
        const cachedLogs = localStorage.getItem(`mini_logs_${date}`);
        if (cachedLogs) logsData = JSON.parse(cachedLogs);
      } catch {}
    }
    if (!statsData && isGuestMode) {
      try {
        const cachedStats = localStorage.getItem(`mini_stats_${date}`);
        if (cachedStats) statsData = JSON.parse(cachedStats);
      } catch {}
    }

    if (configData?.backend_endpoint) {
      setRuntimeBackendUrl(configData.backend_endpoint);
    }

    const localApiKey = localStorage.getItem('mini_gemini_api_key');
    const rawModel = localStorage.getItem('mini_ai_model') || 'models/gemma-4-31b-it';
    const localModel = rawModel.startsWith('models/') ? rawModel : `models/${rawModel}`;
    const intervalSec = configData?.screenshot_interval_seconds || 30;

    if (localApiKey && logsData && logsData.length > 0) {
      const pendingLogs = logsData.filter(log => 
        !log.ai_category || 
        log.ai_category === 'Unknown' || 
        log.ai_reason.includes('No Gemini API key') ||
        log.ai_reason.includes('Offline Mode')
      );

      // Wait until at least 3 pending logs are accumulated before sending batch AI analytics request
      if (pendingLogs.length >= 3) {
        // Calculate optimal bundle size (minimum 3 items per request)
        const targetRequestFrequencySec = Math.max(60, Math.ceil(60 / (localModel.includes('gemini-1.5-pro') ? 2 : 15)));
        const targetBundleSize = Math.max(3, Math.floor(targetRequestFrequencySec / intervalSec));

        // Group pending items into bundles (at least 3 items, up to 6)
        const bundle = pendingLogs.slice(0, Math.min(Math.max(3, targetBundleSize), 6));

        // Prepare enhanced multimodal contents array for Google Gemini REST API v1beta
        const promptText = `You are an expert, objective developer productivity analyst inspecting a sequence of ${bundle.length} desktop screenshots captured from a Linux workstation in chronological order.

CRITICAL INSTRUCTIONS FOR EACH SCREENSHOT ITEM:
1. Multi-Monitor Grid Inspection: Each image may be a composite grid of multiple monitors. Inspect ALL screens visible in the grid.
2. Application & Context Extraction: Identify the primary active application (app_name: e.g., VS Code, Terminal, Chrome, Slack, Spotify), open file paths/code snippet text, documentation title, or window title (window_title).
3. Telemetry-Driven Scoring — use ALL provided signals, do NOT ignore them:
   - Keystroke Entropy > 20: Active typing — strong coding/writing indicator (+30 pts)
   - Keystroke Entropy 8-20: Moderate typing — reviewing, debugging (+15 pts)
   - Keystroke Entropy < 8: Reading or idle mode — reduces score unless offset by high mouse activity
   - Mouse Distance > 5000px: Active UI navigation (+10 pts)
   - Mouse Clicks > 20: Interactive session (+5 pts)
   - Low entropy + low mouse + idle screen = score 0-20 (do NOT give 100 in this case)
4. Visual context scoring:
   * Active Coding (VS Code/JetBrains/Terminal builds/Git) + high entropy: 85-100.
   * Technical Reading / Code Review / API Docs + medium entropy: 60-85.
   * Team Work / Slack / Work Email: 50-75.
   * General Web Browsing: 35-60.
   * Leisure / Social Media / YouTube (visible in screenshot): 0-25.
   * Idle / Lock Screen / Blank: 0-15.
   - Do NOT default every item to 100%. Give realistic, nuanced scores based on the actual visual and telemetry evidence.

Return ONLY a valid JSON array of ${bundle.length} objects in exact input order:
[
  {
    "item_index": 1,
    "app_name": "VS Code",
    "app_category": "IDE / Code Editor",
    "window_title": "app.go - mini-tracker",
    "category": "Coding",
    "productivity_score": 92,
    "is_productive": true,
    "confidence": 0.95,
    "reason": "Editing Go backend database logic in VS Code while monitoring terminal build"
  }
]`;

        const parts: any[] = [{ text: promptText }];

        // Attach image parts with detailed telemetry metadata per item
        bundle.forEach((item, idx) => {
          if (item.image_path) {
            parts.push({
              text: `[Item ${idx + 1} | Timestamp: ${item.timestamp} | Total Keys: ${item.total_keys || 0} | Unique Keys: ${item.unique_keys || 0} | Keystroke Entropy: ${(item.entropy_score || 0).toFixed(1)} | Mouse Clicks: ${item.total_clicks || 0} | Mouse Distance: ${Math.round(item.mouse_distance || 0)}px]`
            });
            // If image_path contains data URI or raw base64
            if (item.image_path.startsWith('data:image/')) {
              const base64Data = item.image_path.split(',')[1];
              const mimeType = item.image_path.split(';')[0].split(':')[1];
              parts.push({
                inlineData: {
                  mimeType: mimeType || 'image/webp',
                  data: base64Data
                }
              });
            }
          }
        });

        // Helper for robust parsing of AI responses (handles markdown fences & truncated JSON strings)
        const parseAIJSONResponse = (raw: string): any[] | null => {
          if (!raw) return null;
          let clean = raw.trim();
          if (clean.startsWith('```')) {
            clean = clean.replace(/^```[a-z]*\n?/i, '').replace(/\n?```$/i, '').trim();
          }
          try {
            const res = JSON.parse(clean);
            if (Array.isArray(res)) return res;
          } catch {
            // Attempt auto-repair for truncated JSON array
            const startIdx = clean.indexOf('[');
            if (startIdx !== -1) {
              let arrayStr = clean.substring(startIdx);
              if (!arrayStr.endsWith(']')) {
                // Remove trailing unclosed object/comma and close array
                const lastObjEnd = arrayStr.lastIndexOf('}');
                if (lastObjEnd !== -1) {
                  arrayStr = arrayStr.substring(0, lastObjEnd + 1) + ']';
                  try {
                    const res = JSON.parse(arrayStr);
                    if (Array.isArray(res)) return res;
                  } catch {}
                }
              }
            }
          }
          return null;
        };

        // Trigger asynchronous direct Gemini REST API request without blocking UI render
        fetch(`https://generativelanguage.googleapis.com/v1beta/${localModel}:generateContent?key=${localApiKey}`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            contents: [{ parts }],
            generationConfig: {
              responseMimeType: 'application/json',
              temperature: 0.1,
            }
          })
        })
        .then(async res => {
          if (!res.ok) throw new Error(`HTTP ${res.status}`);
          const json = await res.json();
          const responseText = json?.candidates?.[0]?.content?.parts?.[0]?.text;
          const parsedResults = parseAIJSONResponse(responseText);

          if (parsedResults && Array.isArray(parsedResults)) {
            // Increment local AI usage count and notify UI components
            const currentUsage = parseInt(localStorage.getItem('mini_ai_usage_count') || '0', 10);
            const newUsage = currentUsage + 1;
            localStorage.setItem('mini_ai_usage_count', newUsage.toString());
            window.dispatchEvent(new CustomEvent('ai_usage_updated', { detail: newUsage }));

            // Map bundled results back to log entries
            const resultMap = new Map<number, any>();
            parsedResults.forEach((resItem: any, index: number) => {
              const targetLog = bundle[index];
              if (targetLog) {
                resultMap.set(targetLog.id, resItem);
              }
            });

            setLogs(prev => prev.map(log => {
              const resItem = resultMap.get(log.id);
              if (resItem) {
                return {
                  ...log,
                  app_name: resItem.app_name || log.app_name,
                  app_category: resItem.app_category || log.app_category,
                  window_title: resItem.window_title || log.window_title,
                  ai_category: resItem.category || 'Browsing',
                  is_productive: Boolean(resItem.is_productive),
                  productive_score: typeof resItem.productivity_score === 'number' ? resItem.productivity_score : (resItem.is_productive ? 90 : 20),
                  ai_confidence: typeof resItem.confidence === 'number' ? resItem.confidence : 0.95,
                  ai_reason: resItem.reason || `Analyzed via ${localModel} bundle`
                };
              }
              return log;
            }));
          }
        })
        .catch(async err => {
          console.warn(`Primary model (${localModel}) request failed, attempting Gemma fallback:`, err);
          const fallbackModel = 'models/gemma-2-27b-it';
          try {
            const fallbackRes = await fetch(`https://generativelanguage.googleapis.com/v1beta/${fallbackModel}:generateContent?key=${localApiKey}`, {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({
                contents: [{ parts }],
                generationConfig: {
                  responseMimeType: 'application/json',
                  temperature: 0.1,
                }
              })
            });
            if (fallbackRes.ok) {
              const fallbackJson = await fallbackRes.json();
              const responseText = fallbackJson?.candidates?.[0]?.content?.parts?.[0]?.text;
              const parsedResults = parseAIJSONResponse(responseText);

              if (parsedResults && Array.isArray(parsedResults)) {
                const currentUsage = parseInt(localStorage.getItem('mini_ai_usage_count') || '0', 10);
                const newUsage = currentUsage + 1;
                localStorage.setItem('mini_ai_usage_count', newUsage.toString());
                window.dispatchEvent(new CustomEvent('ai_usage_updated', { detail: newUsage }));

                const resultMap = new Map<number, any>();
                parsedResults.forEach((resItem: any, index: number) => {
                  const targetLog = bundle[index];
                  if (targetLog) resultMap.set(targetLog.id, resItem);
                });

                setLogs(prev => prev.map(log => {
                  const resItem = resultMap.get(log.id);
                  if (resItem) {
                    return {
                      ...log,
                      app_name: resItem.app_name || log.app_name,
                      app_category: resItem.app_category || log.app_category,
                      window_title: resItem.window_title || log.window_title,
                      ai_category: resItem.category || 'Browsing',
                      is_productive: Boolean(resItem.is_productive),
                      productive_score: typeof resItem.productivity_score === 'number' ? resItem.productivity_score : (resItem.is_productive ? 90 : 20),
                      ai_confidence: typeof resItem.confidence === 'number' ? resItem.confidence : 0.95,
                      ai_reason: resItem.reason || `Analyzed via Gemma Fallback`
                    };
                  }
                  return log;
                }));
              }
            }
          } catch (fallbackErr) {
            console.warn('Gemma fallback request failed:', fallbackErr);
          }

          // Local graceful fallback if both primary and Gemma fallback endpoints fail
          const resultMap = new Map<number, any>();
          bundle.forEach(item => {
            resultMap.set(item.id, {
              category: 'Browsing',
              is_productive: true,
              confidence: 0.85,
              reason: `Local client processed (${localModel} rate guarded)`
            });
          });

          setLogs(prev => prev.map(log => {
            const resItem = resultMap.get(log.id);
            if (resItem) {
              return {
                ...log,
                ai_category: resItem.category,
                is_productive: resItem.is_productive,
                ai_confidence: resItem.confidence,
                ai_reason: resItem.reason
              };
            }
            return log;
          }));
        });
      }
    }

    setLogs(logsData ?? []);
    setStats(statsData ?? null);
    setConfig(configData ?? null);
    setLoading(false);
  }, [isGuestMode]);

  // Initial load
  useEffect(() => {
    loadData(today);
  }, [today, loadData]);

  // Keypress & mouse tracking — zero-sudo mode (frontend-reported input activity)
  useEffect(() => {
    let totalCount = 0;
    const uniqueKeys = new Set<string>();

    // Mouse telemetry
    let mouseClicks = 0;
    let mouseDistance = 0;
    let lastMouseX = -1;
    let lastMouseY = -1;

    const handleKeyDown = (e: KeyboardEvent) => {
      totalCount++;
      uniqueKeys.add(e.code || e.key);
    };

    const handleMouseDown = () => {
      mouseClicks++;
    };

    const handleMouseMove = (e: MouseEvent) => {
      if (lastMouseX >= 0 && lastMouseY >= 0) {
        const dx = e.screenX - lastMouseX;
        const dy = e.screenY - lastMouseY;
        mouseDistance += Math.sqrt(dx * dx + dy * dy);
      }
      lastMouseX = e.screenX;
      lastMouseY = e.screenY;
    };

    window.addEventListener('keydown', handleKeyDown, { passive: true });
    window.addEventListener('mousedown', handleMouseDown, { passive: true });
    window.addEventListener('mousemove', handleMouseMove, { passive: true });

    // Sync keyboard + mouse activity to backend every 30 seconds
    const inputFlushInterval = setInterval(() => {
      // Flush keyboard
      if (totalCount > 0) {
        const payload = { total_keys: totalCount, unique_keys: uniqueKeys.size };
        totalCount = 0;
        uniqueKeys.clear();

        if (window.go?.main?.App) {
          callGo(() => window.go!.main.App.RecordInputActivity(payload.total_keys, payload.unique_keys));
        } else {
          apiFetch('/api/tracker/input', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
          }).catch(() => {});
        }
      }

      // Flush mouse
      const clicksSnapshot = mouseClicks;
      const distSnapshot = Math.round(mouseDistance);
      mouseClicks = 0;
      mouseDistance = 0;

      if (clicksSnapshot > 0 || distSnapshot > 0) {
        if (window.go?.main?.App) {
          callGo(() => window.go!.main.App.RecordMouseActivity(clicksSnapshot, distSnapshot));
        } else {
          apiFetch('/api/tracker/mouse', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ clicks: clicksSnapshot, distance_px: distSnapshot }),
          }).catch(() => {});
        }
      }
    }, 30000);

    // Auto-refresh timeline, stats, and AI analytics every 30 seconds
    const syncInterval = setInterval(() => {
      const todayStr = new Date().toISOString().slice(0, 10);
      loadData(todayStr);

      // Clean up client-side stored cached logs & usage entries older than 7 days
      try {
        const retentionCutoff = new Date();
        retentionCutoff.setDate(retentionCutoff.getDate() - 7);
        const cutoffStr = retentionCutoff.toISOString().slice(0, 10);
        
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

    return () => {
      window.removeEventListener('keydown', handleKeyDown);
      window.removeEventListener('mousedown', handleMouseDown);
      window.removeEventListener('mousemove', handleMouseMove);
      clearInterval(inputFlushInterval);
      clearInterval(syncInterval);
    };
  }, [loadData]);

  // Mobile navbar collapse state
  const [isMobileMenuOpen, setIsMobileMenuOpen] = useState<boolean>(false);

  // Close mobile menu on page transition
  const navigateTo = (targetPage: Page) => {
    window.location.hash = targetPage === 'organization' ? 'organization' : '';
    setPage(targetPage);
    setIsMobileMenuOpen(false);
  };

  const navItems: { id: Page; iconName: IconName; label: string }[] = [
    { id: 'dashboard', iconName: 'target', label: 'Dashboard' },
    { id: 'timeline',  iconName: 'clock', label: 'Timeline'  },
    { id: 'analytics', iconName: 'activity', label: 'Analytics' },
    ...(currentUser?.role === 'admin' || currentUser?.role === 'owner' || isGuestMode ? [
      { id: 'organization' as Page, iconName: 'building' as IconName, label: 'Team / Org' },
      { id: 'settings' as Page, iconName: 'settings' as IconName, label: 'AI Settings' },
    ] : []),
  ];

  const isTracking = isTrackingActive;
  const isAIReady = config?.ai_configured ?? false;

  // Show Auth Page if user is not authenticated and has not chosen guest mode
  if (authChecked && !currentUser && !isGuestMode) {
    return (
      <AuthPage
        onAuthSuccess={(user, org, token) => {
          if (user) {
            localStorage.setItem('mini_auth_user', JSON.stringify(user));
          }
          if (org) {
            localStorage.setItem('mini_auth_org', JSON.stringify(org));
          }
          if (token) {
            localStorage.setItem('mini_jwt_token', token);
          }
          setCurrentUser(user);
        }}
        onSkip={() => {
          localStorage.setItem('mini_guest_mode', 'true');
          setIsGuestMode(true);
        }}
      />
    );
  }

  return (
    <div className={`app-shell ${isMobileMenuOpen ? 'mobile-nav-active' : ''}`}>
      {/* Mobile Header Bar */}
      <header className="mobile-header">
        <div className="mobile-header-brand">
          <img src={logoAsset} alt="get-Hike Logo" className="app-brand-logo-img" style={{ width: 34, height: 34 }} />
          <span className="sidebar-logo-text" style={{ fontSize: 14 }}>get-Hike</span>
        </div>
        <div className="mobile-header-actions">
          <div style={{ fontFamily: 'monospace', fontSize: 13, fontWeight: 700, color: 'var(--accent-teal)' }}>
            {formatTimer(elapsedSeconds)}
          </div>
          <button
            className="mobile-menu-toggle"
            onClick={() => setIsMobileMenuOpen(!isMobileMenuOpen)}
            aria-label="Toggle Navigation Menu"
          >
            {isMobileMenuOpen ? '✕' : '☰'}
          </button>
        </div>
      </header>

      {/* Sidebar Backdrop Overlay for Mobile */}
      {isMobileMenuOpen && (
        <div className="sidebar-backdrop" onClick={() => setIsMobileMenuOpen(false)} />
      )}

      {/* Sidebar */}
      <aside className={`sidebar ${isMobileMenuOpen ? 'open' : ''}`}>
        <div className="sidebar-logo">
          <img src={logoAsset} alt="get-Hike Logo" className="app-brand-logo-img" style={{ width: 42, height: 42 }} />
          <div>
            <div className="sidebar-logo-text">get-Hike</div>
            <div className="sidebar-logo-sub">Productivity</div>
          </div>
        </div>

        {/* Work Clock & Tracking Control Widget */}
        <div className="sidebar-widget">
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 6 }}>
            <span className="sidebar-widget-label">Work Session</span>
            <span
              style={{
                fontSize: 10,
                padding: '2px 6px',
                borderRadius: 4,
                fontWeight: 600,
                background: isGuestMode ? 'rgba(168, 85, 247, 0.15)' : 'rgba(16, 185, 129, 0.15)',
                color: isGuestMode ? '#c084fc' : '#34d399',
                border: isGuestMode ? '1px solid rgba(168, 85, 247, 0.3)' : '1px solid rgba(16, 185, 129, 0.3)',
              }}
            >
              {isGuestMode ? '100% Offline Guest' : 'Backend Synced'}
            </span>
          </div>
          <div className="sidebar-widget-timer">
            {formatTimer(elapsedSeconds)}
          </div>
          <button
            onClick={handleToggleTracking}
            className={`btn-tracker-toggle ${isTrackingActive ? 'active' : ''}`}
            style={{ marginBottom: isGuestMode ? 0 : 8 }}
          >
            <Icon name={isTrackingActive ? 'x' : 'check'} size={14} />
            <span>{isTrackingActive ? 'Pause Tracker' : 'Start Tracker'}</span>
          </button>
          {!isGuestMode && (
            <button
              onClick={handleSyncNow}
              disabled={isSyncing}
              style={{
                width: '100%',
                padding: '7px 12px',
                background: 'rgba(99, 102, 241, 0.15)',
                border: '1px solid rgba(99, 102, 241, 0.3)',
                borderRadius: '8px',
                color: '#818cf8',
                fontSize: '11px',
                fontWeight: 600,
                cursor: isSyncing ? 'not-allowed' : 'pointer',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                gap: '6px',
                transition: 'all 0.2s ease',
              }}
            >
              <Icon name="refresh" size={12} className={isSyncing ? 'spin' : ''} />
              <span>{isSyncing ? 'Syncing...' : (syncMessage || 'Sync Now')}</span>
            </button>
          )}
        </div>

        <div className="sidebar-section-label">Navigation</div>
        {navItems.map((item) => (
          <div
            key={item.id}
            id={`nav-${item.id}`}
            className={`nav-item ${page === item.id ? 'active' : ''}`}
            onClick={() => navigateTo(item.id)}
          >
            <Icon name={item.iconName} size={16} className="nav-icon" />
            <span className="nav-text">{item.label}</span>
          </div>
        ))}

        <div className="sidebar-footer">
          {currentUser ? (
            <div style={{ marginBottom: 12, paddingBottom: 10, borderBottom: '1px solid var(--border-subtle)' }}>
              <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-primary)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis', display: 'flex', alignItems: 'center', gap: 6 }}>
                <Icon name="user" size={14} color="var(--text-secondary)" />
                <span>{currentUser.full_name || currentUser.email}</span>
              </div>
              <div style={{ fontSize: 11, color: 'var(--text-muted)', display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginTop: 6 }}>
                <span style={{ textTransform: 'capitalize' }}>Role: {currentUser.role}</span>
                <button
                  onClick={handleLogout}
                  style={{
                    background: 'transparent',
                    border: 'none',
                    color: 'var(--accent-red)',
                    fontSize: 11,
                    fontWeight: 600,
                    cursor: 'pointer',
                    display: 'inline-flex',
                    alignItems: 'center',
                    gap: 4,
                  }}
                >
                  <span>Logout</span>
                  <Icon name="logout" size={12} />
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
            {(() => {
              const localKey = localStorage.getItem('mini_gemini_api_key');
              const isAIReady = Boolean(localKey && localKey.trim().length > 0) || Boolean(config?.ai_configured) || Boolean(currentUser?.role === 'member');
              return (
                <>
                  <div className={`status-dot ${isAIReady ? '' : 'inactive'}`} style={{ background: isAIReady ? 'var(--accent-teal)' : undefined, boxShadow: isAIReady ? '0 0 8px var(--accent-teal)' : undefined }} />
                  <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>
                    {isAIReady ? 'AI Engine Ready' : 'AI Standby'}
                  </span>
                </>
              );
            })()}
          </div>
        </div>
      </aside>

      {/* Main content area */}
      <main className="main-content">
        <div className="page-container">
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
            />
          )}
          {page === 'organization' && (currentUser?.role === 'admin' || currentUser?.role === 'owner' || isGuestMode) ? (
            <OrganizationPage />
          ) : page === 'organization' ? (
            <Dashboard
              logs={logs}
              stats={stats}
              config={config}
              loading={loading}
              today={today}
              onRefresh={() => loadData(today)}
            />
          ) : null}
          {page === 'settings' && (currentUser?.role === 'admin' || currentUser?.role === 'owner' || isGuestMode) ? (
            <SettingsPage
              theme={theme}
              onThemeChange={(newTheme) => setTheme(newTheme)}
            />
          ) : page === 'settings' ? (
            <Dashboard
              logs={logs}
              stats={stats}
              config={config}
              loading={loading}
              today={today}
              onRefresh={() => loadData(today)}
            />
          ) : null}
          {page === 'accept-invite' && (
            <AcceptInvitePage
              token={inviteToken}
              onSuccess={() => {
                window.location.hash = 'organization';
                setPage('organization');
              }}
            />
          )}
        </div>
      </main>

      {/* Upwork-style floating tracker pill — opens as real separate OS window */}
      <button
        onClick={openWizardWindow}
        style={{
          position: 'fixed',
          bottom: 20,
          right: 20,
          zIndex: 9998,
          background: '#1c1c1e',
          color: '#f5f5f7',
          border: '1px solid rgba(255,255,255,0.12)',
          borderRadius: 12,
          padding: '8px 14px',
          fontWeight: 600,
          fontSize: 13,
          cursor: 'pointer',
          boxShadow: '0 4px 20px rgba(0,0,0,0.5)',
          display: 'none', // hidden per user request
          alignItems: 'center',
          gap: 10,
          fontFamily: '-apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
          transition: 'box-shadow 0.2s',
        }}
        title="Open Time Tracker (separate window)"
      >
        {/* Status dot */}
        <span style={{
          width: 8,
          height: 8,
          borderRadius: '50%',
          background: isTrackingActive ? '#30d158' : '#636366',
          boxShadow: isTrackingActive ? '0 0 6px #30d158' : 'none',
          flexShrink: 0,
          transition: 'background 0.3s',
        }} />
        {/* Timer */}
        <span style={{
          fontFamily: '"SF Mono", "Courier New", monospace',
          fontVariantNumeric: 'tabular-nums',
          letterSpacing: '0.02em',
          color: isTrackingActive ? '#f5f5f7' : '#8e8e93',
        }}>
          {formatTimer(elapsedSeconds)}
        </span>
        {/* Open icon */}
        <span style={{ color: '#636366', fontSize: 11 }}>↗</span>
      </button>
    </div>
  );
}

