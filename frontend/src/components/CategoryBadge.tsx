import React from 'react';

interface CategoryBadgeProps {
  category: string;
}

const CATEGORY_STYLES: Record<string, { varName: string }> = {
  Coding: { varName: '--accent-green' },
  Writing: { varName: '--accent-teal' },
  Browsing: { varName: '--accent-cyan' },
  Communication: { varName: '--accent-indigo' },
  Design: { varName: '--accent-purple' },
  'Social Media': { varName: '--accent-amber' },
  'Video/Entertainment': { varName: '--accent-red' },
  Idle: { varName: '--text-muted' },
  Other: { varName: '--text-muted' },
};

export function CategoryBadge({ category }: CategoryBadgeProps) {
  if (!category || category === 'Unknown' || category === 'Failed') {
    const isFailed = category === 'Failed';
    return (
      <span style={{ 
        color: isFailed ? 'var(--accent-red)' : 'var(--text-muted)', 
        fontSize: 11, 
        fontStyle: 'normal',
        fontWeight: 500,
        display: 'inline-flex',
        alignItems: 'center',
        gap: 6
      }}>
        {isFailed && <span style={{ width: 6, height: 6, borderRadius: '50%', backgroundColor: 'var(--accent-red)' }} />}
        {isFailed ? 'Analysis Failed' : 'Pending AI analysis…'}
      </span>
    );
  }

  const styleConfig = CATEGORY_STYLES[category] || CATEGORY_STYLES.Other;
  const colorVar = `var(${styleConfig.varName})`;

  return (
    <span
      className="category-badge"
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 6,
        padding: '4px 10px',
        borderRadius: 'var(--radius-sm)',
        fontSize: 11,
        fontWeight: 600,
        backgroundColor: `color-mix(in srgb, ${colorVar} 12%, transparent)`,
        color: colorVar,
        border: `1px solid color-mix(in srgb, ${colorVar} 30%, transparent)`,
        letterSpacing: '0.3px',
        boxShadow: `0 2px 8px color-mix(in srgb, ${colorVar} 10%, transparent)`,
      }}
    >
      <span style={{ 
        width: 6, height: 6, borderRadius: '50%', backgroundColor: colorVar,
        boxShadow: `0 0 6px ${colorVar}`
      }} />
      <span>{category}</span>
    </span>
  );
}

