# lazyagent docs

This folder holds the public-facing documentation for lazyagent. It has two kinds of files:

- **`index.astro`** — the Astro source for the [lazyagent.dev/docs](https://lazyagent.dev/docs) page. This is the single source of truth for the public docs.
- **`API.md`** — developer-facing reference for the HTTP API, linked from the root README. Plain Markdown, lives only here.

## Why keep the Astro source inside the lazyagent repo?

The website repo (`lazyagent.dev`) tracks this one upstream. Whenever a feature lands here we update `docs/index.astro` in the same commit so docs never drift from behavior. The website build then picks up the latest page from here instead of being edited independently.

## Syncing to lazyagent.dev

When you want to publish an update, copy `docs/index.astro` from this repo into the website repo:

```bash
# From the root of illegal.studio workspace
cp lazyagent/docs/index.astro lazyagent.dev/src/pages/docs/index.astro

# Update the upstream pointer so the website records the synced commit
cd lazyagent.dev
git log -1 --pretty=format:'%H' ../lazyagent > /tmp/synced-sha
sed -i '' "s/Last synced commit:.*/Last synced commit: \`$(cat /tmp/synced-sha)\` ($(date +%Y-%m-%d))/" UPSTREAM.md
git add src/pages/docs/index.astro UPSTREAM.md
git commit -m "docs: sync from lazyagent"
```

## Layout dependencies

`index.astro` imports `DocsLayout`, `DocsNav`, and `Footer` from the website repo's `src/layouts/` and `src/components/`. Those are site-wide chrome and live only in `lazyagent.dev` — the import paths (`../../layouts/…`, `../../components/…`) are written to resolve from `lazyagent.dev/src/pages/docs/index.astro`, so copying the file straight in keeps everything wired up.

If those layout files change in a way that breaks the page (new required props, etc.), update `index.astro` in this repo to match, then sync.

## Local preview

To preview changes you made here:

```bash
cp lazyagent/docs/index.astro lazyagent.dev/src/pages/docs/index.astro
cd lazyagent.dev
npm run dev  # Astro dev server, usually http://localhost:4321
```
