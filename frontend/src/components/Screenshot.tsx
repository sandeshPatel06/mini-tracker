import { useState, useEffect } from 'react';

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
      setSrc(`/api/image?path=${encodeURIComponent(imagePath)}`);
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
      setSrc(`/api/image?path=${encodeURIComponent(imagePath)}`);
    }
  }, [imagePath]);

  if (!imagePath) return null;

  return (
    <div
      style={{
        position: 'fixed',
        top: 0,
        left: 0,
        right: 0,
        bottom: 0,
        background: 'rgba(0, 0, 0, 0.85)',
        backdropFilter: 'blur(8px)',
        zIndex: 9999,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        padding: 24,
      }}
      onClick={onClose}
    >
      <div
        style={{
          position: 'relative',
          maxWidth: '90vw',
          maxHeight: '90vh',
          background: 'var(--bg-card)',
          borderRadius: 12,
          padding: 8,
          border: '1px solid var(--border-color)',
          boxShadow: '0 20px 40px rgba(0,0,0,0.5)',
        }}
        onClick={(e) => e.stopPropagation()}
      >
        <button
          onClick={onClose}
          style={{
            position: 'absolute',
            top: -12,
            right: -12,
            background: 'var(--accent-red)',
            color: '#fff',
            border: 'none',
            borderRadius: '50%',
            width: 28,
            height: 28,
            cursor: 'pointer',
            fontWeight: 'bold',
            fontSize: 14,
            boxShadow: '0 2px 8px rgba(0,0,0,0.3)',
          }}
        >
          ✕
        </button>
        {src ? (
          <img
            src={src}
            alt="Enlarged Screenshot"
            style={{
              maxWidth: '100%',
              maxHeight: '80vh',
              borderRadius: 8,
              display: 'block',
            }}
          />
        ) : (
          <div style={{ padding: 40, color: 'var(--text-muted)' }}>Loading image…</div>
        )}
        <div style={{ padding: '8px 4px 4px 4px', fontSize: 12, color: 'var(--text-muted)', textAlign: 'center' }}>
          {imagePath}
        </div>
      </div>
    </div>
  );
}
