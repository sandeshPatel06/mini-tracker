import { apiFetch } from '../api';

const MISS_TTL_MS = 5 * 60 * 1000;

type CacheEntry = { value: string | null; at: number };

const cache = new Map<string, CacheEntry>();
const pending = new Map<string, Promise<string | null>>();

export function cleanAppName(name: string): string {
  let value = (name || '').trim().replace(/^["'`]+|["'`]+$/g, '');
  for (const separator of [' | ', ' — ', ' – ', ' :: ']) {
    const at = value.indexOf(separator);
    if (at >= 2) {
      value = value.slice(0, at);
      break;
    }
  }
  const colon = value.search(/[:：]/);
  if (colon >= 2) value = value.slice(0, colon);
  value = value.trim();
  if (/\.exe$/i.test(value)) value = value.slice(0, -4);
  return value.trim();
}

export function normalizeAppName(name: string): string {
  return cleanAppName(name)
    .toLowerCase()
    .replace(/[^\p{L}\p{N}]+/gu, '');
}

function cacheKey(name: string): string {
  return cleanAppName(name).toLowerCase();
}

export function getCachedAppIcon(name: string): string | null | undefined {
  const key = cacheKey(name);
  if (!key) return undefined;
  const entry = cache.get(key);
  if (!entry) return undefined;
  if (entry.value === null && Date.now() - entry.at > MISS_TTL_MS) {
    cache.delete(key);
    return undefined;
  }
  return entry.value;
}

export function getAppIcon(name: string): Promise<string | null> {
  const key = cacheKey(name);
  if (!key) return Promise.resolve(null);

  const cached = getCachedAppIcon(name);
  if (cached !== undefined) return Promise.resolve(cached);

  const inFlight = pending.get(key);
  if (inFlight) return inFlight;

  const promise = loadAppIcon(name)
    .catch(() => null)
    .then((value) => {
      cache.set(key, { value, at: Date.now() });
      pending.delete(key);
      return value;
    });

  pending.set(key, promise);
  return promise;
}

async function loadAppIcon(name: string): Promise<string | null> {
  const goApp = (window as any).go?.main?.App;
  if (typeof goApp?.GetAppIcon === 'function') {
    const data = await goApp.GetAppIcon(name);
    return typeof data === 'string' && data ? data : null;
  }

  try {
    const response = await apiFetch(`/api/app-icon?name=${encodeURIComponent(name)}`);
    if (!response.ok) return null;
    const blob = await response.blob();
    if (!blob.size) return null;
    return await blobToDataURL(blob);
  } catch {
    return null;
  }
}

function blobToDataURL(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result || ''));
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(blob);
  });
}
