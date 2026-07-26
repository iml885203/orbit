// Used by graph canvas (DependencyEdge, ExternalNode, GroupNode, edge-path)
// to derive deterministic per-id offsets and colours.

export function stableHash(value: string): number {
    let h = 0
    for (let i = 0; i < value.length; i++) h = (h * 31 + value.charCodeAt(i)) | 0
    return Math.abs(h)
}

export function hashHue(name: string): number {
    return stableHash(name) % 360
}
