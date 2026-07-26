// envShortName strips the .yaml extension from an env file basename
// (e.g. "development.yaml" → "development"). Daemon-side `envShortName`
// in internal/daemon/envs.go is the source of truth; this mirrors it
// so the UI stops re-deriving the same regex in five places.
export function envShortName(name: string): string {
  return name.replace(/\.yaml$/, '')
}
