import { readdirSync, readFileSync } from 'node:fs';
import { defineConfig } from 'astro/config';
import mdx from '@astrojs/mdx';
import sitemap from '@astrojs/sitemap';
import tailwindcss from '@tailwindcss/vite';

// Build a URL-to-date map from blog frontmatter for sitemap lastmod.
const blogDates = new Map();
for (const file of readdirSync(new URL('./src/content/blog/', import.meta.url))) {
  if (!file.endsWith('.mdx') && !file.endsWith('.md')) continue;
  const text = readFileSync(new URL(`./src/content/blog/${file}`, import.meta.url), 'utf-8');
  const fm = text.match(/^---\n([\s\S]*?)\n---/);
  if (!fm) continue;
  const mod = fm[1].match(/dateModified:\s*(.+)/);
  const pub = fm[1].match(/datePublished:\s*(.+)/);
  const date = mod ? new Date(mod[1].trim()) : pub ? new Date(pub[1].trim()) : null;
  if (date) {
    const slug = file.replace(/\.mdx?$/, '');
    blogDates.set(`/blog/${slug}/`, date);
  }
}

export default defineConfig({
  site: 'https://stacked-diffs-flow.com',
  integrations: [
    mdx(),
    sitemap({
      serialize(item) {
        const path = new URL(item.url).pathname;
        const date = blogDates.get(path);
        if (date) item.lastmod = date;
        return item;
      },
    }),
  ],
  output: 'static',
  markdown: {
    smartypants: false,
  },
  vite: {
    plugins: [tailwindcss()],
  },
});
