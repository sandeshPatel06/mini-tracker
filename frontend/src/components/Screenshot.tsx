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

  useEffect(() => {
    if (!imagePath) {
      setSrc(null);
      setLoading(false);
      return;
    }

    if (imageMemoryCache.has(imagePath)) {
      setSrc(imageMemoryCache.get(imagePath)!);
      setLoading(false);
      return;
    }

    let isMounted = true;
    setLoading(true);

    fetchImageData(imagePath).then((data) => {
      if (isMounted) {
        setSrc(data);
        setLoading(false);
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
      <div className="timeline-thumb-placeholder">
        <Icon name="desktop" size={16} color="var(--text-muted)" />
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

  const handleZoomIn = () => setZoomScale(prev => Math.min(3, parseFloat((prev + 0.25).toFixed(2))));
  const handleZoomOut = () => setZoomScale(prev => Math.max(0.5, parseFloat((prev - 0.25).toFixed(2))));
  const handleResetZoom = () => setZoomScale(1);

  const titleNode = imagePath ? (
    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', width: '100%', paddingRight: 16 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <Icon name="desktop" size={18} color="var(--accent-primary)" />
        <span style={{ fontSize: 15, fontWeight: 700, color: 'var(--text-primary)' }}>Desktop Screenshot Inspection</span>
      </div>

      {/* Interactive Zoom Controls */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
        <button
          onClick={handleZoomOut}
          disabled={zoomScale <= 0.5}
          className="btn btn-secondary btn-sm"
          title="Zoom Out (-25%)"
          style={{ height: 28, padding: '0 8px', fontSize: 12 }}
        >
          -
        </button>
        <button
          onClick={handleResetZoom}
          className="btn btn-secondary btn-sm"
          title="Reset Zoom (100%)"
          style={{ height: 28, padding: '0 8px', fontSize: 11, fontFamily: 'monospace' }}
        >
          {Math.round(zoomScale * 100)}%
        </button>
        <button
          onClick={handleZoomIn}
          disabled={zoomScale >= 3}
          className="btn btn-secondary btn-sm"
          title="Zoom In (+25%)"
          style={{ height: 28, padding: '0 8px', fontSize: 12 }}
        >
          +
        </button>
      </div>
    </div>
  ) : null;

  return (
    <Modal
      isOpen={Boolean(imagePath)}
      onClose={onClose}
      title={titleNode}
      maxWidth={1060}
    >
      <div style={{ overflow: 'auto', maxHeight: '78vh', borderRadius: 'var(--radius-md)', background: 'var(--bg-base)', padding: 12, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        {loading ? (
          <div style={{ padding: '60px 100px', color: 'var(--text-muted)', fontSize: 13, textAlign: 'center', display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 10 }}>
            <Icon name="refresh" size={24} className="spin" color="var(--accent-primary)" />
            <span>Loading full-resolution screenshot...</span>
          </div>
        ) : src ? (
          <div style={{ transition: 'transform 0.15s ease-out', transform: `scale(${zoomScale})`, transformOrigin: 'top center', display: 'inline-block' }}>
            <img
              src={src}
              alt="Desktop Screenshot Inspection"
              style={{
                maxWidth: '100%',
                maxHeight: '70vh',
                borderRadius: 'var(--radius-sm)',
                display: 'block',
                objectFit: 'contain',
                border: '1px solid var(--border-subtle)',
                boxShadow: 'var(--shadow-card)',
                imageRendering: '-webkit-optimize-contrast',
              }}
            />
          </div>
        ) : (
          <div style={{ padding: '40px 60px', color: 'var(--text-muted)', fontSize: 13, textAlign: 'center' }}>
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
