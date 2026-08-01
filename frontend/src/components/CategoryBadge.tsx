import React from 'react';

interface CategoryBadgeProps {
  category: string;
}

const CATEGORY_STYLES: Record<string, { icon: string; bg: string; color: string; border: string }> = {
  Coding: {
    icon: '💻',
    bg: 'rgba(99, 102, 241, 0.15)',
    color: '#818cf8',
    border: 'rgba(99, 102, 241, 0.3)',
  },
  Writing: {
    icon: '📝',
    bg: 'rgba(59, 130, 246, 0.15)',
    color: '#60a5fa',
    border: 'rgba(59, 130, 246, 0.3)',
  },
  Browsing: {
    icon: '🌐',
    bg: 'rgba(45, 212, 191, 0.15)',
    color: '#2dd4bf',
    border: 'rgba(45, 212, 191, 0.3)',
  },
  Communication: {
    icon: '💬',
    bg: 'rgba(16, 185, 129, 0.15)',
    color: '#34d399',
    border: 'rgba(16, 185, 129, 0.3)',
  },
  Design: {
    icon: '🎨',
    bg: 'rgba(236, 72, 153, 0.15)',
    color: '#f472b6',
    border: 'rgba(236, 72, 153, 0.3)',
  },
  'Social Media': {
    icon: '📲',
    bg: 'rgba(245, 158, 11, 0.15)',
    color: '#fbbf24',
    border: 'rgba(245, 158, 11, 0.3)',
  },
  'Video/Entertainment': {
    icon: '🍿',
    bg: 'rgba(249, 115, 22, 0.15)',
    color: '#fb923c',
    border: 'rgba(249, 115, 22, 0.3)',
  },
  Idle: {
    icon: '💤',
    bg: 'rgba(148, 163, 184, 0.12)',
    color: '#94a3b8',
    border: 'rgba(148, 163, 184, 0.25)',
  },
  Other: {
    icon: '📁',
    bg: 'rgba(148, 163, 184, 0.12)',
    color: '#cbd5e1',
    border: 'rgba(148, 163, 184, 0.25)',
  },
};

export function CategoryBadge({ category }: CategoryBadgeProps) {
  if (!category || category === 'Unknown') {
    return (
      <span style={{ color: 'var(--text-muted)', fontSize: 12, fontStyle: 'italic' }}>
        Pending AI analysis…
      </span>
    );
  }

  const style = CATEGORY_STYLES[category] || CATEGORY_STYLES.Other;

  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 5,
        padding: '2px 8px',
        borderRadius: 6,
        fontSize: 11,
        fontWeight: 600,
        backgroundColor: style.bg,
        color: style.color,
        border: `1px solid ${style.border}`,
        letterSpacing: '0.2px',
      }}
    >
      <span>{style.icon}</span>
      <span>{category}</span>
    </span>
  );
}
