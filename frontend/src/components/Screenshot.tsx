import { useState, useEffect, useCallback } from 'react';
import { buildApiUrl } from '../api';
import { Modal } from './Modal';
import { Icon } from './Icon';

declare const window: Window & {
  go?: {
    main: {
      App: {
        GetImageBase64: (path: string) => Promise<string>;
        GetThumbnailBase64?: (path: string) => Promise<string>;
      };
    };
  };
};

// In-Memory Image Cache (LRU up to 100 items) to eliminate IPC / network latency & UI lag
const imageMemoryCache = new Map<string, string>();
const pendingRequests = new Map<string, Promise<string | null>>();
const MAX_CACHE_SIZE = 100;

// Separate small cache for list-view thumbnails so timeline scrolling never
// swaps a preview with the huge full-resolution data URL (and vice versa).
const thumbMemoryCache = new Map<string, string>();
const pendingThumbRequests = new Map<string, Promise<string | null>>();

// The JS bridge delivers binding calls one after another, so a page with many
// rows would otherwise queue dozens of full-size image reads and freeze the
// UI. Previews are therefore requested through a small queue that keeps only
// a couple of requests in flight.
const MAX_CONCURRENT_THUMBS = 3;
let activeThumbRequests = 0;
const thumbQueue: Array<() => void> = [];

function acquireThumbSlot(): Promise<void> {
  if (activeThumbRequests < MAX_CONCURRENT_THUMBS) {
    activeThumbRequests += 1;
    return Promise.resolve();
  }
  return new Promise<void>((resolve) => {
    thumbQueue.push(() => {
      activeThumbRequests += 1;
      resolve();
    });
  });
}

function releaseThumbSlot() {
  activeThumbRequests = Math.max(0, activeThumbRequests - 1);
  const next = thumbQueue.shift();
  if (next) next();
}

function cacheImage(path: string, dataUrl: string) {
  if (imageMemoryCache.size >= MAX_CACHE_SIZE) {
    const firstKey = imageMemoryCache.keys().next().value;
    if (firstKey) imageMemoryCache.delete(firstKey);
  }
  imageMemoryCache.set(path, dataUrl);
}

function cacheThumb(path: string, dataUrl: string) {
  if (thumbMemoryCache.size >= MAX_CACHE_SIZE) {
    const firstKey = thumbMemoryCache.keys().next().value;
    if (firstKey) thumbMemoryCache.delete(firstKey);
  }
  thumbMemoryCache.set(path, dataUrl);
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

// fetchThumbData returns a lightweight preview for list rows: the Go-side WebP
// thumbnail when available, falling back to the standard image path (full data
// URL on desktop, direct URL on web).
function fetchThumbData(imagePath: string): Promise<string | null> {
  if (!imagePath) return Promise.resolve(null);

  if (thumbMemoryCache.has(imagePath)) {
    return Promise.resolve(thumbMemoryCache.get(imagePath)!);
  }

  if (pendingThumbRequests.has(imagePath)) {
    return pendingThumbRequests.get(imagePath)!;
  }

  const promise = (async () => {
    try {
      const goApp = (window as any).go?.main?.App;
      if (goApp?.GetThumbnailBase64) {
        await acquireThumbSlot();
        try {
          const res = await goApp.GetThumbnailBase64(imagePath);
          if (res) {
            cacheThumb(imagePath, res);
            return res;
          }
        } finally {
          releaseThumbSlot();
        }
      }
      if (!goApp) {
        // Web mode: a relative API URL is already far lighter than raw data
        const url = buildApiUrl(`/api/image?path=${encodeURIComponent(imagePath)}`);
        cacheThumb(imagePath, url);
        return url;
      }
    } catch {
      // fall through to the full-image loader
    } finally {
      pendingThumbRequests.delete(imagePath);
    }
    const full = await fetchImageData(imagePath);
    if (full) cacheThumb(imagePath, full);
    return full;
  })();

  pendingThumbRequests.set(imagePath, promise);
  return promise;
}

interface ThumbProps {
  imagePath?: string;
  onClick?: () => void;
  className?: string;
}

export function ScreenshotThumb({ imagePath, onClick, className }: ThumbProps) {
  const [node, setNode] = useState<HTMLElement | null>(null);
  const [inView, setInView] = useState(false);
  const [src, setSrc] = useState<string | null>(() => {
    return imagePath && thumbMemoryCache.has(imagePath) ? thumbMemoryCache.get(imagePath)! : null;
  });
  const [loading, setLoading] = useState(false);
  const [failed, setFailed] = useState(false);

  // Previews are only requested once the row is (nearly) visible, so a long
  // list never floods the bridge with image requests.
  useEffect(() => {
    if (!node || inView) return;
    if (typeof IntersectionObserver === 'undefined') {
      setInView(true);
      return;
    }
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          setInView(true);
          observer.disconnect();
        }
      },
      { rootMargin: '300px 0px' }
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, [node, inView]);

  useEffect(() => {
    if (!imagePath) {
      setSrc(null);
      setLoading(false);
      setFailed(false);
      return;
    }

    if (thumbMemoryCache.has(imagePath)) {
      setSrc(thumbMemoryCache.get(imagePath)!);
      setLoading(false);
      setFailed(false);
      return;
    }

    if (!inView) {
      setSrc(null);
      setLoading(false);
      return;
    }

    let isMounted = true;
    setLoading(true);
    setFailed(false);

    fetchThumbData(imagePath).then((data) => {
      if (isMounted) {
        setSrc(data);
        setLoading(false);
        setFailed(!data);
      }
    });

    return () => { isMounted = false; };
  }, [imagePath, inView]);

  if (loading && !src) {
    return (
      <div ref={setNode} className="timeline-thumb-placeholder skeleton">
        <Icon name="desktop" size={16} color="var(--text-muted)" />
      </div>
    );
  }

  if (!src) {
    return (
      <div
        ref={setNode}
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
      ref={setNode}
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
        style={{ height: 28, padding: '0 8px', display: 'inline-flex', alignItems: 'center', opacity: zoomScale <= 0.5 ? 0.6 : undefined, cursor: zoomScale <= 0.5 ? 'not-allowed' : undefined }}
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
        style={{ height: 28, padding: '0 8px', display: 'inline-flex', alignItems: 'center', opacity: zoomScale >= 3 ? 0.6 : undefined, cursor: zoomScale >= 3 ? 'not-allowed' : undefined }}
      >
        <Icon name="plus" size={14} />
      </button>
    </div>
  ) : null;

  // At 100% the screenshot is scaled down to fit the viewer, so no scrollbar
  // is needed. Scrolling only kicks in once the image is zoomed in.
  const zoomed = zoomScale > 1;

  return (
    <Modal
      isOpen={Boolean(imagePath)}
      onClose={onClose}
      title={titleNode}
      actions={zoomControls}
      maxWidth={1060}
      footer={
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 10, width: '100%' }}>
          <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
            Press Esc to close · zoom via header buttons or +/-
          </span>
          <button
            type="button"
            onClick={onClose}
            className="btn btn-primary btn-sm"
          >
            Close Inspection
          </button>
        </div>
      }
    >
      <div
        style={{
          overflow: zoomed ? 'auto' : 'hidden',
          height: 'clamp(160px, calc(100vh - 260px), 78vh)',
          maxHeight: '78vh',
          borderRadius: 'var(--radius-md)',
          background: 'var(--bg-base)',
          padding: 12,
          display: 'flex',
          alignItems: zoomed ? 'flex-start' : 'center',
          justifyContent: 'center',
        }}
      >
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
              width: zoomed ? `${zoomScale * 100}%` : 'auto',
              height: 'auto',
              maxWidth: zoomed ? 'none' : '100%',
              maxHeight: zoomed ? 'none' : '100%',
              objectFit: 'contain',
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
        <div style={{ marginTop: 10, fontSize: 11, fontFamily: 'monospace', color: 'var(--text-muted)', textAlign: 'center', wordBreak: 'break-all' }}>
          {imagePath}
        </div>
      )}
    </Modal>
  );
}
