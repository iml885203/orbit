import { createServer } from "node:http";
import { createReadStream, statSync } from "node:fs";
const host = process.env.PREVIEW_HOST ?? "127.0.0.1";
createServer((req, res) => {
  res.setHeader("Cache-Control", "no-store");
  const path = new URL(req.url, "http://localhost").pathname;
  if (path === "/") {
    res.setHeader("Content-Type", "text/html; charset=utf-8");
    res.end(
      '<!doctype html><meta name="viewport" content="width=device-width, initial-scale=1"><title>Orbit 展示影片</title><body style="margin:0;background:#080d14;display:grid;place-items:center;min-height:100vh"><video controls playsinline preload="metadata" style="width:100%;max-height:100vh" src="/video.mp4"></video>',
    );
    return;
  }
  if (path !== "/video.mp4" && path !== "/poster.png") {
    res.writeHead(404);
    res.end();
    return;
  }
  const file = new URL(path === '/poster.png' ? '../../docs/assets/orbit-showcase.png' : '../out/orbit-launch.mp4', import.meta.url);
  const size = statSync(file).size;
  const range = /^bytes=(\d+)-(\d*)$/.exec(req.headers.range ?? "");
  const start = range ? Number(range[1]) : 0,
    end = range && range[2] ? Math.min(Number(range[2]), size - 1) : size - 1;
  if (start > end || start >= size) {
    res.writeHead(416, { "Content-Range": `bytes */${size}` });
    res.end();
    return;
  }
  res.setHeader("Content-Type", path === '/poster.png' ? 'image/png' : 'video/mp4');
  res.setHeader("Accept-Ranges", "bytes");
  res.setHeader("Content-Length", end - start + 1);
  if (range) res.setHeader("Content-Range", `bytes ${start}-${end}/${size}`);
  res.writeHead(range ? 206 : 200);
  if (req.method === "HEAD") {
    res.end();
    return;
  }
  createReadStream(file, { start, end }).pipe(res);
}).listen(8765, host, () => console.log(`Preview: http://${host}:8765`));
