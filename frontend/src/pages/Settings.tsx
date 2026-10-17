import { useState, useEffect, useCallback } from 'react';
import { apiFetch } from '../api';

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
  const [model, setModel] = useState(() => localStorage.getItem('mini_ai_model') || 'models/gemma-4-31b-it');
  const [availableModels, setAvailableModels] = useState<AIModel[]>([]);
  const [fetchingModels, setFetchingModels] = useState<boolean>(false);
  const [usageSummary, setUsageSummary] = useState<{ total_requests: number; prompt_tokens: number; candidate_tokens: number; total_tokens: number } | null>(null);
  const [usageCount, setUsageCount] = useState<number>(() => {
    return parseInt(localStorage.getItem('mini_ai_usage_count') || '0', 10);
  });
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [statusMessage, setStatusMessage] = useState<string | null>(null);
  const [nextSyncSeconds, setNextSyncSeconds] = useState<number>(Math.max(5, syncIntervalSeconds));

  const fetchUsageSummary = useCallback(async () => {
    try {
      const res = await apiFetch('/api/user/usage');
      if (res.ok) {
        const data = await res.json();
        setUsageSummary(data);
      }
    } catch {}
  }, []);

  // 1-second countdown timer for next AI sync interval & usage count sync
  useEffect(() => {
    fetchUsageSummary();
    const syncUsage = () => {
      const stored = parseInt(localStorage.getItem('mini_ai_usage_count') || '0', 10);
      setUsageCount(stored);
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

    // Derive the countdown from the real configured capture/sync interval
    const intervalSeconds = Math.max(5, syncIntervalSeconds || 30);
    setNextSyncSeconds(intervalSeconds);
    const timer = setInterval(() => {
      setNextSyncSeconds(prev => (prev <= 1 ? intervalSeconds : prev - 1));
    }, 1000);
    const usageTimer = setInterval(syncUsage, 20000);

    return () => {
      clearInterval(timer);
      clearInterval(usageTimer);
      window.removeEventListener('ai_usage_updated', handleCustomUsageEvent);
      window.removeEventListener('storage', syncUsage);
    };
  }, [fetchUsageSummary, syncIntervalSeconds]);

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
        const newUsage = parseInt(localStorage.getItem('mini_ai_usage_count') || '0', 10) + 1;
        localStorage.setItem('mini_ai_usage_count', newUsage.toString());
        setUsageCount(newUsage);
        window.dispatchEvent(new CustomEvent('ai_usage_updated', { detail: newUsage }));
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
      localStorage.setItem('mini_ai_usage_count', '0');
      setUsageCount(0);
      setStatusMessage('✓ Successfully deleted all local screenshots and reset database!');
      setTimeout(() => window.location.reload(), 1500);
    } catch {
      setStatusMessage('✕ Failed to clear local data.');
    }
  };

  const selectedModelInfo = availableModels.find(m => m.name === model || m.name === `models/${model}`);

  return (
    <div className="fade-in-up">
      <form onSubmit={(e) => { e.preventDefault(); handleSave(); }}>
        <div className="page-header" style={{ marginBottom: 24, display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: 16 }}>
          <div>
            <h1 className="page-title">AI Engine & Model Settings</h1>
            <p className="page-subtitle">
              Fetch models, rate limits, configure API keys, and track direct client-side usage.
            </p>
          </div>
          <button
            type="submit"
            disabled={saving}
            className="btn btn-primary"
            style={{ padding: '10px 24px', fontSize: 14, fontWeight: 700, opacity: saving ? 0.6 : undefined, cursor: saving ? 'not-allowed' : undefined }}
          >
            {saving ? 'Saving…' : '💾 Save Settings & Selected Model'}
          </button>
        </div>

        {statusMessage && (
          <div className={`alert ${statusMessage.startsWith('✓') ? 'alert-success' : statusMessage.startsWith('✕') ? 'alert-error' : 'alert-info'}`} style={{ marginBottom: 20 }}>
            {statusMessage}
          </div>
        )}

        {/* Appearance & Color Theme System Options */}
        <div className="card" style={{ marginBottom: 24 }}>
          <div className="card-header">
            <span className="card-title">🎨 Appearance & Theme System</span>
          </div>
          <div className="card-body">
            <p style={{ fontSize: 13, color: 'var(--text-secondary)', marginBottom: 16 }}>
              Choose your interface color system: Dark Mode, Light Mode, or Auto System Preference matching your operating system.
            </p>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(140px, 1fr))', gap: 12 }}>
              {[
                { id: 'dark', title: '🌙 Dark Mode', desc: 'Deep dark contrast optimized for low light' },
                { id: 'light', title: '☀️ Light Mode', desc: 'Clean bright palette for daytime clarity' },
                { id: 'auto', title: '💻 Auto System', desc: 'Sync automatically with OS preferences' }
              ].map(t => (
                <div
                  key={t.id}
                  onClick={() => onThemeChange?.(t.id as any)}
                  style={{
                    padding: 14,
                    borderRadius: 'var(--radius-md)',
                    border: '1.5px solid',
                    borderColor: theme === t.id ? 'var(--accent-primary)' : 'var(--border-subtle)',
                    background: theme === t.id ? 'var(--bg-elevated)' : 'var(--bg-surface)',
                    cursor: 'pointer',
                    transition: 'var(--transition)',
                    display: 'flex',
                    flexDirection: 'column',
                    gap: 4
                  }}
                >
                  <div style={{ fontWeight: 700, fontSize: 13, color: theme === t.id ? 'var(--accent-primary-text)' : 'var(--text-primary)' }}>
                    {t.title}
                  </div>
                  <div style={{ fontSize: 11, color: 'var(--text-secondary)' }}>
                    {t.desc}
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>

        {/* Screenshot Capture Frequency Settings Card */}
        <div className="card" style={{ marginBottom: 24 }}>
          <div className="card-header">
            <span className="card-title">📷 Screenshot Capture Frequency</span>
          </div>
          <div className="card-body">
            <div className="form-group">
              <label className="form-label">Automatic Capture Interval</label>
              <select
                value={screenshotInterval}
                onChange={(e) => setScreenshotInterval(parseInt(e.target.value, 10))}
                className="form-select"
              >
                <option value={15}>Every 15 Seconds (High Frequency Testing)</option>
                <option value={30}>Every 30 Seconds (Active Tracking)</option>
                <option value={60}>Every 1 Minute (Standard Balanced Default)</option>
                <option value={120}>Every 2 Minutes (Low Resource Saver)</option>
                <option value={300}>Every 5 Minutes (Minimal Sync Mode)</option>
              </select>
            </div>
            <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
              ⏱️ Controls how often the background activity tracker captures desktop screenshots and keyboard entropy for AI analysis.
            </div>
          </div>
        </div>

        {/* Model Selection & API Key Card */}
        <div className="card" style={{ marginBottom: 24 }}>
          <div className="card-header">
            <span className="card-title">API Key & Dynamic Model Selection</span>
          </div>
          <div className="card-body">
            <div className="form-group">
              <label className="form-label">Google Gemini API Key</label>
              <div style={{ display: 'flex', gap: 10 }}>
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
                  style={{
                    whiteSpace: 'nowrap',
                    opacity: fetchingModels || !apiKey.trim() ? 0.6 : undefined,
                    cursor: fetchingModels || !apiKey.trim() ? 'not-allowed' : undefined,
                  }}
                >
                  {fetchingModels ? 'Fetching Models...' : '🔄 Fetch Models'}
                </button>
              </div>
            </div>

            <div className="form-group">
              <label className="form-label">
                Selected AI Model {availableModels.length > 0 && `(${availableModels.length} models fetched)`}
              </label>
              <select
                value={model}
                onChange={(e) => setModel(e.target.value)}
                className="form-select"
              >
                {availableModels.length > 0 ? (
                  availableModels.map(m => (
                    <option key={m.name} value={m.name}>
                      {m.displayName} (Input Limit: {m.inputTokenLimit.toLocaleString()} tokens)
                    </option>
                  ))
                ) : (
                  <>
                    <option value="models/gemma-4-31b-it">Gemma 4 31B IT (Fast & Recommended)</option>
                    <option value="models/gemini-2.5-pro">Gemini 2.5 Pro (Deep Reasoning)</option>
                    <option value="models/gemini-2.0-flash">Gemini 2.0 Flash (Open Multimodal Fallback)</option>
                    <option value="models/gemini-2.5-flash">Gemini 2.5 Flash (Balanced)</option>
                  </>
                )}
              </select>
            </div>

            {selectedModelInfo && (
              <div style={{ padding: 12, background: 'var(--bg-elevated)', borderRadius: 'var(--radius-md)', marginBottom: 16, border: '1px solid var(--border-subtle)' }}>
                <div style={{ fontWeight: 600, fontSize: 13, color: 'var(--teal-text)', marginBottom: 4 }}>
                  {selectedModelInfo.displayName} Limits & Specifications:
                </div>
                <div style={{ fontSize: 12, color: 'var(--text-secondary)' }}>
                  • <strong>Input Context Limit:</strong> {selectedModelInfo.inputTokenLimit ? selectedModelInfo.inputTokenLimit.toLocaleString() : 'N/A'} tokens<br />
                  • <strong>Output Token Limit:</strong> {selectedModelInfo.outputTokenLimit ? selectedModelInfo.outputTokenLimit.toLocaleString() : 'N/A'} tokens<br />
                  • <strong>Description:</strong> {selectedModelInfo.description || 'Google Gemini AI Generative Model.'}
                </div>
              </div>
            )}

            <div style={{ display: 'flex', gap: 12, marginTop: 20 }}>
              <button
                type="submit"
                disabled={saving}
                className="btn btn-primary"
                style={{ padding: '10px 24px', fontSize: 14, fontWeight: 700, opacity: saving ? 0.6 : undefined, cursor: saving ? 'not-allowed' : undefined }}
              >
                💾 Save Configuration & Selected Model
              </button>
              <button type="button" onClick={handleTestAPI} className="btn btn-secondary">
                ⚡ Test API & Selected Model
              </button>
            </div>
          </div>
        </div>
      </form>

      {/* Live usage & static provider rate-limit reference */}
      {(() => {
        const modelLower = model.toLowerCase();
        const isGemma = modelLower.includes('gemma');
        const isPro = modelLower.includes('pro');

        return (
          <div className="card">
            <div className="card-header">
              <span className="card-title">📊 AI Usage & Provider Rate Limits</span>
            </div>
            <div className="card-body">
              {/* Active Model Header Banner */}
              <div style={{ padding: 14, background: 'var(--bg-elevated)', borderRadius: 'var(--radius-md)', marginBottom: 20, display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
                <div style={{ minWidth: 0 }}>
                  <div style={{ fontSize: 11, textTransform: 'uppercase', color: 'var(--text-muted)', fontWeight: 700, letterSpacing: 0.5 }}>Currently Active Model</div>
                  <div
                    style={{ fontSize: 16, fontWeight: 700, color: 'var(--teal-text)', marginTop: 2, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
                    title={selectedModelInfo?.displayName || model.replace('models/', '')}
                  >
                    {selectedModelInfo?.displayName || model.replace('models/', '')}
                  </div>
                </div>
                <div className="badge badge-productive" style={{ fontSize: 12, flexShrink: 0 }}>
                  {isGemma ? 'Gemma family' : isPro ? 'Gemini Pro family' : 'Gemini Flash family'}
                </div>
              </div>

              {/* Live tracked usage stat cards */}
              <div className="stats-grid" style={{ marginBottom: 20 }}>
                <div className="stat-card" style={{ border: '1px solid var(--accent-purple)', background: 'var(--bg-elevated)' }}>
                  <span className="stat-label" style={{ color: 'var(--teal-text)', fontWeight: 700 }}>⏳ Next AI Sync In</span>
                  <div className="stat-value text-purple" style={{ fontSize: 24, fontWeight: 800, fontVariantNumeric: 'tabular-nums' }}>
                    {nextSyncSeconds}s
                  </div>
                  <div className="stat-sub">Based on your {syncIntervalSeconds}s capture interval</div>
                </div>
                <div className="stat-card">
                  <span className="stat-label">Total AI Requests</span>
                  <div className="stat-value text-purple" style={{ fontSize: 24 }}>{usageSummary?.total_requests ?? usageCount}</div>
                  <div className="stat-sub">Tracked API requests</div>
                </div>
                <div className="stat-card">
                  <span className="stat-label">Prompt Tokens</span>
                  <div className="stat-value text-cyan" style={{ fontSize: 24 }}>{(usageSummary?.prompt_tokens ?? 0).toLocaleString()}</div>
                  <div className="stat-sub">Input prompt tokens</div>
                </div>
                <div className="stat-card">
                  <span className="stat-label">Candidate Tokens</span>
                  <div className="stat-value text-amber" style={{ fontSize: 24 }}>{(usageSummary?.candidate_tokens ?? 0).toLocaleString()}</div>
                  <div className="stat-sub">AI output tokens</div>
                </div>
                <div className="stat-card">
                  <span className="stat-label">Total Tokens Tracked</span>
                  <div className="stat-value text-green" style={{ fontSize: 24 }}>{(usageSummary?.total_tokens ?? 0).toLocaleString()}</div>
                  <div className="stat-sub">Combined API tokens</div>
                </div>
              </div>

              {/* Provider-limits note + batching behaviour */}
              <div style={{ padding: 14, background: 'var(--bg-elevated)', borderRadius: 'var(--radius-md)', border: '1px solid var(--border-subtle)', fontSize: 12, color: 'var(--text-secondary)' }}>
                <div style={{ fontWeight: 600, color: 'var(--teal-text)', marginBottom: 4, display: 'flex', alignItems: 'center', gap: 6 }}>
                  ℹ️ Provider rate-limit reference (not live quota)
                </div>
                <div>
                  • Free-tier RPM / RPD / TPM limits are enforced by Google per model and are <strong>not</strong> tracked or displayed here — check <strong>Google AI Studio → Rate limits</strong> for the current limits of <code>{model.replace('models/', '')}</code>.<br />
                  • The token and request numbers above are live usage accumulated by this app.<br />
                  • <strong>Dynamic Batching:</strong> Captures are bundled based on the capture interval ({screenshotInterval}s) to reduce API calls.
                </div>
              </div>
            </div>
          </div>
        );
      })()}

      {/* Danger Zone: Separate & Hidden from Primary Action Bar */}
      <div className="card" style={{ marginTop: 24, border: '1px solid color-mix(in srgb, var(--accent-red) 30%, transparent)' }}>
        <div className="card-header" style={{ borderBottom: '1px solid color-mix(in srgb, var(--accent-red) 20%, transparent)' }}>
          <span className="card-title" style={{ color: 'var(--danger-text)' }}>⚠️ Danger Zone - Data Purge</span>
        </div>
        <div className="card-body">
          <p style={{ fontSize: 13, color: 'var(--text-secondary)', marginBottom: 16 }}>
            Permanently purge local screenshot caches and reset the activity database. All settings and API keys are preserved.
          </p>
          <button
            onClick={handleClearData}
            className="btn"
            style={{
              background: 'color-mix(in srgb, var(--accent-red) 15%, transparent)',
              border: '1px solid var(--accent-red)',
              color: 'var(--danger-text)',
              fontWeight: 600,
              cursor: 'pointer'
            }}
          >
            🗑️ Clear All Stored Data & Reset Database
          </button>
        </div>
      </div>
    </div>
  );
}
