import { mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const websiteRoot = fileURLToPath(new URL('../', import.meta.url))
const repositoryRoot = resolve(websiteRoot, '..')
const skillRoot = join(repositoryRoot, 'plugins/orbit/skills/orbit')
const mirrorRoot = join(websiteRoot, '.vitepress/dist/agent')

function filesBelow(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = join(directory, entry.name)
    return entry.isDirectory() ? filesBelow(path) : [path]
  })
}

rmSync(mirrorRoot, { recursive: true, force: true })
for (const source of filesBelow(skillRoot)) {
  const destination = join(mirrorRoot, relative(skillRoot, source))
  mkdirSync(dirname(destination), { recursive: true })
  writeFileSync(destination, readFileSync(source))
}

console.log('published the canonical Orbit agent skill')
