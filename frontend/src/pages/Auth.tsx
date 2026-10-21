import React, { useState } from 'react';
import { User, Organization } from '../types';
import { apiFetch, buildApiUrl } from '../api';
import { Alert } from '../components/Alert';
import logoAsset from '../assets/logo.png';

interface AuthProps {
  onAuthSuccess: (user: User, org: Organization | null, token?: string) => void;
  onSkip?: () => void;
  backendUnavailable?: boolean;
  onRetryConnection?: () => Promise<boolean>;
}

export function AuthPage({ onAuthSuccess, onSkip, backendUnavailable = false, onRetryConnection }: AuthProps) {
  const [isLogin, setIsLogin] = useState(true);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [fullName, setFullName] = useState('');
  const [orgName, setOrgName] = useState('');
  const [loading, setLoading] = useState(false);
  const [oauthLoading, setOauthLoading] = useState<'google' | 'azure' | null>(null);
  const [error, setError] = useState('');
  const [retryingConnection, setRetryingConnection] = useState(false);
  const [retryFailed, setRetryFailed] = useState(false);

  const pollTimerRef = React.useRef<any>(null);
  const oauthTimeoutRef = React.useRef<any>(null);
  const pollCountRef = React.useRef(0);

  React.useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const authErr = params.get('error');
    if (authErr) {
      try {
        setError(decodeURIComponent(authErr));
      } catch {
        setError(authErr);
      }
    }

    return () => {
      if (pollTimerRef.current) clearInterval(pollTimerRef.current);
      if (oauthTimeoutRef.current) clearTimeout(oauthTimeoutRef.current);
    };
  }, []);

  const stopAuthPolling = () => {
    if (pollTimerRef.current) {
      clearInterval(pollTimerRef.current);
      pollTimerRef.current = null;
    }
    if (oauthTimeoutRef.current) {
      clearTimeout(oauthTimeoutRef.current);
      oauthTimeoutRef.current = null;
    }
  };

  const [pollCount, setPollCount] = useState(0);

  const startAuthPolling = () => {
    if (pollTimerRef.current) clearInterval(pollTimerRef.current);
    pollCountRef.current = 0;
    setPollCount(0);
    pollTimerRef.current = setInterval(async () => {
      pollCountRef.current += 1;
      setPollCount(pollCountRef.current);
      if (pollCountRef.current > 40) {
        stopAuthPolling();
        setOauthLoading(null);
        setError('Sign-in was cancelled or timed out. Try again.');
        return;
      }
      try {
        const res = await apiFetch('/api/auth/me');
        if (res.ok) {
          const data = await res.json();
          if (data.authenticated && data.user) {
            stopAuthPolling();
            setOauthLoading(null);
            onAuthSuccess(data.user, data.org || null);
          }
        }
      } catch {
        // Continue polling silently until auth completes
      }
    }, 1500);
  };

  const cancelOAuth = () => {
    stopAuthPolling();
    setOauthLoading(null);
    setPollCount(0);
  };

  const getPasswordStrength = (pwd: string) => {
    if (!pwd) return { score: 0, text: '', color: 'transparent' };
    let score = 0;
    if (pwd.length >= 8) score++;
    if (pwd.length >= 12) score++;
    if (/[A-Z]/.test(pwd) && /[a-z]/.test(pwd)) score++;
    if (/[0-9]/.test(pwd)) score++;
    if (/[^A-Za-z0-9]/.test(pwd)) score++;

    if (score <= 2) return { score: 1, text: 'Weak (min 8 chars, mix numbers & letters recommended)', color: 'var(--accent-red)' };
    if (score <= 3) return { score: 2, text: 'Medium', color: 'var(--accent-amber)' };
    return { score: 3, text: 'Strong', color: 'var(--accent-green)' };
  };

  const passwordStrength = getPasswordStrength(password);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');

    if (!isLogin && password.length < 8) {
      setError('Password must be at least 8 characters long.');
      return;
    }

    setLoading(true);

    const endpoint = isLogin ? '/api/auth/login' : '/api/org/register';
    const payload = isLogin
      ? { email, password }
      : { name: orgName || 'My Workspace', email, password, full_name: fullName };

    try {
      const res = await apiFetch(endpoint, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });

      const data = await res.json();
      if (res.ok && data.success) {
        onAuthSuccess(data.user, data.org || null, data.token);
      } else {
        setError(data.error || data.message || 'Authentication failed. Please check your credentials.');
      }
    } catch (err: any) {
      setError('Network error. Unable to reach authentication server.');
    } finally {
      setLoading(false);
    }
  };

  const handleOAuth = (provider: 'google' | 'azure') => {
    setError('');
    setOauthLoading(provider);

    const redirectTarget = encodeURIComponent('/auth-success');
    const targetUrl = buildApiUrl(`/api/auth/oauth/${provider}?redirect=${redirectTarget}`);

    // Begin background polling for login completion
    startAuthPolling();
    oauthTimeoutRef.current = setTimeout(() => {
      stopAuthPolling();
      setOauthLoading(null);
      setError('Sign-in was cancelled or timed out. Try again.');
    }, 60000);

    // Trigger external system browser opening to avoid webview disallowed_useragent errors
    if ((window as any).runtime?.BrowserOpenURL) {
      (window as any).runtime.BrowserOpenURL(targetUrl);
    } else {
      const opened = window.open(targetUrl, '_blank');
      if (!opened) {
        window.location.href = targetUrl;
      }
    }
  };

  const handleRetryConnection = async () => {
    if (!onRetryConnection) return;
    setRetryingConnection(true);
    setRetryFailed(false);
    const reachable = await onRetryConnection();
    setRetryingConnection(false);
    if (!reachable) setRetryFailed(true);
  };

  if (backendUnavailable) {
    return (
      <div className="auth-container">
        <div className="card fade-in-up" style={{ width: '100%', maxWidth: 400, margin: 'auto', flexShrink: 0 }}>
          <div className="card-header" style={{ justifyContent: 'center', gap: 10 }}>
            <img
              src={logoAsset}
              alt="get-Hike Logo"
              style={{ width: 26, height: 26, display: 'block', filter: 'drop-shadow(0 4px 8px color-mix(in srgb, var(--accent-indigo) 40%, transparent))' }}
            />
            <span className="card-title" style={{ fontSize: 14, textTransform: 'none', letterSpacing: 0, color: 'var(--text-primary)' }}>
              get-Hike
            </span>
          </div>

          <div className="card-body" style={{ textAlign: 'center' }}>
            <div style={{ fontSize: 15, fontWeight: 700, marginBottom: 8 }}>Server unavailable</div>
            <p style={{ fontSize: 13, color: 'var(--text-secondary)', lineHeight: 1.55, marginBottom: 16 }}>
              We couldn't connect to the get-Hike server. You can continue in <strong>Guest Mode</strong> — tracking and
              AI analysis run locally on this device and your data stays private.
            </p>

            {onSkip && (
              <button
                type="button"
                className="btn btn-primary w-full"
                onClick={onSkip}
                style={{ padding: '10px 16px', fontWeight: 700 }}
              >
                Continue in Guest Mode
              </button>
            )}

            {onRetryConnection && (
              <button
                type="button"
                className="btn btn-secondary w-full"
                onClick={handleRetryConnection}
                disabled={retryingConnection}
                style={{ marginTop: 8, opacity: retryingConnection ? 0.6 : undefined, cursor: retryingConnection ? 'wait' : undefined }}
              >
                {retryingConnection ? 'Checking connection…' : 'Retry connection'}
              </button>
            )}

            {retryFailed && (
              <div className="alert alert-error" style={{ marginTop: 12, fontSize: 12 }} role="alert">
                Still can't reach the server. Check your network or server address and try again.
              </div>
            )}

            <p style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 14, lineHeight: 1.5 }}>
              Sign-in, team sync, and shared analytics will be available once the server is reachable.
            </p>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="auth-container">
      <div className="card fade-in-up" style={{ width: '100%', maxWidth: 400, margin: 'auto', flexShrink: 0 }}>
        {/* Logo & Header */}
        <div className="card-header" style={{ justifyContent: 'center', gap: 10 }}>
          <img
            src={logoAsset}
            alt="get-Hike Logo"
            style={{ width: 26, height: 26, display: 'block', filter: 'drop-shadow(0 4px 8px color-mix(in srgb, var(--accent-indigo) 40%, transparent))' }}
          />
          <span className="card-title" style={{ fontSize: 14, textTransform: 'none', letterSpacing: 0, color: 'var(--text-primary)' }}>
            get-Hike
          </span>
        </div>

        <div className="card-body">
          {/* One-line subtitle */}
          <div
            title="Privacy-First Linux Productivity Platform & AI Analyzer"
            style={{ fontSize: 12, color: 'var(--text-muted)', textAlign: 'center', marginBottom: 14, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
          >
            Privacy-first productivity & AI analyzer
          </div>

          {/* Segmented Sign in / Register selector */}
          <div role="tablist" style={{ display: 'flex', gap: 8, marginBottom: 14 }}>
            <button
              type="button"
              role="tab"
              aria-selected={isLogin}
              className={isLogin ? 'btn btn-primary' : 'btn btn-secondary'}
              style={{ flex: 1, padding: '8px 10px', fontSize: 13, fontWeight: isLogin ? 700 : 500 }}
              onClick={() => { setIsLogin(true); setError(''); }}
            >
              Sign in
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={!isLogin}
              className={!isLogin ? 'btn btn-primary' : 'btn btn-secondary'}
              style={{ flex: 1, padding: '8px 10px', fontSize: 13, fontWeight: !isLogin ? 700 : 500 }}
              onClick={() => { setIsLogin(false); setError(''); }}
            >
              Register
            </button>
          </div>

          {/* Error Alert */}
          {error && <Alert type="error" message={error} style={{ marginBottom: 14 }} />}

          {/* Social SSO OAuth Buttons */}
          <div className="oauth-button-group">
            <button
              type="button"
              className="btn-oauth btn-google"
              onClick={() => handleOAuth('google')}
              disabled={loading || oauthLoading !== null}
              title={`${isLogin ? 'Sign in' : 'Sign up'} with Google`}
              style={{ opacity: loading || oauthLoading !== null ? 0.6 : undefined, cursor: loading || oauthLoading !== null ? 'not-allowed' : undefined }}
            >
              <svg width="18" height="18" viewBox="0 0 24 24" className="oauth-icon">
                <path
                  fill="#4285F4"
                  d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92c-.26 1.37-1.04 2.53-2.21 3.31v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.09z"
                />
                <path
                  fill="#34A853"
                  d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z"
                />
                <path
                  fill="#FBBC05"
                  d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.06H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.94l2.85-2.22.81-.63z"
                />
                <path
                  fill="#EA4335"
                  d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.06l3.66 2.84c.87-2.6 3.3-4.52 6.16-4.52z"
                />
              </svg>
              <span>{oauthLoading === 'google' ? 'Connecting...' : 'Google'}</span>
            </button>

            <button
              type="button"
              className="btn-oauth btn-azure"
              onClick={() => handleOAuth('azure')}
              disabled={loading || oauthLoading !== null}
              title={`${isLogin ? 'Sign in' : 'Sign up'} with Azure AD`}
              style={{ opacity: loading || oauthLoading !== null ? 0.6 : undefined, cursor: loading || oauthLoading !== null ? 'not-allowed' : undefined }}
            >
              <svg width="18" height="18" viewBox="0 0 23 23" className="oauth-icon">
                <path fill="#f35325" d="M1 1h10v10H1z" />
                <path fill="#81bc06" d="M12 1h10v10H12z" />
                <path fill="#05a6f0" d="M1 12h10v10H1z" />
                <path fill="#ffba08" d="M12 12h10v10H12z" />
              </svg>
              <span>{oauthLoading === 'azure' ? 'Connecting...' : 'Azure AD'}</span>
            </button>
          </div>

          {oauthLoading && (
            <div style={{ textAlign: 'center', marginTop: 12, padding: '10px 14px', borderRadius: 'var(--radius-sm)', background: 'var(--bg-elevated)', border: '1px solid var(--border-subtle)' }}>
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 8, fontSize: 13, color: 'var(--text-primary)', marginBottom: 6 }}>
                <span className="spinner-sm" />
                <span>Waiting for {oauthLoading === 'google' ? 'Google' : 'Azure'} sign-in...</span>
              </div>
              <div style={{ width: '100%', height: 4, background: 'var(--border-subtle)', borderRadius: 2, overflow: 'hidden', marginBottom: 6 }}>
                <div style={{ width: `${Math.min(100, Math.round((pollCount / 40) * 100))}%`, height: '100%', background: 'var(--accent-primary)', transition: 'width 0.3s ease' }} />
              </div>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', fontSize: 11, color: 'var(--text-muted)' }}>
                <span>Checking credentials ({pollCount}/40)</span>
                <button type="button" className="btn-link-guest" onClick={cancelOAuth} style={{ fontSize: 11 }}>
                  Cancel sign-in
                </button>
              </div>
            </div>
          )}

          <div className="auth-divider">
            <span>Or with email</span>
          </div>

          {/* Email & Password Form */}
          <form onSubmit={handleSubmit}>
            {!isLogin && (
              <>
                <div className="form-group">
                  <label className="form-label" htmlFor="auth-fullname">Full Name</label>
                  <input
                    id="auth-fullname"
                    type="text"
                    className="form-input"
                    placeholder="John Doe"
                    autoComplete="name"
                    value={fullName}
                    onChange={(e) => setFullName(e.target.value)}
                    required={!isLogin}
                  />
                </div>

                <div className="form-group">
                  <label className="form-label" htmlFor="auth-orgname">Organization / Workspace Name</label>
                  <input
                    id="auth-orgname"
                    type="text"
                    className="form-input"
                    placeholder="Acme Corp"
                    value={orgName}
                    onChange={(e) => setOrgName(e.target.value)}
                    required={!isLogin}
                  />
                </div>
              </>
            )}

            <div className="form-group">
              <label className="form-label" htmlFor="auth-email">Work Email</label>
              <input
                id="auth-email"
                type="email"
                className="form-input"
                placeholder="you@company.com"
                autoComplete="email"
                value={email}
                onChange={(e) => { setEmail(e.target.value); if (error) setError(''); }}
                required
              />
            </div>

            <div className="form-group" style={{ marginBottom: 14 }}>
              <label className="form-label" htmlFor="auth-password">Password</label>
              <div style={{ position: 'relative' }}>
                <input
                  id="auth-password"
                  type={showPassword ? 'text' : 'password'}
                  className="form-input"
                  placeholder="••••••••••••"
                  autoComplete={isLogin ? 'current-password' : 'new-password'}
                  value={password}
                  onChange={(e) => { setPassword(e.target.value); if (error) setError(''); }}
                  required
                  style={{ paddingRight: 44, width: '100%' }}
                />
                <button
                  type="button"
                  aria-label={showPassword ? 'Hide password' : 'Show password'}
                  onClick={() => setShowPassword((v) => !v)}
                  style={{ position: 'absolute', right: 10, top: '50%', transform: 'translateY(-50%)', background: 'none', border: 'none', color: 'var(--text-muted)', cursor: 'pointer', padding: 4, fontSize: 12 }}
                >
                  {showPassword ? 'Hide' : 'Show'}
                </button>
              </div>
              {!isLogin && password && (
                <div style={{ marginTop: 6, fontSize: 11, display: 'flex', alignItems: 'center', gap: 6 }}>
                  <div style={{ flex: '0 0 48px', height: 4, background: 'var(--border-subtle)', borderRadius: 2, overflow: 'hidden' }}>
                    <div style={{ width: `${(passwordStrength.score / 3) * 100}%`, height: '100%', background: passwordStrength.color, transition: 'all 0.2s ease' }} />
                  </div>
                  <span style={{ color: passwordStrength.color, fontWeight: 500 }}>
                    {passwordStrength.text}
                  </span>
                </div>
              )}
            </div>

            <button
              type="submit"
              className="btn btn-primary w-full"
              disabled={loading}
              style={{ padding: '10px 16px', fontWeight: 700, opacity: loading ? 0.6 : undefined, cursor: loading ? 'not-allowed' : undefined }}
            >
              {loading ? (
                <>
                  <span className="spinner-sm" /> {isLogin ? 'Signing in…' : 'Creating account…'}
                </>
              ) : (
                isLogin ? 'Sign In' : 'Create Account'
              )}
            </button>
          </form>
        </div>

        {/* Guest Local Desktop Mode Fallback */}
        {onSkip && (
          <div style={{ padding: '0 18px 16px', textAlign: 'center' }}>
            <button
              type="button"
              className="btn-link-guest"
              onClick={onSkip}
              title="Continue as Guest (Local Offline Desktop Mode)"
              style={{ fontSize: 12 }}
            >
              💻 Continue as Guest (Local Mode)
            </button>
          </div>
        )}
      </div>
    </div>
  );
}
