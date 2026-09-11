import { Easing, interpolate } from "remotion";

const enter = (t: number, start: number, duration = 0.5) =>
  interpolate(t, [start, start + duration], [0, 1], {
    easing: Easing.bezier(0.16, 1, 0.3, 1),
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });
const mono = { fontFamily: "Geist Mono, monospace" };
export const AgentOpening = ({ time: t }: { time: number }) => {
  const exit = enter(t, 6.85, 0.5);
  const appearance = (start: number) => ({
    opacity: enter(t, start),
    transform: `translateY(${18 * (1 - enter(t, start))}px)`,
  });
  return (
    <div
      style={{
        position: "absolute",
        left: 370,
        top: 160,
        width: 1180,
        opacity: 1 - exit,
        transform: `translateY(${-45 * exit}px) scale(${1 - 0.035 * exit})`,
        transformOrigin: "50% 100%",
      }}
    >
      <div
        style={{
          ...mono,
          color: "#8b949e",
          fontSize: 20,
          letterSpacing: 2,
          marginBottom: 45,
          ...appearance(0),
        }}
      >
        ONE REQUEST. YOUR AGENT TAKES IT FROM HERE.
      </div>
      <div
        style={{
          display: "flex",
          justifyContent: "flex-end",
          ...appearance(0.15),
        }}
      >
        <div
          style={{
            padding: "27px 34px",
            background: "#1b2b40",
            border: "1px solid #354f6e",
            borderRadius: "22px 22px 6px 22px",
            maxWidth: 900,
          }}
        >
          <div
            style={{
              ...mono,
              fontSize: 16,
              color: "#8ec5ff",
              marginBottom: 13,
            }}
          >
            YOU
          </div>
          <div style={{ fontSize: 43, letterSpacing: -1 }}>
            Get this project running with Orbit.
          </div>
        </div>
      </div>
      <div
        style={{ marginTop: 42, display: "flex", gap: 22, ...appearance(1.25) }}
      >
        <div
          style={{
            width: 44,
            height: 44,
            display: "grid",
            placeItems: "center",
            border: "1px solid #415a75",
            borderRadius: 12,
            color: "#8ec5ff",
            fontSize: 26,
          }}
        >
          ✳
        </div>
        <div style={{ flex: 1 }}>
          <div
            style={{
              ...mono,
              color: "#8b949e",
              fontSize: 16,
              marginBottom: 15,
            }}
          >
            CODING AGENT
          </div>
          <div style={{ fontSize: 31, lineHeight: 1.45 }}>
            I’ll inspect the environment, then start the stack.
          </div>
          <div
            style={{
              marginTop: 28,
              border: "1px solid #303d4d",
              borderRadius: 12,
              background: "#101923",
              overflow: "hidden",
              ...appearance(2.35),
            }}
          >
            <div
              style={{
                padding: "16px 25px",
                borderBottom: "1px solid #263344",
                display: "flex",
                justifyContent: "space-between",
                ...mono,
                fontSize: 15,
                color: "#8b949e",
              }}
            >
              <span>TERMINAL · AGENT TOOL CALL</span>
              <span style={{ color: t >= 4 ? "#7ee787" : "#8ec5ff" }}>
                {t >= 4 ? "✓ inspected" : "running"}
              </span>
            </div>
            <div
              style={{
                ...mono,
                fontSize: 27,
                padding: "22px 25px",
                color: "#8ec5ff",
              }}
            >
              <span style={{ color: "#61758b" }}>$ </span>orbit inspect --json
            </div>
            <div
              style={{
                ...mono,
                fontSize: 19,
                lineHeight: 1.65,
                color: "#a9b9cb",
                padding: "0 25px 22px",
                ...appearance(3.65),
              }}
            >
              5 resources · 3 host processes · 2 containers
              <br />
              <span style={{ color: "#7ee787" }}>
                Next action: orbit up --json
              </span>
            </div>
          </div>
          <div
            style={{
              marginTop: 16,
              padding: "22px 25px",
              border: "1px solid #405877",
              borderRadius: 12,
              background: "#142236",
              ...mono,
              fontSize: 27,
              color: "#8ec5ff",
              display: "flex",
              justifyContent: "space-between",
              ...appearance(5.05),
            }}
          >
            <span>
              <span style={{ color: "#61758b" }}>$ </span>orbit up --json
            </span>
            <span style={{ fontSize: 17, alignSelf: "center" }}>
              Starting dependencies…
            </span>
          </div>
        </div>
      </div>
    </div>
  );
};
