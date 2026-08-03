import React, { useState, useEffect } from 'react';
import { apiFetch } from '../api';

interface AcceptInviteProps {
  token: string;
  onSuccess: () => void;
}

export const AcceptInvitePage: React.FC<AcceptInviteProps> = ({ token, onSuccess }) => {
  const [invitation, setInvitation] = useState<{ email: string; org_name: string; role: string } | null>(null);
  const [fullName, setFullName] = useState('');
  const [password, setPassword] = useState('');
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const checkToken = async () => {
      setLoading(true);
      try {
        const res = await apiFetch(`/api/org/invite-info?token=${token}`);
        const data = await res.json();
        if (!res.ok || data.valid === false) {
          throw new Error(data.error || data.message || 'Invalid or expired invitation token');
        }
        setInvitation(data);
      } catch (err: any) {
        setError(err.message || 'Invitation verification failed');
      } finally {
        setLoading(false);
      }
    };
    if (token) {
      checkToken();
    } else {
      setError('No invitation token provided in URL');
      setLoading(false);
    }
  }, [token]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!fullName.trim() || !password) return;

    setSubmitting(true);
    setError(null);

    try {
      const res = await apiFetch('/api/org/accept-invite', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          token,
          full_name: fullName.trim(),
          password
        })
      });

      const data = await res.json();
      if (!res.ok) {
        throw new Error(data.message || data.error || 'Failed to accept invitation');
      }

      onSuccess();
    } catch (err: any) {
      setError(err.message || 'Error creating user account');
    } finally {
      setSubmitting(false);
    }
  };

  if (loading) {
    return (
      <div className="empty-state">
        <div className="skeleton" style={{ width: 40, height: 40, borderRadius: '50%' }} />
        <div className="empty-state-title" style={{ marginTop: 12 }}>Loading invitation...</div>
      </div>
    );
  }

  if (error && !invitation) {
    return (
      <div className="card fade-in-up" style={{ maxWidth: 440, margin: '48px auto', textAlign: 'center' }}>
        <div className="card-body" style={{ padding: 32 }}>
          <div style={{ fontSize: 36, marginBottom: 12 }}>⚠️</div>
          <h2 style={{ fontSize: 18, fontWeight: 700, color: 'var(--text-primary)', marginBottom: 8 }}>Invalid Invitation</h2>
          <p style={{ fontSize: 13, color: 'var(--text-secondary)', marginBottom: 20 }}>{error}</p>
          <button
            onClick={() => window.location.hash = ''}
            className="btn btn-secondary"
          >
            Return to Dashboard
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="fade-in-up" style={{ maxWidth: 440, margin: '32px auto' }}>
      <div className="card">
        <div className="card-body" style={{ padding: 28 }}>
          <div style={{ textAlign: 'center', marginBottom: 20 }}>
            <span className="badge badge-productive" style={{ marginBottom: 10 }}>
              Team Onboarding
            </span>
            <h1 style={{ fontSize: 20, fontWeight: 700, color: 'var(--text-primary)', marginTop: 8 }}>
              Join {invitation?.org_name}
            </h1>
            <p style={{ fontSize: 13, color: 'var(--text-secondary)', marginTop: 4 }}>
              You've been invited to test get-Hike as a <span style={{ fontWeight: 600, color: 'var(--accent-purple)' }}>{invitation?.role}</span>.
            </p>
          </div>

          {error && (
            <div className="alert alert-error">
              {error}
            </div>
          )}

          <form onSubmit={handleSubmit}>
            <div className="form-group">
              <label className="form-label">Email Address</label>
              <input
                type="email"
                disabled
                value={invitation?.email || ''}
                className="form-input"
                style={{ opacity: 0.7, cursor: 'not-allowed' }}
              />
            </div>

            <div className="form-group">
              <label className="form-label">Full Name</label>
              <input
                type="text"
                required
                placeholder="Jane Doe"
                value={fullName}
                onChange={(e) => setFullName(e.target.value)}
                className="form-input"
              />
            </div>

            <div className="form-group">
              <label className="form-label">Create Password</label>
              <input
                type="password"
                required
                minLength={6}
                placeholder="••••••••"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                className="form-input"
              />
            </div>

            <button
              type="submit"
              disabled={submitting}
              className="btn btn-primary w-full"
              style={{ marginTop: 12, padding: '10px 16px' }}
            >
              {submitting ? 'Creating Account...' : 'Complete Onboarding & Join Team'}
            </button>
          </form>
        </div>
      </div>
    </div>
  );
};
