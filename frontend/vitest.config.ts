import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import path from 'path';

export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./vitest.setup.ts'],
    exclude: ['node_modules/**', 'e2e/**'],
    coverage: {
      provider: 'v8',
      include: ['components/**', 'app/**', 'hooks/**', 'lib/**'],
      exclude: ['**/*.d.ts', 'app/**/layout.tsx', 'app/fonts/**'],
      thresholds: {
        lines: 88,
        statements: 88,
        functions: 88,
        branches: 88,
      },
    },
  },
  resolve: {
    alias: { '@': path.resolve(__dirname, '.') },
  },
});
