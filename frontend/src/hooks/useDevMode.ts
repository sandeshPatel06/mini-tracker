import { useCallback, useEffect, useRef, useState } from 'react';

const DEV_MODE_SESSION_KEY = 'mini_dev_mode_session';
const TAP_TARGET = 7;
const TAP_WINDOW_MS = 2000;

export interface DevModeState {
  enabled: boolean;
  tapProgress: number;
  registerTap: () => void;
  disable: () => void;
  open: boolean;
  setOpen: (open: boolean) => void;
}

// Dev mode only exists inside the Wails desktop shell. In a plain browser
// (window.go missing) taps are ignored and dev mode can never be enabled.
function hasWailsApp(): boolean {
  const go = (window as any)?.go;
  return Boolean(go?.main?.App);
}

export function useDevMode(): DevModeState {
  const [enabled, setEnabled] = useState<boolean>(() => {
    if (!hasWailsApp()) return false;
    try {
      return sessionStorage.getItem(DEV_MODE_SESSION_KEY) === 'true';
    } catch {
      return false;
    }
  });
  const [tapProgress, setTapProgress] = useState(0);
  const [open, setOpen] = useState(false);
  const tapProgressRef = useRef(0);
  const lastTapAtRef = useRef(0);
  const enabledRef = useRef(enabled);
  enabledRef.current = enabled;

  const registerTap = useCallback(() => {
    if (enabledRef.current || !hasWailsApp()) return;
    const now = Date.now();
    const next = now - lastTapAtRef.current > TAP_WINDOW_MS ? 1 : tapProgressRef.current + 1;
    lastTapAtRef.current = now;

    if (next >= TAP_TARGET) {
      tapProgressRef.current = 0;
      lastTapAtRef.current = 0;
      setTapProgress(0);
      try {
        sessionStorage.setItem(DEV_MODE_SESSION_KEY, 'true');
      } catch {
        // sessionStorage unavailable — dev mode still works for this session render
      }
      setEnabled(true);
      return;
    }

    tapProgressRef.current = next;
    setTapProgress(next);
  }, []);

  const disable = useCallback(() => {
    tapProgressRef.current = 0;
    lastTapAtRef.current = 0;
    setTapProgress(0);
    setEnabled(false);
    setOpen(false);
    try {
      sessionStorage.removeItem(DEV_MODE_SESSION_KEY);
    } catch {
      // Ignore storage errors
    }
  }, []);

  // Ctrl+Shift+D toggles the menu, but only once dev mode is unlocked
  useEffect(() => {
    if (!enabled) return;
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.ctrlKey && event.shiftKey && (event.key === 'D' || event.key === 'd')) {
        event.preventDefault();
        setOpen((prev) => !prev);
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [enabled]);

  return { enabled, tapProgress, registerTap, disable, open, setOpen };
}
