import { defineConfig } from 'astro/config';
import mdx from '@astrojs/mdx';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  site: 'https://stacked-diffs-flow.com',
  integrations: [mdx()],
  output: 'static',
  markdown: {
    smartypants: false,
  },
  vite: {
    plugins: [tailwindcss()],
  },
});
