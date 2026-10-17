// API helper module for environment-aware backend URL resolution

let runtimeBackendUrl = '';

export function setRuntimeBackendUrl(url: string) {
  if (url) {
    runtimeBackendUrl = url.replace(/\/$/, '');
  }
}

export function getApiBaseUrl(): string {
  if (runtimeBackendUrl) {
    return runtimeBackendUrl;
  }
  const envUrl =
    (import.meta as any).env?.VITE_BACKEND_URL ||
    (import.meta as any).env?.VITE_API_URL ||
    (import.meta as any).env?.VITE_BACKEND_ENDPOINT;

  if (envUrl) {
    return envUrl.replace(/\/$/, '');
  }
  if ((window as any).__BACKEND_URL__) {
    return (window as any).__BACKEND_URL__.replace(/\/$/, '');
  }
  return '';
}

export function buildApiUrl(path: string): string {
  const base = getApiBaseUrl();
  const cleanPath = path.startsWith('/') ? path : '/' + path;
  return base ? `${base}${cleanPath}` : cleanPath;
}

// ---------------------------------------------------------------------------
// Dev-only request monitor — records apiFetch timing into a capped ring buffer.
// Enabled only when DevMenu sets window.__DEV_MONITOR__ = true.
// ---------------------------------------------------------------------------

export interface DevApiLogEntry {
  time: string;
  method: string;
  path: string;
  status: number;
  ms: number;
  ok: boolean;
}

const DEV_API_LOG_LIMIT = 100;
let devApiLog: DevApiLogEntry[] = [];

export function getDevApiLog(): DevApiLogEntry[] {
  return devApiLog.slice();
}

export function clearDevApiLog(): void {
  devApiLog = [];
}

function recordDevApiCall(entry: DevApiLogEntry): void {
  if ((window as any).__DEV_MONITOR__ !== true) return;
  devApiLog.push(entry);
  if (devApiLog.length > DEV_API_LOG_LIMIT) {
    devApiLog.splice(0, devApiLog.length - DEV_API_LOG_LIMIT);
  }
}

export function apiFetch(path: string, options: RequestInit = {}): Promise<Response> {
  const url = buildApiUrl(path);
  const method = (options.method || 'GET').toUpperCase();
  const startedAt = Date.now();
  const headers = new Headers(options.headers || {});

  // Attach JWT or session token from localStorage for robust authentication
  try {
    const savedToken = localStorage.getItem('mini_jwt_token');
    if (savedToken) {
      headers.set('Authorization', `Bearer ${savedToken}`);
    } else {
      const savedUser = localStorage.getItem('mini_auth_user');
      if (savedUser) {
        const parsed = JSON.parse(savedUser);
        if (parsed?.token) {
          headers.set('Authorization', `Bearer ${parsed.token}`);
        } else if (parsed?.id) {
          headers.set('Authorization', `Bearer ${parsed.id}`);
          headers.set('X-Session-User-ID', `${parsed.id}`);
        }
      }
    }
  } catch {
    // Ignore JSON parse errors
  }

  return fetch(url, {
    credentials: 'include',
    ...options,
    headers,
  })
    .then((response) => {
      recordDevApiCall({
        time: new Date().toISOString(),
        method,
        path,
        status: response.status,
        ms: Date.now() - startedAt,
        ok: response.ok,
      });
      if (response.status === 401) {
        window.dispatchEvent(new CustomEvent('mini-auth-expired'));
      }
      return response;
    })
    .catch((error) => {
      recordDevApiCall({
        time: new Date().toISOString(),
        method,
        path,
        status: 0,
        ms: Date.now() - startedAt,
        ok: false,
      });
      throw error;
    });
}
