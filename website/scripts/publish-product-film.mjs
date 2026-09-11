import { copyFile, mkdir } from 'node:fs/promises'

const mediaDirectory = new URL('../.vitepress/dist/media/', import.meta.url)
await mkdir(mediaDirectory, { recursive: true })
await copyFile(
  new URL('../../docs/assets/orbit-launch.mp4', import.meta.url),
  new URL('orbit-launch.mp4', mediaDirectory),
)
console.log('Published Orbit product film: /media/orbit-launch.mp4')
