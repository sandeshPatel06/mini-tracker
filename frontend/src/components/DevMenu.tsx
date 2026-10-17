import { useCallback, useEffect, useMemo, useState } from 'react';
import { Modal } from './Modal';
import {
  BrowserOpenURL,
  ClipboardSetText,
  WindowReload,
} from '../../wailsjs/runtime/runtime';
import {
  BackupDatabase,
  ClearAllLocalData,
  ClearAppLogs,
  ClearDemoLogs,
  GetAppLogs,
  GetDBTableCounts,
  GetDevDiagnostics,
  GetScreenshotCacheStats,
  GetSyncStatus,
  ListAppLogFiles,
  ListAvailableModels,
  OpenFolder,
  ProcessPendingLogs,
  PurgeScreenshotCache,
  RunAIDebugOnLatest,
  RunCollectNow,
  SaveDiagnosticsBundle,
  SeedDemoLogs,
  SetLogLevel,
  StopTrackerDaemon,
  ToggleTracking,
  TriggerSyncNow,
} from '../../wailsjs/go/main/App';
import { clearDevApiLog, getDevApiLog } from '../api';
import type { DevApiLogEntry } from '../api';

export interface DevMenuProps {
  isOpen: boolean;
  onClose: () => void;
  theme: 'dark' | 'light' | 'auto';
  onThemeChange: (theme: 'dark' | 'light' | 'auto') => void;
  onDisable: () => void;
}

type DevTab = 'logs' | 'diagnostics' | 'actions' | 'ai' | 'share' | 'debug';

interface DevResult {
  text: string;
  error?: boolean;
}

interface AppLogFile {
  name: string;
  size_bytes: number;
  mod_time: string;
}

interface DevTabProps {
  results: Record<string, DevResult>;
  report: (id: string, text: string, error?: boolean) => void;
  run: (id: string, fn: () => Promise<string> | string) => Promise<void>;
}

const TABS: { id: DevTab; label: string }[] = [
  { id: 'logs', label: 'Logs' },
  { id: 'diagnostics', label: 'Diagnostics' },
  { id: 'actions', label: 'Actions' },
  { id: 'ai', label: 'AI' },
  { id: 'share', label: 'Share' },
  { id: 'debug', label: 'Debug' },
];

// The app emits these as DOM CustomEvents (see App.tsx / api.ts), so the
// event monitor listens on window instead of the Wails event bridge.
const MONITORED_EVENTS = ['mini-auth-expired', 'ai_usage_updated'];

const THEMES: Array<'dark' | 'light' | 'auto'> = ['dark', 'light', 'auto'];

const AI_SUMMARY_KEYS = ['ai_model', 'ai_configured', 'ai_key_masked', 'pending_ai_logs'];

function errorText(err: unknown): string {
  if (err instanceof Error && err.message) return err.message;
  if (typeof err === 'string' && err) return err;
  return 'Action failed';
}

function useActionRunner() {
  const [results, setResults] = useState<Record<string, DevResult>>({});
  const report = useCallback((id: string, text: string, error = false) => {
    setResults((prev) => ({ ...prev, [id]: { text, error: error || undefined } }));
  }, []);
  const run = useCallback(
    async (id: string, fn: () => Promise<string> | string) => {
      try {
        const text = await fn();
        report(id, text);
      } catch (err) {
        report(id, errorText(err), true);
      }
    },
    [report]
  );
  return { results, report, run };
}

function ResultLine({ result }: { result?: DevResult }) {
  if (!result) return null;
  return <div className={`dev-result${result.error ? ' error' : ''}`}>{result.text}</div>;
}

async function copyToClipboard(text: string): Promise<boolean> {
  try {
    const ok = await ClipboardSetText(text);
    if (ok) return true;
  } catch {
    // Fall through to the web clipboard API
  }
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    // Clipboard unavailable
  }
  return false;
}

function formatBytes(value: unknown): string {
  const num = typeof value === 'number' ? value : Number(value);
  if (!Number.isFinite(num) || num < 0) {
    return value === null || value === undefined ? '—' : String(value);
  }
  if (num < 1024) return `${num} B`;
  const units = ['KB', 'MB', 'GB', 'TB'];
  let size = num / 1024;
  let unit = 0;
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024;
    unit += 1;
  }
  return `${size >= 10 ? size.toFixed(1) : size.toFixed(2)} ${units[unit]}`;
}

function isSensitiveKey(key: string): boolean {
  const lower = key.toLowerCase();
  if (lower === 'ai_key_masked') return false;
  return lower.includes('token') || lower.includes('secret') || lower.includes('key');
}

function formatDiagnosticValue(key: string, value: unknown): string {
  if (value === null || value === undefined) return '—';
  if (typeof value === 'boolean') return value ? 'true' : 'false';
  const lower = key.toLowerCase();
  if (lower.endsWith('_bytes') || lower.endsWith('_size_bytes')) return formatBytes(value);
  if (typeof value === 'number') return Number.isInteger(value) ? String(value) : value.toFixed(2);
  if (typeof value === 'object') {
    try {
      const text = JSON.stringify(value);
      return text.length > 240 ? `${text.slice(0, 240)}…` : text;
    } catch {
      return '[object]';
    }
  }
  const text = String(value);
  return text.length > 0 ? text : '—';
}

function jsonPreview(value: unknown, limit: number): string {
  let text = '';
  if (typeof value === 'string') {
    text = value;
  } else {
    try {
      text = JSON.stringify(value) ?? '';
    } catch {
      text = String(value);
    }
  }
  return text.length > limit ? `${text.slice(0, limit)}…` : text;
}

function formatLogTime(iso: string): string {
  const date = new Date(iso);
  return Number.isNaN(date.getTime()) ? iso : date.toLocaleTimeString();
}

function KvRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="dev-kv">
      <span className="dev-kv-key">{label}</span>
      <span className="dev-kv-val">{value}</span>
    </div>
  );
}

function KvBlock({ entries }: { entries: Array<[string, unknown]> }) {
  if (entries.length === 0) return <p className="dev-muted">No data.</p>;
  return (
    <div>
      {entries.map(([key, value]) => (
        <KvRow key={key} label={key} value={formatDiagnosticValue(key, value)} />
      ))}
    </div>
  );
}

/* ------------------------------------------------------------------ */
/* Logs tab                                                            */
/* ------------------------------------------------------------------ */

function LogsTab({ results, report, run }: DevTabProps) {
  const [files, setFiles] = useState<AppLogFile[]>([]);
  const [selectedFile, setSelectedFile] = useState('');
  const [tailLines, setTailLines] = useState(500);
  const [logText, setLogText] = useState('');
  const [loading, setLoading] = useState(false);
  const [autoRefresh, setAutoRefresh] = useState(false);
  const [level, setLevel] = useState<'all' | 'debug' | 'info' | 'warn' | 'error'>('all');
  const [search, setSearch] = useState('');
  const [wrap, setWrap] = useState(true);
  const [refreshTick, setRefreshTick] = useState(0);

  const loadFiles = useCallback(async () => {
    try {
      const list = await ListAppLogFiles();
      setFiles(Array.isArray(list) ? list : []);
    } catch {
      setFiles([]);
    }
  }, []);

  const loadLogs = useCallback(
    async (fileName: string, tail: number) => {
      setLoading(true);
      try {
        const text = await GetAppLogs(fileName, tail);
        setLogText(typeof text === 'string' ? text : '');
      } catch (err) {
        setLogText('');
        report('logs-load', errorText(err), true);
      } finally {
        setLoading(false);
      }
    },
    [report]
  );

  useEffect(() => {
    void loadFiles();
  }, [loadFiles]);

  useEffect(() => {
    void loadLogs(selectedFile, tailLines);
  }, [selectedFile, tailLines, refreshTick, loadLogs]);

  // Auto-refresh only runs while this tab is mounted (i.e. visible and checked)
  useEffect(() => {
    if (!autoRefresh) return;
    const timer = window.setInterval(() => {
      void loadLogs(selectedFile, tailLines);
    }, 2000);
    return () => window.clearInterval(timer);
  }, [autoRefresh, selectedFile, tailLines, loadLogs]);

  const filteredLines = useMemo(() => {
    const lines = logText.length > 0 ? logText.split('\n') : [];
    const levelKey = level === 'all' ? '' : level;
    const searchKey = search.trim().toLowerCase();
    return lines.filter((line) => {
      const lower = line.toLowerCase();
      if (levelKey && !lower.includes(levelKey)) return false;
      if (searchKey && !lower.includes(searchKey)) return false;
      return true;
    });
  }, [logText, level, search]);

  const totalLines = logText.length > 0 ? logText.split('\n').length : 0;

  return (
    <div>
      <div className="dev-toolbar">
        <label className="dev-field">
          File
          <select
            className="dev-select"
            value={selectedFile}
            onChange={(event) => setSelectedFile(event.target.value)}
          >
            <option value="">Today</option>
            {files.map((file) => (
              <option key={file.name} value={file.name}>
                {file.name}
              </option>
            ))}
          </select>
        </label>
        <label className="dev-field">
          Tail
          <select
            className="dev-select"
            value={tailLines}
            onChange={(event) => setTailLines(Number(event.target.value))}
          >
            <option value={100}>100</option>
            <option value={500}>500</option>
            <option value={1000}>1000</option>
          </select>
        </label>
        <label className="dev-field">
          Level
          <select
            className="dev-select"
            value={level}
            onChange={(event) =>
              setLevel(event.target.value as 'all' | 'debug' | 'info' | 'warn' | 'error')
            }
          >
            <option value="all">All</option>
            <option value="debug">Debug</option>
            <option value="info">Info</option>
            <option value="warn">Warn</option>
            <option value="error">Error</option>
          </select>
        </label>
        <label className="dev-field">
          <input
            className="dev-checkbox"
            type="checkbox"
            checked={wrap}
            onChange={(event) => setWrap(event.target.checked)}
          />
          Wrap
        </label>
        <label className="dev-field">
          <input
            className="dev-checkbox"
            type="checkbox"
            checked={autoRefresh}
            onChange={(event) => setAutoRefresh(event.target.checked)}
          />
          Auto-refresh (2s)
        </label>
        <input
          className="dev-input"
          style={{ minWidth: 160 }}
          type="search"
          placeholder="Search logs…"
          value={search}
          onChange={(event) => setSearch(event.target.value)}
        />
      </div>

      <div className="dev-btn-row" style={{ margin: '10px 0' }}>
        <button
          type="button"
          className="btn btn-secondary btn-sm"
          onClick={() => {
            void loadFiles();
            setRefreshTick((tick) => tick + 1);
          }}
        >
          Refresh
        </button>
        <button
          type="button"
          className="btn btn-secondary btn-sm"
          onClick={() =>
            void run('logs-copy', async () => {
              const ok = await copyToClipboard(filteredLines.join('\n'));
              return ok ? `Copied ${filteredLines.length} log line(s)` : 'Copy failed';
            })
          }
        >
          Copy logs
        </button>
        <button
          type="button"
          className="btn btn-secondary btn-sm"
          onClick={() =>
            void run('logs-folder', async () => {
              await OpenFolder('logs');
              return 'Opened logs folder';
            })
          }
        >
          Open logs folder
        </button>
        <button
          type="button"
          className="btn btn-secondary btn-sm"
          onClick={() =>
            void run('logs-clear', async () => {
              if (!window.confirm('Clear all application log files? This cannot be undone.')) {
                return 'Cancelled';
              }
              await ClearAppLogs();
              await loadFiles();
              setRefreshTick((tick) => tick + 1);
              return 'Logs cleared';
            })
          }
        >
          Clear logs
        </button>
      </div>

      <ResultLine result={results['logs-load']} />
      <ResultLine result={results['logs-copy']} />
      <ResultLine result={results['logs-folder']} />
      <ResultLine result={results['logs-clear']} />

      <div className={`dev-log-view${wrap ? ' wrap' : ''}`} role="log" aria-label="Application log output">
        {loading && filteredLines.length === 0 ? (
          <div className="dev-log-empty">Loading…</div>
        ) : filteredLines.length === 0 ? (
          <div className="dev-log-empty">No matching log lines.</div>
        ) : (
          filteredLines.map((line, index) => (
            <div className="dev-log-line" key={index}>
              {line.length > 0 ? line : '\u00a0'}
            </div>
          ))
        )}
      </div>
      <p className="dev-muted" style={{ marginTop: 6 }}>
        {filteredLines.length} of {totalLines} lines shown
        {autoRefresh ? ' · auto-refreshing every 2s' : ''}
      </p>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/* Diagnostics tab                                                     */
/* ------------------------------------------------------------------ */

interface DiagnosticsState {
  diagnostics: Record<string, any> | null;
  dbCounts: Record<string, number> | null;
  cacheStats: Record<string, any> | null;
  syncStatus: Record<string, any> | null;
  recentErrors: string[];
}

function DiagnosticsTab({ results, run }: DevTabProps) {
  const [state, setState] = useState<DiagnosticsState | null>(null);
  const [loading, setLoading] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [diagnostics, dbCounts, cacheStats, syncStatus, logText] = await Promise.all([
        GetDevDiagnostics().catch(() => null),
        GetDBTableCounts().catch(() => null),
        GetScreenshotCacheStats().catch(() => null),
        GetSyncStatus().catch(() => null),
        GetAppLogs('today', 1000).catch(() => ''),
      ]);
      const recentErrors = (typeof logText === 'string' ? logText : '')
        .split('\n')
        .filter((line) => /error|fatal|panic/i.test(line))
        .slice(-5);
      setState({ diagnostics, dbCounts, cacheStats, syncStatus, recentErrors });
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const diagnosticsEntries = state?.diagnostics
    ? Object.entries(state.diagnostics).filter(([key]) => !isSensitiveKey(key))
    : [];

  return (
    <div>
      <div className="dev-btn-row" style={{ marginBottom: 8 }}>
        <button type="button" className="btn btn-secondary btn-sm" onClick={() => void load()}>
          {loading ? 'Refreshing…' : 'Refresh'}
        </button>
        <button
          type="button"
          className="btn btn-secondary btn-sm"
          onClick={() =>
            void run('diag-copy', async () => {
              const raw = JSON.stringify(
                {
                  diagnostics: state?.diagnostics ?? null,
                  db_table_counts: state?.dbCounts ?? null,
                  screenshot_cache: state?.cacheStats ?? null,
                  sync_status: state?.syncStatus ?? null,
                },
                null,
                2
              );
              const ok = await copyToClipboard(raw);
              return ok ? 'Copied diagnostics JSON' : 'Copy failed';
            })
          }
        >
          Copy diagnostics
        </button>
      </div>
      <ResultLine result={results['diag-copy']} />

      <div className="dev-section-title">Environment</div>
      <KvBlock entries={diagnosticsEntries} />

      <div className="dev-section-title">Database tables</div>
      <KvBlock entries={state?.dbCounts ? Object.entries(state.dbCounts) : []} />

      <div className="dev-section-title">Screenshot cache</div>
      <KvBlock entries={state?.cacheStats ? Object.entries(state.cacheStats) : []} />

      <div className="dev-section-title">Sync status</div>
      <KvBlock entries={state?.syncStatus ? Object.entries(state.syncStatus) : []} />

      <div className="dev-section-title">Recent errors (from today's log)</div>
      {state && state.recentErrors.length > 0 ? (
        <div className="dev-log-view dev-log-errors">
          {state.recentErrors.map((line, index) => (
            <div className="dev-log-line" key={index}>
              {line}
            </div>
          ))}
        </div>
      ) : (
        <p className="dev-muted">No recent error/fatal/panic lines found.</p>
      )}
    </div>
  );
}

/* ------------------------------------------------------------------ */
/* Actions tab                                                         */
/* ------------------------------------------------------------------ */

interface DevAction {
  id: string;
  label: string;
  hint: string;
  handler: () => Promise<string>;
}

function ActionsTab({ results, run }: DevTabProps) {
  const actions: DevAction[] = [
    {
      id: 'collect',
      label: 'Collect Now',
      hint: 'Run one capture cycle immediately',
      handler: async () => {
        await RunCollectNow();
        return 'Collection triggered';
      },
    },
    {
      id: 'sync',
      label: 'Sync Now',
      hint: 'Push pending telemetry to the backend',
      handler: async () => {
        await TriggerSyncNow();
        return 'Sync triggered';
      },
    },
    {
      id: 'process-ai',
      label: 'Process Pending AI',
      hint: 'Run AI analysis on pending captures',
      handler: async () => {
        const processed = await ProcessPendingLogs();
        return `Processed ${processed} pending log(s)`;
      },
    },
    {
      id: 'toggle-tracking',
      label: 'Toggle Tracking',
      hint: 'Pause or resume the tracker',
      handler: async () => {
        const status = await ToggleTracking();
        const active = Boolean(status && (status as Record<string, any>).active);
        return active ? 'Tracking is now active' : 'Tracking is now paused';
      },
    },
    {
      id: 'open-data',
      label: 'Open Data Folder',
      hint: 'Open the local data directory',
      handler: async () => {
        await OpenFolder('data');
        return 'Opened data folder';
      },
    },
    {
      id: 'open-screenshots',
      label: 'Open Screenshots Folder',
      hint: 'Open the screenshot cache directory',
      handler: async () => {
        await OpenFolder('screenshots');
        return 'Opened screenshots folder';
      },
    },
    {
      id: 'stop-daemon',
      label: 'Stop Background Daemon',
      hint: 'Stop the background tracker daemon',
      handler: async () => {
        if (!window.confirm('Stop the background tracker daemon? Tracking will pause.')) {
          return 'Cancelled';
        }
        await StopTrackerDaemon();
        return 'Background daemon stopped';
      },
    },
    {
      id: 'reload-ui',
      label: 'Reload UI',
      hint: 'Reload the webview frontend',
      handler: async () => {
        WindowReload();
        return 'Reloading UI…';
      },
    },
    {
      id: 'purge-screenshots',
      label: 'Purge Screenshot Cache',
      hint: 'Delete cached screenshot files',
      handler: async () => {
        let statsText = '';
        try {
          const stats = await GetScreenshotCacheStats();
          if (stats) {
            statsText = ` — ${stats.count ?? '?'} file(s), ${formatBytes(stats.total_bytes ?? 0)}`;
          }
        } catch {
          // Stats are best-effort
        }
        if (!window.confirm(`Purge screenshot cache${statsText}? This deletes cached screenshots.`)) {
          return 'Cancelled';
        }
        const deleted = await PurgeScreenshotCache();
        return `Deleted ${deleted} cached screenshot(s)`;
      },
    },
    {
      id: 'backup-db',
      label: 'Backup Database…',
      hint: 'Save a copy of the local database',
      handler: async () => {
        const path = await BackupDatabase();
        return path ? `Saved: ${path}` : 'Cancelled';
      },
    },
    {
      id: 'clear-all',
      label: 'Clear All Local Data',
      hint: 'Delete all local logs, screenshots and cached data',
      handler: async () => {
        if (!window.confirm('Delete ALL local tracked data? This cannot be undone.')) {
          return 'Cancelled';
        }
        if (
          !window.confirm(
            'Are you absolutely sure? This permanently removes local logs, screenshots and cached data.'
          )
        ) {
          return 'Cancelled';
        }
        await ClearAllLocalData();
        return 'All local data cleared';
      },
    },
  ];

  return (
    <div>
      <div className="dev-section-title">Maintenance actions</div>
      <div className="dev-action-list">
        {actions.map((action) => (
          <div className="dev-action" key={action.id}>
            <div className="dev-action-head">
              <button
                type="button"
                className="btn btn-secondary btn-sm"
                onClick={() => void run(action.id, action.handler)}
              >
                {action.label}
              </button>
              <span className="dev-hint">{action.hint}</span>
            </div>
            <ResultLine result={results[action.id]} />
          </div>
        ))}
      </div>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/* AI tab                                                              */
/* ------------------------------------------------------------------ */

function AITab({ results, report, run }: DevTabProps) {
  const [diagnostics, setDiagnostics] = useState<Record<string, any> | null>(null);
  const [debugResult, setDebugResult] = useState<Record<string, any> | null>(null);
  const [models, setModels] = useState<string[]>([]);
  const [logLevel, setLogLevel] = useState<'all' | 'debug' | 'info' | 'warn' | 'error'>('all');

  const loadDiagnostics = useCallback(async () => {
    try {
      const diag = await GetDevDiagnostics();
      setDiagnostics(diag);
    } catch {
      setDiagnostics(null);
    }
  }, []);

  useEffect(() => {
    void loadDiagnostics();
  }, [loadDiagnostics]);

  return (
    <div>
      <div className="dev-section-title">AI configuration</div>
      {diagnostics ? (
        <>
          {AI_SUMMARY_KEYS.map((key) => (
            <KvRow key={key} label={key} value={formatDiagnosticValue(key, diagnostics[key])} />
          ))}
          <div className="dev-btn-row" style={{ marginTop: 8 }}>
            <button
              type="button"
              className="btn btn-secondary btn-sm"
              onClick={() => void loadDiagnostics()}
            >
              Refresh
            </button>
          </div>
        </>
      ) : (
        <p className="dev-muted">AI diagnostics unavailable.</p>
      )}

      <div className="dev-section-title">Debug analysis</div>
      <div className="dev-btn-row">
        <button
          type="button"
          className="btn btn-secondary btn-sm"
          onClick={() =>
            void run('ai-analyze', async () => {
              const result = await RunAIDebugOnLatest();
              setDebugResult(result ?? null);
              if (result && result.error) throw new Error(String(result.error));
              return 'Analysis complete';
            })
          }
        >
          Analyze Latest Pending
        </button>
        <button
          type="button"
          className="btn btn-secondary btn-sm"
          onClick={() =>
            void run('ai-models', async () => {
              const list = await ListAvailableModels();
              const safe = Array.isArray(list) ? list : [];
              setModels(safe);
              return `Found ${safe.length} model(s)`;
            })
          }
        >
          List Models
        </button>
      </div>
      <ResultLine result={results['ai-analyze']} />
      <ResultLine result={results['ai-models']} />

      {debugResult && (
        <div style={{ marginTop: 10 }}>
          {Object.keys(debugResult).map((key) => (
            <KvRow key={key} label={key} value={formatDiagnosticValue(key, debugResult[key])} />
          ))}
        </div>
      )}

      {models.length > 0 && (
        <>
          <div className="dev-chip-row">
            {models.map((model) => (
              <button
                key={model}
                type="button"
                className="dev-chip"
                onClick={() => {
                  report('ai-model-copy', `Copying ${model}…`);
                  void copyToClipboard(model).then((ok) =>
                    report('ai-model-copy', ok ? `Copied ${model}` : 'Copy failed', !ok)
                  );
                }}
              >
                {model}
              </button>
            ))}
          </div>
          <ResultLine result={results['ai-model-copy']} />
        </>
      )}

      <div className="dev-section-title">Backend log level</div>
      <div className="dev-btn-row">
        <select
          className="dev-select"
          value={logLevel}
          onChange={(event) => {
            const value = event.target.value as 'all' | 'debug' | 'info' | 'warn' | 'error';
            setLogLevel(value);
            void run('ai-loglevel', async () => {
              const mapped = value === 'all' ? 'debug' : value;
              await SetLogLevel(mapped);
              return value === 'all'
                ? 'Log level set to debug (most verbose)'
                : `Log level set to ${value}`;
            });
          }}
        >
          <option value="all">All</option>
          <option value="debug">Debug</option>
          <option value="info">Info</option>
          <option value="warn">Warn</option>
          <option value="error">Error</option>
        </select>
      </div>
      <ResultLine result={results['ai-loglevel']} />
    </div>
  );
}

/* ------------------------------------------------------------------ */
/* Share tab                                                           */
/* ------------------------------------------------------------------ */

function ShareTab({ results, run }: DevTabProps) {
  const composeBundle = useCallback(async (): Promise<string> => {
    const [diagnostics, dbCounts, cacheStats, syncStatus, logs] = await Promise.all([
      GetDevDiagnostics().catch(() => null),
      GetDBTableCounts().catch(() => null),
      GetScreenshotCacheStats().catch(() => null),
      GetSyncStatus().catch(() => null),
      GetAppLogs('today', 200).catch(() => ''),
    ]);
    const lines: string[] = [];
    lines.push('# get-Hike dev diagnostics');
    lines.push('');
    lines.push(`- Generated: ${new Date().toISOString()}`);
    lines.push('');
    lines.push('## Environment');
    if (diagnostics) {
      Object.keys(diagnostics).forEach((key) => {
        if (isSensitiveKey(key)) return;
        lines.push(`- ${key}: ${formatDiagnosticValue(key, diagnostics[key])}`);
      });
    } else {
      lines.push('- (diagnostics unavailable)');
    }
    lines.push('');
    lines.push('## Database tables');
    if (dbCounts && Object.keys(dbCounts).length > 0) {
      Object.entries(dbCounts).forEach(([key, value]) => {
        lines.push(`- ${key}: ${value}`);
      });
    } else {
      lines.push('- (unavailable)');
    }
    lines.push('');
    lines.push('## Screenshot cache');
    if (cacheStats && Object.keys(cacheStats).length > 0) {
      Object.keys(cacheStats).forEach((key) => {
        lines.push(`- ${key}: ${formatDiagnosticValue(key, cacheStats[key])}`);
      });
    } else {
      lines.push('- (unavailable)');
    }
    lines.push('');
    lines.push('## Sync status');
    if (syncStatus && Object.keys(syncStatus).length > 0) {
      Object.keys(syncStatus).forEach((key) => {
        lines.push(`- ${key}: ${formatDiagnosticValue(key, syncStatus[key])}`);
      });
    } else {
      lines.push('- (unavailable)');
    }
    lines.push('');
    lines.push('## Recent logs (last 200 lines)');
    lines.push('```');
    lines.push(typeof logs === 'string' && logs.length > 0 ? logs : '(logs unavailable)');
    lines.push('```');
    return lines.join('\n').slice(0, 60000);
  }, []);

  return (
    <div>
      <div className="dev-section-title">Diagnostics bundle</div>
      <p className="dev-muted">
        Bundles environment, database, cache, sync status and the last 200 log lines. Sensitive
        values are omitted.
      </p>
      <div className="dev-btn-row" style={{ marginTop: 10 }}>
        <button
          type="button"
          className="btn btn-secondary btn-sm"
          onClick={() =>
            void run('share-copy', async () => {
              const bundle = await composeBundle();
              const ok = await copyToClipboard(bundle);
              return ok ? `Copied diagnostics bundle (${bundle.length} chars)` : 'Copy failed';
            })
          }
        >
          Copy Diagnostics Bundle
        </button>
        <button
          type="button"
          className="btn btn-secondary btn-sm"
          onClick={() =>
            void run('share-issue', async () => {
              const bundle = await composeBundle();
              const url =
                'https://github.com/sandeshPatel06/mini-tracker/issues/new?title=' +
                encodeURIComponent('Bug: ') +
                '&body=' +
                encodeURIComponent(bundle.slice(0, 6000));
              try {
                BrowserOpenURL(url);
              } catch {
                window.open(url, '_blank', 'noopener');
              }
              return 'Opened GitHub issue in browser';
            })
          }
        >
          Create GitHub Issue
        </button>
        <button
          type="button"
          className="btn btn-secondary btn-sm"
          onClick={() =>
            void run('share-save', async () => {
              const bundle = await composeBundle();
              const path = await SaveDiagnosticsBundle(bundle);
              return path ? `Saved: ${path}` : 'Cancelled';
            })
          }
        >
          Save Bundle to File
        </button>
        <button
          type="button"
          className="btn btn-secondary btn-sm"
          onClick={() =>
            void run('share-path', async () => {
              const diagnostics = await GetDevDiagnostics();
              const path = String(diagnostics?.today_log_file || diagnostics?.logs_dir || '');
              if (!path) return 'Log file path unavailable';
              const ok = await copyToClipboard(path);
              return ok ? `Copied: ${path}` : 'Copy failed';
            })
          }
        >
          Copy Log File Path
        </button>
      </div>
      <div style={{ marginTop: 8 }}>
        <ResultLine result={results['share-copy']} />
        <ResultLine result={results['share-issue']} />
        <ResultLine result={results['share-save']} />
        <ResultLine result={results['share-path']} />
      </div>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/* Debug tab                                                           */
/* ------------------------------------------------------------------ */

interface DevEventEntry {
  time: string;
  event: string;
  payload: string;
}

function DebugTab({
  results,
  run,
  theme,
  onThemeChange,
  onDisable,
}: DevTabProps & {
  theme: DevMenuProps['theme'];
  onThemeChange: DevMenuProps['onThemeChange'];
  onDisable: () => void;
}) {
  const [storageEntries, setStorageEntries] = useState<Array<[string, string]>>([]);
  const [eventsEnabled, setEventsEnabled] = useState(false);
  const [eventLog, setEventLog] = useState<DevEventEntry[]>([]);
  const [apiMonitor, setApiMonitor] = useState(false);
  const [apiLog, setApiLog] = useState<DevApiLogEntry[]>([]);
  const [seedCount, setSeedCount] = useState(10);

  const refreshStorage = useCallback(() => {
    const entries: Array<[string, string]> = [];
    try {
      for (let index = 0; index < localStorage.length; index += 1) {
        const key = localStorage.key(index);
        if (key === null) continue;
        entries.push([key, localStorage.getItem(key) ?? '']);
      }
    } catch {
      // localStorage unavailable
    }
    entries.sort((a, b) => a[0].localeCompare(b[0]));
    setStorageEntries(entries);
  }, []);

  useEffect(() => {
    refreshStorage();
  }, [refreshStorage]);

  useEffect(() => {
    if (!eventsEnabled) return;
    const handler = (event: Event) => {
      const detail = (event as CustomEvent).detail;
      setEventLog((prev) => {
        const next = [
          ...prev,
          {
            time: new Date().toLocaleTimeString(),
            event: event.type,
            payload: detail === undefined ? '' : jsonPreview(detail, 300),
          },
        ];
        return next.length > 100 ? next.slice(next.length - 100) : next;
      });
    };
    MONITORED_EVENTS.forEach((name) => window.addEventListener(name, handler));
    return () => MONITORED_EVENTS.forEach((name) => window.removeEventListener(name, handler));
  }, [eventsEnabled]);

  useEffect(() => {
    (window as any).__DEV_MONITOR__ = apiMonitor;
    if (!apiMonitor) return;
    setApiLog(getDevApiLog());
    const timer = window.setInterval(() => setApiLog(getDevApiLog()), 2000);
    return () => {
      window.clearInterval(timer);
      (window as any).__DEV_MONITOR__ = false;
    };
  }, [apiMonitor]);

  return (
    <div>
      <div className="dev-section-title">Theme</div>
      <div className="dev-btn-row">
        {THEMES.map((value) => (
          <button
            key={value}
            type="button"
            className={`dev-chip${theme === value ? ' active' : ''}`}
            aria-pressed={theme === value}
            onClick={() => onThemeChange(value)}
          >
            {value}
          </button>
        ))}
      </div>

      <div className="dev-section-title">localStorage</div>
      <div className="dev-btn-row" style={{ marginBottom: 8 }}>
        <button type="button" className="btn btn-secondary btn-sm" onClick={refreshStorage}>
          Refresh
        </button>
        <button
          type="button"
          className="btn btn-secondary btn-sm"
          onClick={() =>
            void run('debug-storage-copy', async () => {
              const payload = JSON.stringify(Object.fromEntries(storageEntries), null, 2);
              const ok = await copyToClipboard(payload);
              return ok
                ? `Copied ${storageEntries.length} localStorage entries`
                : 'Copy failed';
            })
          }
        >
          Copy all
        </button>
      </div>
      <ResultLine result={results['debug-storage-copy']} />
      {storageEntries.length === 0 ? (
        <p className="dev-muted">localStorage is empty.</p>
      ) : (
        <div>
          {storageEntries.map(([key, value]) => (
            <KvRow key={key} label={key} value={jsonPreview(value, 200)} />
          ))}
        </div>
      )}

      <div className="dev-section-title">Event monitor</div>
      <div className="dev-btn-row" style={{ marginBottom: 8 }}>
        <label className="dev-field">
          <input
            className="dev-checkbox"
            type="checkbox"
            checked={eventsEnabled}
            onChange={(event) => setEventsEnabled(event.target.checked)}
          />
          Capture events
        </label>
        <button
          type="button"
          className="btn btn-secondary btn-sm"
          onClick={() => setEventLog([])}
        >
          Clear
        </button>
        <button
          type="button"
          className="btn btn-secondary btn-sm"
          onClick={() =>
            void run('debug-events-copy', async () => {
              const ok = await copyToClipboard(JSON.stringify(eventLog, null, 2));
              return ok ? `Copied ${eventLog.length} event(s)` : 'Copy failed';
            })
          }
        >
          Copy
        </button>
      </div>
      <ResultLine result={results['debug-events-copy']} />
      <div className="dev-log-view dev-log-errors">
        {eventLog.length === 0 ? (
          <div className="dev-log-empty">No events captured yet.</div>
        ) : (
          eventLog.map((entry, index) => (
            <div className="dev-log-line" key={index}>
              {entry.time} {entry.event}
              {entry.payload ? ` · ${entry.payload}` : ''}
            </div>
          ))
        )}
      </div>
      <p className="dev-muted" style={{ marginTop: 4 }}>
        Listening on: {MONITORED_EVENTS.join(', ')}
      </p>

      <div className="dev-section-title">API monitor</div>
      <div className="dev-btn-row" style={{ marginBottom: 8 }}>
        <label className="dev-field">
          <input
            className="dev-checkbox"
            type="checkbox"
            checked={apiMonitor}
            onChange={(event) => setApiMonitor(event.target.checked)}
          />
          Capture apiFetch calls
        </label>
        <button
          type="button"
          className="btn btn-secondary btn-sm"
          onClick={() => setApiLog(getDevApiLog())}
        >
          Refresh
        </button>
        <button
          type="button"
          className="btn btn-secondary btn-sm"
          onClick={() => {
            clearDevApiLog();
            setApiLog([]);
          }}
        >
          Clear
        </button>
        <button
          type="button"
          className="btn btn-secondary btn-sm"
          onClick={() =>
            void run('debug-api-copy', async () => {
              const ok = await copyToClipboard(JSON.stringify(apiLog, null, 2));
              return ok ? `Copied ${apiLog.length} request(s)` : 'Copy failed';
            })
          }
        >
          Copy
        </button>
      </div>
      <ResultLine result={results['debug-api-copy']} />
      <div className="dev-log-view dev-log-errors">
        {apiLog.length === 0 ? (
          <div className="dev-log-empty">No requests captured yet.</div>
        ) : (
          apiLog.map((entry, index) => (
            <div className="dev-log-line" key={index}>
              {formatLogTime(entry.time)} {entry.method} {entry.path} → {entry.status} (
              {entry.ms} ms){entry.ok ? '' : ' ✗'}
            </div>
          ))
        )}
      </div>

      <div className="dev-section-title">Demo data</div>
      <div className="dev-btn-row" style={{ marginBottom: 8 }}>
        <label className="dev-field">
          Count
          <input
            className="dev-input"
            type="number"
            min={1}
            max={50}
            style={{ width: 72 }}
            value={seedCount}
            onChange={(event) => setSeedCount(Number(event.target.value))}
          />
        </label>
        <button
          type="button"
          className="btn btn-secondary btn-sm"
          onClick={() =>
            void run('debug-seed', async () => {
              const count = Math.max(1, Math.min(50, Math.floor(seedCount) || 1));
              if (!window.confirm(`Seed ${count} demo log(s)?`)) return 'Cancelled';
              const inserted = await SeedDemoLogs(count);
              return `Inserted ${inserted} demo log(s)`;
            })
          }
        >
          Seed Demo Logs
        </button>
        <button
          type="button"
          className="btn btn-secondary btn-sm"
          onClick={() =>
            void run('debug-clear-demo', async () => {
              if (!window.confirm('Delete all demo logs?')) return 'Cancelled';
              const deleted = await ClearDemoLogs();
              return `Deleted ${deleted} demo log(s)`;
            })
          }
        >
          Clear Demo Logs
        </button>
      </div>
      <ResultLine result={results['debug-seed']} />
      <ResultLine result={results['debug-clear-demo']} />

      <div className="dev-section-title">Dev mode</div>
      <div className="dev-btn-row">
        <button type="button" className="btn btn-secondary btn-sm" onClick={onDisable}>
          Disable Dev Mode
        </button>
      </div>
      <p className="dev-muted" style={{ marginTop: 6 }}>
        Dev mode is session-only and disappears when the app restarts.
      </p>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/* Root menu                                                           */
/* ------------------------------------------------------------------ */

export function DevMenu({ isOpen, onClose, theme, onThemeChange, onDisable }: DevMenuProps) {
  const [activeTab, setActiveTab] = useState<DevTab>('logs');
  const { results, report, run } = useActionRunner();

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      maxWidth={860}
      title={
        <>
          Developer Menu
          <span className="dev-badge" style={{ marginLeft: 10 }}>
            DEV
          </span>
        </>
      }
    >
      <div className="dev-overlay-body">
        <div className="dev-layout">
          <div
            className="dev-tab-rail"
            role="tablist"
            aria-orientation="vertical"
            aria-label="Developer menu sections"
          >
            {TABS.map((tab) => (
              <button
                key={tab.id}
                type="button"
                role="tab"
                id={`dev-tab-${tab.id}`}
                aria-selected={activeTab === tab.id}
                aria-controls={`dev-panel-${tab.id}`}
                className={`dev-tab${activeTab === tab.id ? ' active' : ''}`}
                onClick={() => setActiveTab(tab.id)}
              >
                {tab.label}
              </button>
            ))}
          </div>
          <div
            className="dev-content"
            role="tabpanel"
            id={`dev-panel-${activeTab}`}
            aria-labelledby={`dev-tab-${activeTab}`}
          >
            {activeTab === 'logs' && <LogsTab results={results} report={report} run={run} />}
            {activeTab === 'diagnostics' && (
              <DiagnosticsTab results={results} report={report} run={run} />
            )}
            {activeTab === 'actions' && <ActionsTab results={results} report={report} run={run} />}
            {activeTab === 'ai' && <AITab results={results} report={report} run={run} />}
            {activeTab === 'share' && <ShareTab results={results} report={report} run={run} />}
            {activeTab === 'debug' && (
              <DebugTab
                results={results}
                report={report}
                run={run}
                theme={theme}
                onThemeChange={onThemeChange}
                onDisable={onDisable}
              />
            )}
          </div>
        </div>
      </div>
    </Modal>
  );
}
