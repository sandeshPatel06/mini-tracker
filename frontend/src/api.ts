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

export function apiFetch(path: string, options: RequestInit = {}): Promise<Response> {
  const url = buildApiUrl(path);
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
  });
}
