import React, { useState, useEffect } from 'react';

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
        const res = await fetch(`/api/org/invite-info?token=${token}`);
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
      const res = await fetch('/api/org/accept-invite', {
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
      <div className="flex items-center justify-center min-h-[400px]">
        <div className="animate-spin rounded-full h-10 w-10 border-b-2 border-sky-400"></div>
      </div>
    );
  }

  if (error && !invitation) {
    return (
      <div className="max-w-md mx-auto my-12 bg-slate-800 border border-rose-500/30 rounded-2xl p-8 shadow-2xl text-center space-y-4">
        <div className="w-12 h-12 bg-rose-500/10 text-rose-400 rounded-full flex items-center justify-center mx-auto text-2xl">
          ⚠️
        </div>
        <h2 className="text-xl font-bold text-slate-100">Invalid Invitation</h2>
        <p className="text-slate-400 text-sm">{error}</p>
        <button
          onClick={() => window.location.hash = ''}
          className="px-5 py-2.5 bg-slate-700 hover:bg-slate-600 text-slate-200 font-medium rounded-xl text-sm transition-colors"
        >
          Return to Dashboard
        </button>
      </div>
    );
  }

  return (
    <div className="max-w-md mx-auto my-8 animate-fade-in">
      <div className="bg-slate-800/80 border border-slate-700/60 rounded-2xl p-8 shadow-2xl backdrop-blur-md space-y-6">
        <div className="text-center space-y-2">
          <span className="px-3 py-1 bg-sky-500/10 text-sky-400 border border-sky-500/30 rounded-full text-xs font-semibold uppercase tracking-wider">
            Team Onboarding
          </span>
          <h1 className="text-2xl font-bold text-slate-100 mt-2">Join {invitation?.org_name}</h1>
          <p className="text-slate-400 text-sm">
            You've been invited to test Mini Tracker as a <span className="font-semibold text-slate-200">{invitation?.role}</span>.
          </p>
        </div>

        {error && (
          <div className="p-3 bg-rose-500/10 border border-rose-500/30 rounded-xl text-rose-400 text-sm">
            {error}
          </div>
        )}

        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="block text-xs font-semibold uppercase text-slate-400 mb-1.5">
              Email Address
            </label>
            <input
              type="email"
              disabled
              value={invitation?.email || ''}
              className="w-full bg-slate-900/60 border border-slate-700/50 rounded-xl px-4 py-2.5 text-sm text-slate-400 cursor-not-allowed"
            />
          </div>

          <div>
            <label className="block text-xs font-semibold uppercase text-slate-400 mb-1.5">
              Full Name
            </label>
            <input
              type="text"
              required
              placeholder="Jane Doe"
              value={fullName}
              onChange={(e) => setFullName(e.target.value)}
              className="w-full bg-slate-900 border border-slate-700 rounded-xl px-4 py-2.5 text-sm text-slate-100 placeholder-slate-500 focus:outline-none focus:border-sky-400"
            />
          </div>

          <div>
            <label className="block text-xs font-semibold uppercase text-slate-400 mb-1.5">
              Create Password
            </label>
            <input
              type="password"
              required
              minLength={6}
              placeholder="••••••••"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className="w-full bg-slate-900 border border-slate-700 rounded-xl px-4 py-2.5 text-sm text-slate-100 placeholder-slate-500 focus:outline-none focus:border-sky-400"
            />
          </div>

          <button
            type="submit"
            disabled={submitting}
            className="w-full py-3 bg-gradient-to-r from-sky-500 to-blue-600 hover:from-sky-400 hover:to-blue-500 text-white font-medium rounded-xl text-sm shadow-lg shadow-sky-500/20 transition-all duration-200 mt-2"
          >
            {submitting ? 'Creating Account...' : 'Complete Onboarding & Join Team'}
          </button>
        </form>
      </div>
    </div>
  );
};
