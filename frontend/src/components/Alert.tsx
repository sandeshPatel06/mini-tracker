import React, { ReactNode } from 'react';

export type AlertType = 'error' | 'success' | 'warning' | 'info';

interface AlertProps {
  type?: AlertType;
  message?: ReactNode;
  children?: ReactNode;
  onRetry?: () => void;
  retryText?: string;
  className?: string;
  style?: React.CSSProperties;
}

export function Alert({
  type = 'error',
  message,
  children,
  onRetry,
  retryText = 'Retry',
  className = '',
  style,
}: AlertProps) {
  const content = children || message;
  if (!content) return null;

  return (
    <div
      className={`alert alert-${type} ${className}`.trim()}
      role="alert"
      style={style}
    >
      <div style={{ flex: 1 }}>{content}</div>
      {onRetry && (
        <button
          type="button"
          className="btn btn-secondary btn-sm"
          onClick={onRetry}
          style={{ flexShrink: 0 }}
        >
          {retryText}
        </button>
      )}
    </div>
  );
}
