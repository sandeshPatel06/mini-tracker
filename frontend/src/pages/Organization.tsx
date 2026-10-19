import React, { useState, useEffect } from 'react';
import { Organization, User, Invitation } from '../types';
import { apiFetch, getApiBaseUrl } from '../api';
import { Modal } from '../components/Modal';

export const OrganizationPage: React.FC = () => {
  const [org, setOrg] = useState<Organization | null>(null);
  const [members, setMembers] = useState<User[]>([]);
  const [invitations, setInvitations] = useState<Invitation[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadFailed, setLoadFailed] = useState(false);

  // Team & organization features live on the Get-Hike server. The desktop build
  // runs standalone unless a synced/authenticated backend is configured — detect
  // that up front so users see a friendly explanation instead of a fetch failure.
  const [serverRequirementsMet] = useState<{ desktop: boolean; hasBackend: boolean }>(() => {
    try {
      return {
        desktop: Boolean((window as any).go?.main?.App),
        hasBackend: Boolean(getApiBaseUrl()),
      };
    } catch {
      return { desktop: false, hasBackend: true };
    }
  });
  const isStandaloneDesktop = serverRequirementsMet.desktop && !serverRequirementsMet.hasBackend;

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
      } else {
        setErrorMessage('Failed to reset usage counters.');
      }
    } catch {} finally {
      setResettingUsage(false);
    }
  };

  const fetchTeamData = async () => {
    setLoading(true);
    setLoadFailed(false);
    try {
      const res = await apiFetch('/api/org/members?org_id=1');
      if (!res.ok) {
        setLoadFailed(true);
        return;
      }
      const data = await res.json();
      setOrg(data.org);
      setMembers(data.members || []);
      setInvitations(data.invitations || []);
      fetchUsageData();
    } catch (err) {
      console.error('Failed to fetch team data:', err);
      setLoadFailed(true);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (isStandaloneDesktop) {
      setLoading(false);
      return;
    }
    fetchTeamData();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  if (isStandaloneDesktop || (loadFailed && !org && serverRequirementsMet.desktop)) {
    return (
      <div className="fade-in-up">
        <div className="page-header" style={{ marginBottom: 16 }}>
          <div>
            <h1 className="page-title">Team &amp; Organization</h1>
            <p className="page-subtitle">Manage members, permissions and invitations.</p>
          </div>
        </div>
        <div
          className="card"
          role="status"
          style={{ border: '1px solid color-mix(in srgb, var(--accent-purple) 35%, transparent)', marginBottom: 16 }}
        >
          <div className="card-body" style={{ padding: '28px 22px', textAlign: 'center' }}>
            <div style={{ fontSize: 34, marginBottom: 10 }}>🔌</div>
            <div className="empty-state-title" style={{ fontSize: 17 }}>
              {isStandaloneDesktop
                ? 'Local Mode — Team features need the Get-Hike server'
                : 'Server not reachable — Team features are offline locally'}
            </div>
            <div className="empty-state-desc" style={{ maxWidth: 520, margin: '10px auto 0' }}>
              Aap desktop app is samay server se connect nahi hai, is wajah se team members,
              organization aur invitations load nahi ho sakte. Aapka tracking, screenshots aur AI
              analysis sab local DB mein smooth chal raha hai — sirf <strong>team features</strong> ke
              liye server connection chahiye.
            </div>
            <div style={{ marginTop: 16, display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 10 }}>
              <div style={{ fontSize: 13, color: 'var(--text-secondary)' }}>
                Check karein: server chal raha hai / reachable hai, ya Sign in karke sync enable karein.
              </div>
              <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap', justifyContent: 'center' }}>
                {!isStandaloneDesktop && (
                  <button
                    className="btn btn-secondary btn-sm"
                    onClick={() => { setLoadFailed(false); fetchTeamData(); }}
                  >
                    🔄 Retry Connection
                  </button>
                )}
              </div>
              <span className="badge badge-pending" style={{ fontSize: 11 }}>
                Team management requires server connection
              </span>
            </div>
          </div>
        </div>
      </div>
    );
  }

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

      const raw = await res.text();
      let data: any;
      try {
        data = JSON.parse(raw);
      } catch {
        data = { error: raw };
      }
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

  const copyToClipboard = async (text: string, token: string) => {
    try {
      if (!navigator.clipboard?.writeText) throw new Error('Clipboard unavailable');
      await navigator.clipboard.writeText(text);
      setCopiedToken(token);
      setTimeout(() => setCopiedToken(null), 2500);
    } catch {
      setErrorMessage('Copy failed — select the link and copy it manually.');
    }
  };

  if (loading && !org) {
    return (
      <div className="empty-state" style={{ minHeight: 400 }}>
        <div className="skeleton" style={{ width: 48, height: 48, borderRadius: '50%' }} />
        <div className="empty-state-title" style={{ marginTop: 12 }} aria-busy="true">Loading organization details...</div>
      </div>
    );
  }

  if (loadFailed && !org) {
    return (
      <div className="empty-state" style={{ minHeight: 400 }}>
        <div className="empty-state-title">Couldn't load your team</div>
        <div className="empty-state-desc">Check your connection or sign in again, then retry.</div>
        <div style={{ marginTop: 12 }}>
          <button className="btn btn-primary" onClick={fetchTeamData}>Retry</button>
        </div>
      </div>
    );
  }

  const cellPad: React.CSSProperties = { padding: '8px 12px' };

  return (
    <div className="fade-in-up">
      {/* Header */}
      <div className="page-header" style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', flexWrap: 'wrap', gap: 12 }}>
        <div style={{ minWidth: 0, flex: '1 1 auto' }}>
          <div className="flex items-center gap-12" style={{ minWidth: 0 }}>
            <h1
              className="page-title"
              style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
              title={org?.name || 'Company Organization'}
            >
              {org?.name || 'Company Organization'}
            </h1>
            <span className="badge badge-productive" style={{ flexShrink: 0 }}>Corporate Beta</span>
          </div>
          <p className="page-subtitle">
            Manage your team members, permissions, and email invitations for beta productivity tracking.
          </p>
        </div>

        <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap' }}>
          <button
            onClick={() => {
              setShowInviteModal(true);
              setInviteEmail('');
              setInviteUrl(null);
              setErrorMessage(null);
            }}
            className="btn btn-primary"
            style={{ padding: '10px 18px', fontSize: 14, fontWeight: 600 }}
          >
            ➕ Invite Team Member
          </button>
        </div>
      </div>

      {errorMessage && !showInviteModal && (
        <div className="alert alert-error" style={{ marginBottom: 16 }}>
          {errorMessage}
        </div>
      )}

      {/* Admin-only organization API usage */}
      {!usageForbidden && orgUsage && (
        <div className="card" style={{ marginBottom: 16, border: '1px solid color-mix(in srgb, var(--accent-purple) 30%, transparent)' }}>
          <div className="card-header">
            <span className="card-title" style={{ color: 'var(--text-secondary)' }}>
              ⚡ Organization API Usage <span style={{ color: 'var(--text-muted)', fontWeight: 600 }}>(Admin View)</span>
            </span>
            <button
              onClick={handleResetUsage}
              disabled={resettingUsage}
              className="btn btn-secondary btn-sm"
              title="Recommended after Google resets your API key quota"
              style={{ opacity: resettingUsage ? 0.6 : undefined, cursor: resettingUsage ? 'not-allowed' : undefined, flexShrink: 0 }}
            >
              {resettingUsage ? 'Resetting…' : '🔄 Reset Usage'}
            </button>
          </div>

          <div className="card-body">
            <div className="stats-grid" style={{ marginBottom: orgUsage.user_breakdown && orgUsage.user_breakdown.length > 0 ? 16 : 0 }}>
              <div className="stat-card">
                <span className="stat-label">Total Requests</span>
                <div className="stat-value" style={{ fontSize: 22 }}>{orgUsage.total_requests || 0}</div>
              </div>
              <div className="stat-card">
                <span className="stat-label">Prompt Tokens</span>
                <div className="stat-value text-cyan" style={{ fontSize: 22 }}>{(orgUsage.prompt_tokens || 0).toLocaleString()}</div>
              </div>
              <div className="stat-card">
                <span className="stat-label">Candidate Tokens</span>
                <div className="stat-value text-amber" style={{ fontSize: 22 }}>{(orgUsage.candidate_tokens || 0).toLocaleString()}</div>
              </div>
              <div className="stat-card">
                <span className="stat-label">Total Tokens</span>
                <div className="stat-value text-green" style={{ fontSize: 22 }}>{(orgUsage.total_tokens || 0).toLocaleString()}</div>
              </div>
            </div>

            {orgUsage.user_breakdown && orgUsage.user_breakdown.length > 0 && (
              <div>
                <div style={{ fontSize: 12, fontWeight: 700, color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.7px', marginBottom: 8 }}>
                  Member API Usage Breakdown
                </div>
                <div className="table-container">
                  <table className="data-table">
                    <thead>
                      <tr>
                        <th style={cellPad}>Member</th>
                        <th style={cellPad}>Email</th>
                        <th style={cellPad}>Role</th>
                        <th style={cellPad}>API Requests</th>
                        <th style={cellPad}>Prompt Tokens</th>
                        <th style={cellPad}>Output Tokens</th>
                        <th style={cellPad}>Total Tokens</th>
                      </tr>
                    </thead>
                    <tbody>
                      {orgUsage.user_breakdown.map((u: any) => (
                        <tr key={u.user_id}>
                          <td
                            style={{ ...cellPad, fontWeight: 600, maxWidth: 200, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
                            title={u.full_name}
                          >
                            {u.full_name}
                          </td>
                          <td
                            className="text-muted"
                            style={{ ...cellPad, maxWidth: 220, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
                            title={u.email}
                          >
                            {u.email}
                          </td>
                          <td style={cellPad}>
                            <span className="badge badge-pending">{u.role.toUpperCase()}</span>
                          </td>
                          <td style={cellPad}>{u.total_requests}</td>
                          <td className="text-muted" style={cellPad}>{u.prompt_tokens.toLocaleString()}</td>
                          <td className="text-muted" style={cellPad}>{u.candidate_tokens.toLocaleString()}</td>
                          <td style={{ ...cellPad, fontWeight: 700, color: 'var(--teal-text)' }}>
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

      {/* Team members + pending invitations (merged) */}
      <div className="card">
        <div className="card-header">
          <span className="card-title">👥 Team Members</span>
          <span
            style={{ fontSize: 12, color: 'var(--text-muted)', fontWeight: 500, flexShrink: 0, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
            title={`${members.length} active members · ${invitations.length} pending invitations · ${orgUsage ? (orgUsage.total_tokens || 0).toLocaleString() : '—'} tokens across all key sources`}
          >
            {members.length} active · {invitations.length} pending · {orgUsage ? (orgUsage.total_tokens || 0).toLocaleString() : '—'} tokens
          </span>
        </div>

        <div className="card-body" style={{ padding: 0 }}>
          {members.length === 0 ? (
            <div style={{ padding: '14px 18px', fontSize: 13, color: 'var(--text-muted)' }}>No team members yet.</div>
          ) : (
            members.map((u, i) => (
              <div
                key={u.id}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 12,
                  padding: '10px 18px',
                  borderBottom: i < members.length - 1 ? '1px solid var(--border-subtle)' : 'none',
                }}
              >
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div
                    style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
                    title={u.full_name}
                  >
                    {u.full_name}
                  </div>
                  <div
                    style={{ fontSize: 12, color: 'var(--text-muted)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
                    title={u.email}
                  >
                    {u.email}
                  </div>
                </div>
                <span
                  className={`badge ${
                    u.role === 'owner' ? 'badge-productive' :
                    u.role === 'admin' ? 'badge-productive' :
                    'badge-pending'
                  }`}
                  style={{ flexShrink: 0 }}
                >
                  {u.role.toUpperCase()}
                </span>
                <span style={{ fontSize: 12, color: 'var(--text-muted)', flexShrink: 0 }}>
                  {new Date(u.created_at).toLocaleDateString()}
                </span>
              </div>
            ))
          )}
        </div>

        {invitations.length > 0 && (
          <>
            <div className="card-header" style={{ borderTop: '1px solid var(--border-subtle)' }}>
              <span className="card-title">📨 Pending Invitations</span>
              <span style={{ fontSize: 12, color: 'var(--text-muted)', fontWeight: 500 }}>{invitations.length}</span>
            </div>
            <div className="card-body" style={{ padding: 0 }}>
              {invitations.map((inv, i) => {
                const inviteLink = `${window.location.origin}/#/accept-invite?token=${inv.token}`;
                return (
                  <div
                    key={inv.id}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: 12,
                      padding: '10px 18px',
                      borderBottom: i < invitations.length - 1 ? '1px solid var(--border-subtle)' : 'none',
                    }}
                  >
                    <div style={{ flex: 1, minWidth: 0 }}>
                      <div
                        style={{ fontSize: 13, fontWeight: 500, color: 'var(--text-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
                        title={inv.email}
                      >
                        {inv.email}
                      </div>
                      <div style={{ fontSize: 12, color: 'var(--text-muted)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                        Expires {new Date(inv.expires_at).toLocaleString()}
                      </div>
                    </div>
                    <span className="badge badge-pending" style={{ flexShrink: 0 }}>
                      {inv.role.toUpperCase()}
                    </span>
                    <button
                      onClick={() => copyToClipboard(inviteLink, inv.token)}
                      className="btn btn-secondary btn-sm"
                      style={{ flexShrink: 0 }}
                    >
                      {copiedToken === inv.token ? '✓ Copied!' : '📋 Copy Link'}
                    </button>
                  </div>
                );
              })}
            </div>
          </>
        )}
      </div>

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
                  title={inviteUrl}
                  style={{ fontFamily: 'monospace', fontSize: 12, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
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
              <div style={{ display: 'flex', gap: 8 }}>
                {([
                  { id: 'member', label: '👤 Member', desc: 'Standard user access' },
                  { id: 'admin', label: '🛡️ Admin', desc: 'Team management access' },
                ] as const).map((r) => (
                  <button
                    key={r.id}
                    type="button"
                    title={r.desc}
                    onClick={() => setInviteRole(r.id)}
                    className={inviteRole === r.id ? 'btn btn-primary' : 'btn btn-secondary'}
                    style={{ flex: 1, padding: '8px 10px', fontSize: 13, fontWeight: inviteRole === r.id ? 700 : 500 }}
                  >
                    {r.label}
                  </button>
                ))}
              </div>
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
                style={{ opacity: sending ? 0.6 : undefined, cursor: sending ? 'not-allowed' : undefined }}
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
