import { writeFileSync } from "node:fs";
const rate = 48000,
  seconds = 33.5,
  data = Buffer.alloc(rate * seconds * 4);
const cuts = [0, 2.35, 5.05, 7.2, 10.9, 15.5, 20.4, 24.5, 29.5];
for (let i = 0; i < rate * seconds; i++) {
  const t = i / rate,
    beat = t % 0.6;
  const envelope = Math.min(1, t / 1.5, Math.max(0, (seconds - t) / 1.5));
  const chord =
    t < 15.5
      ? [110, 164.81, 220]
      : t < 24.5
        ? [98, 146.83, 196]
        : [130.81, 196, 261.63];
  let sample = chord.reduce(
    (s, h) =>
      s +
      Math.sin(2 * Math.PI * h * t) * 0.032 +
      Math.sin(2 * Math.PI * h * 1.002 * t) * 0.022,
    0,
  );
  sample +=
    Math.sin(2 * Math.PI * (48 * beat + 3 * (1 - Math.exp(-beat * 30)))) *
    Math.exp(-beat * 18) *
    0.16;
  const note = [0, 2, 1, 2, 0, 1, 2, 1][Math.floor(t / 0.3) % 8],
    phase = t % 0.3;
  sample +=
    Math.sin(2 * Math.PI * chord[note] * 4 * t) * Math.exp(-phase * 17) * 0.04;
  for (const cut of cuts) {
    const d = t - cut;
    if (d >= 0 && d < 1.2)
      sample +=
        Math.sin(2 * Math.PI * (60 * d + 24 * (1 - Math.exp(-d * 8)))) *
        Math.exp(-d * 5) *
        0.18;
  }
  for (let channel = 0; channel < 2; channel++)
    data.writeInt16LE(
      Math.round(Math.tanh(sample) * envelope * 27000),
      (i * 2 + channel) * 2,
    );
}
const header = Buffer.alloc(44);
header.write("RIFF");
header.writeUInt32LE(data.length + 36, 4);
header.write("WAVEfmt ", 8);
header.writeUInt32LE(16, 16);
header.writeUInt16LE(1, 20);
header.writeUInt16LE(2, 22);
header.writeUInt32LE(rate, 24);
header.writeUInt32LE(rate * 4, 28);
header.writeUInt16LE(4, 32);
header.writeUInt16LE(16, 34);
header.write("data", 36);
header.writeUInt32LE(data.length, 40);
writeFileSync(
  new URL("../public/score.wav", import.meta.url),
  Buffer.concat([header, data]),
);
