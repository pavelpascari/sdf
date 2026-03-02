import { defineConfig } from 'astro/config';
import mdx from '@astrojs/mdx';
import sitemap from '@astrojs/sitemap';
import tailwindcss from '@tailwindcss/vite';
import { blogDateMap, docsDateMap } from './src/utils/content-dates';

const blogDates = blogDateMap();
const docsDates = docsDateMap();

export default defineConfig({
  site: 'https://stacked-diffs-flow.com',
  integrations: [
    mdx(),
    sitemap({
      serialize(item) {
        const path = new URL(item.url).pathname;
        const date = blogDates.get(path) ?? docsDates.get(path);
        item.lastmod = date ?? item.lastmod ?? new Date();
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
