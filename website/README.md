# get-Hike Marketing Website

This directory contains the production marketing website for **get-Hike**, an open-source, privacy-first Linux productivity tracker with optional Gemini AI analysis and optional team features (MIT License, © REAK INFOTECH LLP).

Built with **Astro 5**, **TypeScript**, and **Tailwind CSS**.

---

## 🚀 Getting Started

### Prerequisites

- **Node.js** v18.14.0 or later (v20+ recommended)
- **npm** or **pnpm**

### Development

To start the local Astro development server:

```bash
cd website
npm install
npm run dev
```

Open [http://localhost:4321](http://localhost:4321) in your browser.

### Production Build & Preview

To generate the static build:

```bash
npm run build
```

The compiled static assets and HTML files are output into `website/dist/`.

To preview the production build locally:

```bash
npm run preview
```

---

## 📁 Directory Structure

```text
website/
├── astro.config.mjs       # Astro configuration (integrations, sitemap, tailwind)
├── package.json           # Dependencies and scripts
├── tsconfig.json          # TypeScript strict configuration
├── public/                # Static assets served at root
│   ├── favicon.svg        # Scalable SVG brand icon
│   ├── favicon.png        # 32x32 raster favicon
│   ├── og-default.png     # 1200x630 social card image (Open Graph / Twitter)
│   ├── og-default.svg     # Source SVG template for the social card
│   └── robots.txt         # Search engine directives & sitemap reference
├── scripts/
│   └── generate-og.js     # Sharp utility to regenerate branded OG & favicon images
└── src/
    ├── components/        # Reusable UI components
    │   ├── BaseHead.astro    # Full SEO, OG, Twitter, and JSON-LD meta tags
    │   ├── Button.astro      # Accessible button & link component
    │   ├── CodeBlock.astro   # Terminal snippet with one-click copy feedback
    │   ├── CTASection.astro  # Bottom conversion section with install command
    │   ├── FAQ.astro         # Accessible accordion (ARIA expanded & keyboard navigation)
    │   ├── FeatureCard.astro # Feature card with emerald border styling
    │   ├── Footer.astro      # Site footer with MIT license and links
    │   ├── Header.astro      # Sticky navigation with mobile drawer & focus trap
    │   ├── Step.astro        # Numbered step card for How It Works
    │   └── TrustStrip.astro  # High-impact trust bar (MIT, Linux, No keylogging, Local-first)
    ├── layouts/
    │   └── BaseLayout.astro  # Base shell with skip-links, Header, Main, and Footer
    ├── pages/
    │   ├── index.astro       # Landing page with hero, features, workflow, FAQ, CTA
    │   ├── features.astro    # Deep technical feature and architecture breakdown
    │   ├── get-started.astro # Step-by-step installation, prerequisites, systemd guide
    │   ├── privacy.astro     # Transparent privacy policy aligned with product code
    │   ├── changelog.astro   # Release timeline and milestones
    │   └── 404.astro         # Branded not-found page
    └── styles/
        └── global.css        # Tailwind CSS imports, color tokens, and accessibility styling
```

---

## 🎨 Design System

- **Base Colors:** Deep dark surface `#080e11`, elevated card `#0d171c`, accent border `rgba(16, 185, 129, 0.16)`
- **Brand Accent:** Emerald `#10b981` (and `#34d399` highlights)
- **Typography:** Modern clean system UI stack with monospace accents for terminal commands
- **Accessibility:**
  - Semantic HTML landmarks (`header`, `main`, `nav`, `footer`)
  - Skip link (`#main-content`)
  - Full keyboard accessibility and visible focus rings
  - `prefers-reduced-motion` compliance
  - 44×44px minimum touch targets

---

## 🌐 SEO & Social Previews

Every page includes:
- Unique title (≤ 60 chars) and meta description (≤ 155 chars)
- Canonical URL
- Open Graph (`og:title`, `og:description`, `og:image`, `og:url`)
- Twitter Cards (`summary_large_image`)
- Valid JSON-LD `SoftwareApplication` on `/`
- Auto-generated `sitemap-index.xml` via `@astrojs/sitemap`

---

## 🚢 Deployment

The output in `dist/` is purely static and can be deployed to any static host:

- **Cloudflare Pages:** Build command `npm run build`, output directory `dist`
- **Vercel:** Framework Preset `Astro`, output directory `dist`
- **Netlify:** Build command `npm run build`, publish directory `dist`
- **GitHub Pages:** Deploy via GitHub Actions using the standard Astro deployment action
