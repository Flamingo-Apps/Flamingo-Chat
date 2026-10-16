import react from '@vitejs/plugin-react'
// vitest/config's defineConfig extends Vite's with a `test` field -
// using it here instead of plain `vite` is what gives this file's `test`
// block below its types.
import { defineConfig } from 'vitest/config'

// Port 8000, not Vite's default 5173: Google's OAuth client (DEC-0027)
// only authorizes http://localhost and http://localhost:8000 as sign-in
// origins - see .claude/build-plan.md §2 and AGENTS.md.
// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    port: 8000,
    strictPort: true,
  },
  test: {
    environment: 'node',
  },
})
