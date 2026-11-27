import React, { createContext, useContext, useMemo, useState } from "react";

import {
  defaultCabinForNode,
  resolveEffectiveWeather,
  seasonFromDate,
  type AudioEnginePref,
  type EffectiveWeather,
  type NodeViewMode,
  type Season,
  type WeatherMode,
} from "./assets";

export type SeasonMode = "auto" | Season;

interface StardewEnvContextValue {
  seasonMode: SeasonMode;
  effectiveSeason: Season;
  weatherMode: WeatherMode;
  effectiveWeather: EffectiveWeather;
  farmCollapsed: boolean;
  nodeViewMode: NodeViewMode;
  cabinStyles: Record<number, string>;
  audioEnginePref: AudioEnginePref;
  customAssetBaseUrl: string;
  setSeasonMode: (mode: SeasonMode) => void;
  setWeatherMode: (mode: WeatherMode) => void;
  setFarmCollapsed: (collapsed: boolean) => void;
  setNodeViewMode: (mode: NodeViewMode) => void;
  getNodeCabin: (nodeId: number) => string;
  setNodeCabin: (nodeId: number, cabinId: string) => void;
  setAudioEnginePref: (pref: AudioEnginePref) => void;
  setCustomAssetBaseUrl: (url: string) => void;
}

const KEYS = {
  season: "flux:stardew:season",
  weather: "flux:stardew:weather",
  collapsed: "flux:stardew:farm-collapsed",
  nodeView: "flux:stardew:node-view",
  cabins: "flux:stardew:cabins",
  audioPref: "flux:stardew:audio-pref",
  assetBase: "flux:stardew:asset-base",
} as const;

function readStr(key: string, fallback: string): string {
  try {
    return localStorage.getItem(key) ?? fallback;
  } catch {
    return fallback;
  }
}

function writeStr(key: string, val: string): void {
  try {
    localStorage.setItem(key, val);
  } catch {
    // ignore quota / private mode
  }
}

function readCabins(): Record<number, string> {
  try {
    const raw = localStorage.getItem(KEYS.cabins);

    if (!raw) return {};
    const parsed = JSON.parse(raw) as Record<string, string>;

    return parsed && typeof parsed === "object" ? parsed : {};
  } catch {
    return {};
  }
}

const StardewEnvContext = createContext<StardewEnvContextValue | null>(null);

export const StardewEnvProvider: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => {
  const [seasonMode, setSeasonState] = useState<SeasonMode>(() => {
    const v = readStr(KEYS.season, "auto");

    return v === "spring" || v === "summer" || v === "fall" || v === "winter"
      ? v
      : "auto";
  });
  const [weatherMode, setWeatherState] = useState<WeatherMode>(() => {
    const v = readStr(KEYS.weather, "auto");

    return v === "clear" || v === "wind" || v === "rain" || v === "snow"
      ? v
      : "auto";
  });
  const [farmCollapsed, setCollapsedState] = useState<boolean>(
    () => readStr(KEYS.collapsed, "false") === "true",
  );
  const [nodeViewMode, setNodeViewState] = useState<NodeViewMode>(() =>
    readStr(KEYS.nodeView, "strip") === "cabin" ? "cabin" : "strip",
  );
  const [cabinStyles, setCabinStyles] =
    useState<Record<number, string>>(readCabins);
  const [audioEnginePref, setAudioPrefState] = useState<AudioEnginePref>(() =>
    readStr(KEYS.audioPref, "auto") === "synth" ? "synth" : "auto",
  );
  const [customAssetBaseUrl, setAssetBaseState] = useState<string>(() =>
    readStr(KEYS.assetBase, ""),
  );

  const effectiveSeason = seasonMode === "auto" ? seasonFromDate() : seasonMode;
  const effectiveWeather = resolveEffectiveWeather(
    weatherMode,
    effectiveSeason,
  );

  const value = useMemo<StardewEnvContextValue>(
    () => ({
      seasonMode,
      effectiveSeason,
      weatherMode,
      effectiveWeather,
      farmCollapsed,
      nodeViewMode,
      cabinStyles,
      audioEnginePref,
      customAssetBaseUrl,
      setSeasonMode: (mode) => {
        setSeasonState(mode);
        writeStr(KEYS.season, mode);
      },
      setWeatherMode: (mode) => {
        setWeatherState(mode);
        writeStr(KEYS.weather, mode);
      },
      setFarmCollapsed: (collapsed) => {
        setCollapsedState(collapsed);
        writeStr(KEYS.collapsed, String(collapsed));
      },
      setNodeViewMode: (mode) => {
        setNodeViewState(mode);
        writeStr(KEYS.nodeView, mode);
      },
      getNodeCabin: (nodeId) =>
        cabinStyles[nodeId] ?? defaultCabinForNode(nodeId),
      setNodeCabin: (nodeId, cabinId) => {
        setCabinStyles((prev) => {
          const next = { ...prev, [nodeId]: cabinId };

          writeStr(KEYS.cabins, JSON.stringify(next));

          return next;
        });
      },
      setAudioEnginePref: (pref) => {
        setAudioPrefState(pref);
        writeStr(KEYS.audioPref, pref);
      },
      setCustomAssetBaseUrl: (url) => {
        setAssetBaseState(url);
        writeStr(KEYS.assetBase, url);
      },
    }),
    [
      seasonMode,
      effectiveSeason,
      weatherMode,
      effectiveWeather,
      farmCollapsed,
      nodeViewMode,
      cabinStyles,
      audioEnginePref,
      customAssetBaseUrl,
    ],
  );

  return (
    <StardewEnvContext.Provider value={value}>
      {children}
    </StardewEnvContext.Provider>
  );
};

export function useStardewEnv(): StardewEnvContextValue {
  const ctx = useContext(StardewEnvContext);

  if (!ctx) {
    throw new Error("useStardewEnv must be used within <StardewEnvProvider>");
  }

  return ctx;
}
