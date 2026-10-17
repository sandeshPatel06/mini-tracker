import React, { useState, useEffect } from 'react';
import { apiFetch } from '../api';

const parseJsonSafe = async (res: Response): Promise<any> => {
  const text = await res.text();
  try {
    return JSON.parse(text);
  } catch {
    return { error: text };
  }
};

const friendlyError = (msg: string): string => {
  const m = (msg || '').toLowerCase();
  if (m.includes('expired')) return 'This invitation has expired — ask an admin to resend it.';
  if (m.includes('used') || m.includes('accepted')) return 'This invitation has already been used.';
  if (m.includes('not found') || m.includes('invalid')) return 'Invalid invitation link.';
  return msg;
};

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
        const data = await parseJsonSafe(res);
        if (!res.ok || data.valid === false) {
          throw new Error(data.error || data.message || 'Invalid or expired invitation token');
        }
        setInvitation(data);
      } catch (err: any) {
        setError(friendlyError(err.message || 'Invitation verification failed'));
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

      const data = await parseJsonSafe(res);
      if (!res.ok) {
        throw new Error(data.message || data.error || 'Failed to accept invitation');
      }

      onSuccess();
    } catch (err: any) {
      setError(friendlyError(err.message || 'Error creating user account'));
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
      <div className="fade-in-up" style={{ maxWidth: 400, margin: '48px auto' }}>
        <div className="card">
          <div className="card-header">
            <span className="card-title">⚠️ Invalid Invitation</span>
          </div>
          <div className="card-body">
            <div className="alert alert-error">{error}</div>
            <button
              onClick={() => window.location.hash = ''}
              className="btn btn-secondary w-full"
            >
              Return to Dashboard
            </button>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="fade-in-up" style={{ maxWidth: 400, margin: '48px auto' }}>
      <div className="card">
        <div className="card-header">
          <span className="card-title">✉️ Team Invitation</span>
        </div>
        <div className="card-body">
          <div style={{ textAlign: 'center', marginBottom: 14 }}>
            <div
              style={{ fontSize: 16, fontWeight: 700, color: 'var(--text-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
              title={invitation?.org_name}
            >
              Join {invitation?.org_name}
            </div>
            <div
              style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
              title={`You've been invited as a ${invitation?.role}.`}
            >
              You've been invited as a <span style={{ fontWeight: 600, color: 'var(--accent-purple)' }}>{invitation?.role}</span>.
            </div>
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
                title={invitation?.email || ''}
                style={{ opacity: 0.7, cursor: 'not-allowed', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
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
              style={{ padding: '10px 16px', fontWeight: 700, opacity: submitting ? 0.6 : undefined, cursor: submitting ? 'not-allowed' : undefined }}
            >
              {submitting ? 'Creating Account...' : 'Create Account & Join Team'}
            </button>
          </form>
        </div>
      </div>
    </div>
  );
};
