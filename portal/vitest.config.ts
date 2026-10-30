import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

// CI 的 runner 只有 4 个 vCPU，且开着 coverage 时会变慢；本地机器核多，
// 用例跑得快。两边的并发和超时分开设，本地保持严格。
const isCI = Boolean(process.env.CI)

export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    include: ['src/**/*.test.{ts,tsx}'],
    restoreMocks: true,
    unstubGlobals: true,
    // console 外壳上的查询在 jsdom 下单次需要数秒，并行跑满核时更慢，
    // 默认 5 秒不够。
    maxWorkers: isCI ? 2 : undefined,
    testTimeout: isCI ? 60_000 : 20_000,
    hookTimeout: isCI ? 60_000 : 20_000,
    coverage: {
      provider: 'v8',
      reporter: ['text', 'html', 'lcov'],
      reportsDirectory: 'coverage',
      include: ['src/**/*.{ts,tsx}'],
      exclude: [
        'src/**/*.test.{ts,tsx}',
        'src/test/**',
        'src/main.tsx',
        'src/assets/**',
      ],
    },
  },
})
