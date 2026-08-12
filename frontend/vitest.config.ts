import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

// Test-only config: the production build config (vite.config.ts) outputs the
// embedded dashboard into ../internal/server/web and must stay untouched.
// happy-dom is used because jsdom's worker bootstrap is incompatible with
// this runtime (worker channel closes during environment setup).
export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'happy-dom',
    globals: false,
    include: ['src/**/*.test.{ts,tsx}'],
    setupFiles: ['./src/test/setup.ts'],
    // This runtime intermittently returns EPERM when many worker processes
    // open node_modules files concurrently. Serializing execution (single
    // fork, no file parallelism) keeps the test run deterministic.
    pool: 'forks',
    poolOptions: { forks: { singleFork: true } },
    fileParallelism: false,
    testTimeout: 20000,
  },
});
