import { memo } from "react";
import { GripVertical } from "lucide-react";

import { Chip } from "@/shadcn-bridge/heroui/chip";
import { ListRow } from "@/components/page-kit";
import { getConnectionStatusMeta } from "@/pages/node/display";
import { Node } from "@/pages/node/types";
import { SortableItem } from "@/pages/node/helpers";
import { NodeLiveStats } from "@/pages/node/node-live-stats";
import { formatUptime } from "@/pages/node/format";
import { useThemeContext } from "@/themes/context";
import { useStardewEnv } from "@/themes/stardew/env-context";
import { StardewCabin } from "@/themes/stardew/components/stardew-cabin";

export interface NodeRowActions {
  toggleSelect: (id: number) => void;
  setSheetNodeId: (id: number) => void;
  onPickCabin?: (node: Node) => void;
}
interface NodeRowProps {
  node: Node;
  progress?: { stage: string; percent: number; message: string };
  month?: [number, number];
  selected: boolean;
  selectMode: boolean;
  isDisplayFiltered: boolean;
  wide: boolean;
  actions: NodeRowActions;
}
export const NodeRow = memo(function NodeRow(props: NodeRowProps) {
  return (
    <SortableItem id={props.node.id}>
      {(listeners) => <NodeRowContent {...props} listeners={listeners} />}
    </SortableItem>
  );
});

const NodeRowContent = memo(function NodeRowContent({
  node,
  progress,
  month,
  selected,
  selectMode,
  isDisplayFiltered,
  wide,
  actions,
  listeners,
}: NodeRowProps & { listeners: any }) {
  const { toggleSelect, setSheetNodeId, onPickCabin } = actions;
  const { activeThemeId, effectiveMode } = useThemeContext();
  const { getNodeCabin } = useStardewEnv();
  const isStardew = activeThemeId === "stardew";
  const isNight = effectiveMode === "dark";

  const isRemoteNode = node.isRemote === 1;
  const online = node.connectionStatus === "online";

  const upgrading = progress && progress.percent < 100;
  const address =
    node.serverIpV4?.trim() || node.serverIpV6?.trim() || node.serverIp.trim();
  const meta = isRemoteNode
    ? [address, node.syncError ? "同步失败" : ""]
    : [
        address,
        upgrading ? `升级中 ${progress.percent}%` : node.version || "未连接过",
        online && node.systemInfo
          ? `已运行 ${formatUptime(node.systemInfo.uptime)}`
          : "",
      ];

  return (
    <ListRow
      badges={
        <>
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
        </>
      }
      footer={
        !isRemoteNode && !wide ? (
          <NodeLiveStats
            info={online ? (node.systemInfo ?? null) : null}
            month={month}
            variant="footer"
          />
        ) : undefined
      }
      meta={meta.filter(Boolean).join(" · ")}
      right={
        <>
          {!isRemoteNode && wide && (
            <NodeLiveStats
              info={online ? (node.systemInfo ?? null) : null}
              month={month}
              variant="row"
            />
          )}
          {!isDisplayFiltered && (
            <span
              className="cursor-grab touch-none p-1 text-default-300 transition-opacity hover:text-default-500 active:cursor-grabbing sm:opacity-0 sm:group-hover:opacity-100"
              title="拖动排序"
              {...listeners}
            >
              <GripVertical className="h-4 w-4" />
            </span>
          )}
        </>
      }
      selectMode={selectMode}
      selected={selected}
      status={upgrading ? "warn" : online ? "ok" : "bad"}
      statusTitle={getConnectionStatusMeta(node.connectionStatus).text}
      title={
        <span className="inline-flex items-center gap-2">
          {isStardew && !isRemoteNode && (
            <button
              aria-label={`更换 ${node.name} 的小屋外观`}
              className="stardew-inset inline-flex shrink-0 p-0.5 transition-transform hover:scale-105"
              title="点击选择小屋样式"
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                onPickCabin?.(node);
              }}
            >
              <StardewCabin
                cabinId={getNodeCabin(node.id)}
                isNight={isNight}
                online={online}
                size={24}
              />
            </button>
          )}
          <span className="truncate">{node.name}</span>
        </span>
      }
      onPress={() => setSheetNodeId(node.id)}
      onSelect={() => toggleSelect(node.id)}
    />
  );
});
