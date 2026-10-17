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
  const isFailed = category === 'Failed';
  const isPending = !category || category === 'Unknown' || category === 'Failed';

  let colorVar: string;
  let label: string;

  if (isFailed) {
    colorVar = 'var(--accent-red)';
    label = 'Analysis Failed';
  } else if (isPending) {
    colorVar = 'var(--text-muted)';
    label = 'Pending AI analysis…';
  } else {
    const styleConfig = CATEGORY_STYLES[category] || CATEGORY_STYLES.Other;
    colorVar = `var(${styleConfig.varName})`;
    label = category;
  }

  return (
    <span
      className="category-badge"
      title={label}
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
      <span
        style={{
          width: 6,
          height: 6,
          borderRadius: '50%',
          backgroundColor: colorVar,
          boxShadow: `0 0 6px ${colorVar}`,
          flexShrink: 0,
        }}
      />
      <span
        style={{
          maxWidth: 140,
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
        }}
      >
        {label}
      </span>
    </span>
  );
}
