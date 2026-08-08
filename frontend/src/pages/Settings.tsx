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
}

export function SettingsPage({ theme = 'auto', onThemeChange }: SettingsPageProps) {
  const [apiKey, setApiKey] = useState(() => localStorage.getItem('mini_gemini_api_key') || '');
  const [model, setModel] = useState(() => localStorage.getItem('mini_ai_model') || 'models/gemma-4-31b-it');
  const [availableModels, setAvailableModels] = useState<AIModel[]>([]);
  const [fetchingModels, setFetchingModels] = useState<boolean>(false);
  const [usageSummary, setUsageSummary] = useState<{ total_requests: number; prompt_tokens: number; candidate_tokens: number; total_tokens: number } | null>(null);
  const [usageCount, setUsageCount] = useState<number>(() => {
    return parseInt(localStorage.getItem('mini_ai_usage_count') || '0', 10);
  });
  const [statusMessage, setStatusMessage] = useState<string | null>(null);
  const [nextSyncSeconds, setNextSyncSeconds] = useState<number>(30);

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

    const timer = setInterval(() => {
      setNextSyncSeconds(prev => (prev <= 1 ? 30 : prev - 1));
      syncUsage();
    }, 1000);

    return () => {
      clearInterval(timer);
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
    if (apiKey.trim()) {
      fetchAvailableModels(apiKey);
    }
  }, [apiKey, fetchAvailableModels]);

  const [screenshotInterval, setScreenshotInterval] = useState<number>(() => {
    return parseInt(localStorage.getItem('mini_screenshot_interval') || '60', 10);
  });

  const handleSave = async () => {
    localStorage.setItem('mini_gemini_api_key', apiKey.trim());
    localStorage.setItem('mini_ai_model', model);
    localStorage.setItem('mini_screenshot_interval', screenshotInterval.toString());

    const cleanModel = model.replace('models/', '');
    if ((window as any).go?.main?.App) {
      try {
        if ((window as any).go.main.App.UpdateGeminiAPIKey) {
          await (window as any).go.main.App.UpdateGeminiAPIKey(apiKey.trim());
        }
        if ((window as any).go.main.App.UpdateAIModel) {
          await (window as any).go.main.App.UpdateAIModel(cleanModel);
        }
      } catch { }
    } else {
      try {
        await apiFetch('/api/config', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            gemini_api_key: apiKey.trim(),
            ai_model: model,
            screenshot_interval_seconds: screenshotInterval
          }),
        });
      } catch { }
    }

    setStatusMessage('✓ Settings & Selected Model saved successfully!');
    setTimeout(() => setStatusMessage(null), 3000);
  };

  const handleTestAPI = async () => {
    if (!apiKey.trim()) {
      setStatusMessage('Please enter an API Key first.');
      return;
    }
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
        setUsageCount(prev => prev + 1);
        setStatusMessage(`✓ Connected successfully to ${cleanModel}!`);
      } else {
        const err = await res.json();
        setStatusMessage(`✕ API Error: ${err.error?.message || 'Invalid key or model.'}`);
      }
    } catch {
      setStatusMessage('✕ Connection failed. Check network or API key.');
    }
  };

  const handleClearData = async () => {
    if (!window.confirm('Are you sure you want to delete all stored screenshots, logs, and activity records? This action cannot be undone.')) {
      return;
    }
    try {
      if ((window as any).go?.main?.App) {
        await (window as any).go.main.App.ClearAllLocalData();
      }
      localStorage.removeItem('mini_guest_logs');
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
          <button type="submit" className="btn btn-primary" style={{ padding: '10px 24px', fontSize: 14, fontWeight: 700 }}>
            💾 Save Settings & Selected Model
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
                    borderColor: theme === t.id ? 'var(--accent-purple)' : 'var(--border-subtle)',
                    background: theme === t.id ? 'var(--bg-elevated)' : 'var(--bg-surface)',
                    cursor: 'pointer',
                    transition: 'var(--transition)',
                    display: 'flex',
                    flexDirection: 'column',
                    gap: 4
                  }}
                >
                  <div style={{ fontWeight: 700, fontSize: 13, color: theme === t.id ? 'var(--accent-teal)' : 'var(--text-primary)' }}>
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
                  style={{ whiteSpace: 'nowrap' }}
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
                    <option value="models/gemma-4-31b-it">Gemini 2.5 Flash (Fast & Recommended)</option>
                    <option value="models/gemini-2.5-pro">Gemini 2.5 Pro (Deep Reasoning)</option>
                    <option value="models/Gemma 4 31B IT">Gemini 2.0 Flash</option>
                    <option value="models/gemma-4-31b-it">Gemma 4 31B IT (Open Weights Multimodal Fallback)</option>
                  </>
                )}
              </select>
            </div>

            {selectedModelInfo && (
              <div style={{ padding: 12, background: 'var(--bg-elevated)', borderRadius: 'var(--radius-md)', marginBottom: 16, border: '1px solid var(--border-subtle)' }}>
                <div style={{ fontWeight: 600, fontSize: 13, color: 'var(--accent-teal)', marginBottom: 4 }}>
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
              <button type="submit" className="btn btn-primary" style={{ padding: '10px 24px', fontSize: 14, fontWeight: 700 }}>
                💾 Save Configuration & Selected Model
              </button>
              <button type="button" onClick={handleTestAPI} className="btn btn-secondary">
                ⚡ Test API & Selected Model
              </button>
            </div>
          </div>
        </div>
      </form>

      {/* Active Model Quota & Remaining Capacity Monitor */}
      {(() => {
        const isPro = model.includes('gemini-1.5-pro');
        const isFlash2 = model.includes('Gemma 4 31B IT');

        const rpmLimit = isPro ? 2 : isFlash2 ? 10 : 15;
        const rpdLimit = isPro ? 50 : 1500;
        const tpmLimit = isPro ? '32,000' : isFlash2 ? '4,000,000' : '1,000,000';
        const contextLimit = selectedModelInfo?.inputTokenLimit
          ? selectedModelInfo.inputTokenLimit.toLocaleString()
          : isPro ? '2,097,152' : '1,048,576';

        const remainingRpd = Math.max(0, rpdLimit - usageCount);
        const usedPercent = Math.min(100, Math.round((usageCount / rpdLimit) * 100));

        return (
          <div className="card">
            <div className="card-header">
              <span className="card-title">📊 Active Model Quota & Remaining Capacity</span>
            </div>
            <div className="card-body">
              {/* Active Model Header Banner */}
              <div style={{ padding: 14, background: 'var(--bg-elevated)', borderRadius: 'var(--radius-md)', marginBottom: 20, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                <div>
                  <div style={{ fontSize: 11, textTransform: 'uppercase', color: 'var(--text-muted)', fontWeight: 700, letterSpacing: 0.5 }}>Currently Active Model</div>
                  <div style={{ fontSize: 16, fontWeight: 700, color: 'var(--accent-teal)', marginTop: 2 }}>
                    {selectedModelInfo?.displayName || model.replace('models/', '')}
                  </div>
                </div>
                <div className="badge badge-productive" style={{ fontSize: 12 }}>
                  {rpdLimit.toLocaleString()} RPD Quota
                </div>
              </div>

              {/* Progress & Remaining Stat Cards */}
              <div className="stats-grid" style={{ marginBottom: 20 }}>
                <div className="stat-card" style={{ border: '1px solid var(--accent-purple)', background: 'var(--bg-elevated)' }}>
                  <span className="stat-label" style={{ color: 'var(--accent-teal)', fontWeight: 700 }}>⏳ Next AI Sync In</span>
                  <div className="stat-value text-purple" style={{ fontSize: 24, fontWeight: 800 }}>
                    {nextSyncSeconds}s
                  </div>
                  <div className="stat-sub">Auto screenshot analysis sync</div>
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

              {/* Visual Usage Progress Bar */}
              <div style={{ marginBottom: 16 }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 12, marginBottom: 6, fontWeight: 600 }}>
                  <span style={{ color: 'var(--text-secondary)' }}>Daily Quota Consumption</span>
                  <span style={{ color: 'var(--accent-teal)' }}>{usedPercent}% Used</span>
                </div>
                <div className="entropy-bar-track" style={{ height: 8 }}>
                  <div
                    className="entropy-bar-fill"
                    style={{
                      width: `${usedPercent}%`,
                      background: usedPercent > 80 ? 'var(--accent-red)' : 'var(--accent-purple)'
                    }}
                  />
                </div>
              </div>

              {/* Dynamic Screenshot Bundling & Rate Guard Information */}
              <div style={{ padding: 14, background: 'var(--bg-elevated)', borderRadius: 'var(--radius-md)', border: '1px solid var(--border-subtle)', fontSize: 12, color: 'var(--text-secondary)' }}>
                <div style={{ fontWeight: 600, color: 'var(--accent-teal)', marginBottom: 4, display: 'flex', alignItems: 'center', gap: 6 }}>
                  📦 Multi-Screenshot Adaptive Bundling & Rate Guard Enabled
                </div>
                <div>
                  • <strong>Dynamic Batching:</strong> Automatically bundles captures based on interval ({screenshotInterval}s) to preserve your <code>{rpmLimit} RPM</code> rate limit.<br />
                  • <strong>Request Optimization:</strong> Enforces max 1 batch request per minute with structured JSON payload responses.<br />
                  • <strong>Quota Protection:</strong> Saves up to 75% of your API quota while analyzing activity with high precision.
                </div>
              </div>
            </div>
          </div>
        );
      })()}

      {/* Danger Zone: Separate & Hidden from Primary Action Bar */}
      <div className="card" style={{ marginTop: 24, border: '1px solid rgba(239, 68, 68, 0.3)' }}>
        <div className="card-header" style={{ borderBottom: '1px solid rgba(239, 68, 68, 0.2)' }}>
          <span className="card-title" style={{ color: 'var(--accent-red)' }}>⚠️ Danger Zone - Data Purge</span>
        </div>
        <div className="card-body">
          <p style={{ fontSize: 13, color: 'var(--text-secondary)', marginBottom: 16 }}>
            Permanently purge local screenshot caches and reset the activity database. All settings and API keys are preserved.
          </p>
          <button
            onClick={handleClearData}
            className="btn"
            style={{
              background: 'rgba(239, 68, 68, 0.15)',
              border: '1px solid var(--accent-red)',
              color: 'var(--accent-red)',
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
