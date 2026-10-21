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
import { localISODate, isPendingLog, bumpUsageCount } from './utils';
import { EventsOn } from '../wailsjs/runtime/runtime';
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
        GetTodayTrackedSeconds: () => Promise<number>;
        GetTrackingStatus: () => Promise<{ active?: boolean; elapsed_seconds?: number } | null>;
        ToggleTracking: () => Promise<{ active?: boolean; elapsed_seconds?: number } | null>;
        SetActiveTask: (task: string) => Promise<string>;
        GetActiveTask: () => Promise<string>;
        RecordInputActivity: (totalKeys: number, uniqueKeys: number) => Promise<void>;
        RecordMouseActivity: (clicks: number) => Promise<void>;
        ClearAllLocalData: () => Promise<boolean>;
        GetImageBase64: (imagePath: string) => Promise<string>;
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
    return fn().catch((err) => {
      console.warn('[Wails] Desktop binding call failed:', err);
      return null;
    });
  } catch (err) {
    console.warn('[Wails] Synchronous desktop binding exception:', err);
    return Promise.resolve(null);
  }
}

const AUTH_STORAGE_KEYS = ['mini_auth_user', 'mini_guest_mode', 'mini_jwt_token', 'mini_auth_org'];

function clearStoredAuth() {
  AUTH_STORAGE_KEYS.forEach((key) => localStorage.removeItem(key));
}

function syncBackendAuthSession(token: string, isGuest: boolean) {
  if (window.go?.main?.App?.SetAuthSession) {
    callGo(() => window.go!.main.App.SetAuthSession(token, isGuest));
  }
}

// Robust parsing of AI responses (handles markdown fences & truncated JSON strings)
function parseAIJSONResponse(raw: string): any[] | null {
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
}

// Loads the screenshot at imagePath as a data URL. Wails uses GetImageBase64;
// web mode fetches /api/image and converts the blob via FileReader.
async function loadImageAsDataUrl(imagePath: string): Promise<string | null> {
  if (!imagePath) return null;
  if (window.go?.main?.App?.GetImageBase64) {
    const dataUrl = await callGo(() => window.go!.main.App.GetImageBase64(imagePath));
    return dataUrl && dataUrl.startsWith('data:image/') ? dataUrl : null;
  }
  try {
    const res = await apiFetch(`/api/image?path=${encodeURIComponent(imagePath)}`);
    if (!res.ok) return null;
    const blob = await res.blob();
    return await new Promise<string | null>((resolve) => {
      const reader = new FileReader();
      reader.onload = () => {
        const result = typeof reader.result === 'string' ? reader.result : '';
        resolve(result.startsWith('data:image/') ? result : null);
      };
      reader.onerror = () => resolve(null);
      reader.readAsDataURL(blob);
    });
  } catch {
    return null;
  }
}

interface VisionAnalysisOptions {
  candidateLogs: LogEntry[];
  apiKey: string;
  model: string;
  intervalSec: number;
  userTask: string;
  submittedIds: Set<number>;
  resultCache: Map<number, Partial<LogEntry>>;
  setLogs: (updater: (prev: LogEntry[]) => LogEntry[]) => void;
}

// Sends a real multimodal bundle of pending captures to Gemini. Only values the
// model actually returned are applied; items whose image cannot be loaded are
// skipped and left pending instead of being fabricated.
async function runClientVisionAnalysis(options: VisionAnalysisOptions): Promise<void> {
  const { candidateLogs, apiKey, model, intervalSec, userTask, submittedIds, resultCache, setLogs } = options;

  const targetRequestFrequencySec = Math.max(60, Math.ceil(60 / (model.includes('gemini-2.5-pro') || model.includes('gemini-1.5-pro') ? 2 : 15)));
  const targetBundleSize = Math.max(3, Math.floor(targetRequestFrequencySec / Math.max(5, intervalSec)));
  const bundle = candidateLogs.slice(0, Math.min(Math.max(3, targetBundleSize), 6));

  const loadedItems: { item: LogEntry; text: string; inlineData: { mimeType: string; data: string } }[] = [];
  for (const item of bundle) {
    if (!item.image_path) continue;
    const dataUrl = await loadImageAsDataUrl(item.image_path);
    if (!dataUrl || !dataUrl.includes(',')) continue;
    const mimeType = dataUrl.substring(dataUrl.indexOf(':') + 1, dataUrl.indexOf(';')) || 'image/jpeg';
    loadedItems.push({
      item,
      text: `[Item ${loadedItems.length + 1} | Timestamp: ${item.timestamp} | Window Title: ${item.window_title || 'unknown'} | App Name: ${item.app_name || 'unknown'} | App Category: ${item.app_category || 'unknown'} | Total Keys: ${item.total_keys || 0} | Unique Keys: ${item.unique_keys || 0} | Keystroke Entropy: ${(item.entropy_score || 0).toFixed(1)} | Mouse Clicks: ${item.total_clicks || 0}]`,
      inlineData: { mimeType, data: dataUrl.split(',')[1] },
    });
  }

  // Require real images for the batch — never fabricate missing ones.
  if (loadedItems.length < 3) return;

  const taskGoal = userTask.trim()
    ? `CURRENT TARGET USER TASK (GOAL): "${userTask.trim()}"\nEVALUATION RULE: Analyze how relevant the visible desktop content and user input (typing/mouse) are to achieving THIS SPECIFIC TASK. High scores require direct alignment with this task context.`
    : 'CURRENT TARGET USER TASK: General software development and workstation tasks.';

  const promptText = `You are an expert, objective developer productivity analyst inspecting a sequence of ${loadedItems.length} desktop screenshots captured from a Linux workstation in chronological order.

${taskGoal}

CRITICAL INSTRUCTIONS FOR EACH SCREENSHOT ITEM:
1. Multi-Monitor Grid Inspection: Each image may be a composite grid of multiple monitors. Inspect ALL screens visible in the grid.
2. Application & Context Extraction: Identify the primary active application (app_name: e.g., VS Code, Terminal, Chrome, Slack, Spotify), open file paths/code snippet text, documentation title, or window title (window_title) from the image AND the provided per-item metadata.
3. Telemetry & Task Alignment Scoring — use ALL provided signals dynamically:
   - Direct work on stated task: +40 to +60 pts
   - Related docs / technical research: +30 to +45 pts
   - Unrelated browsing / messaging: +10 to +30 pts
   - Leisure / social media / idle: 0 to 15 pts
   - Keystroke Entropy > 20: Active typing/writing (+25 pts)
   - Keystroke Entropy 8-20: Moderate typing (+15 pts)
   - Keystroke Entropy < 8: Reading/idle mode — lowers score unless offset by heavy mouse-click activity
   - Mouse Clicks > 20: Interactive session (+5 pts)
   - Combined low entropy + few clicks + off-task screen = score 0-20 (NEVER default to 100).

Return ONLY a valid JSON array of ${loadedItems.length} objects in exact input order. Each object must contain exactly these fields:
- "item_index": number — 1-based index matching the input item order.
- "app_name": string — the primary active application actually visible, or an empty string if unreadable.
- "app_category": string — application class (e.g., "IDE / Code Editor"), or an empty string.
- "window_title": string — the visible window title or document name, or an empty string.
- "category": string — productivity category (e.g., "Coding", "Browsing", "Communication"), or an empty string.
- "productivity_score": number — 0-100 computed from the screenshot, telemetry, and task alignment.
- "is_productive": boolean.
- "confidence": number — 0-1.
- "reason": string — concise evidence-based justification.
Base every field strictly on the provided screenshot and metadata. Do NOT invent applications, window titles, or values you cannot verify.`;

  const parts: any[] = [{ text: promptText }];
  loadedItems.forEach(({ text, inlineData }) => {
    parts.push({ text });
    parts.push({ inlineData });
  });

  loadedItems.forEach(({ item }) => submittedIds.add(item.id));

  const applyResults = (parsedResults: any[]): number => {
    const patches = new Map<number, Partial<LogEntry>>();
    parsedResults.forEach((resItem: any, index: number) => {
      if (!resItem || typeof resItem.productivity_score !== 'number') return;
      const declaredIndex = typeof resItem.item_index === 'number' ? resItem.item_index - 1 : index;
      const target = loadedItems[declaredIndex] || loadedItems[index];
      if (!target) return;
      const score = resItem.productivity_score;
      const patch: Partial<LogEntry> = {
        productive_score: score,
        is_productive: typeof resItem.is_productive === 'boolean' ? resItem.is_productive : score >= 50,
      };
      if (resItem.app_name) patch.app_name = resItem.app_name;
      if (resItem.app_category) patch.app_category = resItem.app_category;
      if (resItem.window_title) patch.window_title = resItem.window_title;
      if (resItem.category) patch.ai_category = resItem.category;
      if (typeof resItem.confidence === 'number') patch.ai_confidence = resItem.confidence;
      if (resItem.reason) patch.ai_reason = resItem.reason;
      patches.set(target.item.id, patch);
    });
    if (patches.size === 0) return 0;
    // Keep patches in a session cache so later DB refreshes don't lose them
    patches.forEach((patch, id) => resultCache.set(id, patch));
    setLogs((prev) => prev.map((log) => {
      const patch = patches.get(log.id);
      return patch ? { ...log, ...patch } : log;
    }));

    // Persist to the backend DB in web mode so a refresh or a new tab does not
    // re-analyze the same captures (desktop persists via the Go-side pipeline).
    if (!window.go?.main?.App) {
      patches.forEach((patch, id) => {
        if (!patch.ai_category) return;
        void apiFetch('/api/ai/log-result', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            id,
            category: patch.ai_category,
            app_name: patch.app_name || '',
            app_category: patch.app_category || '',
            window_title: patch.window_title || '',
            is_productive: !!patch.is_productive,
            productive_score: patch.productive_score ?? 0,
            ai_confidence: patch.ai_confidence ?? 0,
            ai_reason: patch.ai_reason || '',
          }),
        }).catch(() => null);
      });
    }
    return patches.size;
  };

  const bumpUsage = () => {
    bumpUsageCount(1);
  };

  const requestModel = async (modelName: string): Promise<number> => {
    const res = await fetch(`https://generativelanguage.googleapis.com/v1beta/${modelName}:generateContent?key=${apiKey}`, {
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
    if (!res.ok) return 0;
    const json = await res.json();
    const parsedResults = parseAIJSONResponse(json?.candidates?.[0]?.content?.parts?.[0]?.text);
    if (!parsedResults) return 0;
    const applied = applyResults(parsedResults);
    if (applied > 0) bumpUsage();
    return applied;
  };

  let appliedCount = 0;
  try {
    appliedCount = await requestModel(model);
  } catch {
    appliedCount = 0;
  }
  if (appliedCount === 0) {
    const fallbackModel = 'models/gemini-2.0-flash';
    if (fallbackModel !== model) {
      try {
        appliedCount = await requestModel(fallbackModel);
      } catch {
        appliedCount = 0;
      }
    }
  }
  // Allow a later refresh to retry only when nothing was applied at all.
  if (appliedCount === 0) {
    loadedItems.forEach(({ item }) => submittedIds.delete(item.id));
  }
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
  const [hasLoaded, setHasLoaded] = useState(false);
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
  const [taskInput, setTaskInput] = useState<string>('');

  const saveTask = (text: string) => {
    const words = text.trim().split(/\s+/).filter(Boolean);
    const limited = words.slice(0, 20).join(' ');
    setUserTask(limited);
    localStorage.setItem('mini_user_task', limited);
    if (window.go?.main?.App?.SetActiveTask) {
      callGo(() => window.go!.main.App.SetActiveTask(limited));
    }
    setShowTaskModal(false);
  };

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
    const checkSession = async () => {
      // Desktop: resolve the configured backend URL before probing auth,
      // otherwise relative requests hit the Wails asset server.
      if ((window as any).go?.main?.App?.GetConfig) {
        try {
          const appConfig = await (window as any).go.main.App.GetConfig();
          if (appConfig?.backend_endpoint) {
            setRuntimeBackendUrl(appConfig.backend_endpoint);
          }
        } catch {
          // Keep the relative URL; the probe below decides reachability.
        }
      }

      try {
        const res = await apiFetch('/api/auth/me');
        const data = res.ok ? await res.json() : null;
        setBackendUnavailable(false);
        if (data && data.authenticated && data.user) {
          setCurrentUser(data.user);
          localStorage.setItem('mini_auth_user', JSON.stringify(data.user));
        } else {
          // Stale cached user: clear it so the auth page renders instead of the app shell
          localStorage.removeItem('mini_auth_user');
          setCurrentUser(null);
          // First run (no stored session): the login screen shows as-is —
          // the user decides between Sign In and Continue as Guest.
          if (localStorage.getItem('mini_guest_mode') === 'true') {
            setIsGuestMode(true);
          }
        }
      } catch {
        // Server unreachable / offline: desktop shows guest-only sign-in.
        if ((window as any).go?.main?.App) {
          setBackendUnavailable(true);
        }
        if (localStorage.getItem('mini_guest_mode') === 'true') {
          setIsGuestMode(true);
          return;
        }
        // Already signed in before: restore the locally stored session so the
        // login screen is skipped while the backend is not reachable.
        try {
          const cached = localStorage.getItem('mini_auth_user');
          if (cached) {
            const parsed = JSON.parse(cached);
            if (parsed?.id) {
              setCurrentUser(parsed);
            }
          }
        } catch {
          // Ignore malformed cached user JSON
        }
      } finally {
        setAuthChecked(true);
      }
    };

    checkSession();
  }, []);

  const retryBackendConnection = useCallback(async (): Promise<boolean> => {
    if ((window as any).go?.main?.App?.GetConfig) {
      try {
        const appConfig = await (window as any).go.main.App.GetConfig();
        if (appConfig?.backend_endpoint) {
          setRuntimeBackendUrl(appConfig.backend_endpoint);
        }
      } catch {
        // Retry below reports the failure.
      }
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

  // Keep the Wails backend session in sync with the real auth state (JWT + guest flag)
  useEffect(() => {
    const token = localStorage.getItem('mini_jwt_token') || '';
    if (isGuestMode) {
      syncBackendAuthSession('', true);
    } else if (token) {
      syncBackendAuthSession(token, false);
    }
  }, [currentUser, isGuestMode]);

  // Clear local auth state when the API layer reports an expired/invalid session
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
    // Always clear locally, even if the server-confirmed logout failed
    clearStoredAuth();
    syncBackendAuthSession('', true);
    setCurrentUser(null);
    setIsGuestMode(false);
    setPage('dashboard');
  };

  // Desktop: background Go-side AI batch runs broadcast usage events. Mirror them
  // into the day-scoped local counter so the Settings usage panel stays live and
  // resets every morning even without a backend endpoint (web mode keeps the
  // server-driven counter via /api/user/usage).
  useEffect(() => {
    if (!(window as any).go?.main?.App) return;
    const off = EventsOn('ai_usage_updated', (payload: any) => {
      const requests = typeof payload?.requests === 'number' && payload.requests > 0 ? payload.requests : 1;
      bumpUsageCount(requests);
    });
    return () => {
      try { (off as unknown as () => void)?.(); } catch { /* noop */ }
    };
  }, []);

  // Fetch initial tracker status & restore accumulated today's duration.
  // Prefer the Wails backend status when running as a desktop app; only use the
  // HTTP endpoint in web mode so the two sources cannot race each other.
  useEffect(() => {    const app = (window as any).go?.main?.App;
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

  // Hash-based and query-param routing handler for invitations & page routes
  const checkHashRoute = useCallback(() => {
    // Check standard query string first (e.g. ?invite=TOKEN or ?token=TOKEN or ?page=accept-invite)
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
    const cleanHash = hash.split('?')[0]; // Strip any trailing query params attached to hash
    const route = cleanHash.replace(/^#\/?/, '');
    if (route === 'dashboard' || route === 'timeline' || route === 'analytics' || route === 'settings' || route === 'organization') {
      setPage(route);
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
    } catch (err) {
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

    // Precedence: explicit range > explicit single date > current state defaults
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

    if (window.go?.main?.App) {
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
        Promise.all(dates.map(d => callGo(() => window.go!.main.App.GetLogsByDate(d)))),
        callGo(() => window.go!.main.App.GetStats(statsDate)),
        callGo(() => window.go!.main.App.GetConfig()),
      ]);
      const fetchedLists = logsLists.filter((list): list is LogEntry[] => Array.isArray(list));
      if (fetchedLists.length === 0) {
        logsData = null;
      } else {
        // Canonical ascending order in every mode; consumers derive their own display order
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

    // A newer load was requested while this one was in flight — ignore stale results
    if (requestId !== requestIdRef.current) return;

    if (logsData === null) {
      setLoadError("Couldn't reach the tracker backend.");
    } else {
      setLoadError(null);
    }

    if (configData?.backend_endpoint) {
      setRuntimeBackendUrl(configData.backend_endpoint);
    }

    // Determine whether AI processing is available. In desktop mode it checks local key;
    // in web mode it uses server-configured AI or personal key without exposing secrets to localStorage.
    const isDesktop = !!window.go?.main?.App;
    const localApiKey = isDesktop ? localStorage.getItem('mini_gemini_api_key') : null;
    const serverAiConfigured = Boolean(configData?.ai_configured);
    setAiKeyPresent(Boolean(localApiKey || serverAiConfigured));
    const rawModel = localStorage.getItem('mini_ai_model') || 'models/gemini-3.5-flash-lite';
    const localModel = rawModel.startsWith('models/') ? rawModel : `models/${rawModel}`;
    const intervalSec = configData?.screenshot_interval_seconds || 30;

    // In web mode, dispatch batch AI requests to server proxy /api/ai/analyze-batch
    // In desktop mode, if local API key is provided, runClientVisionAnalysis
    if (logsData && logsData.length > 0 && (localApiKey || (!isDesktop && serverAiConfigured))) {
      const candidateLogs = logsData
        .filter(isPendingLog)
        .filter((log) => !aiSubmittedLogIdsRef.current.has(log.id));

      if (candidateLogs.length >= 3 && !aiAnalysisInFlightRef.current) {
        aiAnalysisInFlightRef.current = true;
        if (!isDesktop) {
          // Web mode: send log IDs to server proxy
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
                  // Only update state if the user hasn't switched views/dates since the batch started
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

    // Re-apply client-side AI patches for logs the backend still reports as pending
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

  // Initial load
  useEffect(() => {
    loadData(today);
  }, [today, loadData]);

  // Keypress & mouse click tracking — zero-sudo mode (frontend-reported input activity)
  useEffect(() => {
    if (!isTrackingActive) return;

    let totalCount = 0;
    const uniqueKeys = new Set<string>();

    let mouseClicks = 0;

    const handleKeyDown = (e: KeyboardEvent) => {
      totalCount++;
      uniqueKeys.add(e.code || e.key);
    };

    const handleMouseDown = () => {
      mouseClicks++;
    };

    window.addEventListener('keydown', handleKeyDown, { passive: true });
    window.addEventListener('mousedown', handleMouseDown, { passive: true });

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

      // Flush mouse clicks to the active backend (Wails desktop or web API)
      const clicksSnapshot = mouseClicks;
      mouseClicks = 0;

      if (clicksSnapshot > 0) {
        if (window.go?.main?.App) {
          callGo(() => window.go!.main.App.RecordMouseActivity(clicksSnapshot));
        } else {
          apiFetch('/api/tracker/input', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ total_clicks: clicksSnapshot }),
          }).catch(() => {});
        }
      }
    }, 30000);

    // Auto-refresh timeline, stats, and AI analytics every 30 seconds
    const syncInterval = setInterval(() => {
      // No explicit date/range: reuse the currently selected view (stateRef)
      loadDataRef.current(undefined, undefined, undefined, undefined, { background: true });

      // Clean up client-side stored cached logs & usage entries older than 7 days
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

    return () => {
      window.removeEventListener('keydown', handleKeyDown);
      window.removeEventListener('mousedown', handleMouseDown);
      clearInterval(inputFlushInterval);
      clearInterval(syncInterval);
    };
  }, [isTrackingActive, loadData]);

  // Mobile navbar collapse state
  const [isMobileMenuOpen, setIsMobileMenuOpen] = useState<boolean>(false);

  useEffect(() => {
    if (!isMobileMenuOpen) return;
    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setIsMobileMenuOpen(false);
    };
    window.addEventListener('keydown', handleEscape);
    return () => window.removeEventListener('keydown', handleEscape);
  }, [isMobileMenuOpen]);

  // Close mobile menu on page transition
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

  const isTracking = isTrackingActive;

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

  // Signs the user in automatically after accepting an email invitation: the
  // server mints a JWT session with the accept response, so no manual login is needed.
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

  // Show Auth Page if user is not authenticated and has not chosen guest mode
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
        onOpenTaskModal={() => {
          setTaskInput(userTask);
          setShowTaskModal(true);
        }}
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
                // Timeline always shows a single day — reset any Analytics range so it can't leak
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

      {/* Target Task Goal Modal Dialog Component */}
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

      {/* Developer Menu (desktop-only, session unlocked) */}
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

