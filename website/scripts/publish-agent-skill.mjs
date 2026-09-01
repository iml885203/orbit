import { mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs'
import { createHash } from 'node:crypto'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const websiteRoot = fileURLToPath(new URL('../', import.meta.url))
const repositoryRoot = resolve(websiteRoot, '..')
const skillRoot = join(repositoryRoot, 'plugins/orbit/skills/orbit')
const mirrorRoot = join(websiteRoot, '.vitepress/dist/agent')
const discoveryRoot = join(websiteRoot, '.vitepress/dist/.well-known/agent-skills')

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

const skillArtifact = readFileSync(join(skillRoot, 'SKILL.md'))
const discoveryIndex = {
  $schema: 'https://schemas.agentskills.io/discovery/0.2.0/schema.json',
  skills: [{
    name: 'orbit',
    type: 'skill-md',
    description: 'Operate and diagnose local development environments with the Orbit CLI.',
    url: 'https://orbit.dotw.me/agent/SKILL.md',
    digest: `sha256:${createHash('sha256').update(skillArtifact).digest('hex')}`,
  }],
}
mkdirSync(discoveryRoot, { recursive: true })
writeFileSync(join(discoveryRoot, 'index.json'), `${JSON.stringify(discoveryIndex, null, 2)}\n`)

console.log('published the canonical Orbit agent skill and discovery index')
