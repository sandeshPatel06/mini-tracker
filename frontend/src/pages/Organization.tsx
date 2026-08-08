import React, { useState, useEffect } from 'react';
import { Organization, User, Invitation } from '../types';
import { apiFetch } from '../api';
import { Modal } from '../components/Modal';

export const OrganizationPage: React.FC = () => {
  const [org, setOrg] = useState<Organization | null>(null);
  const [members, setMembers] = useState<User[]>([]);
  const [invitations, setInvitations] = useState<Invitation[]>([]);
  const [loading, setLoading] = useState(true);
  
  // Modal state
  const [showInviteModal, setShowInviteModal] = useState(false);
  const [inviteEmail, setInviteEmail] = useState('');
  const [inviteRole, setInviteRole] = useState<'admin' | 'member'>('member');
  const [sending, setSending] = useState(false);
  const [inviteUrl, setInviteUrl] = useState<string | null>(null);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [copiedToken, setCopiedToken] = useState<string | null>(null);

  // Admin usage tracking state
  const [orgUsage, setOrgUsage] = useState<any>(null);
  const [usageForbidden, setUsageForbidden] = useState(false);
  const [resettingUsage, setResettingUsage] = useState(false);

  const fetchUsageData = async () => {
    try {
      const res = await apiFetch('/api/org/usage');
      if (res.status === 403) {
        setUsageForbidden(true);
        return;
      }
      if (res.ok) {
        const data = await res.json();
        setOrgUsage(data);
      }
    } catch {}
  };

  const handleResetUsage = async () => {
    if (!window.confirm('Reset organization API usage counters? This is recommended if Google reset your API key quota.')) {
      return;
    }
    setResettingUsage(true);
    try {
      const res = await apiFetch('/api/org/usage/reset', { method: 'POST' });
      if (res.ok) {
        fetchUsageData();
      }
    } catch {} finally {
      setResettingUsage(false);
    }
  };

  const fetchTeamData = async () => {
    setLoading(true);
    try {
      const res = await apiFetch('/api/org/members?org_id=1');
      if (res.ok) {
        const data = await res.json();
        setOrg(data.org);
        setMembers(data.members || []);
        setInvitations(data.invitations || []);
      }
      fetchUsageData();
    } catch (err) {
      console.error('Failed to fetch team data:', err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchTeamData();
  }, []);

  const handleSendInvite = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!inviteEmail.trim()) return;

    setSending(true);
    setErrorMessage(null);
    setInviteUrl(null);

    try {
      const res = await apiFetch('/api/org/invite', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          org_id: org?.id || 1,
          email: inviteEmail.trim(),
          role: inviteRole
        })
      });

      const data = await res.json();
      if (!res.ok) {
        throw new Error(data.message || data.error || 'Failed to send invitation');
      }

      setInviteUrl(data.invite_url);
      fetchTeamData();
    } catch (err: any) {
      setErrorMessage(err.message || 'Error sending invitation');
    } finally {
      setSending(false);
    }
  };

  const copyToClipboard = (text: string, token: string) => {
    navigator.clipboard.writeText(text);
    setCopiedToken(token);
    setTimeout(() => setCopiedToken(null), 2500);
  };

  if (loading && !org) {
    return (
      <div className="empty-state" style={{ minHeight: 400 }}>
        <div className="skeleton" style={{ width: 48, height: 48, borderRadius: '50%' }} />
        <div className="empty-state-title" style={{ marginTop: 12 }}>Loading organization details...</div>
      </div>
    );
  }

  return (
    <div className="fade-in-up">
      {/* Header Banner */}
      <div className="page-header" style={{ marginBottom: 24 }}>
        <div>
          <div className="flex items-center gap-12">
            <h1 className="page-title">{org?.name || 'Company Organization'}</h1>
            <span className="badge badge-productive">Corporate Beta</span>
          </div>
          <p className="page-subtitle">
            Manage your team members, permissions, and email invitations for beta productivity tracking.
          </p>
        </div>

        <button
          onClick={() => {
            setShowInviteModal(true);
            setInviteEmail('');
            setInviteUrl(null);
            setErrorMessage(null);
          }}
          className="btn btn-primary"
        >
          <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M18 9v3m0 0v3m0-3h3m-3 0h-3m-2-5a4 4 0 11-8 0 4 4 0 018 0zM3 20a6 6 0 0112 0v1H3v-1z" />
          </svg>
          Invite Team Member
        </button>
      </div>

      {/* Summary Stat Grid */}
      <div className="stats-grid" style={{ marginBottom: 24 }}>
        <div className="stat-card">
          <span className="stat-label">Total Active Members</span>
          <div className="stat-value">{members.length}</div>
        </div>
        <div className="stat-card">
          <span className="stat-label">Pending Invitations</span>
          <div className="stat-value text-amber">{invitations.length}</div>
        </div>
        <div className="stat-card">
          <span className="stat-label">Org Token Usage</span>
          <div className="stat-value text-purple">
            {orgUsage ? (orgUsage.total_tokens || 0).toLocaleString() : '—'}
          </div>
          <div className="stat-sub">Tokens across all key sources</div>
        </div>
      </div>

      {/* Admin-Only Organization API Key & Token Usage Section */}
      {!usageForbidden && orgUsage && (
        <div className="card" style={{ marginBottom: 24, border: '1px solid var(--accent-purple)' }}>
          <div className="card-header" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <h2 className="card-title flex items-center gap-8 text-purple">
              ⚡ Organization API Key & Token Usage (Admin View)
            </h2>
            <button
              onClick={handleResetUsage}
              disabled={resettingUsage}
              className="btn btn-secondary"
              style={{ fontSize: 12, padding: '4px 12px' }}
            >
              {resettingUsage ? 'Resetting...' : '🔄 Reset Usage (Google Quota Reset)'}
            </button>
          </div>

          <div className="card-body">
            <div className="stats-grid" style={{ marginBottom: 16 }}>
              <div className="stat-card">
                <span className="stat-label">Total Requests</span>
                <div className="stat-value">{orgUsage.total_requests || 0}</div>
              </div>
              <div className="stat-card">
                <span className="stat-label">Prompt Tokens</span>
                <div className="stat-value text-cyan">{(orgUsage.prompt_tokens || 0).toLocaleString()}</div>
              </div>
              <div className="stat-card">
                <span className="stat-label">Candidate Tokens</span>
                <div className="stat-value text-amber">{(orgUsage.candidate_tokens || 0).toLocaleString()}</div>
              </div>
              <div className="stat-card">
                <span className="stat-label">Total Tokens</span>
                <div className="stat-value text-green">{(orgUsage.total_tokens || 0).toLocaleString()}</div>
              </div>
            </div>

            {/* Member Token Usage Breakdown */}
            {orgUsage.user_breakdown && orgUsage.user_breakdown.length > 0 && (
              <div style={{ marginTop: 16 }}>
                <h4 style={{ fontSize: 13, color: 'var(--text-secondary)', marginBottom: 8 }}>
                  Member API Usage Breakdown
                </h4>
                <div className="table-container">
                  <table className="data-table">
                    <thead>
                      <tr>
                        <th>Member</th>
                        <th>Email</th>
                        <th>Role</th>
                        <th>API Requests</th>
                        <th>Prompt Tokens</th>
                        <th>Output Tokens</th>
                        <th>Total Tokens</th>
                      </tr>
                    </thead>
                    <tbody>
                      {orgUsage.user_breakdown.map((u: any) => (
                        <tr key={u.user_id}>
                          <td style={{ fontWeight: 600 }}>{u.full_name}</td>
                          <td className="text-muted">{u.email}</td>
                          <td>
                            <span className="badge badge-pending">{u.role.toUpperCase()}</span>
                          </td>
                          <td>{u.total_requests}</td>
                          <td className="text-muted">{u.prompt_tokens.toLocaleString()}</td>
                          <td className="text-muted">{u.candidate_tokens.toLocaleString()}</td>
                          <td style={{ fontWeight: 700, color: 'var(--accent-teal)' }}>
                            {u.total_tokens.toLocaleString()}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            )}
          </div>
        </div>
      )}

      {/* Active Team Members List */}
      <div className="card" style={{ marginBottom: 24 }}>
        <div className="card-header">
          <h2 className="card-title flex items-center gap-8">
            <svg className="w-5 h-5 text-purple" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4.354a4 4 0 110 5.292M15 21H3v-1a6 6 0 0112 0v1zm0 0h6v-1a6 6 0 00-9-5.197M13 7a4 4 0 11-8 0 4 4 0 018 0z" />
            </svg>
            Active Team Members
          </h2>
        </div>

        <div className="card-body" style={{ padding: 0 }}>
          <div className="table-container" style={{ border: 'none' }}>
            <table className="data-table">
              <thead>
                <tr>
                  <th>User</th>
                  <th>Email</th>
                  <th>Role</th>
                  <th>Joined Date</th>
                </tr>
              </thead>
              <tbody>
                {members.map((u) => (
                  <tr key={u.id}>
                    <td style={{ fontWeight: 600 }}>{u.full_name}</td>
                    <td className="text-muted">{u.email}</td>
                    <td>
                      <span className={`badge ${
                        u.role === 'owner' ? 'badge-productive' :
                        u.role === 'admin' ? 'badge-productive' :
                        'badge-pending'
                      }`}>
                        {u.role.toUpperCase()}
                      </span>
                    </td>
                    <td className="text-muted" style={{ fontSize: 12 }}>
                      {new Date(u.created_at).toLocaleDateString()}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      </div>

      {/* Pending Invitations Section */}
      {invitations.length > 0 && (
        <div className="card">
          <div className="card-header">
            <h2 className="card-title flex items-center gap-8">
              <svg className="w-5 h-5 text-amber" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 8l7.89 5.26a2 2 0 002.22 0L21 8M5 19h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
              </svg>
              Pending Email Invitations
            </h2>
          </div>

          <div className="card-body" style={{ padding: 0 }}>
            <div className="table-container" style={{ border: 'none' }}>
              <table className="data-table">
                <thead>
                  <tr>
                    <th>Invited Email</th>
                    <th>Target Role</th>
                    <th>Expires</th>
                    <th style={{ textAlign: 'right' }}>Invite Link</th>
                  </tr>
                </thead>
                <tbody>
                  {invitations.map((inv) => {
                    const inviteLink = `${window.location.origin}/#/accept-invite?token=${inv.token}`;
                    return (
                      <tr key={inv.id}>
                        <td style={{ fontWeight: 500 }}>{inv.email}</td>
                        <td>
                          <span className="badge badge-pending">
                            {inv.role.toUpperCase()}
                          </span>
                        </td>
                        <td className="text-muted" style={{ fontSize: 12 }}>
                          {new Date(inv.expires_at).toLocaleString()}
                        </td>
                        <td style={{ textAlign: 'right' }}>
                          <button
                            onClick={() => copyToClipboard(inviteLink, inv.token)}
                            className="btn btn-secondary btn-sm"
                          >
                            {copiedToken === inv.token ? '✓ Copied!' : '📋 Copy Link'}
                          </button>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      )}

      {/* Invite Modal */}
      <Modal
        isOpen={showInviteModal}
        onClose={() => setShowInviteModal(false)}
        title="Invite Team Member"
      >
        {errorMessage && (
          <div className="alert alert-error" style={{ marginBottom: 16 }}>
            {errorMessage}
          </div>
        )}

        {inviteUrl ? (
          <div className="flex flex-col gap-16">
            <div className="alert alert-success">
              ✓ Invitation created successfully! Email dispatched (or copy link below).
            </div>
            <div className="form-group">
              <label className="form-label">Direct Invitation Link</label>
              <div className="flex gap-8">
                <input
                  type="text"
                  readOnly
                  value={inviteUrl}
                  className="form-input"
                  style={{ fontFamily: 'monospace', fontSize: 12 }}
                />
                <button
                  onClick={() => copyToClipboard(inviteUrl, 'modal')}
                  className="btn btn-primary btn-sm"
                  style={{ whiteSpace: 'nowrap' }}
                >
                  {copiedToken === 'modal' ? 'Copied!' : 'Copy Link'}
                </button>
              </div>
            </div>
            <button
              onClick={() => {
                setInviteEmail('');
                setInviteUrl(null);
              }}
              className="btn btn-secondary w-full"
            >
              Invite Another Member
            </button>
          </div>
        ) : (
          <form onSubmit={handleSendInvite}>
            <div className="form-group">
              <label className="form-label">Email Address</label>
              <input
                type="email"
                required
                placeholder="colleague@company.com"
                value={inviteEmail}
                onChange={(e) => setInviteEmail(e.target.value)}
                className="form-input"
              />
            </div>

            <div className="form-group" style={{ marginBottom: 20 }}>
              <label className="form-label">Role & Permissions</label>
              <select
                value={inviteRole}
                onChange={(e) => setInviteRole(e.target.value as 'admin' | 'member')}
                className="form-select"
              >
                <option value="member">Member — Standard User Access</option>
                <option value="admin">Admin — Team Management Access</option>
              </select>
            </div>

            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 10, paddingTop: 12, borderTop: '1px solid var(--border-subtle)' }}>
              <button
                type="button"
                onClick={() => setShowInviteModal(false)}
                className="btn btn-secondary"
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={sending}
                className="btn btn-primary"
              >
                {sending ? 'Sending...' : 'Send Invitation'}
              </button>
            </div>
          </form>
        )}
      </Modal>
    </div>
  );
};
