/**
 * ThemeSettings — theme picker card for the Settings page
 * ========================================================
 * Shows:
 *  • Mode toggle (light / dark / system)
 *  • Grid of registered themes with preview dots
 *  • Stardew Valley preferences when "stardew" is active
 *  • "Reset to default" option
 */

import type { ThemeMode } from "@/themes/registry";

import React from "react";
import toast from "react-hot-toast";

import { Card, CardBody } from "@/shadcn-bridge/heroui/card";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Input } from "@/shadcn-bridge/heroui/input";
import { useThemeContext } from "@/themes/context";
import { DEFAULT_THEME_ID } from "@/themes/registry";
import { useStardewEnv, type SeasonMode } from "@/themes/stardew/env-context";
import {
  type AudioEnginePref,
  type WeatherMode,
} from "@/themes/stardew/assets";

// ─── Constants ──────────────────────────────────────────────────────────────

const MODE_OPTIONS: Array<{ value: ThemeMode; label: string }> = [
  { value: "light", label: "亮色" },
  { value: "dark", label: "暗色" },
  { value: "system", label: "跟随系统" },
];

const SEASON_OPTIONS: Array<{ value: SeasonMode; label: string }> = [
  { value: "auto", label: "自动（按当前月份）" },
  { value: "spring", label: "春" },
  { value: "summer", label: "夏" },
  { value: "fall", label: "秋" },
  { value: "winter", label: "冬" },
];

const WEATHER_OPTIONS: Array<{ value: WeatherMode; label: string }> = [
  { value: "auto", label: "自动联动" },
  { value: "clear", label: "晴朗" },
  { value: "wind", label: "微风" },
  { value: "rain", label: "细雨" },
  { value: "snow", label: "飘雪" },
];

const AUDIO_OPTIONS: Array<{ value: AudioEnginePref; label: string }> = [
  { value: "auto", label: "优先原声 .ogg（缺失时自动用 16-bit 合成器）" },
  { value: "synth", label: "仅使用内置 16-bit 合成器" },
];

// ─── Component ──────────────────────────────────────────────────────────────

export const ThemeSettings: React.FC = () => {
  const {
    themes,
    activeThemeId,
    mode,
    effectiveMode,
    switchTheme,
    resetTheme,
    setMode,
  } = useThemeContext();
  const {
    seasonMode,
    weatherMode,
    audioEnginePref,
    customAssetBaseUrl,
    setSeasonMode,
    setWeatherMode,
    setAudioEnginePref,
    setCustomAssetBaseUrl,
  } = useStardewEnv();

  const handleModeChange = (m: ThemeMode) => {
    setMode(m);
    const label = m === "light" ? "亮色" : m === "dark" ? "暗色" : "跟随系统";

    toast.success(`已切换为${label}模式`);
  };

  const handleThemeSelect = (id: string) => {
    switchTheme(id);
    const theme = themes.find((t) => t.id === id);

    toast.success(`已切换主题「${theme?.name ?? id}」`);
  };

  const handleReset = () => {
    resetTheme();
    toast.success("已恢复默认主题（星露谷农场）");
  };

  return (
    <Card>
      <CardBody className="p-6">
        <h2 className="mb-5 text-lg font-medium text-foreground">主题设置</h2>

        {/* ── Mode toggle ────────────────────────────────────── */}
        <div className="mb-6">
          <p className="mb-2 text-sm">
            外观模式（星露谷主题下对应白天 / 夜晚）
          </p>
          <div className="inline-flex gap-1">
            {MODE_OPTIONS.map((opt) => (
              <button
                key={opt.value}
                className={`rounded-md px-3 py-1.5 text-sm transition-colors ${
                  mode === opt.value
                    ? "bg-default-100 font-medium text-foreground"
                    : "text-default-500 hover:text-foreground"
                }`}
                type="button"
                onClick={() => handleModeChange(opt.value)}
              >
                {opt.label}
              </button>
            ))}
          </div>
        </div>

        {/* ── Theme grid ─────────────────────────────────────── */}
        <div className="mb-4">
          <p className="mb-2 text-sm">
            选择主题
            <span className="ml-2 text-xs text-default-400">
              共 {themes.length} 个可用主题
            </span>
          </p>

          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {themes.map((theme) => {
              const isActive = activeThemeId === theme.id;
              const previewTokens =
                effectiveMode === "dark" && theme.tokens?.dark
                  ? theme.tokens.dark
                  : theme.tokens?.light;

              const primary = previewTokens?.["--primary"] ?? "#2563eb";
              const secondary = previewTokens?.["--secondary"] ?? "#6366f1";
              const success = previewTokens?.["--success"] ?? "#16a34a";
              const danger = previewTokens?.["--danger"] ?? "#dc2626";
              const bg = previewTokens?.["--background"] ?? "#ffffff";

              return (
                <button
                  key={theme.id}
                  className={`relative overflow-hidden rounded-lg border text-left transition-colors ${
                    isActive
                      ? "border-foreground"
                      : "border-divider hover:bg-default-50"
                  }`}
                  type="button"
                  onClick={() => handleThemeSelect(theme.id)}
                >
                  <div className="flex h-8">
                    <div className="flex-1" style={{ background: primary }} />
                    <div className="flex-1" style={{ background: secondary }} />
                    <div className="flex-1" style={{ background: success }} />
                    <div className="flex-1" style={{ background: danger }} />
                    <div
                      className="flex-1"
                      style={{
                        background: bg,
                        borderLeft: "1px solid rgba(0,0,0,0.06)",
                      }}
                    />
                  </div>

                  <div className="p-3">
                    <div className="flex items-center gap-2">
                      <p className="truncate text-sm font-semibold text-foreground">
                        {theme.name}
                      </p>
                      {isActive && (
                        <span className="shrink-0 rounded bg-primary/15 px-1.5 py-0.5 text-[10px] font-bold text-primary">
                          当前
                        </span>
                      )}
                    </div>
                    {theme.description && (
                      <p className="mt-0.5 truncate text-xs text-default-500">
                        {theme.description}
                      </p>
                    )}
                    <p className="mt-1 text-[10px] text-default-400">
                      {theme.author} · v{theme.version}
                    </p>
                  </div>

                  {isActive && (
                    <span className="absolute right-2 top-2 h-1.5 w-1.5 rounded-full bg-foreground" />
                  )}
                </button>
              );
            })}
          </div>
        </div>

        {/* ── Stardew Preferences ────────────────────────────── */}
        {activeThemeId === "stardew" && (
          <div className="mt-5 space-y-4 rounded-lg border border-divider bg-default-50 p-4">
            <div className="text-sm font-semibold">星露谷农场偏好</div>
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <span className="block text-xs text-default-500">默认季节</span>
                <div className="flex flex-wrap gap-1.5">
                  {SEASON_OPTIONS.map((opt) => (
                    <button
                      key={opt.value}
                      className={`rounded px-2.5 py-1 text-xs font-medium transition-colors ${
                        seasonMode === opt.value
                          ? "bg-primary text-primary-foreground"
                          : "border border-divider bg-background text-default-600 hover:bg-default-100"
                      }`}
                      type="button"
                      onClick={() => setSeasonMode(opt.value)}
                    >
                      {opt.label}
                    </button>
                  ))}
                </div>
              </div>

              <div className="space-y-1.5">
                <span className="block text-xs text-default-500">天气效果</span>
                <div className="flex flex-wrap gap-1.5">
                  {WEATHER_OPTIONS.map((opt) => (
                    <button
                      key={opt.value}
                      className={`rounded px-2.5 py-1 text-xs font-medium transition-colors ${
                        weatherMode === opt.value
                          ? "bg-primary text-primary-foreground"
                          : "border border-divider bg-background text-default-600 hover:bg-default-100"
                      }`}
                      type="button"
                      onClick={() => setWeatherMode(opt.value)}
                    >
                      {opt.label}
                    </button>
                  ))}
                </div>
              </div>
            </div>

            <div className="space-y-1.5">
              <span className="block text-xs text-default-500">
                背景音乐引擎
              </span>
              <div className="flex flex-wrap gap-1.5">
                {AUDIO_OPTIONS.map((opt) => (
                  <button
                    key={opt.value}
                    className={`rounded px-2.5 py-1 text-xs font-medium transition-colors ${
                      audioEnginePref === opt.value
                        ? "bg-primary text-primary-foreground"
                        : "border border-divider bg-background text-default-600 hover:bg-default-100"
                    }`}
                    type="button"
                    onClick={() => setAudioEnginePref(opt.value)}
                  >
                    {opt.label}
                  </button>
                ))}
              </div>
            </div>

            <Input
              description="留空则默认读取 /stardew/（本地部署前可运行 pnpm run stardew:assets 一键拉取四季图集、小屋和 .ogg 配乐）"
              label="自定义星露谷素材地址（可选）"
              placeholder="例如: https://cdn.example.com/stardew/"
              size="sm"
              value={customAssetBaseUrl}
              variant="bordered"
              onChange={(e) => setCustomAssetBaseUrl(e.target.value)}
            />
          </div>
        )}

        {/* ── Reset ──────────────────────────────────────────── */}
        {activeThemeId && activeThemeId !== DEFAULT_THEME_ID && (
          <div className="pt-3">
            <Button
              className="text-default-500"
              size="sm"
              variant="light"
              onPress={handleReset}
            >
              ↩ 恢复默认主题（星露谷农场）
            </Button>
          </div>
        )}
      </CardBody>
    </Card>
  );
};
