import { Moon, Pause, Play, Sun, Volume2 } from "lucide-react";

import { SEASON_LABELS } from "../assets";
import { useStardewAudio } from "../audio/audio-context";
import { useStardewEnv } from "../env-context";

import { useThemeContext } from "@/themes/context";

/** Compact persistent music & day/night controls embedded in AdminLayout's sidebar footer. */
export function StardewSidebarControls({ collapsed }: { collapsed: boolean }) {
  const { effectiveMode, setMode } = useThemeContext();
  const { effectiveSeason } = useStardewEnv();
  const { playing, activeEngine, volume, trackTitle, togglePlay, setVolume } =
    useStardewAudio();
  const isNight = effectiveMode === "dark";

  if (collapsed) {
    return (
      <div className="flex flex-col items-center gap-1 pb-1">
        <button
          aria-label={playing ? "暂停配乐" : "播放配乐"}
          className="flex h-8 w-full items-center justify-center rounded-md text-default-500 hover:bg-default-100 hover:text-foreground"
          title={`${trackTitle}（${playing ? "点击暂停" : "点击播放"}）`}
          type="button"
          onClick={togglePlay}
        >
          {playing ? (
            <Pause className="h-4 w-4 text-primary" />
          ) : (
            <Play className="h-4 w-4" />
          )}
        </button>
        <button
          aria-label={isNight ? "切换白天" : "切换夜晚"}
          className="flex h-8 w-full items-center justify-center rounded-md text-default-500 hover:bg-default-100 hover:text-foreground"
          title={isNight ? "切换白天" : "切换夜晚"}
          type="button"
          onClick={() => setMode(isNight ? "light" : "dark")}
        >
          {isNight ? <Moon className="h-4 w-4" /> : <Sun className="h-4 w-4" />}
        </button>
      </div>
    );
  }

  return (
    <div className="stardew-inset mb-2 space-y-2 p-2.5 text-xs">
      <div className="flex items-center justify-between gap-2">
        <div className="min-w-0">
          <div className="truncate font-semibold text-foreground">
            {trackTitle}
          </div>
          <div className="truncate text-[11px] text-default-500">
            {SEASON_LABELS[effectiveSeason]}
            {isNight ? "夜" : "日"} ·{" "}
            {playing
              ? activeEngine === "ogg"
                ? "原声播放中"
                : "16-bit 合成中"
              : "已暂停"}
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          <button
            aria-label={isNight ? "切换白天" : "切换夜晚"}
            className="flex h-7 w-7 items-center justify-center rounded border border-border bg-background text-foreground hover:bg-default-100"
            title={isNight ? "切换白天" : "切换夜晚"}
            type="button"
            onClick={() => setMode(isNight ? "light" : "dark")}
          >
            {isNight ? (
              <Moon className="h-3.5 w-3.5" />
            ) : (
              <Sun className="h-3.5 w-3.5" />
            )}
          </button>
          <button
            aria-label={playing ? "暂停配乐" : "播放配乐"}
            className="flex h-7 w-7 items-center justify-center rounded bg-primary text-primary-foreground hover:opacity-90"
            title={playing ? "暂停配乐" : "播放配乐"}
            type="button"
            onClick={togglePlay}
          >
            {playing ? (
              <Pause className="h-3.5 w-3.5" />
            ) : (
              <Play className="h-3.5 w-3.5" />
            )}
          </button>
        </div>
      </div>
      <div className="flex items-center gap-2">
        <Volume2 className="h-3.5 w-3.5 shrink-0 text-default-400" />
        <input
          aria-label="背景音乐音量"
          className="h-1.5 w-full cursor-pointer accent-primary"
          max={100}
          min={0}
          type="range"
          value={Math.round(volume * 100)}
          onChange={(e) => setVolume(Number(e.target.value) / 100)}
        />
        <span className="w-7 text-right text-[11px] tabular-nums text-default-500">
          {Math.round(volume * 100)}%
        </span>
      </div>
    </div>
  );
}

/** Compact music & day/night controls for the H5 mobile top bar. */
export function StardewMobileControls() {
  const { effectiveMode, setMode } = useThemeContext();
  const { playing, togglePlay } = useStardewAudio();
  const isNight = effectiveMode === "dark";

  return (
    <div className="flex items-center gap-1.5">
      <button
        aria-label={isNight ? "切换白天" : "切换夜晚"}
        className="flex h-8 w-8 items-center justify-center rounded border border-border bg-content1 text-foreground"
        type="button"
        onClick={() => setMode(isNight ? "light" : "dark")}
      >
        {isNight ? <Moon className="h-4 w-4" /> : <Sun className="h-4 w-4" />}
      </button>
      <button
        aria-label={playing ? "暂停配乐" : "播放配乐"}
        className="flex h-8 items-center gap-1 rounded bg-primary px-2.5 text-xs font-semibold text-primary-foreground"
        type="button"
        onClick={togglePlay}
      >
        {playing ? (
          <Pause className="h-3.5 w-3.5" />
        ) : (
          <Play className="h-3.5 w-3.5" />
        )}
        <span>{playing ? "暂停" : "配乐"}</span>
      </button>
    </div>
  );
}
