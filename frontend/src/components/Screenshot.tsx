import { useState, useEffect } from 'react';
import { buildApiUrl } from '../api';
import { Modal } from './Modal';

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
    return (
      <div className="timeline-thumb-placeholder skeleton">
        <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.5">
          <rect x="2" y="3" width="20" height="14" rx="2" />
          <line x1="8" y1="21" x2="16" y2="21" />
          <line x1="12" y1="17" x2="12" y2="21" />
        </svg>
      </div>
    );
  }

  if (!src) {
    return (
      <div className="timeline-thumb-placeholder">
        <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.5">
          <rect x="2" y="3" width="20" height="14" rx="2" />
          <line x1="8" y1="21" x2="16" y2="21" />
          <line x1="12" y1="17" x2="12" y2="21" />
        </svg>
      </div>
    );
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

interface ImageModalProps {
  imagePath: string | null;
  onClose: () => void;
}

export function ImageModal({ imagePath, onClose }: ImageModalProps) {
  const [src, setSrc] = useState<string | null>(null);

  useEffect(() => {
    if (!imagePath) return;
    if (window.go?.main?.App?.GetImageBase64) {
      window.go.main.App.GetImageBase64(imagePath).then((res) => setSrc(res));
    } else {
      setSrc(buildApiUrl(`/api/image?path=${encodeURIComponent(imagePath)}`));
    }
  }, [imagePath]);

  return (
    <Modal
      isOpen={Boolean(imagePath)}
      onClose={onClose}
      title="Desktop Screenshot Inspection"
      maxWidth={1000}
    >
      {src ? (
        <img
          src={src}
          alt="Desktop Screenshot Inspection"
          style={{
            maxWidth: '100%',
            maxHeight: '75vh',
            borderRadius: 8,
            display: 'block',
            objectFit: 'contain',
            border: '1px solid var(--border-subtle)',
            margin: '0 auto',
          }}
        />
      ) : (
        <div style={{ padding: '60px 100px', color: 'var(--text-muted)', fontSize: 14, textAlign: 'center' }}>
          Loading full-resolution screenshot...
        </div>
      )}
      <div style={{ marginTop: 12, fontSize: 11, fontFamily: 'monospace', color: 'var(--text-muted)', textAlign: 'center', wordBreak: 'break-all' }}>
        {imagePath}
      </div>
    </Modal>
  );
}
