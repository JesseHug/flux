import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";

import { STARDEW_TRACKS, type SceneKey } from "../assets";
import { useStardewEnv } from "../env-context";

import {
  StardewMusicEngine,
  type ActiveEngine,
  type MusicSnapshot,
  type MusicStatus,
} from "./music-engine";

import { useThemeContext } from "@/themes/context";

interface StardewAudioContextValue {
  playing: boolean;
  status: MusicStatus;
  activeEngine: ActiveEngine;
  volume: number;
  trackTitle: string;
  sceneZh: string;
  togglePlay: () => void;
  setVolume: (v: number) => void;
}

const VOLUME_KEY = "flux:stardew:volume";

function readVolume(): number {
  try {
    const n = Number(localStorage.getItem(VOLUME_KEY));

    if (Number.isFinite(n) && n >= 0 && n <= 1) return n;
  } catch {
    // ignore
  }

  return 0.45;
}

const StardewAudioContext = createContext<StardewAudioContextValue | null>(
  null,
);

export const StardewAudioProvider: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => {
  const { effectiveMode, activeThemeId } = useThemeContext();
  const { effectiveSeason, audioEnginePref, customAssetBaseUrl } =
    useStardewEnv();
  const sceneKey: SceneKey = `${effectiveSeason}-${effectiveMode === "dark" ? "night" : "day"}`;

  const [snapshot, setSnapshot] = useState<MusicSnapshot>(() => ({
    enabled: false,
    status: "off",
    activeEngine: "ogg",
    scene: sceneKey,
    volume: readVolume(),
  }));

  const engineRef = useRef<StardewMusicEngine | null>(null);

  if (!engineRef.current) {
    engineRef.current = new StardewMusicEngine({
      volume: readVolume(),
      onState: setSnapshot,
    });
  }

  useEffect(() => {
    return () => {
      engineRef.current?.dispose();
      engineRef.current = null;
    };
  }, []);

  useEffect(() => {
    engineRef.current?.configure(audioEnginePref, customAssetBaseUrl);
  }, [audioEnginePref, customAssetBaseUrl]);

  useEffect(() => {
    engineRef.current?.setScene(sceneKey);
  }, [sceneKey]);

  // Stop music automatically if the user switches away from the stardew theme.
  useEffect(() => {
    if (activeThemeId && activeThemeId !== "stardew" && snapshot.enabled) {
      engineRef.current?.disable();
    }
  }, [activeThemeId, snapshot.enabled]);

  const togglePlay = useCallback(() => {
    const engine = engineRef.current;

    if (!engine) return;
    if (snapshot.enabled) {
      engine.disable();
    } else {
      engine.enable();
    }
  }, [snapshot.enabled]);

  const setVolume = useCallback((v: number) => {
    const clamped = Math.max(0, Math.min(1, v));

    try {
      localStorage.setItem(VOLUME_KEY, String(clamped));
    } catch {
      // ignore
    }
    engineRef.current?.setVolume(clamped);
  }, []);

  const meta = STARDEW_TRACKS[sceneKey];

  const value = useMemo<StardewAudioContextValue>(
    () => ({
      playing: snapshot.enabled,
      status: snapshot.status,
      activeEngine: snapshot.activeEngine,
      volume: snapshot.volume,
      trackTitle: meta.title,
      sceneZh: meta.sceneZh,
      togglePlay,
      setVolume,
    }),
    [snapshot, meta, togglePlay, setVolume],
  );

  return (
    <StardewAudioContext.Provider value={value}>
      {children}
    </StardewAudioContext.Provider>
  );
};

export function useStardewAudio(): StardewAudioContextValue {
  const ctx = useContext(StardewAudioContext);

  if (!ctx) {
    throw new Error(
      "useStardewAudio must be used within <StardewAudioProvider>",
    );
  }

  return ctx;
}
