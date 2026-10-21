import React, { ReactNode } from 'react';

interface StatCardProps {
  label: string;
  value: string | number;
  valueTitle?: string;
  sub?: string;
  valueClass?: string;
  valueColor?: string;
  leading?: ReactNode;
}

export function StatCard({
  label,
  value,
  valueTitle,
  sub,
  valueClass,
  valueColor,
  leading,
}: StatCardProps) {
  return (
    <div className="stat-card">
      <span className="stat-label">{label}</span>
      <div
        className={`stat-value${valueClass ? ` ${valueClass}` : ''}`}
        title={valueTitle}
        style={valueColor ? { color: valueColor } : undefined}
      >
        {leading}
        <span className="stat-value-text">{value}</span>
      </div>
      {sub && <div className="stat-sub">{sub}</div>}
    </div>
  );
}
