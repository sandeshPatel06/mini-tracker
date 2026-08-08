import React from 'react';

interface CategoryBadgeProps {
  category: string;
}

const CATEGORY_STYLES: Record<string, { bg: string; color: string; border: string }> = {
  Coding: {
    bg: 'rgba(99, 102, 241, 0.12)',
    color: '#818cf8',
    border: 'rgba(99, 102, 241, 0.25)',
  },
  Writing: {
    bg: 'rgba(59, 130, 246, 0.12)',
    color: '#60a5fa',
    border: 'rgba(59, 130, 246, 0.25)',
  },
  Browsing: {
    bg: 'rgba(20, 184, 166, 0.12)',
    color: '#2dd4bf',
    border: 'rgba(20, 184, 166, 0.25)',
  },
  Communication: {
    bg: 'rgba(16, 185, 129, 0.12)',
    color: '#34d399',
    border: 'rgba(16, 185, 129, 0.25)',
  },
  Design: {
    bg: 'rgba(236, 72, 153, 0.12)',
    color: '#f472b6',
    border: 'rgba(236, 72, 153, 0.25)',
  },
  'Social Media': {
    bg: 'rgba(245, 158, 11, 0.12)',
    color: '#fbbf24',
    border: 'rgba(245, 158, 11, 0.25)',
  },
  'Video/Entertainment': {
    bg: 'rgba(249, 115, 22, 0.12)',
    color: '#fb923c',
    border: 'rgba(249, 115, 22, 0.25)',
  },
  Idle: {
    bg: 'rgba(148, 163, 184, 0.1)',
    color: '#94a3b8',
    border: 'rgba(148, 163, 184, 0.2)',
  },
  Other: {
    bg: 'rgba(148, 163, 184, 0.1)',
    color: '#cbd5e1',
    border: 'rgba(148, 163, 184, 0.2)',
  },
};

export function CategoryBadge({ category }: CategoryBadgeProps) {
  if (!category || category === 'Unknown') {
    return (
      <span style={{ color: 'var(--text-muted)', fontSize: 12, fontStyle: 'normal' }}>
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
        gap: 6,
        padding: '3px 9px',
        borderRadius: 4,
        fontSize: 11,
        fontWeight: 600,
        backgroundColor: style.bg,
        color: style.color,
        border: `1px solid ${style.border}`,
        letterSpacing: '0.2px',
      }}
    >
      <span style={{ width: 6, height: 6, borderRadius: '50%', backgroundColor: style.color }} />
      <span>{category}</span>
    </span>
  );
}

