import { useEffect, useState } from 'react';
import type { CSSProperties } from 'react';
import { Icon } from './Icon';
import { brandIconFor } from '../lib/appIconBrands';
import { cleanAppName, getAppIcon, getCachedAppIcon } from '../lib/appIcons';

interface AppIconProps {
  name?: string | null;
  size?: number;
  className?: string;
}

function firstCharacter(value: string): string {
  return Array.from(value)[0] || '';
}

function initialsFor(name: string): string {
  const tokens = cleanAppName(name).split(/[\s._-]+/).filter(Boolean);
  if (tokens.length === 0) return '?';
  if (tokens.length >= 2) {
    if (/^[A-Z0-9]{2,3}$/.test(tokens[0])) return tokens[0];
    return (firstCharacter(tokens[0]) + firstCharacter(tokens[1])).toUpperCase();
  }
  const token = tokens[0];
  if (/^[A-Z0-9]{2,3}$/.test(token)) return token;
  return firstCharacter(token).toUpperCase();
}

function hueFor(name: string): number {
  let hash = 0;
  for (const character of name) {
    hash = (hash * 31 + (character.codePointAt(0) || 0)) >>> 0;
  }
  return hash % 360;
}

function monogramStyle(name: string, size: number): CSSProperties {
  const hue = hueFor(cleanAppName(name).toLowerCase());
  return {
    width: size,
    height: size,
    fontSize: Math.max(9, Math.round(size * 0.46)),
    background: `linear-gradient(135deg, hsl(${hue} 45% 40%), hsl(${(hue + 28) % 360} 52% 30%))`,
  };
}

export function AppIcon({ name, size = 16, className = '' }: AppIconProps) {
  const appName = (name || '').trim();
  const [loaded, setLoaded] = useState<{ name: string; src: string | null }>(() => ({
    name: appName,
    src: appName ? getCachedAppIcon(appName) || null : null,
  }));
  const [brokenFor, setBrokenFor] = useState<string | null>(null);
  const systemSrc = loaded.name === appName ? loaded.src : null;
  const broken = brokenFor === appName;

  useEffect(() => {
    let active = true;

    if (!appName) {
      setLoaded({ name: '', src: null });
      return () => {
        active = false;
      };
    }

    const cached = getCachedAppIcon(appName);
    if (cached !== undefined) {
      setLoaded({ name: appName, src: cached });
      return () => {
        active = false;
      };
    }

    setLoaded({ name: appName, src: null });
    getAppIcon(appName).then((source) => {
      if (active) setLoaded({ name: appName, src: source });
    });

    return () => {
      active = false;
    };
  }, [appName]);

  if (!appName) {
    return (
      <span
        className={`app-icon app-icon-fallback ${className}`.trim()}
        style={{ width: size, height: size }}
        aria-hidden="true"
      >
        <Icon name="desktop" size={Math.max(12, Math.round(size * 0.7))} />
      </span>
    );
  }

  const source = broken ? null : systemSrc || brandIconFor(appName);
  if (source) {
    return (
      <img
        className={`app-icon ${className}`.trim()}
        src={source}
        width={size}
        height={size}
        alt=""
        title={appName}
        aria-hidden="true"
        draggable={false}
        loading="lazy"
        decoding="async"
        style={{ width: size, height: size }}
        onError={() => {
          if (systemSrc) {
            setLoaded({ name: appName, src: null });
          } else {
            setBrokenFor(appName);
          }
        }}
      />
    );
  }

  return (
    <span
      className={`app-icon app-icon-mono ${className}`.trim()}
      style={monogramStyle(appName, size)}
      title={appName}
      aria-hidden="true"
    >
      {initialsFor(appName)}
    </span>
  );
}
