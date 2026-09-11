import { Img, staticFile, interpolate, Easing } from "remotion";
import layout from "../public/dashboard-layout.json";

const settle = (t: number, start: number, duration = 1) =>
  interpolate(t, [start, start + duration], [0, 1], {
    easing: Easing.bezier(0.65, 0, 0.25, 1),
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });
export const Finale = ({ t }: { t: number }) => {
  const reveal = settle(t, 26.5, 0.6);
  return (
    <div style={{ position: "absolute", inset: 0, pointerEvents: "none" }}>
      <div
        style={{
          position: "absolute",
          inset: 0,
          opacity: settle(t, 25.7, 0.6),
          background: "#080d14",
        }}
      />
      {layout.nodes.map((n, i) => {
        const p = settle(t, 25.65 + i * 0.035, 1.05);
        const fromX = 400 + ((n.x + n.width / 2) * 1120) / 1440,
          fromY = 275 + ((n.y + n.height / 2) * 1120) / 1440;
        const x =
          (1 - p) ** 2 * fromX +
          2 * (1 - p) * p * (fromX + (i % 2 ? 200 : -200)) +
          p * p * 960;
        const y =
          (1 - p) ** 2 * fromY + 2 * (1 - p) * p * (fromY - 240) + p * p * 320;
        return (
          <div
            key={n.id}
            style={{
              position: "absolute",
              left: 0,
              top: 0,
              width: 32,
              height: 32,
              borderRadius: 32,
              background: "#8ec5ff",
              boxShadow: "0 0 32px #8ec5ff80",
              transform: `translate(${x - 16}px,${y - 16}px) scale(${1 - 0.75 * p})`,
              opacity: settle(t, 25.6, 0.15) * (1 - reveal),
            }}
          />
        );
      })}
      <div
        style={{
          position: "absolute",
          left: 780,
          top: 140,
          width: 360,
          height: 360,
          border: "1px solid #8ec5ff",
          borderRadius: "50%",
          transform: `scale(${0.15 + settle(t, 26.4, 1.4) * 1.7})`,
          opacity: Math.sin(settle(t, 26.4, 1.4) * Math.PI) * 0.3,
        }}
      />
      <Img
        src={staticFile("orbit-logo.svg")}
        style={{
          position: "absolute",
          left: 884,
          top: 244,
          width: 152,
          height: 152,
          opacity: reveal,
          transform: `scale(${0.7 + 0.3 * reveal}) rotate(${-45 * (1 - reveal)}deg)`,
        }}
      />
      <div
        style={{
          position: "absolute",
          top: 420,
          left: 0,
          right: 0,
          textAlign: "center",
        }}
      >
        {[
          ["Orbit", 112, "#e6edf3", 26.65],
          ["Build the product.", 42, "#e6edf3", 26.95],
          ["Let your agent run the project.", 42, "#8ec5ff", 27.15],
          ["orbit.dotw.me", 24, "#8b949e", 27.45],
        ].map(([text, size, color, start], i) => (
          <div
            key={i}
            style={{
              overflow: "hidden",
              marginTop: i === 1 ? 24 : i === 3 ? 42 : 4,
            }}
          >
            <div
              style={{
                fontSize: Number(size),
                color: String(color),
                fontWeight: i === 0 ? 600 : 500,
                letterSpacing: i === 0 ? -6 : -1,
                fontFamily: i === 3 ? "Geist Mono" : "Geist",
                transform: `translateY(${120 * (1 - settle(t, Number(start), 0.65))}%)`,
              }}
            >
              {text}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
};
