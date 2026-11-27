export type Season = "spring" | "summer" | "fall" | "winter";
export type Phase = "day" | "night";
export type SceneKey = `${Season}-${Phase}`;
export type WeatherMode = "auto" | "clear" | "wind" | "rain" | "snow";
export type EffectiveWeather = "clear" | "wind" | "rain" | "snow";
export type AudioEnginePref = "auto" | "synth";
export type NodeViewMode = "strip" | "cabin";

export interface CabinCatalogItem {
  id: string;
  name: string;
}

export const DEFAULT_CABIN_ID = "farmhouse-3";

export const CABIN_CATALOG: readonly CabinCatalogItem[] = [
  { id: "farmhouse-3", name: "农舍" },
  { id: "big-shed-3", name: "大谷仓" },
  { id: "shed-3", name: "小木棚" },
  { id: "junimo-3", name: "祝尼魔小屋" },
  { id: "mill-3", name: "风车磨坊" },
  { id: "wizard-tower-3", name: "法师塔" },
  { id: "witch-hut-3", name: "女巫小屋" },
  { id: "deluxe-coop-3", name: "豪华鸡舍" },
  { id: "slime-hutch-3", name: "史莱姆屋" },
  { id: "manor-house-3", name: "镇长庄园" },
  { id: "fish-shop-3", name: "威利鱼店" },
  { id: "island-farmhouse-3", name: "姜岛农舍" },
  { id: "log-3", name: "原木小屋" },
  { id: "stone-3", name: "石砌小屋" },
  { id: "plank-3", name: "木板小屋" },
  { id: "beach-3", name: "海滨小屋" },
] as const;

const CABIN_NAME_MAP = new Map(CABIN_CATALOG.map((c) => [c.id, c.name]));

export function cabinLabel(id: string): string {
  return CABIN_NAME_MAP.get(id) ?? "农舍";
}

export function defaultCabinForNode(nodeId: number): string {
  const idx = Math.abs(nodeId) % CABIN_CATALOG.length;

  return CABIN_CATALOG[idx]?.id ?? DEFAULT_CABIN_ID;
}

export const STARDEW_TRACKS: Record<
  SceneKey,
  { title: string; sceneZh: string; file: string }
> = {
  "spring-day": {
    title: "Spring (It's A Big World Outside)",
    sceneZh: "春日",
    file: "spring-day.ogg",
  },
  "spring-night": {
    title: "Starwatcher (Maru's Theme)",
    sceneZh: "春夜",
    file: "spring-night.ogg",
  },
  "summer-day": {
    title: "Summer (Nature's Crescendo)",
    sceneZh: "夏日",
    file: "summer-day.ogg",
  },
  "summer-night": {
    title: "Dance Of The Moonlight Jellies",
    sceneZh: "夏夜",
    file: "summer-night.ogg",
  },
  "fall-day": {
    title: "The Smell of Mushroom",
    sceneZh: "秋日",
    file: "fall-day.ogg",
  },
  "fall-night": {
    title: "Raven's Descent",
    sceneZh: "秋夜",
    file: "fall-night.ogg",
  },
  "winter-day": {
    title: "Ancient (The Wind Can Be Still)",
    sceneZh: "冬日",
    file: "winter-day.ogg",
  },
  "winter-night": {
    title: "Nocturne of Ice",
    sceneZh: "冬夜",
    file: "winter-night.ogg",
  },
};

export const SEASON_LABELS: Record<Season, string> = {
  spring: "春",
  summer: "夏",
  fall: "秋",
  winter: "冬",
};

export const WEATHER_LABELS: Record<EffectiveWeather, string> = {
  clear: "晴",
  wind: "微风",
  rain: "细雨",
  snow: "飘雪",
};

function resolveBase(customBase?: string): string {
  const trimmed = customBase?.trim();

  if (trimmed) {
    return trimmed.endsWith("/") ? trimmed : `${trimmed}/`;
  }

  const base =
    typeof import.meta !== "undefined" &&
    (import.meta as unknown as { env?: { BASE_URL?: string } }).env?.BASE_URL
      ? String(
          (import.meta as unknown as { env: { BASE_URL: string } }).env
            .BASE_URL,
        )
      : "/";

  return `${base.endsWith("/") ? base : `${base}/`}stardew/`;
}

export function landscapeUrl(scene: SceneKey, customBase?: string): string {
  return `${resolveBase(customBase)}landscape/${scene}.png`;
}

export function cabinUrl(cabinId: string, customBase?: string): string {
  return `${resolveBase(customBase)}cabins/${cabinId}.png`;
}

export function cabinWindowUrl(cabinId: string, customBase?: string): string {
  return `${resolveBase(customBase)}windows/${cabinId}.png`;
}

export function statIconUrl(
  name: "farm-computer" | "scarecrow" | "seedling" | "wilted-plant",
  customBase?: string,
): string {
  return `${resolveBase(customBase)}stats/${name}.png`;
}

export function musicUrl(scene: SceneKey, customBase?: string): string {
  return `${resolveBase(customBase)}music/${STARDEW_TRACKS[scene].file}`;
}

export function seasonFromDate(d = new Date()): Season {
  const month = d.getMonth() + 1;

  if (month >= 3 && month <= 5) return "spring";
  if (month >= 6 && month <= 8) return "summer";
  if (month >= 9 && month <= 11) return "fall";

  return "winter";
}

export function resolveEffectiveWeather(
  mode: WeatherMode,
  season: Season,
): EffectiveWeather {
  if (mode !== "auto") return mode;
  if (season === "spring" || season === "fall") return "wind";
  if (season === "winter") return "snow";

  return "clear";
}
