import React, { useState, useEffect } from 'react';
import { Organization, User, Invitation } from '../types';
import { apiFetch } from '../api';

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
          <span className="stat-label">Beta Plan Status</span>
          <div className="stat-value text-green">Unlimited Beta</div>
        </div>
      </div>

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
      {showInviteModal && (
        <div className="modal-backdrop">
          <div className="modal-card">
            <div className="modal-header">
              <h3 className="modal-title">Invite Team Member</h3>
              <button
                onClick={() => setShowInviteModal(false)}
                className="modal-close"
              >
                ✕
              </button>
            </div>

            <div className="modal-body">
              {errorMessage && (
                <div className="alert alert-error">
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

                  <div className="form-group">
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

                  <div className="modal-footer" style={{ border: 'none', padding: '16px 0 0' }}>
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
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
