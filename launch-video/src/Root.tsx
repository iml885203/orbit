import { Composition } from "remotion";
import { Film } from "./Film";
import "@fontsource/geist/500.css";
import "@fontsource/geist/600.css";
import "@fontsource/geist-mono/400.css";
export const Root = () => (
  <Composition
    id="Orbit"
    component={Film}
    width={1920}
    height={1080}
    fps={60}
    durationInFrames={2010}
  />
);
