import { defineConfig } from 'astro/config';
import mdx from '@astrojs/mdx';
import sitemap from '@astrojs/sitemap';
import tailwindcss from '@tailwindcss/vite';
import { blogDateMap } from './src/utils/content-dates';

export default defineConfig({
  site: 'https://stacked-diffs-flow.com',
  integrations: [
    mdx(),
    sitemap({
      serialize(item) {
        const path = new URL(item.url).pathname;
        const date = blogDateMap().get(path);
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
