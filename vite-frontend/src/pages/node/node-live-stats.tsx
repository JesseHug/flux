import type { NodeSystemInfo } from "@/pages/node/system-info";

function bytes(v: number): string {
  const units = ["B", "K", "M", "G", "T", "P"];
  let i = 0;

  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }

  return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)}${units[i]}`;
}

function usageColorClass(v: number): string {
  if (v >= 85) return "text-danger";
  if (v >= 60) return "text-warning-600";

  return "text-foreground";
}

function usageBarBg(v: number): string {
  if (v >= 85) return "bg-danger";
  if (v >= 60) return "bg-warning";

  return "bg-success";
}

/**
 * Unified internal resource telemetry bar for a node:
 * CPU, Memory, Disk (with compact meter), real-time ↑/↓ speed, and monthly traffic.
 * - "row": horizontal strip at the end of a wide list row
 * - "footer": compact grid under the name on narrow screens
 * - "card": embedded telemetry box inside a Cabin Card
 */
export function NodeLiveStats({
  info,
  month,
  variant,
}: {
  info: NodeSystemInfo | null;
  /** This month's [in, out] bytes. */
  month?: [number, number];
  variant: "row" | "footer" | "card";
}) {
  const up = info ? (info.netOutSpeed ?? info.uploadSpeed) : undefined;
  const down = info ? (info.netInSpeed ?? info.downloadSpeed) : undefined;
  const rate = (v?: number) => (v === undefined ? "—" : `${bytes(v)}/s`);
  const total = month ? bytes(month[0] + month[1]) : "—";
  const monthHint =
    new Date().getTimezoneOffset() % 60 !== 0
      ? "按本地时区统计；跨月的旧小时记录按时长估算"
      : "按本地时区统计";
  const split = month ? `↑${bytes(month[1])} ↓${bytes(month[0])}` : "";

  const usage = info
    ? (
        [
          ["CPU", info.cpuUsage],
          ["内存", info.memoryUsage],
          ["磁盘", info.diskUsage],
        ] as const
      ).filter(([, v]) => v !== undefined)
    : [];

  const speedText =
    info && (up !== undefined || down !== undefined)
      ? `↑ ${rate(up)} ↓ ${rate(down)}`
      : null;

  if (variant === "card") {
    return (
      <div className="stardew-inset space-y-2.5 p-3 text-xs tabular-nums">
        {usage.length > 0 ? (
          <div className="grid grid-cols-3 gap-2.5">
            {usage.map(([label, v]) => {
              const pct = Math.max(0, Math.min(100, v ?? 0));

              return (
                <div key={label}>
                  <div className="mb-1 flex items-center justify-between gap-1">
                    <span className="text-default-500">{label}</span>
                    <span className={`font-bold ${usageColorClass(pct)}`}>
                      {Math.round(pct)}%
                    </span>
                  </div>
                  <div className="h-1.5 w-full overflow-hidden rounded-xs border border-border bg-default-200/70">
                    <div
                      className={`h-full transition-[width] duration-300 ${usageBarBg(pct)}`}
                      style={{ width: `${Math.max(3, pct)}%` }}
                    />
                  </div>
                </div>
              );
            })}
          </div>
        ) : (
          <div className="py-1 text-center text-default-400">
            {info ? "暂无硬件占用数据" : "服务器离线 · 暂无实时占用"}
          </div>
        )}
        {(speedText || month) && (
          <div className="flex items-center justify-between gap-2 border-t border-dashed border-border pt-2">
            <span className="truncate text-default-600">
              {speedText ?? "网速 —"}
            </span>
            <span
              className="shrink-0 font-semibold text-foreground"
              title={month ? `${split} ${monthHint}` : undefined}
            >
              本月 {total}
            </span>
          </div>
        )}
      </div>
    );
  }

  if (variant === "footer") {
    if (usage.length === 0 && !speedText && !month) return null;

    return (
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs tabular-nums text-default-500 lg:hidden">
        {usage.map(([label, v]) => (
          <span key={label} className="whitespace-nowrap">
            {label}{" "}
            <span className={`font-semibold ${usageColorClass(v!)}`}>
              {Math.round(v!)}%
            </span>
          </span>
        ))}
        {speedText && <span className="whitespace-nowrap">{speedText}</span>}
        {month && (
          <span
            className="whitespace-nowrap font-medium text-foreground"
            title={`${split} ${monthHint}`}
          >
            本月 {total}
          </span>
        )}
      </div>
    );
  }

  return (
    <div className="hidden items-center gap-4 rounded border border-divider bg-default-50/70 px-3 py-1.5 text-xs tabular-nums lg:flex">
      {usage.length > 0 ? (
        <div className="flex items-center gap-3.5">
          {usage.map(([label, v]) => {
            const pct = Math.max(0, Math.min(100, v ?? 0));

            return (
              <div key={label} className="w-16">
                <div className="mb-0.5 flex items-center justify-between text-[11px]">
                  <span className="text-default-500">{label}</span>
                  <span className={`font-semibold ${usageColorClass(pct)}`}>
                    {Math.round(pct)}%
                  </span>
                </div>
                <div className="h-1.5 w-full overflow-hidden rounded-xs bg-default-200">
                  <div
                    className={`h-full transition-[width] duration-300 ${usageBarBg(pct)}`}
                    style={{ width: `${Math.max(3, pct)}%` }}
                  />
                </div>
              </div>
            );
          })}
        </div>
      ) : (
        <span className="w-48 text-default-400">离线 · 无实时资源</span>
      )}
      {speedText && (
        <span className="w-36 truncate whitespace-nowrap text-default-600">
          {speedText}
        </span>
      )}
      <span
        className="w-20 text-right font-semibold text-foreground"
        title={month ? `${split} ${monthHint}` : undefined}
      >
        {month ? `本月 ${total}` : "—"}
      </span>
    </div>
  );
}
