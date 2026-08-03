import React, { useState } from 'react';
import { User, Organization } from '../types';
import { apiFetch, buildApiUrl } from '../api';
import logoAsset from '../assets/logo.png';

interface AuthProps {
  onAuthSuccess: (user: User, org: Organization | null) => void;
  onSkip?: () => void;
}

export function AuthPage({ onAuthSuccess, onSkip }: AuthProps) {
  const [isLogin, setIsLogin] = useState(true);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [fullName, setFullName] = useState('');
  const [orgName, setOrgName] = useState('');
  const [loading, setLoading] = useState(false);
  const [oauthLoading, setOauthLoading] = useState<'google' | 'azure' | null>(null);
  const [error, setError] = useState('');

  const pollTimerRef = React.useRef<any>(null);

  React.useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const authErr = params.get('error');
    if (authErr) {
      setError(decodeURIComponent(authErr));
    }

    return () => {
      if (pollTimerRef.current) clearInterval(pollTimerRef.current);
    };
  }, []);

  const startAuthPolling = () => {
    if (pollTimerRef.current) clearInterval(pollTimerRef.current);
    pollTimerRef.current = setInterval(async () => {
      try {
        const res = await apiFetch('/api/auth/me');
        if (res.ok) {
          const data = await res.json();
          if (data.authenticated && data.user) {
            if (pollTimerRef.current) clearInterval(pollTimerRef.current);
            setOauthLoading(null);
            onAuthSuccess(data.user, data.org || null);
          }
        }
      } catch {
        // Continue polling silently until auth completes
      }
    }, 1500);
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
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
        onAuthSuccess(data.user, data.org || null);
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

  return (
    <div className="auth-container">
      <div className="auth-card">
        {/* Logo & Header */}
        <div className="auth-header">
          <img src={logoAsset} alt="get-Hike Logo" style={{ width: 72, height: 72, margin: '0 auto 12px', display: 'block', filter: 'drop-shadow(0 6px 12px rgba(99, 102, 241, 0.4))' }} />
          <h1 className="auth-title">get-Hike</h1>
          <p className="auth-subtitle">
            Privacy-First Linux Productivity Platform & AI Analyzer
          </p>
        </div>

        {/* Tab Selector */}
        <div className="auth-tabs">
          <button
            type="button"
            className={`auth-tab ${isLogin ? 'active' : ''}`}
            onClick={() => { setIsLogin(true); setError(''); }}
          >
            Sign In
          </button>
          <button
            type="button"
            className={`auth-tab ${!isLogin ? 'active' : ''}`}
            onClick={() => { setIsLogin(false); setError(''); }}
          >
            Create Account
          </button>
        </div>

        {/* Error Alert */}
        {error && <div className="auth-error-alert">{error}</div>}

        {/* Social SSO OAuth Buttons */}
        <div className="oauth-button-group">
          <button
            type="button"
            className="btn-oauth btn-google"
            onClick={() => handleOAuth('google')}
            disabled={loading || oauthLoading !== null}
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
            <span>{oauthLoading === 'google' ? 'Connecting to Google...' : `${isLogin ? 'Sign in' : 'Sign up'} with Google`}</span>
          </button>

          <button
            type="button"
            className="btn-oauth btn-azure"
            onClick={() => handleOAuth('azure')}
            disabled={loading || oauthLoading !== null}
          >
            <svg width="18" height="18" viewBox="0 0 23 23" className="oauth-icon">
              <path fill="#f35325" d="M1 1h10v10H1z" />
              <path fill="#81bc06" d="M12 1h10v10H12z" />
              <path fill="#05a6f0" d="M1 12h10v10H1z" />
              <path fill="#ffba08" d="M12 12h10v10H12z" />
            </svg>
            <span>{oauthLoading === 'azure' ? 'Connecting to Azure AD...' : `${isLogin ? 'Sign in' : 'Sign up'} with Azure AD`}</span>
          </button>
        </div>

        <div className="auth-divider">
          <span>Or with email</span>
        </div>

        {/* Email & Password Form */}
        <form onSubmit={handleSubmit} className="auth-form">
          {!isLogin && (
            <>
              <div className="form-group">
                <label className="form-label">Full Name</label>
                <input
                  type="text"
                  className="form-input"
                  placeholder="John Doe"
                  value={fullName}
                  onChange={(e) => setFullName(e.target.value)}
                  required={!isLogin}
                />
              </div>

              <div className="form-group">
                <label className="form-label">Organization / Workspace Name</label>
                <input
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
            <label className="form-label">Work Email</label>
            <input
              type="email"
              className="form-input"
              placeholder="you@company.com"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
            />
          </div>

          <div className="form-group">
            <label className="form-label">Password</label>
            <input
              type="password"
              className="form-input"
              placeholder="••••••••••••"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
          </div>

          <button
            type="submit"
            className="btn-auth-submit"
            disabled={loading}
          >
            {loading ? (
              <span className="spinner-sm" />
            ) : (
              isLogin ? 'Sign In to Account' : 'Create Organization Account'
            )}
          </button>
        </form>

        {/* Guest Local Desktop Mode Fallback */}
        {onSkip && (
          <div style={{ marginTop: 20, textAlign: 'center' }}>
            <button
              type="button"
              className="btn-link-guest"
              onClick={onSkip}
            >
              💻 Continue as Guest (Local Offline Desktop Mode)
            </button>
          </div>
        )}
      </div>
    </div>
  );
}
