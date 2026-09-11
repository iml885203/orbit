import { createServer } from "node:http";
import { readFile, writeFile } from "node:fs/promises";
import { chromium } from "playwright";

const videoBytes = await readFile("out/orbit-launch.mp4");
const server = createServer((req, res) => {
  if (req.url === "/film.mp4") {
    res.writeHead(200, {
      "Content-Type": "video/mp4",
      "Content-Length": videoBytes.length,
    });
    res.end(videoBytes);
    return;
  }
  res.setHeader("Content-Type", "text/html");
  res.end(
    '<body style="margin:0;background:#080d14"><video style="width:100%;height:100vh" autoplay muted playsinline src="/film.mp4"></video></body>',
  );
});
await new Promise((r) => server.listen(0, "127.0.0.1", r));
const browser = await chromium.launch({ channel: "chromium", headless: true });
try {
  const page = await browser.newPage({
    viewport: { width: 1280, height: 720 },
  });
  const errors = [];
  page.on("pageerror", (e) => errors.push(String(e)));
  await page.goto(`http://127.0.0.1:${server.address().port}`);
  const samples = [];
  for (const time of [2, 4.3, 5.8, 7.1, 10, 14, 18, 21.3, 23.1, 25.1, 26.1, 29.7, 32.5]) {
    await page.waitForFunction(
      (t) => document.querySelector("video").currentTime >= t,
      time,
      { timeout: 40000 },
    );
    await page.screenshot({ path: `out/playback-${time}.png` });
    samples.push(
      await page
        .locator("video")
        .evaluate((v) => ({
          time: v.currentTime,
          readyState: v.readyState,
          error: v.error?.message ?? null,
        })),
    );
  }
  await page.waitForFunction(
    () => document.querySelector("video").ended,
    null,
    { timeout: 5000 },
  );
  const result = await page.locator("video").evaluate((v) => {
    const q = v.getVideoPlaybackQuality();
    return {
      ended: v.ended,
      duration: v.duration,
      width: v.videoWidth,
      height: v.videoHeight,
      quality: {
        totalVideoFrames: q.totalVideoFrames,
        droppedVideoFrames: q.droppedVideoFrames,
        corruptedVideoFrames: q.corruptedVideoFrames,
      },
    };
  });
  await writeFile(
    "out/playback-review.json",
    JSON.stringify({ errors, samples, result }, null, 2),
  );
  console.log(JSON.stringify({ errors, result }, null, 2));
  if (errors.length || samples.some((s) => s.error) || !result.ended)
    process.exitCode = 1;
} finally {
  await browser.close();
  server.closeAllConnections();
  server.close();
}
