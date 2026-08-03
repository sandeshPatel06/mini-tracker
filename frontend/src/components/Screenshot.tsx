import { useState, useEffect } from 'react';
import { buildApiUrl } from '../api';

declare const window: Window & {
  go?: {
    main: {
      App: {
        GetImageBase64: (path: string) => Promise<string>;
      };
    };
  };
};

interface ThumbProps {
  imagePath?: string;
  onClick?: () => void;
  className?: string;
}

export function ScreenshotThumb({ imagePath, onClick, className }: ThumbProps) {
  const [src, setSrc] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!imagePath) {
      setSrc(null);
      return;
    }

    setLoading(true);
    if (window.go?.main?.App?.GetImageBase64) {
      window.go.main.App.GetImageBase64(imagePath)
        .then((res) => {
          if (res) setSrc(res);
          setLoading(false);
        })
        .catch(() => {
          setLoading(false);
        });
    } else {
      setSrc(buildApiUrl(`/api/image?path=${encodeURIComponent(imagePath)}`));
      setLoading(false);
    }
  }, [imagePath]);

  if (loading) {
    return <div className="timeline-thumb-placeholder skeleton">🖥️</div>;
  }

  if (!src) {
    return <div className="timeline-thumb-placeholder">🖥️</div>;
  }

  return (
    <img
      src={src}
      alt="Desktop Screenshot"
      className={className || "timeline-thumb-img"}
      onClick={onClick}
      style={{ cursor: onClick ? 'pointer' : 'default' }}
      onError={() => setSrc(null)}
    />
  );
}

interface ModalProps {
  imagePath: string | null;
  onClose: () => void;
}

export function ImageModal({ imagePath, onClose }: ModalProps) {
  const [src, setSrc] = useState<string | null>(null);

  useEffect(() => {
    if (!imagePath) return;
    if (window.go?.main?.App?.GetImageBase64) {
      window.go.main.App.GetImageBase64(imagePath).then(res => setSrc(res));
    } else {
      setSrc(buildApiUrl(`/api/image?path=${encodeURIComponent(imagePath)}`));
    }
  }, [imagePath]);

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [onClose]);

  if (!imagePath) return null;

  return (
    <div
      style={{
        position: 'fixed',
        top: 0,
        left: 0,
        right: 0,
        bottom: 0,
        background: 'rgba(10, 10, 18, 0.88)',
        backdropFilter: 'blur(12px)',
        zIndex: 99999,
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        padding: '32px 24px',
        animation: 'fadeIn 0.2s ease-out'
      }}
      onClick={onClose}
    >
      <div
        style={{
          position: 'relative',
          maxWidth: '92vw',
          maxHeight: '88vh',
          background: 'var(--bg-surface)',
          borderRadius: 16,
          padding: 12,
          border: '1px solid var(--border-medium)',
          boxShadow: '0 25px 60px rgba(0, 0, 0, 0.6)',
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center'
        }}
        onClick={(e) => e.stopPropagation()}
      >
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', width: '100%', marginBottom: 10, padding: '4px 8px' }}>
          <span style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-secondary)' }}>
            🖥️ Desktop Screenshot Inspection
          </span>
          <button
            onClick={onClose}
            className="btn btn-secondary"
            style={{
              padding: '4px 12px',
              fontSize: 12,
              borderRadius: 20,
              cursor: 'pointer'
            }}
          >
            ✕ Close (ESC)
          </button>
        </div>

        {src ? (
          <img
            src={src}
            alt="Desktop Screenshot Inspection"
            style={{
              maxWidth: '100%',
              maxHeight: '78vh',
              borderRadius: 10,
              display: 'block',
              objectFit: 'contain',
              border: '1px solid var(--border-subtle)'
            }}
          />
        ) : (
          <div style={{ padding: '60px 100px', color: 'var(--text-muted)', fontSize: 14 }}>
            ⏳ Loading full-resolution screenshot…
          </div>
        )}

        <div style={{ marginTop: 10, fontSize: 11, fontFamily: 'monospace', color: 'var(--text-muted)', textAlign: 'center', wordBreak: 'break-all' }}>
          {imagePath}
        </div>
      </div>
    </div>
  );
}
