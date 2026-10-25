// Copy the Vite build next to the Go sources so a `-tags portalembed` build
// can embed it. Runs at the end of `pnpm build`.
import { cpSync, existsSync, rmSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '../..')
const from = resolve(root, 'portal/dist')
const to = resolve(root, 'internal/gateway/portal/webdist')

if (!existsSync(dirname(to))) {
  // The portal package is not around: nothing to sync.
  console.log(`skip embed sync: ${dirname(to)} does not exist`)
  process.exit(0)
}

rmSync(to, { recursive: true, force: true })
cpSync(from, to, { recursive: true })
console.log('embed sync: portal/dist -> internal/gateway/portal/webdist')
