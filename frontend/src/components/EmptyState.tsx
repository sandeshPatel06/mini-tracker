import React, { ReactNode } from 'react';

interface EmptyStateProps {
  icon?: ReactNode;
  title: string;
  description?: string;
  action?: ReactNode;
  minHeight?: number | string;
  className?: string;
}

export function EmptyState({
  icon,
  title,
  description,
  action,
  minHeight,
  className = '',
}: EmptyStateProps) {
  return (
    <div
      className={`empty-state ${className}`.trim()}
      style={minHeight ? { minHeight } : undefined}
    >
      {icon && <div className="empty-state-icon">{icon}</div>}
      <div className="empty-state-title">{title}</div>
      {description && <div className="empty-state-desc">{description}</div>}
      {action && <div style={{ marginTop: 14 }}>{action}</div>}
    </div>
  );
}
