import { useState, useEffect, useCallback } from 'react';
import { buildApiUrl } from '../api';
import { Modal } from './Modal';
import { Icon } from './Icon';

declare const window: Window & {
  go?: {
    main: {
      App: {
        GetImageBase64: (path: string) => Promise<string>;
      };
    };
  };
};

// In-Memory Image Cache (LRU up to 100 items) to eliminate IPC / network latency & UI lag
const imageMemoryCache = new Map<string, string>();
const pendingRequests = new Map<string, Promise<string | null>>();
const MAX_CACHE_SIZE = 100;

function cacheImage(path: string, dataUrl: string) {
  if (imageMemoryCache.size >= MAX_CACHE_SIZE) {
    const firstKey = imageMemoryCache.keys().next().value;
    if (firstKey) imageMemoryCache.delete(firstKey);
  }
  imageMemoryCache.set(path, dataUrl);
}

function fetchImageData(imagePath: string): Promise<string | null> {
  if (!imagePath) return Promise.resolve(null);
  if (imagePath.startsWith('data:image/') || imagePath.startsWith('blob:')) {
    return Promise.resolve(imagePath);
  }

  // Instant cache hit — 0ms lag!
  if (imageMemoryCache.has(imagePath)) {
    return Promise.resolve(imageMemoryCache.get(imagePath)!);
  }

  // Deduplicate concurrent requests for identical image path
  if (pendingRequests.has(imagePath)) {
    return pendingRequests.get(imagePath)!;
  }

  const promise = (async () => {
    try {
      if (window.go?.main?.App?.GetImageBase64) {
        const res = await window.go.main.App.GetImageBase64(imagePath);
        if (res) {
          cacheImage(imagePath, res);
          return res;
        }
      } else {
        const url = buildApiUrl(`/api/image?path=${encodeURIComponent(imagePath)}`);
        cacheImage(imagePath, url);
        return url;
      }
    } catch {
      // Ignore load errors
    } finally {
      pendingRequests.delete(imagePath);
    }
    return null;
  })();

  pendingRequests.set(imagePath, promise);
  return promise;
}

interface ThumbProps {
  imagePath?: string;
  onClick?: () => void;
  className?: string;
}

export function ScreenshotThumb({ imagePath, onClick, className }: ThumbProps) {
  const [src, setSrc] = useState<string | null>(() => {
    return imagePath && imageMemoryCache.has(imagePath) ? imageMemoryCache.get(imagePath)! : null;
  });
  const [loading, setLoading] = useState(false);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    if (!imagePath) {
      setSrc(null);
      setLoading(false);
      setFailed(false);
      return;
    }

    if (imageMemoryCache.has(imagePath)) {
      setSrc(imageMemoryCache.get(imagePath)!);
      setLoading(false);
      setFailed(false);
      return;
    }

    let isMounted = true;
    setLoading(true);
    setFailed(false);

    fetchImageData(imagePath).then((data) => {
      if (isMounted) {
        setSrc(data);
        setLoading(false);
        setFailed(!data);
      }
    });

    return () => { isMounted = false; };
  }, [imagePath]);

  if (loading && !src) {
    return (
      <div className="timeline-thumb-placeholder skeleton">
        <Icon name="desktop" size={16} color="var(--text-muted)" />
      </div>
    );
  }

  if (!src) {
    return (
      <div
        className="timeline-thumb-placeholder"
        title={failed ? 'Failed to load screenshot' : 'No screenshot captured'}
      >
        <Icon name="desktop" size={16} color="var(--text-muted)" />
      </div>
    );
  }

  const thumbClassName = `${className ? `${className} ` : ''}timeline-thumb-img`;

  return (
    <button
      type="button"
      onClick={onClick}
      className={thumbClassName}
      style={{
        padding: 0,
        appearance: 'none',
        cursor: onClick ? 'pointer' : 'default',
      }}
      aria-label="Open screenshot"
    >
      <img
        src={src}
        alt=""
        style={{
          display: 'block',
          width: '100%',
          height: '100%',
          objectFit: 'cover',
          borderRadius: 'inherit',
        }}
        onError={() => {
          setSrc(null);
          setFailed(true);
        }}
      />
    </button>
  );
}

interface ImageModalProps {
  imagePath: string | null;
  onClose: () => void;
}

export function ImageModal({ imagePath, onClose }: ImageModalProps) {
  const [src, setSrc] = useState<string | null>(null);
  const [loading, setLoading] = useState<boolean>(false);
  const [zoomScale, setZoomScale] = useState<number>(1);

  // Clear previous image state immediately on open to eliminate old image lingering/flickering
  useEffect(() => {
    if (!imagePath) {
      setSrc(null);
      setLoading(false);
      setZoomScale(1);
      return;
    }

    setZoomScale(1);

    if (imageMemoryCache.has(imagePath)) {
      setSrc(imageMemoryCache.get(imagePath)!);
      setLoading(false);
      return;
    }

    setSrc(null);
    setLoading(true);
    let isMounted = true;

    fetchImageData(imagePath).then((data) => {
      if (isMounted) {
        setSrc(data);
        setLoading(false);
      }
    });

    return () => { isMounted = false; };
  }, [imagePath]);

  const handleZoomIn = useCallback(
    () => setZoomScale(prev => Math.min(3, parseFloat((prev + 0.25).toFixed(2)))),
    []
  );
  const handleZoomOut = useCallback(
    () => setZoomScale(prev => Math.max(0.5, parseFloat((prev - 0.25).toFixed(2)))),
    []
  );
  const handleResetZoom = useCallback(() => setZoomScale(1), []);

  useEffect(() => {
    if (!imagePath) return;

    const handleZoomKeyDown = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement | null;
      if (
        target &&
        (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable)
      ) {
        return;
      }

      if (e.key === '+' || e.key === '=') {
        e.preventDefault();
        handleZoomIn();
      } else if (e.key === '-' || e.key === '_') {
        e.preventDefault();
        handleZoomOut();
      } else if (e.key === '0') {
        e.preventDefault();
        handleResetZoom();
      }
    };

    window.addEventListener('keydown', handleZoomKeyDown);
    return () => window.removeEventListener('keydown', handleZoomKeyDown);
  }, [imagePath, handleZoomIn, handleZoomOut, handleResetZoom]);

  const titleNode = imagePath ? (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
      <Icon name="desktop" size={18} color="var(--accent-primary)" />
      <span style={{ fontSize: 15, fontWeight: 700, color: 'var(--text-primary)' }}>
        Desktop Screenshot Inspection
      </span>
    </span>
  ) : null;

  const zoomControls = imagePath ? (
    <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
      <button
        onClick={handleZoomOut}
        disabled={zoomScale <= 0.5}
        className="btn btn-secondary btn-sm"
        title="Zoom Out (-)"
        aria-label="Zoom out"
        style={{ height: 28, padding: '0 8px', display: 'inline-flex', alignItems: 'center' }}
      >
        <Icon name="minus" size={14} />
      </button>
      <button
        onClick={handleResetZoom}
        className="btn btn-secondary btn-sm"
        title="Reset Zoom (100%)"
        aria-label="Reset zoom to 100%"
        style={{ height: 28, padding: '0 8px', fontSize: 11, fontFamily: 'monospace' }}
      >
        {Math.round(zoomScale * 100)}%
      </button>
      <button
        onClick={handleZoomIn}
        disabled={zoomScale >= 3}
        className="btn btn-secondary btn-sm"
        title="Zoom In (+)"
        aria-label="Zoom in"
        style={{ height: 28, padding: '0 8px', display: 'inline-flex', alignItems: 'center' }}
      >
        <Icon name="plus" size={14} />
      </button>
    </div>
  ) : null;

  return (
    <Modal
      isOpen={Boolean(imagePath)}
      onClose={onClose}
      title={titleNode}
      actions={zoomControls}
      maxWidth={1060}
    >
      <div style={{ overflow: 'auto', maxHeight: '78vh', borderRadius: 'var(--radius-md)', background: 'var(--bg-base)', padding: 12, display: 'flex', alignItems: 'center' }}>
        {loading ? (
          <div style={{ margin: 'auto', padding: '60px 100px', color: 'var(--text-muted)', fontSize: 13, textAlign: 'center', display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 10 }}>
            <Icon name="refresh" size={24} className="spin" color="var(--accent-primary)" />
            <span>Loading full-resolution screenshot...</span>
          </div>
        ) : src ? (
          <img
            src={src}
            alt="Desktop Screenshot Inspection"
            style={{
              margin: 'auto',
              width: `${zoomScale * 100}%`,
              maxWidth: 'none',
              height: 'auto',
              borderRadius: 'var(--radius-sm)',
              display: 'block',
              border: '1px solid var(--border-subtle)',
              boxShadow: 'var(--shadow-card)',
              imageRendering: '-webkit-optimize-contrast',
              transition: 'width 0.15s ease-out',
            }}
          />
        ) : (
          <div style={{ margin: 'auto', padding: '40px 60px', color: 'var(--text-muted)', fontSize: 13, textAlign: 'center' }}>
            Failed to load screenshot image
          </div>
        )}
      </div>

      {imagePath && (
        <div style={{ marginTop: 10, fontSize: 11, fontFamily: 'monospace', color: 'var(--text-muted)', textAlign: 'center', wordBreak: 'break-all', opacity: 0.8 }}>
          {imagePath}
        </div>
      )}
    </Modal>
  );
}
