import { useState, useEffect, useCallback } from 'react';
import { apiFetch } from '../api';
import { getTodayUsageCount, bumpUsageCount, resetUsageCount } from '../utils';
import { CustomSelect } from '../components/CustomSelect';
import type { SelectOption } from '../components/CustomSelect';
import { Icon } from '../components/Icon';

interface AIModel {
  name: string;
  displayName: string;
  description: string;
  inputTokenLimit: number;
  outputTokenLimit: number;
}

interface SettingsPageProps {
  theme?: 'dark' | 'light' | 'auto';
  onThemeChange?: (theme: 'dark' | 'light' | 'auto') => void;
  syncIntervalSeconds?: number;
}

export function SettingsPage({ theme = 'auto', onThemeChange, syncIntervalSeconds = 30 }: SettingsPageProps) {
  const [apiKey, setApiKey] = useState(() => localStorage.getItem('mini_gemini_api_key') || '');
  const [model, setModel] = useState(() => localStorage.getItem('mini_ai_model') || 'models/gemini-3.5-flash-lite');
  const [availableModels, setAvailableModels] = useState<AIModel[]>([]);
  const [fetchingModels, setFetchingModels] = useState<boolean>(false);
  const [usageSummary, setUsageSummary] = useState<{ total_requests: number; prompt_tokens: number; candidate_tokens: number; total_tokens: number } | null>(null);
  const [usageCount, setUsageCount] = useState<number>(() => getTodayUsageCount());
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [statusMessage, setStatusMessage] = useState<string | null>(null);

  const fetchUsageSummary = useCallback(async () => {
    // Desktop runs standalone (no backend): read today's usage from the local DB.
    if ((window as any).go?.main?.App?.GetAIUsageSummary) {
      try {
        const data = await (window as any).go.main.App.GetAIUsageSummary();
        setUsageSummary(data);
      } catch {
        setUsageSummary(null);
      }
      return;
    }
    try {
      const res = await apiFetch('/api/user/usage');
      if (res.ok) {
        const data = await res.json();
        setUsageSummary(data);
      }
    } catch {}
  }, []);

  // Periodic usage refresh + cross-tab/local event sync
  useEffect(() => {
    const syncUsage = () => {
      setUsageCount(getTodayUsageCount());
      fetchUsageSummary();
    };
    syncUsage();

    const handleCustomUsageEvent = (e: Event) => {
      const customEv = e as CustomEvent;
      if (typeof customEv.detail === 'number') {
        setUsageCount(customEv.detail);
      } else {
        syncUsage();
      }
    };

    window.addEventListener('ai_usage_updated', handleCustomUsageEvent);
    window.addEventListener('storage', syncUsage);
    const usageTimer = setInterval(syncUsage, 20000);

    return () => {
      clearInterval(usageTimer);
      window.removeEventListener('ai_usage_updated', handleCustomUsageEvent);
      window.removeEventListener('storage', syncUsage);
    };
  }, [fetchUsageSummary]);

  const fetchAvailableModels = useCallback(async (key: string) => {
    if (!key.trim()) return;
    setFetchingModels(true);
    try {
      const res = await fetch(`https://generativelanguage.googleapis.com/v1beta/models?key=${key.trim()}`);
      if (res.ok) {
        const data = await res.json();
        if (data.models && Array.isArray(data.models)) {
          const generateModels = data.models
            .filter((m: any) => m.supportedGenerationMethods?.includes('generateContent'))
            .map((m: any) => ({
              name: m.name,
              displayName: m.displayName || m.name.replace('models/', ''),
              description: m.description || '',
              inputTokenLimit: m.inputTokenLimit || 0,
              outputTokenLimit: m.outputTokenLimit || 0,
            }));
          setAvailableModels(generateModels);
          setStatusMessage('✓ Dynamically loaded models from Google Gemini API');
          setTimeout(() => setStatusMessage(null), 3000);
        }
      }
    } catch {
      // Ignore network errors on auto-fetch
    } finally {
      setFetchingModels(false);
    }
  }, []);

  useEffect(() => {
    if (!apiKey.trim()) return;
    const t = setTimeout(() => fetchAvailableModels(apiKey), 500);
    return () => clearTimeout(t);
  }, [apiKey, fetchAvailableModels]);

  const [screenshotInterval, setScreenshotInterval] = useState<number>(() => {
    return parseInt(localStorage.getItem('mini_screenshot_interval') || '60', 10);
  });

  const handleSave = async () => {
    setSaving(true);
    let backendError: string | null = null;
    const cleanModel = model.replace('models/', '');
    try {
      if ((window as any).go?.main?.App) {
        if ((window as any).go.main.App.UpdateGeminiAPIKey) {
          await (window as any).go.main.App.UpdateGeminiAPIKey(apiKey.trim());
        }
        if ((window as any).go.main.App.UpdateAIModel) {
          await (window as any).go.main.App.UpdateAIModel(cleanModel);
        }
        if ((window as any).go.main.App.UpdateScreenshotInterval) {
          await (window as any).go.main.App.UpdateScreenshotInterval(screenshotInterval);
        }
      } else {
        const res = await apiFetch('/api/config', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            gemini_api_key: apiKey.trim(),
            ai_model: model,
            screenshot_interval_seconds: screenshotInterval
          }),
        });
        if (!res.ok) {
          throw new Error(`Server returned ${res.status}`);
        }
      }
    } catch (err: any) {
      backendError = err?.message || 'unknown error';
    }

    localStorage.setItem('mini_gemini_api_key', apiKey.trim());
    localStorage.setItem('mini_ai_model', model);
    localStorage.setItem('mini_screenshot_interval', screenshotInterval.toString());

    if (backendError) {
      setStatusMessage(`✕ Saved locally, but backend update failed: ${backendError}`);
    } else {
      setStatusMessage('✓ Settings & Selected Model saved successfully!');
    }
    setTimeout(() => setStatusMessage(null), 4000);
    setSaving(false);
  };

  const handleTestAPI = async () => {
    if (!apiKey.trim()) {
      setStatusMessage('Please enter an API Key first.');
      return;
    }
    if (testing) return;
    setTesting(true);
    setStatusMessage('Testing connection and fetching models…');
    await fetchAvailableModels(apiKey);
    try {
      const cleanModel = model.replace('models/', '');
      const res = await fetch(`https://generativelanguage.googleapis.com/v1beta/models/${cleanModel}:generateContent?key=${apiKey.trim()}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          contents: [{ parts: [{ text: 'Hello, respond with OK if working.' }] }]
        })
      });
      if (res.ok) {
        const newUsage = bumpUsageCount(1);
        setUsageCount(newUsage);
        setStatusMessage(`✓ Connected successfully to ${cleanModel}!`);
      } else {
        const err = await res.json();
        setStatusMessage(`✕ API Error: ${err.error?.message || 'Invalid key or model.'}`);
      }
    } catch {
      setStatusMessage('✕ Connection failed. Check network or API key.');
    } finally {
      setTesting(false);
    }
  };

  const handleClearData = async () => {
    if (!(window as any).go?.main?.App) {
      setStatusMessage('✕ Clearing stored data is only available in the desktop app.');
      return;
    }
    if (!window.confirm('Are you sure you want to delete all stored screenshots, logs, and activity records? This action cannot be undone.')) {
      return;
    }
    try {
      await (window as any).go.main.App.ClearAllLocalData();
      resetUsageCount();
      setUsageCount(0);
      setStatusMessage('✓ Successfully deleted all local screenshots and reset database!');
      setTimeout(() => window.location.reload(), 1500);
    } catch {
      setStatusMessage('✕ Failed to clear local data.');
    }
  };

  const fmtTokens = (n: number) => (n ? n.toLocaleString() : 'N/A');
  const selectedModelInfo = availableModels.find(m => m.name === model || m.name === `models/${model}`);
  const modelOptions: SelectOption[] = availableModels.length > 0
    ? availableModels.map((m) => ({
        value: m.name,
        label: m.displayName,
        meta: `${fmtTokens(m.inputTokenLimit)} in`,
        leading: <Icon name="sparkles" size={14} />,
      }))
    : [
        {
          value: 'models/gemini-3.5-flash-lite',
          label: 'Gemini 3.5 Flash Lite',
          triggerLabel: 'Gemini 3.5 Flash Lite (Fast & Recommended)',
          meta: 'Fast & Recommended',
          leading: <Icon name="sparkles" size={14} />,
        },
        {
          value: 'models/gemini-3.8-flash',
          label: 'Gemini 3.8 Flash',
          triggerLabel: 'Gemini 3.8 Flash (Balanced)',
          meta: 'Balanced',
          leading: <Icon name="sparkles" size={14} />,
        },
        {
          value: 'models/gemini-2.5-pro',
          label: 'Gemini 2.5 Pro',
          triggerLabel: 'Gemini 2.5 Pro (Deep Reasoning)',
          meta: 'Deep Reasoning',
          leading: <Icon name="sparkles" size={14} />,
        },
        {
          value: 'models/gemma-4-31b-it',
          label: 'Gemma 4 31B IT',
          triggerLabel: 'Gemma 4 31B IT (Schema-free Fallback)',
          meta: 'Schema-free',
          leading: <Icon name="sparkles" size={14} />,
        },
      ];
  const modelFamily = (() => {
    const lower = model.toLowerCase();
    if (lower.includes('gemma')) return 'Gemma family';
    if (lower.includes('pro')) return 'Gemini Pro family';
    return 'Gemini Flash family';
  })();

  return (
    <div className="fade-in-up">
      <form onSubmit={(e) => { e.preventDefault(); handleSave(); }}>
        <div className="page-header" style={{ marginBottom: 20, display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: 12 }}>
          <div>
            <h1 className="page-title">AI Engine & Model Settings</h1>
            <p className="page-subtitle">API key, model, capture preferences, and live usage.</p>
          </div>
          <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap' }}>
            <button
              type="button"
              onClick={handleTestAPI}
              disabled={testing}
              className="btn btn-secondary"
              style={{ padding: '10px 18px', fontSize: 14, fontWeight: 600, opacity: testing ? 0.6 : undefined, cursor: testing ? 'not-allowed' : undefined }}
            >
              {testing ? 'Testing…' : '⚡ Test'}
            </button>
            <button
              type="submit"
              disabled={saving}
              className="btn btn-primary"
              style={{ padding: '10px 22px', fontSize: 14, fontWeight: 700, opacity: saving ? 0.6 : undefined, cursor: saving ? 'not-allowed' : undefined }}
            >
              {saving ? 'Saving…' : '💾 Save Settings'}
            </button>
          </div>
        </div>

        {statusMessage && (
          <div className={`alert ${statusMessage.startsWith('✓') ? 'alert-success' : statusMessage.startsWith('✕') ? 'alert-error' : 'alert-info'}`} style={{ marginBottom: 16 }}>
            {statusMessage}
          </div>
        )}

        {/* API Key & Model — primary configuration card */}
        <div className="card" style={{ marginBottom: 16 }}>
          <div className="card-header">
            <span className="card-title">🔑 API Key & Model</span>
          </div>
          <div className="card-body">
            <div className="form-group">
              <label className="form-label">Google Gemini API Key</label>
              <div style={{ display: 'flex', gap: 8 }}>
                <input
                  type="password"
                  placeholder="AIzaSy..."
                  value={apiKey}
                  onChange={(e) => setApiKey(e.target.value)}
                  className="form-input"
                  style={{ fontFamily: 'monospace', flex: 1 }}
                />
                <button
                  type="button"
                  onClick={() => fetchAvailableModels(apiKey)}
                  disabled={fetchingModels || !apiKey.trim()}
                  className="btn btn-secondary"
                  title="Refresh model list from Google"
                  style={{
                    padding: '8px 12px',
                    minWidth: 42,
                    opacity: fetchingModels || !apiKey.trim() ? 0.6 : undefined,
                    cursor: fetchingModels || !apiKey.trim() ? 'not-allowed' : undefined,
                  }}
                >
                  {fetchingModels ? '…' : '↻'}
                </button>
              </div>
            </div>

            <div className="form-group" style={{ marginBottom: selectedModelInfo ? 8 : undefined }}>
              <label className="form-label">
                Model {availableModels.length > 0 && `(${availableModels.length} fetched)`}
              </label>
              <CustomSelect
                fullWidth
                ariaLabel="Model"
                value={model}
                onChange={setModel}
                options={modelOptions}
                triggerStyle={{ height: 38 }}
              />
            </div>

            {selectedModelInfo && (
              <div
                title={selectedModelInfo.description}
                style={{ fontSize: 12, color: 'var(--text-muted)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
              >
                Context {fmtTokens(selectedModelInfo.inputTokenLimit)} · Output {fmtTokens(selectedModelInfo.outputTokenLimit)}
                {selectedModelInfo.description ? ` · ${selectedModelInfo.description}` : ''}
              </div>
            )}
          </div>
        </div>

        {/* Capture & Appearance — compact two-column card */}
        <div className="card" style={{ marginBottom: 16 }}>
          <div className="card-header">
            <span className="card-title">🎨 Capture & Appearance</span>
          </div>
          <div className="card-body">
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(240px, 1fr))', gap: 16 }}>
              <div className="form-group" style={{ marginBottom: 0 }}>
                <label className="form-label">Theme</label>
                <div style={{ display: 'flex', gap: 8 }}>
                  {[
                    { id: 'dark', label: '🌙 Dark', desc: 'Dark contrast for low light' },
                    { id: 'light', label: '☀️ Light', desc: 'Bright palette for daytime' },
                    { id: 'auto', label: '💻 Auto', desc: 'Match OS system preference' }
                  ].map(t => (
                    <button
                      key={t.id}
                      type="button"
                      title={t.desc}
                      onClick={() => onThemeChange?.(t.id as any)}
                      className={theme === t.id ? 'btn btn-primary' : 'btn btn-secondary'}
                      style={{ flex: 1, padding: '8px 10px', fontSize: 13, fontWeight: theme === t.id ? 700 : 500 }}
                    >
                      {t.label}
                    </button>
                  ))}
                </div>
              </div>

              <div className="form-group" style={{ marginBottom: 0 }}>
                <label className="form-label">Capture Interval</label>
                <CustomSelect
                  fullWidth
                  ariaLabel="Capture Interval"
                  value={String(screenshotInterval)}
                  onChange={(value) => setScreenshotInterval(parseInt(value, 10))}
                  options={[
                    { value: '15', label: '15s — High frequency', leading: <Icon name="clock" size={14} /> },
                    { value: '30', label: '30s — Active tracking', leading: <Icon name="clock" size={14} /> },
                    { value: '60', label: '60s — Standard (default)', leading: <Icon name="clock" size={14} /> },
                    { value: '120', label: '2m — Low resource', leading: <Icon name="clock" size={14} /> },
                    { value: '300', label: '5m — Minimal', leading: <Icon name="clock" size={14} /> },
                  ]}
                  triggerStyle={{ height: 38 }}
                />
              </div>
            </div>
            <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 12 }}>
              Captures every {screenshotInterval}s (backend: {syncIntervalSeconds}s). Pending logs are batched (up to 10 per request) to keep AI usage low.
            </div>
          </div>
        </div>
      </form>

      {/* Live usage */}
      <div className="card">
        <div className="card-header" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}>
          <span className="card-title">📊 AI Usage</span>
          <span style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0, fontSize: 12, color: 'var(--text-secondary)' }}>
            <span
              style={{ fontWeight: 700, color: 'var(--teal-text)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', maxWidth: 220 }}
              title={selectedModelInfo?.displayName || model.replace('models/', '')}
            >
              {selectedModelInfo?.displayName || model.replace('models/', '')}
            </span>
            <span className="badge badge-productive" style={{ flexShrink: 0 }}>{modelFamily}</span>
          </span>
        </div>
        <div className="card-body">
          <div className="stats-grid" style={{ marginBottom: 14 }}>
            <div className="stat-card">
              <span className="stat-label">AI Requests Today</span>
              <div className="stat-value text-purple" style={{ fontSize: 24 }}>{usageSummary?.total_requests ?? usageCount}</div>
              <div className="stat-sub">Tracked API requests (resets daily)</div>
            </div>
            <div className="stat-card">
              <span className="stat-label">Prompt Tokens</span>
              <div className="stat-value text-cyan" style={{ fontSize: 24 }}>{(usageSummary?.prompt_tokens ?? 0).toLocaleString()}</div>
              <div className="stat-sub">Input prompt tokens</div>
            </div>
            <div className="stat-card">
              <span className="stat-label">Output Tokens</span>
              <div className="stat-value text-amber" style={{ fontSize: 24 }}>{(usageSummary?.candidate_tokens ?? 0).toLocaleString()}</div>
              <div className="stat-sub">AI output tokens</div>
            </div>
            <div className="stat-card">
              <span className="stat-label">Total Tokens</span>
              <div className="stat-value text-green" style={{ fontSize: 24 }}>{(usageSummary?.total_tokens ?? 0).toLocaleString()}</div>
              <div className="stat-sub">Combined API tokens</div>
            </div>
          </div>

          <div style={{ fontSize: 12, color: 'var(--text-secondary)' }}>
            Free-tier RPM/RPD/TPM limits are enforced by Google per model (check <strong>Google AI Studio → Rate limits</strong>); the numbers above are live app usage. Each completed 5-minute window is analyzed as one session bundle with automatic retry and model fallback. Tokens are kept low: screenshots are downscaled, unchanged idle windows reuse the last screenshot, and stable instructions use Gemini context caching when the model supports it.
          </div>
        </div>
      </div>

      {/* Danger zone */}
      <div className="card" style={{ marginTop: 16, border: '1px solid color-mix(in srgb, var(--accent-red) 30%, transparent)' }}>
        <div className="card-header" style={{ borderBottom: '1px solid color-mix(in srgb, var(--accent-red) 20%, transparent)' }}>
          <span className="card-title" style={{ color: 'var(--danger-text)' }}>⚠️ Danger Zone</span>
        </div>
        <div className="card-body" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 16, flexWrap: 'wrap' }}>
          <p style={{ fontSize: 13, color: 'var(--text-secondary)', margin: 0, flex: 1, minWidth: 220 }}>
            Permanently purge local screenshot caches and reset the activity database. Settings and API keys are preserved.
          </p>
          <button
            onClick={handleClearData}
            className="btn"
            style={{
              background: 'color-mix(in srgb, var(--accent-red) 15%, transparent)',
              border: '1px solid var(--accent-red)',
              color: 'var(--danger-text)',
              fontWeight: 600,
              cursor: 'pointer',
              whiteSpace: 'nowrap'
            }}
          >
            🗑️ Clear All Data & Reset
          </button>
        </div>
      </div>
    </div>
  );
}
