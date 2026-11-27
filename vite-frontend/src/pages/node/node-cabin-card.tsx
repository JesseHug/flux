import type { NodeRowActions } from "@/pages/node/node-row";
import type { Node } from "@/pages/node/types";

import { memo } from "react";
import { GripVertical } from "lucide-react";

import { Chip } from "@/shadcn-bridge/heroui/chip";
import { Checkbox } from "@/shadcn-bridge/heroui/checkbox";
import { getConnectionStatusMeta } from "@/pages/node/display";
import { formatUptime } from "@/pages/node/format";
import { SortableItem } from "@/pages/node/helpers";
import { NodeLiveStats } from "@/pages/node/node-live-stats";
import { useThemeContext } from "@/themes/context";
import { cabinLabel } from "@/themes/stardew/assets";
import { StardewCabin } from "@/themes/stardew/components/stardew-cabin";
import { useStardewEnv } from "@/themes/stardew/env-context";

/** Cabin card representation of a server for Stardew theme grid view. */
export const NodeCabinCard = memo(function NodeCabinCard({
  node,
  progress,
  month,
  selected,
  selectMode,
  isDisplayFiltered,
  actions,
}: {
  node: Node;
  progress?: { stage: string; percent: number; message: string };
  month?: [number, number];
  selected: boolean;
  selectMode: boolean;
  isDisplayFiltered: boolean;
  actions: NodeRowActions;
}) {
  const { effectiveMode } = useThemeContext();
  const { getNodeCabin } = useStardewEnv();
  const isNight = effectiveMode === "dark";
  const isRemoteNode = node.isRemote === 1;
  const online = node.connectionStatus === "online";
  const upgrading = progress && progress.percent < 100;
  const address =
    node.serverIpV4?.trim() || node.serverIpV6?.trim() || node.serverIp.trim();
  const cabinId = getNodeCabin(node.id);

  return (
    <SortableItem id={node.id}>
      {(listeners) => (
        <div
          aria-pressed={selectMode ? selected : undefined}
          className={`stardew-wood-frame group flex cursor-pointer flex-col gap-3 p-4 transition-transform hover:-translate-y-0.5 ${
            selected ? "ring-2 ring-primary" : ""
          }`}
          role="button"
          tabIndex={0}
          onClick={() =>
            selectMode
              ? actions.toggleSelect(node.id)
              : actions.setSheetNodeId(node.id)
          }
          onKeyDown={(e) => {
            if (e.target !== e.currentTarget) return;
            if (e.key === "Enter" || e.key === " ") {
              e.preventDefault();
              if (selectMode) actions.toggleSelect(node.id);
              else actions.setSheetNodeId(node.id);
            }
          }}
        >
          <div className="flex items-start justify-between gap-2">
            <div className="flex min-w-0 items-center gap-3">
              {selectMode && (
                <div role="presentation" onClick={(e) => e.stopPropagation()}>
                  <Checkbox
                    isSelected={selected}
                    onValueChange={() => actions.toggleSelect(node.id)}
                  />
                </div>
              )}
              {!isRemoteNode && (
                <button
                  aria-label={`更换 ${node.name} 的小屋外观`}
                  className="stardew-inset shrink-0 p-1.5 transition-transform hover:scale-105"
                  title="点击选择小屋样式"
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation();
                    actions.onPickCabin?.(node);
                  }}
                >
                  <StardewCabin
                    cabinId={cabinId}
                    isNight={isNight}
                    online={online}
                    size={40}
                  />
                </button>
              )}
              <div className="min-w-0">
                <div className="flex items-center gap-1.5">
                  <span
                    className={`h-2 w-2 shrink-0 rounded-xs ${
                      upgrading
                        ? "bg-warning"
                        : online
                          ? "bg-success"
                          : "bg-danger"
                    }`}
                    title={getConnectionStatusMeta(node.connectionStatus).text}
                  />
                  <span className="truncate text-sm font-bold text-foreground">
                    {node.name}
                  </span>
                  {isRemoteNode && (
                    <Chip className="shrink-0" size="sm" variant="flat">
                      远程
                    </Chip>
                  )}
                  {!isRemoteNode && node.forwardMode === "lite" && (
                    <Chip
                      className="shrink-0"
                      color="secondary"
                      size="sm"
                      variant="flat"
                    >
                      轻量
                    </Chip>
                  )}
                </div>
                <div className="mt-0.5 truncate text-xs tabular-nums text-default-500">
                  {[
                    address,
                    upgrading
                      ? `升级中 ${progress.percent}%`
                      : node.version || "未连接过",
                    online && node.systemInfo
                      ? `运行 ${formatUptime(node.systemInfo.uptime)}`
                      : "",
                  ]
                    .filter(Boolean)
                    .join(" · ")}
                </div>
              </div>
            </div>

            <div className="flex shrink-0 items-center gap-1">
              {!isRemoteNode && (
                <button
                  className="stardew-inset px-2 py-0.5 text-[11px] font-semibold text-default-600 hover:bg-default-200"
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation();
                    actions.onPickCabin?.(node);
                  }}
                >
                  {cabinLabel(cabinId)}
                </button>
              )}
              {!isDisplayFiltered && !selectMode && (
                <span
                  className="cursor-grab touch-none p-1 text-default-400 hover:text-foreground active:cursor-grabbing"
                  role="presentation"
                  title="拖动排序"
                  onClick={(e) => e.stopPropagation()}
                  {...listeners}
                >
                  <GripVertical className="h-4 w-4" />
                </span>
              )}
            </div>
          </div>

          {!isRemoteNode && (
            <NodeLiveStats
              info={online ? (node.systemInfo ?? null) : null}
              month={month}
              variant="card"
            />
          )}
        </div>
      )}
    </SortableItem>
  );
});
