import React from 'react';
import { Icon, IconName } from './Icon';
import { Page, User } from '../types';
import logoAsset from '../assets/logo.png';

interface SidebarProps {
  page: Page;
  isMobileMenuOpen: boolean;
  setIsMobileMenuOpen: (open: boolean) => void;
  currentUser: User | null;
  isGuestMode: boolean;
  setIsGuestMode: (isGuest: boolean) => void;
  elapsedSeconds: number;
  formatTimer: (seconds: number) => string;
  isTrackingActive: boolean;
  handleToggleTracking: () => void;
  trackerError: string | null;
  userTask: string;
  onOpenTaskModal: () => void;
  isSyncing: boolean;
  syncMessage: string | null;
  handleSyncNow: () => void;
  navItems: { id: Page; iconName: IconName; label: string }[];
  navigateTo: (page: Page) => void;
  handleLogout: () => void;
  dev: {
    enabled: boolean;
    registerTap: () => void;
    setOpen: (open: boolean) => void;
  };
  aiKeyPresent: boolean;
  aiConfigured?: boolean;
}

export function Sidebar({
  page,
  isMobileMenuOpen,
  setIsMobileMenuOpen,
  currentUser,
  isGuestMode,
  setIsGuestMode,
  elapsedSeconds,
  formatTimer,
  isTrackingActive,
  handleToggleTracking,
  trackerError,
  userTask,
  onOpenTaskModal,
  isSyncing,
  syncMessage,
  handleSyncNow,
  navItems,
  navigateTo,
  handleLogout,
  dev,
  aiKeyPresent,
  aiConfigured,
}: SidebarProps) {
  return (
    <>
      {/* Mobile Header Bar */}
      <header className="mobile-header">
        <div className="mobile-header-brand" onClick={() => dev.registerTap()} style={{ cursor: 'default' }}>
          <img src={logoAsset} alt="get-Hike Logo" className="app-brand-logo-img" style={{ width: 34, height: 34 }} />
          <span className="sidebar-logo-text" style={{ fontSize: 14 }}>get-Hike</span>
        </div>
        <div className="mobile-header-actions">
          <div style={{ fontFamily: 'monospace', fontSize: 13, fontWeight: 700, color: 'var(--teal-text)', fontVariantNumeric: 'tabular-nums' }}>
            {formatTimer(elapsedSeconds)}
          </div>
          <button
            className="mobile-menu-toggle"
            onClick={() => setIsMobileMenuOpen(!isMobileMenuOpen)}
            aria-label="Toggle Navigation Menu"
            aria-expanded={isMobileMenuOpen}
            aria-controls="sidebar"
          >
            {isMobileMenuOpen ? '✕' : '☰'}
          </button>
        </div>
      </header>

      {/* Sidebar Backdrop Overlay for Mobile */}
      {isMobileMenuOpen && (
        <div className="sidebar-backdrop" onClick={() => setIsMobileMenuOpen(false)} />
      )}

      {/* Sidebar Drawer */}
      <aside className={`sidebar ${isMobileMenuOpen ? 'open' : ''}`} id="sidebar">
        <div className="sidebar-logo" onClick={() => dev.registerTap()} style={{ cursor: 'default' }}>
          <img src={logoAsset} alt="get-Hike Logo" className="app-brand-logo-img" style={{ width: 32, height: 32 }} />
          <div>
            <div className="sidebar-logo-text">get-Hike</div>
            <div className="sidebar-logo-sub">Productivity</div>
          </div>
        </div>

        {/* Work Clock & Tracking Control Widget */}
        <div className="sidebar-widget">
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 6 }}>
            <span className="sidebar-widget-label">Work Session</span>
            <span
              style={{
                fontSize: 11,
                padding: '2px 6px',
                borderRadius: 4,
                fontWeight: 600,
                background: isGuestMode ? 'color-mix(in srgb, var(--accent-purple) 15%, transparent)' : 'color-mix(in srgb, var(--accent-green) 15%, transparent)',
                color: isGuestMode ? 'var(--accent-purple)' : 'var(--accent-primary-text)',
                border: isGuestMode ? '1px solid color-mix(in srgb, var(--accent-purple) 30%, transparent)' : '1px solid color-mix(in srgb, var(--accent-green) 30%, transparent)',
                whiteSpace: 'nowrap',
              }}
            >
              {isGuestMode ? 'Offline Guest' : 'Synced'}
            </span>
          </div>
          <div className="sidebar-widget-timer" style={{ fontVariantNumeric: 'tabular-nums' }}>
            {formatTimer(elapsedSeconds)}
          </div>
          <button
            onClick={handleToggleTracking}
            className={`btn-tracker-toggle ${isTrackingActive ? 'active' : 'paused'}`}
            style={{ marginBottom: 8 }}
          >
            <Icon name={isTrackingActive ? 'pause' : 'play'} size={14} />
            <span>{isTrackingActive ? 'Pause Tracker' : 'Start Tracker'}</span>
          </button>
          {trackerError && (
            <div
              role="alert"
              style={{
                fontSize: 11,
                padding: '6px 8px',
                borderRadius: 'var(--radius-sm)',
                background: 'color-mix(in srgb, var(--accent-red) 15%, transparent)',
                border: '1px solid color-mix(in srgb, var(--accent-red) 35%, transparent)',
                color: 'var(--danger-text)',
                marginBottom: 8,
                display: 'flex',
                alignItems: 'center',
                gap: 6,
              }}
            >
              <Icon name="x" size={12} />
              <span style={{ flex: 1 }}>{trackerError}</span>
            </div>
          )}
          <button
            onClick={onOpenTaskModal}
            className={`btn-task-goal ${userTask ? 'active' : ''}`}
            title={userTask || 'Set target task goal (max 20 words)'}
          >
            <Icon name="target" size={12} />
            <span style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              {userTask ? `Task: ${userTask}` : 'Set Target Task Goal'}
            </span>
          </button>
          {!isGuestMode && (
            <button
              onClick={handleSyncNow}
              disabled={isSyncing}
              className={`btn-sync-now ${isSyncing ? 'syncing' : ''}`}
            >
              <Icon name="refresh" size={12} className={isSyncing ? 'spin' : ''} />
              <span>{isSyncing ? 'Syncing...' : (syncMessage || 'Sync Now')}</span>
            </button>
          )}
        </div>

        <div className="sidebar-section-label">Navigation</div>
        {navItems.map((item) => {
          const active = page === item.id;
          return (
            <button
              key={item.id}
              id={`nav-${item.id}`}
              type="button"
              className={`nav-item ${active ? 'active' : ''}`}
              onClick={() => navigateTo(item.id)}
              aria-current={active ? 'page' : undefined}
              style={{
                background: active ? 'color-mix(in srgb, var(--accent-primary) 9%, transparent)' : 'none',
                border: 'none',
                borderLeft: active ? '3px solid var(--accent-primary)' : '3px solid transparent',
                textAlign: 'left',
                fontFamily: 'inherit',
                cursor: 'pointer',
              }}
            >
              <Icon name={item.iconName} size={16} className="nav-icon" />
              <span className="nav-text">{item.label}</span>
            </button>
          );
        })}

        <div className="sidebar-footer">
          {currentUser ? (
            <div style={{ marginBottom: 12, paddingBottom: 10, borderBottom: '1px solid var(--border-subtle)' }}>
              <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-primary)', display: 'flex', alignItems: 'center', gap: 6 }}>
                <Icon name="user" size={14} color="var(--text-secondary)" />
                <span
                  style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
                  title={currentUser.full_name || currentUser.email}
                >
                  {currentUser.full_name || currentUser.email}
                </span>
              </div>
              <div style={{ fontSize: 11, color: 'var(--text-muted)', display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginTop: 6 }}>
                <span style={{ textTransform: 'capitalize' }}>Role: {currentUser.role}</span>
                <button
                  onClick={handleLogout}
                  style={{
                    background: 'transparent',
                    border: 'none',
                    color: 'var(--danger-text)',
                    fontSize: 11,
                    fontWeight: 600,
                    cursor: 'pointer',
                    display: 'inline-flex',
                    alignItems: 'center',
                    gap: 4,
                  }}
                >
                  <span>Logout</span>
                  <Icon name="logout" size={12} />
                </button>
              </div>
            </div>
          ) : (
            <div style={{ marginBottom: 10 }}>
              <button
                onClick={() => setIsGuestMode(false)}
                className="btn btn-secondary btn-sm w-full"
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  gap: 6,
                  fontSize: 12,
                  marginBottom: 8,
                }}
              >
                <Icon name="lock" size={13} />
                <span>Sign In / Sign Up</span>
              </button>
            </div>
          )}

          {dev.enabled && (
            <button
              type="button"
              className="dev-badge dev-badge-btn"
              onClick={() => dev.setOpen(true)}
              title="Open Developer Menu (Ctrl+Shift+D)"
            >
              DEV
            </button>
          )}

          <div className="status-badge">
            {(() => {
              const isAIReady = aiKeyPresent || Boolean(aiConfigured) || Boolean(currentUser?.role === 'member');
              return (
                <>
                  <div
                    className={`status-dot ${isAIReady ? '' : 'inactive'}`}
                    style={{ background: isAIReady ? 'var(--accent-teal)' : undefined, boxShadow: isAIReady ? '0 0 8px var(--accent-teal)' : undefined }}
                  />
                  <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>
                    {isAIReady ? 'AI Engine Ready' : 'AI Standby'}
                  </span>
                </>
              );
            })()}
          </div>
        </div>
      </aside>
    </>
  );
}
