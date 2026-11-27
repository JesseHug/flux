import { CABIN_CATALOG } from "../assets";

import { StardewCabin } from "./stardew-cabin";

import {
  Modal,
  ModalBody,
  ModalContent,
  ModalHeader,
} from "@/shadcn-bridge/heroui/modal";
import { useThemeContext } from "@/themes/context";

/** Popup selector for picking any of the 16 Stardew Valley cabin appearances for a server. */
export function HousePickerModal({
  isOpen,
  serverName,
  currentCabinId,
  onSelect,
  onClose,
}: {
  isOpen: boolean;
  serverName: string;
  currentCabinId: string;
  onSelect: (cabinId: string) => void;
  onClose: () => void;
}) {
  const { effectiveMode } = useThemeContext();
  const isNight = effectiveMode === "dark";

  return (
    <Modal
      classNames={{
        base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full overflow-hidden",
      }}
      isOpen={isOpen}
      placement="center"
      scrollBehavior="inside"
      size="2xl"
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <ModalContent>
        <ModalHeader closable className="flex flex-col gap-0.5">
          <span className="text-base font-bold">
            选择服务器小屋 · {serverName}
          </span>
          <span className="text-xs font-normal text-default-500">
            点击任意星露谷建筑立即应用（入夜后在线服务器会自动亮起对应窗灯）
          </span>
        </ModalHeader>
        <ModalBody className="pb-5">
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
            {CABIN_CATALOG.map((item) => {
              const selected = item.id === currentCabinId;

              return (
                <button
                  key={item.id}
                  className={`flex flex-col items-center gap-2 rounded-md border p-3 text-center transition-colors ${
                    selected
                      ? "border-primary bg-primary/10"
                      : "border-divider bg-default-50 hover:border-default-400 hover:bg-default-100"
                  }`}
                  type="button"
                  onClick={() => {
                    onSelect(item.id);
                    onClose();
                  }}
                >
                  <StardewCabin
                    online
                    cabinId={item.id}
                    isNight={isNight}
                    size={52}
                  />
                  <span className="text-xs font-semibold text-foreground">
                    {item.name}
                  </span>
                  <span
                    className={`rounded px-1.5 py-0.5 text-[10px] font-semibold ${
                      selected
                        ? "bg-primary text-primary-foreground"
                        : "text-default-400"
                    }`}
                  >
                    {selected ? "当前使用" : "点击选择"}
                  </span>
                </button>
              );
            })}
          </div>
        </ModalBody>
      </ModalContent>
    </Modal>
  );
}
