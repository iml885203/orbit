import React from "react";
import { Finale } from "./Finale";
import { AgentOpening } from "./AgentOpening";
import dashboardLayout from "../public/dashboard-layout.json";
import {
  AbsoluteFill,
  Audio,
  Easing,
  Img,
  interpolate,
  staticFile,
  useCurrentFrame,
} from "remotion";

const blue = "#8ec5ff",
  green = "#7ee787",
  muted = "#8b949e";
const ease = Easing.bezier(0.16, 1, 0.3, 1);
const progress = (t: number, start: number, duration = 0.8) =>
  interpolate(t, [start, start + duration], [0, 1], {
    easing: ease,
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });
const fade = (t: number, start: number, end: number) =>
  progress(t, start, 0.5) * (1 - progress(t, end, 0.4));
const mono: React.CSSProperties = { fontFamily: "Geist Mono, monospace" };
const nodes = [
  {
    name: "web",
    kind: "HOST PROCESS",
    x: 820,
    y: 110,
    ready: 9.8,
    color: "#a371f7",
  },
  {
    name: "catalog-api",
    kind: "HOST PROCESS",
    x: 1260,
    y: 370,
    ready: 9,
    color: "#39c5cf",
  },
  {
    name: "order-api",
    kind: "HOST PROCESS",
    x: 360,
    y: 370,
    ready: 9.2,
    color: "#39c5cf",
  },
  {
    name: "postgres",
    kind: "CONTAINER",
    x: 1260,
    y: 660,
    ready: 8,
    color: muted,
  },
  {
    name: "redis",
    kind: "CONTAINER",
    x: 360,
    y: 660,
    ready: 8.3,
    color: muted,
  },
];
const edges = [
  [0, 1],
  [0, 2],
  [1, 3],
  [2, 3],
  [2, 4],
];
const Label = ({
  children,
  style = {},
}: React.PropsWithChildren<{ style?: React.CSSProperties }>) => (
  <div
    style={{ ...mono, fontSize: 19, letterSpacing: 3, color: muted, ...style }}
  >
    {children}
  </div>
);
const Headline = ({
  t,
  start,
  end,
  eyebrow,
  title,
  sub,
}: {
  t: number;
  start: number;
  end: number;
  eyebrow: string;
  title: string;
  sub?: string;
}) => (
  <div
    style={{
      position: "absolute",
      left: 110,
      top: 100,
      opacity: fade(t, start, end),
      transform: `translateY(${25 * (1 - progress(t, start))}px)`,
      zIndex: 4,
    }}
  >
    <Label>{eyebrow}</Label>
    <div
      style={{
        fontSize: 78,
        letterSpacing: -4,
        marginTop: 22,
        fontWeight: 600,
      }}
    >
      {title.split(" ").map((word, i) => (
        <span
          key={i}
          style={{
            display: "inline-block",
            overflow: "hidden",
            verticalAlign: "top",
            marginRight: 18,
          }}
        >
          <span
            style={{
              display: "inline-block",
              transform: `translateY(${110 * (1 - progress(t, start + i * 0.065, 0.65))}%)`,
            }}
          >
            {word}
          </span>
        </span>
      ))}
    </div>
    {sub && (
      <div style={{ fontSize: 27, color: muted, marginTop: 16 }}>{sub}</div>
    )}
  </div>
);

const Graph = ({ t }: { t: number }) => {
  const assembled = progress(t, 5.8, 1.4),
    fault = t >= 13 && t < 18.8,
    focus = progress(t, 12, 1) - progress(t, 20, 1),
    match = progress(t, 20.7, 1.4);
  const positions = nodes.map((n, i) => {
    const captured = dashboardLayout.nodes.find((item) => item.id === n.name)!;
    const x = interpolate(
      assembled,
      [0, 1],
      [[170, 600, 1260, 360, 1440][i], n.x],
    );
    const y = interpolate(
      assembled,
      [0, 1],
      [[290, 490, 120, 710, 680][i], n.y],
    );
    return {
      x: x + (400 + (captured.x * 1120) / 1440 - x) * match,
      y: y + (275 + (captured.y * 1120) / 1440 - y) * match,
      w: 280 + ((captured.width * 1120) / 1440 - 280) * match,
      h: 130 + ((captured.height * 1120) / 1440 - 130) * match,
    };
  });
  return (
    <div
      style={{
        position: "absolute",
        inset: 0,
        opacity: fade(t, 3.5, 22.1),
        transform: `translate(${-330 * focus * (1 - match)}px,${(110 + 80 * focus) * (1 - match)}px) scale(${(0.82 - 0.27 * focus) * (1 - match) + match})`,
        transformOrigin: "50% 50%",
      }}
    >
      <svg
        width="1920"
        height="1080"
        style={{ position: "absolute", inset: 0, overflow: "visible" }}
      >
        <defs>
          <filter id="glow">
            <feGaussianBlur stdDeviation="5" />
          </filter>
        </defs>
        {edges.map(([a, b], i) => {
          const n = positions[a],
            d = positions[b],
            path = `M${n.x + n.w / 2} ${n.y + n.h} C${n.x + n.w / 2} ${n.y + n.h + 110},${d.x + d.w / 2} ${d.y - 100},${d.x + d.w / 2} ${d.y}`;
          const lit = t > nodes[b].ready + 0.4,
            color =
              fault && (b === 4 || (a === 0 && b === 2))
                ? "#f85149"
                : lit
                  ? blue
                  : "#334153";
          const drawn = progress(t, 6.2 + i * 0.12, 1),
            flow = (t * 0.45 + i * 0.2) % 1;
          return (
            <g
              key={i}
              opacity={
                assembled *
                (fault && b !== 4 && !(a === 0 && b === 2) ? 0.2 : 1)
              }
            >
              <path
                d={path}
                fill="none"
                stroke={color}
                strokeWidth="2"
                pathLength={1}
                strokeDasharray={1}
                strokeDashoffset={1 - drawn}
              />
              {lit && (
                <>
                  <path
                    d={path}
                    fill="none"
                    stroke={color}
                    strokeWidth="8"
                    opacity=".25"
                    filter="url(#glow)"
                    pathLength={1}
                    strokeDasharray=".06 .94"
                    strokeDashoffset={-flow}
                  />
                  <path
                    d={path}
                    fill="none"
                    stroke={color}
                    strokeWidth="3"
                    pathLength={1}
                    strokeDasharray=".035 .965"
                    strokeDashoffset={-flow}
                  />
                </>
              )}
            </g>
          );
        })}
      </svg>
      {nodes.map((n, i) => {
        const ready = t > n.ready,
          broken = fault && i === 4,
          blocked = fault && (i === 2 || i === 0);
        const { x, y, w, h } = positions[i];
        const state = broken
          ? "UNHEALTHY"
          : blocked
            ? "BLOCKED"
            : ready
              ? "HEALTHY"
              : t > 7
                ? "STARTING"
                : "STOPPED";
        const color = broken
          ? "#f85149"
          : blocked
            ? "#d29922"
            : ready
              ? green
              : muted;
        return (
          <div
            key={n.name}
            style={{
              position: "absolute",
              left: 0,
              top: 0,
              width: w,
              height: h,
              boxSizing: "border-box",
              padding: "22px 25px",
              borderRadius: 12,
              border: `1px solid ${broken ? "#f85149" : ready ? "#445e59" : "#303d4d"}`,
              background: "linear-gradient(130deg,#1b2532,#101720)",
              boxShadow: broken ? "0 0 65px #f8514925" : "0 20px 50px #0005",
              transform: `translate(${x}px,${y}px) rotate(${(1 - assembled) * [7, -8, 5, -5, 8][i]}deg)`,
              opacity:
                progress(t, 3.6 + i * 0.15) *
                (1 - 0.65 * focus * (i === 1 || i === 3 ? 1 : 0)),
            }}
          >
            <div
              style={{
                display: "flex",
                justifyContent: "space-between",
                alignItems: "center",
              }}
            >
              <span style={{ ...mono, fontSize: 25 }}>{n.name}</span>
              <span
                style={{
                  width: 9,
                  height: 9,
                  borderRadius: 10,
                  background: color,
                  boxShadow: `0 0 16px ${color}`,
                }}
              />
            </div>
            <div
              style={{
                ...mono,
                color: n.color,
                fontSize: 12,
                letterSpacing: 1.5,
                marginTop: 12,
                opacity: 1 - match,
              }}
            >
              {n.kind}
            </div>
            <div
              style={{
                ...mono,
                color,
                fontSize: 12,
                marginTop: 10,
                opacity: 1 - match,
              }}
            >
              {state}
            </div>
          </div>
        );
      })}
    </div>
  );
};

export const Film = () => {
  const actualTime = useCurrentFrame() / 60;
  const t = Math.max(0, actualTime - 3.5);
  return (
    <AbsoluteFill
      style={{
        background: "#080d14",
        color: "#e6edf3",
        fontFamily: "Geist, sans-serif",
        overflow: "hidden",
      }}
    >
      <Audio src={staticFile("score.wav")} />
      <AbsoluteFill
        style={{
          background:
            "radial-gradient(ellipse at 65% 55%,#15324a70,transparent 63%)",
        }}
      />
      <div
        style={{
          position: "absolute",
          inset: -100,
          backgroundImage: "radial-gradient(#8ec5ff20 1px,transparent 1px)",
          backgroundSize: "36px 36px",
          transform: `translateY(${-t * 2}px)`,
          maskImage: "radial-gradient(ellipse,black,transparent 75%)",
        }}
      />
      <div
        style={{
          position: "absolute",
          left: 110,
          right: 110,
          bottom: 65,
          height: 1,
          background: "#263344",
        }}
      >
        <div
          style={{
            height: 1,
            background: blue,
            transform: `scaleX(${actualTime / 33.5})`,
            transformOrigin: "left",
          }}
        />
      </div>
      <div
        style={{
          position: "absolute",
          bottom: 30,
          left: 110,
          ...mono,
          color: muted,
          fontSize: 14,
        }}
      >
        ORBIT / LOCAL DEVELOPMENT, UNDER CONTROL
      </div>
      <div
        style={{
          position: "absolute",
          bottom: 30,
          right: 110,
          ...mono,
          color: muted,
          fontSize: 14,
        }}
      >
        PRODUCT FILM · ILLUSTRATED WORKFLOW
      </div>
      <AgentOpening time={actualTime} />
      <Headline
        t={t}
        start={3.7}
        end={7.1}
        eyebrow="01 / MAKE THE SYSTEM VISIBLE"
        title="A whole stack. One Orbit."
      />
      <Headline
        t={t}
        start={7.4}
        end={11.7}
        eyebrow="02 / DEPENDENCIES FIRST"
        title="Ready. In the right order."
        sub="Host processes + containers. One dependency graph."
      />
      <Headline
        t={t}
        start={12}
        end={16.5}
        eyebrow="03 / A CONFIG CHANGE BREAKS REDIS"
        title="Find the cause."
      />
      <Headline
        t={t}
        start={16.9}
        end={20.7}
        eyebrow="04 / GIVE YOUR AGENT THE NEXT STEP"
        title="Get back to building."
      />
      <Graph t={t} />
      <div
        style={{
          position: "absolute",
          left: 110,
          top: 925,
          opacity: fade(t, 5, 11.7),
          ...mono,
          fontSize: 27,
          color: blue,
          background: "#080d14eb",
          padding: "23px 30px",
          borderLeft: "2px solid #8ec5ff",
        }}
      >
        $ orbit up --json
      </div>
      <div
        style={{
          position: "absolute",
          left: 1050,
          top: 340,
          width: 690,
          padding: 38,
          boxSizing: "border-box",
          border: "1px solid #59434b",
          borderRadius: 12,
          background: "#10161ff5",
          boxShadow: "0 30px 90px #0009",
          opacity: fade(t, 13.5, 20.7),
          transform: `translateX(${70 * (1 - progress(t, 13.5))}px)`,
        }}
      >
        <Label style={{ color: t < 18.8 ? "#f85149" : green }}>
          {t < 16.9
            ? "ORBIT / DIAGNOSTICS"
            : t < 18.8
              ? "CODING AGENT / RECOVERY"
              : "ORBIT / READINESS"}
        </Label>
        <div style={{ fontSize: 36, marginTop: 28, lineHeight: 1.3 }}>
          {t < 16.9
            ? "redis is unhealthy."
            : t < 18.8
              ? "Fix configuration. Retry."
              : "Dependencies ready."}
        </div>
        <div
          style={{
            ...mono,
            fontSize: 21,
            color: muted,
            lineHeight: 1.8,
            marginTop: 28,
            whiteSpace: "pre-line",
          }}
        >
          {t < 16.9
            ? "$ orbit logs redis --json\n\nBad directive: maxmemroy\nBlocked: order-api → web"
            : t < 18.8
              ? "redis.conf\n− maxmemroy 256mb\n+ maxmemory 256mb\n\n$ orbit up --json"
              : "redis          healthy\norder-api      healthy\nweb            healthy"}
        </div>
      </div>
      <div
        style={{ position: "absolute", inset: 0, opacity: fade(t, 22.1, 25.7) }}
      >
        <Headline
          t={t}
          start={21.1}
          end={25.7}
          eyebrow="05 / ONE SHARED VIEW"
          title="See what your agent sees."
        />
        <div
          style={{
            position: "absolute",
            left: 400,
            top: 275,
            width: 1120,
            transform: "none",
            transformOrigin: "50% 0",
            borderRadius: 12,
            overflow: "hidden",
            border: "1px solid #40516a",
            boxShadow: "0 45px 150px #000",
          }}
        >
          <Img
            src={staticFile("dashboard.png")}
            style={{ width: "100%", display: "block" }}
          />
        </div>
      </div>
      <Finale t={t} />
    </AbsoluteFill>
  );
};
