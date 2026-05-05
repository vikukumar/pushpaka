import { defineConfig } from 'astro/config';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  site: 'https://pushpaka.vikshro.in/',
  base: '/',
  vite: {
    plugins: [tailwindcss()],
  },
  output: 'static'
});
