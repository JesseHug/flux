import { useEffect, useRef, useState } from "react";
import { ChevronDown } from "lucide-react";

import {
  SEASON_LABELS,
  WEATHER_LABELS,
  landscapeUrl,
  type SceneKey,
  type Season,
  type WeatherMode,
} from "../assets";
import { useStardewAudio } from "../audio/audio-context";
import { useStardewEnv, type SeasonMode } from "../env-context";

import { siteConfig } from "@/config/site";
import { useThemeContext } from "@/themes/context";

function PlayTriangleIcon({ playing }: { playing: boolean }) {
  return playing ? (
    <svg
      aria-hidden="true"
      height="16"
      shapeRendering="crispEdges"
      viewBox="0 0 16 16"
      width="16"
    >
      <rect fill="currentColor" height="10" width="3" x="4" y="3" />
      <rect fill="currentColor" height="10" width="3" x="9" y="3" />
    </svg>
  ) : (
    <svg
      aria-hidden="true"
      height="16"
      shapeRendering="crispEdges"
      viewBox="0 0 16 16"
      width="16"
    >
      <path
        d="M4 2H6V3H8V4H10V5H12V6H13V10H12V11H10V12H8V13H6V14H4Z"
        fill="currentColor"
      />
    </svg>
  );
}

function PhasePixelIcon({ isNight }: { isNight: boolean }) {
  if (isNight) {
    return (
      <svg
        aria-hidden="true"
        height="16"
        shapeRendering="crispEdges"
        viewBox="0 0 16 16"
        width="16"
      >
        <path
          d="M9 2H12V3H10V5H9V9H10V11H13V13H11V14H7V13H5V11H4V6H5V4H7V3H9Z"
          fill="#f3c486"
        />
      </svg>
    );
  }

  return (
    <svg
      aria-hidden="true"
      height="16"
      shapeRendering="crispEdges"
      viewBox="0 0 16 16"
      width="16"
    >
      <rect fill="#e69d24" height="6" width="6" x="5" y="5" />
      <rect fill="#e69d24" height="2" width="2" x="7" y="1" />
      <rect fill="#e69d24" height="2" width="2" x="7" y="13" />
      <rect fill="#e69d24" height="2" width="2" x="1" y="7" />
      <rect fill="#e69d24" height="2" width="2" x="13" y="7" />
    </svg>
  );
}

function formatNowClock(): string {
  const d = new Date();
  const hh = String(d.getHours()).padStart(2, "0");
  const mm = String(d.getMinutes()).padStart(2, "0");

  return `${hh}:${mm}`;
}

const SEASON_CHOICES: Array<{ key: SeasonMode; label: string }> = [
  { key: "auto", label: "自动" },
  { key: "spring", label: "春" },
  { key: "summer", label: "夏" },
  { key: "fall", label: "秋" },
  { key: "winter", label: "冬" },
];

const WEATHER_CHOICES: Array<{ key: WeatherMode; label: string }> = [
  { key: "auto", label: "自动" },
  { key: "clear", label: "晴" },
  { key: "wind", label: "微风" },
  { key: "rain", label: "细雨" },
  { key: "snow", label: "飘雪" },
];

const FALLBACK_GRADIENTS: Record<SceneKey, string> = {
  "spring-day":
    "linear-gradient(180deg, #7ec8f2 0%, #6aa876 48%, #5da848 100%)",
  "spring-night":
    "linear-gradient(180deg, #0c1830 0%, #1d3240 48%, #1f3a2c 100%)",
  "summer-day":
    "linear-gradient(180deg, #58b4eb 0%, #529c5c 48%, #499c36 100%)",
  "summer-night":
    "linear-gradient(180deg, #091529 0%, #193230 48%, #183323 100%)",
  "fall-day": "linear-gradient(180deg, #e6a76c 0%, #a86b3e 48%, #b0742c 100%)",
  "fall-night":
    "linear-gradient(180deg, #14142b 0%, #3b2c2a 48%, #36271a 100%)",
  "winter-day":
    "linear-gradient(180deg, #9cc3de 0%, #b9d0e0 48%, #d0e2ed 100%)",
  "winter-night":
    "linear-gradient(180deg, #081221 0%, #24374b 48%, #354a5e 100%)",
};

const PARTICLES = Array.from({ length: 48 }, (_, i) => ({
  x: (i * 53) % 640,
  y: (i * 31) % 238,
  vx: 0.32 + (i % 4) * 0.14,
  vy: 0.42 + (i % 5) * 0.18,
  size: i % 3 === 0 ? 2 : 1,
}));

/**
 * Clean, unobstructed Stardew Valley estate panorama banner with:
 * - Left wood site plaque
 * - Center minimal music track + play button
 * - Right wood clock/weather plaque + scene control popover
 */
export function StardewFarmHero({
  onlineCount,
  totalCount,
}: {
  onlineCount?: number;
  totalCount?: number;
}) {
  const { effectiveMode, setMode } = useThemeContext();
  const {
    seasonMode,
    effectiveSeason,
    weatherMode,
    effectiveWeather,
    farmCollapsed,
    customAssetBaseUrl,
    setSeasonMode,
    setWeatherMode,
    setFarmCollapsed,
  } = useStardewEnv();
  const { playing, trackTitle, sceneZh, togglePlay } = useStardewAudio();

  const [clock, setClock] = useState(formatNowClock);
  const [popoverOpen, setPopoverOpen] = useState(false);
  const [failedUrl, setFailedUrl] = useState<string | null>(null);
  const popoverRef = useRef<HTMLDivElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);

  const isNight = effectiveMode === "dark";
  const sceneKey: SceneKey = `${effectiveSeason}-${isNight ? "night" : "day"}`;
  const bgUrl = landscapeUrl(sceneKey, customAssetBaseUrl);
  const bgFailed = failedUrl === bgUrl;

  useEffect(() => {
    const timer = window.setInterval(() => setClock(formatNowClock()), 30_000);

    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    if (!popoverOpen) return;
    const onDocClick = (e: MouseEvent) => {
      if (
        popoverRef.current &&
        !popoverRef.current.contains(e.target as Node)
      ) {
        setPopoverOpen(false);
      }
    };

    document.addEventListener("click", onDocClick);

    return () => document.removeEventListener("click", onDocClick);
  }, [popoverOpen]);

  useEffect(() => {
    if (farmCollapsed) return;
    const canvas = canvasRef.current;

    if (!canvas) return;
    const ctx = canvas.getContext("2d");

    if (!ctx) return;
    const reduceMotion = window.matchMedia(
      "(prefers-reduced-motion: reduce)",
    ).matches;

    if (reduceMotion) {
      ctx.clearRect(0, 0, canvas.width, canvas.height);

      return;
    }

    let rafId = 0;
    let tick = 0;
    const W = canvas.width;
    const H = canvas.height;

    const draw = () => {
      tick++;
      ctx.clearRect(0, 0, W, H);

      if (
        effectiveWeather === "wind" &&
        (effectiveSeason === "spring" || effectiveSeason === "fall")
      ) {
        ctx.fillStyle =
          effectiveSeason === "spring"
            ? "rgba(255, 209, 227, 0.9)"
            : "rgba(232, 114, 48, 0.9)";
        PARTICLES.forEach((p, idx) => {
          const px = Math.floor((p.x + tick * p.vx * 1.4) % W);
          const py = Math.floor(
            (p.y + tick * p.vy * 0.8 + Math.sin(tick * 0.05 + idx) * 4) % H,
          );

          ctx.fillRect(px, py, 2, 2);
        });
      } else if (effectiveWeather === "rain") {
        ctx.fillStyle = isNight
          ? "rgba(155, 200, 240, 0.48)"
          : "rgba(180, 215, 245, 0.58)";
        PARTICLES.forEach((p) => {
          const px = Math.floor((p.x - tick * 1.5 + W * 10) % W);
          const py = Math.floor((p.y + tick * 3.0) % H);

          ctx.fillRect(px, py, 1, 6);
        });
      } else if (effectiveWeather === "snow") {
        ctx.fillStyle = "rgba(255, 255, 255, 0.92)";
        PARTICLES.forEach((p) => {
          const px = Math.floor((p.x + Math.sin(tick * 0.03 + p.y) * 6) % W);
          const py = Math.floor((p.y + tick * p.vy * 0.72) % H);

          ctx.fillRect(px, py, p.size, p.size);
        });
      }

      rafId = window.requestAnimationFrame(draw);
    };

    rafId = window.requestAnimationFrame(draw);

    return () => window.cancelAnimationFrame(rafId);
  }, [farmCollapsed, effectiveWeather, effectiveSeason, isNight]);

  if (farmCollapsed) {
    return (
      <div className="stardew-wood-sign mb-5 flex h-11 items-center justify-between gap-2 px-3 sm:px-4">
        <div className="flex min-w-0 items-center gap-2.5 text-xs font-bold">
          <span className="truncate">{siteConfig.name}</span>
          {totalCount !== undefined && (
            <span className="stardew-inset px-1.5 py-0.5 tabular-nums text-success">
              在线 {onlineCount ?? 0}/{totalCount}
            </span>
          )}
          <span className="hidden text-default-500 sm:inline">
            {sceneZh} · {WEATHER_LABELS[effectiveWeather]}
          </span>
        </div>
        <div className="flex shrink-0 items-center gap-1.5">
          <button
            className="stardew-inset px-2 py-1 text-xs font-semibold hover:bg-default-200"
            type="button"
            onClick={() => setMode(isNight ? "light" : "dark")}
          >
            {isNight ? "夜晚" : "白天"}
          </button>
          <button
            className="rounded bg-primary px-2 py-1 text-xs font-semibold text-primary-foreground"
            type="button"
            onClick={togglePlay}
          >
            {playing ? "暂停" : "配乐"}
          </button>
          <button
            className="stardew-inset px-2 py-1 text-xs font-semibold hover:bg-default-200"
            type="button"
            onClick={() => setFarmCollapsed(false)}
          >
            展开农场
          </button>
        </div>
      </div>
    );
  }

  return (
    <section
      aria-label="星露谷农场风景"
      className="stardew-wood-frame relative mb-6 overflow-visible"
    >
      <div
        className="relative aspect-[1280/475] min-h-[168px] max-h-[300px] w-full overflow-hidden rounded-[2px]"
        style={{ background: FALLBACK_GRADIENTS[sceneKey] }}
      >
        {!bgFailed && (
          <img
            alt={`${siteConfig.name} ${sceneZh}农场`}
            className="stardew-pixelated block h-full w-full object-cover object-center"
            src={bgUrl}
            onError={() => setFailedUrl(bgUrl)}
          />
        )}
        <canvas
          ref={canvasRef}
          className="stardew-pixelated pointer-events-none absolute inset-0 h-full w-full"
          height={238}
          width={640}
        />

        {/* 1. Top-Left Wood Site Plaque */}
        <div className="stardew-wood-sign absolute left-3 top-3 z-20 px-3 py-1.5 text-center sm:left-5 sm:top-3.5 sm:px-4 sm:py-2">
          <div className="text-xs font-extrabold tracking-wide sm:text-sm">
            {siteConfig.name}
          </div>
          <div className="mt-0.5 text-[11px] font-semibold tabular-nums text-default-500">
            {totalCount !== undefined
              ? `在线 ${onlineCount ?? 0} / ${totalCount}`
              : "星露谷牧场"}
          </div>
        </div>

        {/* 2. Top-Center Minimal Music Track & Play Triangle */}
        <div className="stardew-hero-shadow pointer-events-auto absolute left-1/2 top-2.5 z-20 flex -translate-x-1/2 flex-col items-center text-center text-[#fff9ec] sm:top-3.5">
          <div className="text-[11px] opacity-90">
            {sceneZh} · {playing ? "播放中" : "已暂停"}
          </div>
          <div className="mt-0.5 max-w-[160px] truncate text-xs font-extrabold tracking-wide sm:max-w-xs sm:text-sm">
            {trackTitle}
          </div>
          <button
            aria-label={playing ? "暂停背景音乐" : "播放背景音乐"}
            className="mt-1 flex h-7 w-7 cursor-pointer items-center justify-center rounded-full transition-transform hover:scale-110"
            type="button"
            onClick={togglePlay}
          >
            <PlayTriangleIcon playing={playing} />
          </button>
        </div>

        {/* 3. Top-Right Wood Clock & Weather Plaque */}
        <div
          ref={popoverRef}
          className="absolute right-3 top-3 z-30 sm:right-5 sm:top-3.5"
        >
          <button
            aria-expanded={popoverOpen}
            aria-label="切换季节、昼夜和天气"
            className="stardew-wood-sign flex cursor-pointer items-center gap-2.5 px-2.5 py-1.5 text-left hover:brightness-105 sm:px-3"
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              setPopoverOpen((v) => !v);
            }}
          >
            <div>
              <div className="text-[11px] font-semibold text-default-500">
                {SEASON_LABELS[effectiveSeason]} ·{" "}
                {WEATHER_LABELS[effectiveWeather]}
              </div>
              <div className="text-sm font-extrabold tabular-nums leading-tight sm:text-base">
                {clock}
              </div>
            </div>
            <div className="flex items-center gap-1">
              <PhasePixelIcon isNight={isNight} />
              <ChevronDown className="h-3.5 w-3.5 text-default-500" />
            </div>
          </button>

          {popoverOpen && (
            <div className="stardew-wood-frame absolute right-0 z-40 mt-2 w-64 space-y-3 p-3 text-xs">
              <div>
                <div className="mb-1.5 font-bold text-default-500">
                  季节场景
                </div>
                <div className="grid grid-cols-5 gap-1">
                  {SEASON_CHOICES.map((opt) => {
                    const active = seasonMode === opt.key;

                    return (
                      <button
                        key={opt.key}
                        className={`rounded px-1.5 py-1 font-semibold transition-colors ${
                          active
                            ? "bg-primary text-primary-foreground"
                            : "stardew-inset hover:bg-default-200"
                        }`}
                        type="button"
                        onClick={() => setSeasonMode(opt.key)}
                      >
                        {opt.label}
                      </button>
                    );
                  })}
                </div>
              </div>

              <div>
                <div className="mb-1.5 font-bold text-default-500">
                  昼夜外观
                </div>
                <div className="grid grid-cols-2 gap-1.5">
                  <button
                    className={`rounded py-1 font-semibold ${
                      !isNight
                        ? "bg-primary text-primary-foreground"
                        : "stardew-inset hover:bg-default-200"
                    }`}
                    type="button"
                    onClick={() => setMode("light")}
                  >
                    白天
                  </button>
                  <button
                    className={`rounded py-1 font-semibold ${
                      isNight
                        ? "bg-primary text-primary-foreground"
                        : "stardew-inset hover:bg-default-200"
                    }`}
                    type="button"
                    onClick={() => setMode("dark")}
                  >
                    夜晚
                  </button>
                </div>
              </div>

              <div>
                <div className="mb-1.5 font-bold text-default-500">
                  天气粒子
                </div>
                <div className="grid grid-cols-5 gap-1">
                  {WEATHER_CHOICES.map((opt) => {
                    const active = weatherMode === opt.key;

                    return (
                      <button
                        key={opt.key}
                        className={`rounded px-1 py-1 text-[11px] font-semibold ${
                          active
                            ? "bg-primary text-primary-foreground"
                            : "stardew-inset hover:bg-default-200"
                        }`}
                        type="button"
                        onClick={() => setWeatherMode(opt.key)}
                      >
                        {opt.label}
                      </button>
                    );
                  })}
                </div>
              </div>

              <div className="flex items-center justify-between border-t border-divider pt-2">
                <span className="text-default-500">
                  当前季节：{SEASON_LABELS[effectiveSeason]}（
                  {
                    SEASON_LABELS[
                      seasonMode === "auto"
                        ? (effectiveSeason as Season)
                        : seasonMode
                    ]
                  }
                  ）
                </span>
                <button
                  className="stardew-inset px-2 py-1 font-semibold hover:bg-default-200"
                  type="button"
                  onClick={() => {
                    setFarmCollapsed(true);
                    setPopoverOpen(false);
                  }}
                >
                  收起农场
                </button>
              </div>
            </div>
          )}
        </div>
      </div>
    </section>
  );
}
