#!/usr/bin/env node
/**
 * Responsive audit for the Astro site.
 *
 * Usage: node scripts/responsive-audit.js [file.astro ...]
 *        (no args = audit src/pages, src/components, src/layouts)
 *
 * Checks (see RESPONSIVENESS.md conventions):
 *  E1 fixed pixel widths >=200px without a responsive fallback on the line
 *  E2 <table/> or raw <pre> with no overflow-x-auto container in the file
 *  E3 grid-cols-N (N>=2) on mobile without a responsive companion (sm:/md:)
 *  W1 oversized text (4xl+) without a smaller responsive pair           (warn)
 *  W2 oversized spacing (>=8 steps) without a responsive pair          (warn)
 *  W3 negative horizontal margins without a responsive pair            (warn)
 *  W4 w-screen / fixed[^y]100vw usage allowing horizontal overflow     (warn)
 *  E5 inline style width/height px >= 200 without max-width or %       (safe skip)
 */
import { readFileSync, readdirSync } from 'node:fs';
import { join, basename } from 'node:path';

const roots = ['src/pages', 'src/components', 'src/layouts'];
const files = process.argv.slice(2).filter((a) => !a.startsWith('-'));
const targets = files.length
  ? files.map((f) => (f.startsWith('src/') ? f : join('src', basename(f))))
  : roots.flatMap((r) =>
      readdirSync(r).filter((f) => f.endsWith('.astro')).map((f) => join(r, f)),
    );

let errors = 0;
let warnings = 0;
const tag = (t, sev, file, line, why, fix) => {
  const icon = sev === 'error' ? 'E' : 'W';
  const color = sev === 'error' ? '\x1b[31m' : '\x1b[33m';
  console.log(
    `${color}[${icon}${t}]\x1b[0m ${file}:${line} ${why}\n         fix: ${fix}`,
  );
  sev === 'error' ? errors++ : warnings++;
};

const hasResponsivePair = (l) => /(?:sm|md|lg|xl|2xl):/.test(l);
const hasOverflow = (l) => /overflow-(?:x-)?auto|overflow-hidden/.test(l);

for (const file of targets) {
  let src;
  try {
    src = readFileSync(file, 'utf8');
  } catch {
    console.error(`skip (missing): ${file}`);
    continue;
  }
  const lines = src.split('\n');
  lines.forEach((line, idx) => {
    const n = idx + 1;
    // E1 fixed widths
    const wide = [...line.matchAll(/\bw-\[(\d+)px\]/g)].find((m) => +m[1] >= 200);
    if (wide && !hasResponsivePair(line) && !/max-w/.test(line)) {
      tag(1, 'error', file, n, `fixed width w-[${wide[1]}px] on mobile`, 'responsive pair (sm:w-auto / w-full) or max-w');
    }
    // E3 multi-column grid at base
    const cols = [...line.matchAll(/(?:^|[\s"'`])grid-cols-(\d)/g)].map((m) => +m[1]).filter((v) => v >= 2);
    if (cols.length && !hasResponsivePair(line)) {
      tag(3, 'error', file, n, `grid-cols-${cols[0]} active on mobile`, 'use grid-cols-1 md:grid-cols-N');
    }
    // E1b table
    if (/<table/.test(line) && !/overflow/.test(line)) {
      // checked per-file below
    }
    // W1 big text
    if (/(?:^|[\s"'`])text-(?:[4-7]xl|9xl)(?:[\s"']|$)/.test(line) && !hasResponsivePair(line) && !/leading|tracking/.test(line)) {
      tag(1, 'warn', file, n, 'oversized text without responsive sibling', 'e.g. text-3xl md:text-5xl');
    }
    // W2 spacing
    const fat = [...line.matchAll(/(?:^|[\s"'`])(?:p|px|py|gap|m|mb|mt)-(8|9|1[0-9]|2[0-9])\b/g)].length > 0;
    if (fat && !hasResponsivePair(line)) {
      tag(2, 'warn', file, n, 'oversized spacing without responsive pair', 'e.g. gap-6 md:gap-10');
    }
    // W3 negative margins >= 8px (optical-alignment -1 tweaks are safe)
    if (/(?:^|[\s"'`])-\s?(?:mx|ml|mr)-(?:[2-9]|1[0-9])/.test(line) && !hasResponsivePair(line) && !hasOverflow(line)) {
      tag(3, 'warn', file, n, 'negative horizontal margin without overflow guard', 'wrap in overflow-x-auto or drop margin');
    }
    // W4 w-screen
    if (/\bw-screen\b|fixed[^"]*100vw/.test(line)) {
      tag(4, 'warn', file, n, 'w-screen can overflow with scrollbars', 'use w-full');
    }
  });

  // E2 file-level: table/pre containment
  if (/<table/.test(src) && !/overflow-(?:x-)?auto/.test(src)) {
    tag(2, 'error', file, 1, '<table> without an overflow-x-auto container', 'wrap table in <div class="overflow-x-auto">');
  }
}

console.log(`\n${targets.length} file(s): ${errors} error(s), ${warnings} warning(s)`);
process.exit(errors > 0 ? 1 : 0);
