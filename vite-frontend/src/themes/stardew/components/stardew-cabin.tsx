import { useState } from "react";

import { cabinLabel, cabinUrl, cabinWindowUrl } from "../assets";
import { useStardewEnv } from "../env-context";

function FallbackPixelCabin({
  online,
  night,
  size,
}: {
  online: boolean;
  night: boolean;
  size: number;
}) {
  const win = !online ? "#3d4650" : night ? "#ffd86b" : "#5b89a6";

  return (
    <svg
      aria-hidden="true"
      className={online ? "" : "opacity-60"}
      height={size}
      shapeRendering="crispEdges"
      viewBox="0 0 16 16"
      width={size}
    >
      <rect fill="#78685a" height="3" width="2" x="11" y="2" />
      <rect fill="#a94438" height="2" width="10" x="3" y="4" />
      <rect fill="#732820" height="2" width="12" x="2" y="6" />
      <rect fill="#e3b778" height="6" width="10" x="3" y="8" />
      <rect fill={win} height="2" width="3" x="4" y="9" />
      <rect fill={win} height="2" width="3" x="9" y="9" />
      <rect fill="#593a22" height="3" width="2" x="7" y="11" />
    </svg>
  );
}

/**
 * Renders an authentic Stardew Valley building sprite with automatic night-time
 * lit window overlay, plus an inline pixel SVG fallback when assets are missing.
 */
export function StardewCabin({
  cabinId,
  online = true,
  isNight = false,
  size = 34,
  className = "",
}: {
  cabinId: string;
  online?: boolean;
  isNight?: boolean;
  size?: number;
  className?: string;
}) {
  const { customAssetBaseUrl } = useStardewEnv();
  const [failedSrc, setFailedSrc] = useState<string | null>(null);
  const [winFailedSrc, setWinFailedSrc] = useState<string | null>(null);

  const baseSrc = cabinUrl(cabinId, customAssetBaseUrl);
  const winSrc = cabinWindowUrl(cabinId, customAssetBaseUrl);
  const baseFailed = failedSrc === baseSrc;
  const showLitWindow =
    online && isNight && !baseFailed && winFailedSrc !== winSrc;

  return (
    <span
      className={`relative inline-flex shrink-0 items-center justify-center ${
        online ? "" : "opacity-55 grayscale-[35%]"
      } ${className}`}
      style={{ width: size, height: size }}
    >
      {baseFailed ? (
        <FallbackPixelCabin night={isNight} online={online} size={size} />
      ) : (
        <img
          alt={cabinLabel(cabinId)}
          className="stardew-pixelated h-full w-full object-contain"
          loading="lazy"
          src={baseSrc}
          onError={() => setFailedSrc(baseSrc)}
        />
      )}
      {showLitWindow && (
        <img
          alt=""
          aria-hidden="true"
          className="stardew-pixelated pointer-events-none absolute inset-0 h-full w-full object-contain"
          loading="lazy"
          src={winSrc}
          onError={() => setWinFailedSrc(winSrc)}
        />
      )}
    </span>
  );
}
