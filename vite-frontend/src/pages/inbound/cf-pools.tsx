import type { CFPoolApiItem, CFPoolEntry } from "@/api/types";

import { useEffect, useState } from "react";
import toast from "react-hot-toast";
import { RefreshCw } from "lucide-react";

import {
  deleteCFPool,
  previewCFPoolParse,
  refreshCFPool,
  saveCFPool,
} from "@/api";
import { Segmented, ToggleRow } from "@/pages/subscription/controls";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Chip } from "@/shadcn-bridge/heroui/chip";
import { Input, Textarea } from "@/shadcn-bridge/heroui/input";
import {
  Modal,
  ModalBody,
  ModalContent,
  ModalFooter,
  ModalHeader,
} from "@/shadcn-bridge/heroui/modal";
import { Select, SelectItem } from "@/shadcn-bridge/heroui/select";

const modalBase = "!w-[calc(100%-32px)] !mx-auto sm:!w-full overflow-hidden";

const POOL_INTERVALS: Array<[string, string]> = [
  ["0", "手动"],
  ["6", "6 小时"],
  ["12", "12 小时"],
  ["24", "每天"],
];

const IP_FILTER_LABELS: Record<CFPoolApiItem["ipFilter"], string> = {
  all: "全部（IPv4 + IPv6 + 域名）",
  ipv4: "仅 IPv4",
  ipv6: "仅 IPv6",
  domain: "仅优选域名",
};

interface PoolDraft {
  id?: number;
  name: string;
  kind: "manual" | "url";
  url: string;
  rawText: string;
  userAgent: string;
  intervalHours: number;
  defaultPort: string;
  maxEntries: string;
  ipFilter: "all" | "ipv4" | "ipv6" | "domain";
  enabled: boolean;
}

const emptyDraft = (): PoolDraft => ({
  name: "",
  kind: "manual",
  url: "",
  rawText: "",
  userAgent: "",
  intervalHours: 12,
  defaultPort: "443",
  maxEntries: "10",
  ipFilter: "all",
  enabled: true,
});

const draftOf = (p: CFPoolApiItem): PoolDraft => ({
  id: p.id,
  name: p.name,
  kind: p.kind,
  url: p.url,
  rawText: p.rawText,
  userAgent: p.userAgent ?? "",
  intervalHours: p.intervalHours,
  defaultPort: String(p.defaultPort || 443),
  maxEntries: String(p.maxEntries || 10),
  ipFilter: p.ipFilter || "all",
  enabled: p.enabled,
});

function ago(ms: number): string {
  if (!ms) return "还没更新过";
  const m = Math.floor((Date.now() - ms) / 60000);

  if (m < 1) return "刚刚更新";
  if (m < 60) return `${m} 分钟前更新`;
  if (m < 60 * 24) return `${Math.floor(m / 60)} 小时前更新`;

  return `${Math.floor(m / 1440)} 天前更新`;
}

export function CFPoolsSection({
  pools,
  onChanged,
}: {
  pools: CFPoolApiItem[];
  onChanged: () => Promise<void> | void;
}) {
  const [draft, setDraft] = useState<PoolDraft | null>(null);
  const [saving, setSaving] = useState(false);
  const [refreshing, setRefreshing] = useState<number | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [forceDeleteMsg, setForceDeleteMsg] = useState("");
  const [previewing, setPreviewing] = useState(false);
  const [previewEntries, setPreviewEntries] = useState<CFPoolEntry[] | null>(
    null,
  );

  useEffect(() => {
    setConfirmDelete(false);
    setForceDeleteMsg("");
    setPreviewEntries(null);
  }, [draft?.id]);

  const refresh = async (p: CFPoolApiItem) => {
    setRefreshing(p.id);
    try {
      const res = await refreshCFPool(p.id);

      if (res.code !== 0) {
        toast.error(res.msg || "更新失败");
      } else {
        toast.success(res.msg || "已同步");
      }
      await onChanged();
    } finally {
      setRefreshing(null);
    }
  };

  const runPreview = async () => {
    if (!draft) return;
    setPreviewing(true);
    try {
      const res = await previewCFPoolParse({
        rawText: draft.rawText,
        defaultPort: Number(draft.defaultPort) || 443,
        maxEntries: Number(draft.maxEntries) || 10,
        ipFilter: draft.ipFilter,
      });

      if (res.code !== 0) {
        toast.error(res.msg || "解析失败");

        return;
      }
      setPreviewEntries(res.data?.entries ?? []);
      if ((res.data?.count ?? 0) === 0) {
        toast.error("未识别到有效的 IP 或域名");
      }
    } finally {
      setPreviewing(false);
    }
  };

  const save = async () => {
    if (!draft) return;
    const defaultPort = Number(draft.defaultPort) || 443;
    const maxEntries = Number(draft.maxEntries) || 10;

    if (!draft.name.trim()) {
      toast.error("请填写名称");

      return;
    }
    if (defaultPort < 1 || defaultPort > 65535) {
      toast.error("默认端口必须在 1-65535 之间");

      return;
    }
    if (maxEntries < 1 || maxEntries > 50) {
      toast.error("最多保留条数需在 1-50 之间");

      return;
    }
    setSaving(true);
    try {
      const res = await saveCFPool({
        id: draft.id,
        name: draft.name.trim(),
        kind: draft.kind,
        url: draft.kind === "url" ? draft.url.trim() : "",
        rawText: draft.kind === "manual" ? draft.rawText : "",
        userAgent: draft.kind === "url" ? draft.userAgent.trim() : "",
        intervalHours: draft.kind === "url" ? draft.intervalHours : 0,
        defaultPort,
        maxEntries,
        ipFilter: draft.ipFilter,
        enabled: draft.enabled,
      });

      if (res.code !== 0) {
        toast.error(res.msg || "保存失败");
        if (res.data) {
          setDraft(null);
          await onChanged();
        }

        return;
      }
      toast.success(res.msg && res.msg !== "操作成功" ? res.msg : "已保存");
      setDraft(null);
      await onChanged();
    } finally {
      setSaving(false);
    }
  };

  const remove = async (force: boolean) => {
    if (!draft?.id) return;
    setDeleting(true);
    try {
      const res = await deleteCFPool(draft.id, force);

      if (res.code !== 0) {
        if (!force && (res.msg || "").includes("正被入站使用")) {
          setForceDeleteMsg(res.msg);

          return;
        }
        toast.error(res.msg || "删除失败");

        return;
      }
      toast.success(force ? "已解绑并删除优选池" : "已删除优选池");
      setDraft(null);
      await onChanged();
    } finally {
      setDeleting(false);
    }
  };

  const editing = pools.find((p) => p.id === draft?.id);

  return (
    <section className="space-y-2">
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-semibold text-default-600">CF 优选池</h2>
        <Button
          size="sm"
          variant="light"
          onPress={() => setDraft(emptyDraft())}
        >
          添加
        </Button>
      </div>

      {pools.length === 0 ? (
        <p className="rounded-xl border border-dashed border-divider px-3 py-6 text-center text-xs text-default-500">
          创建可复用的 Cloudflare 优选 IP / 优选域名列表：支持手动粘贴（兼容
          776771.xyz、WeTest、HostMonit、 CloudflareSpeedTest CSV）或远程 URL
          定时拉取。入站绑定优选池后， 分享与订阅会自动展开为多条优选节点。
        </p>
      ) : (
        <div className="grid grid-cols-1 gap-2 2xl:grid-cols-2">
          {pools.map((p) => (
            <div
              key={p.id}
              className="flex cursor-pointer items-center gap-3 rounded-xl border border-divider bg-content1 px-3 py-2.5 shadow-sm transition-colors hover:bg-default-100"
              role="button"
              tabIndex={0}
              onClick={() => setDraft(draftOf(p))}
              onKeyDown={(e) => {
                if (e.target !== e.currentTarget) return;
                if (e.key === "Enter" || e.key === " ") {
                  e.preventDefault();
                  (e.currentTarget as HTMLElement).click();
                }
              }}
            >
              <span
                className={`h-2 w-2 shrink-0 rounded-full ${
                  !p.enabled
                    ? "bg-default-300"
                    : p.lastError
                      ? "bg-danger"
                      : p.entryCount > 0
                        ? "bg-success"
                        : "bg-warning"
                }`}
              />
              <div className="min-w-0 flex-1 space-y-1">
                <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                  <span className="truncate font-medium">{p.name}</span>
                  <Chip size="sm" variant="flat">
                    {p.kind === "url" ? "远程 URL" : "手动列表"}
                  </Chip>
                  {!p.enabled && (
                    <Chip color="warning" size="sm" variant="flat">
                      已停用
                    </Chip>
                  )}
                </div>
                <div className="truncate text-xs text-default-500">
                  {p.entryCount} 条有效地址 · 默认端口 {p.defaultPort} ·{" "}
                  {p.kind === "url"
                    ? `${ago(p.lastFetch)}${p.intervalHours ? ` · 每 ${p.intervalHours} 小时` : " · 手动更新"}`
                    : ago(p.lastFetch).replace("更新", "保存")}
                </div>
                {p.lastError ? (
                  <div className="truncate text-xs text-danger">
                    {p.lastError}
                  </div>
                ) : (
                  p.entries.length > 0 && (
                    <div className="flex flex-wrap gap-1 pt-0.5">
                      {p.entries.slice(0, 3).map((e) => (
                        <span
                          key={`${e.host}:${e.port}`}
                          className="rounded bg-default-100 px-1.5 py-0.5 font-mono text-[10px] text-default-600"
                        >
                          {e.host}:{e.port}
                          {e.remark ? `#${e.remark}` : ""}
                        </span>
                      ))}
                      {p.entries.length > 3 && (
                        <span className="text-[10px] text-default-400">
                          +{p.entries.length - 3}
                        </span>
                      )}
                    </div>
                  )
                )}
              </div>
              {p.kind === "url" && (
                <div
                  className="shrink-0"
                  role="presentation"
                  onClick={(e) => e.stopPropagation()}
                >
                  <Button
                    isIconOnly
                    aria-label={`更新 ${p.name}`}
                    isLoading={refreshing === p.id}
                    size="sm"
                    variant="flat"
                    onPress={() => void refresh(p)}
                  >
                    <RefreshCw className="h-4 w-4" />
                  </Button>
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      <Modal
        classNames={{ base: modalBase }}
        isOpen={Boolean(draft)}
        placement="center"
        scrollBehavior="inside"
        size="xl"
        onOpenChange={(open) => !open && setDraft(null)}
      >
        <ModalContent>
          {(onClose) =>
            draft && (
              <>
                <ModalHeader>
                  <h2 className="text-lg font-bold">
                    {draft.id ? "编辑 CF 优选池" : "新建 CF 优选池"}
                  </h2>
                </ModalHeader>
                <ModalBody className="space-y-4">
                  <Input
                    label="名称"
                    placeholder="例如：电信移动三网优选"
                    value={draft.name}
                    variant="bordered"
                    onChange={(e) =>
                      setDraft({ ...draft, name: e.target.value })
                    }
                  />
                  <Segmented
                    options={[
                      ["manual", "手动粘贴"],
                      ["url", "远程 URL"],
                    ]}
                    value={draft.kind}
                    onChange={(kind) =>
                      setDraft({
                        ...draft,
                        kind: kind as PoolDraft["kind"],
                      })
                    }
                  />
                  {draft.kind === "manual" ? (
                    <div className="space-y-2">
                      <Textarea
                        description="每行一条，支持 IP、IP:端口、IP:端口#备注、[IPv6]:端口#备注、优选域名:端口#备注，也可直接粘贴 CloudflareSpeedTest CSV 或 WeTest / HostMonit JSON"
                        label="优选 IP / 优选域名列表"
                        minRows={5}
                        placeholder={
                          "104.16.132.229:443#移动优选\n172.67.1.2:443#电信优选\ncf.877774.xyz:443#公共优选域名"
                        }
                        value={draft.rawText}
                        variant="bordered"
                        onChange={(e) =>
                          setDraft({ ...draft, rawText: e.target.value })
                        }
                      />
                      <div className="flex items-center justify-between gap-2">
                        <Button
                          isLoading={previewing}
                          size="sm"
                          variant="flat"
                          onPress={() => void runPreview()}
                        >
                          解析预览
                        </Button>
                        {previewEntries && (
                          <span className="text-xs text-default-500">
                            共识别 {previewEntries.length} 条有效地址
                          </span>
                        )}
                      </div>
                      {previewEntries && previewEntries.length > 0 && (
                        <div className="flex max-h-28 flex-wrap gap-1.5 overflow-y-auto rounded-lg border border-divider p-2">
                          {previewEntries.map((e) => (
                            <Chip
                              key={`${e.host}:${e.port}`}
                              size="sm"
                              variant="flat"
                            >
                              {e.host}:{e.port}
                              {e.remark ? ` (${e.remark})` : ""}
                            </Chip>
                          ))}
                        </div>
                      )}
                    </div>
                  ) : (
                    <>
                      <Input
                        description="支持返回纯文本（IP:端口#备注 / 优选域名）、CSV 或常见 JSON 的链接；拉取失败时会自动保留上次缓存"
                        label="优选源 URL"
                        placeholder="https://..."
                        value={draft.url}
                        variant="bordered"
                        onChange={(e) =>
                          setDraft({ ...draft, url: e.target.value })
                        }
                      />
                      <div className="space-y-1.5">
                        <p className="text-sm font-medium">自动更新</p>
                        <Segmented
                          options={POOL_INTERVALS}
                          value={String(draft.intervalHours)}
                          onChange={(v) =>
                            setDraft({ ...draft, intervalHours: Number(v) })
                          }
                        />
                      </div>
                      <Input
                        label="User-Agent（可选）"
                        placeholder="clash.meta/1.19.0"
                        value={draft.userAgent}
                        variant="bordered"
                        onChange={(e) =>
                          setDraft({ ...draft, userAgent: e.target.value })
                        }
                      />
                    </>
                  )}

                  <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
                    <Input
                      description="条目未带端口时使用"
                      label="默认端口"
                      type="number"
                      value={draft.defaultPort}
                      variant="bordered"
                      onChange={(e) =>
                        setDraft({ ...draft, defaultPort: e.target.value })
                      }
                    />
                    <Input
                      description="1-50 条，建议 5-15 条"
                      label="最多保留条数"
                      type="number"
                      value={draft.maxEntries}
                      variant="bordered"
                      onChange={(e) =>
                        setDraft({ ...draft, maxEntries: e.target.value })
                      }
                    />
                    <Select
                      label="地址过滤"
                      selectedKeys={[draft.ipFilter]}
                      onSelectionChange={(keys) =>
                        setDraft({
                          ...draft,
                          ipFilter: (Array.from(keys)[0] ??
                            "all") as PoolDraft["ipFilter"],
                        })
                      }
                    >
                      {Object.entries(IP_FILTER_LABELS).map(([k, label]) => (
                        <SelectItem key={k} textValue={label}>
                          {label}
                        </SelectItem>
                      ))}
                    </Select>
                  </div>

                  <ToggleRow
                    hint="停用后，绑定此优选池的入站将不再展开池内地址"
                    id="cf-pool-enabled"
                    label="启用此优选池"
                    value={draft.enabled}
                    onChange={(enabled) => setDraft({ ...draft, enabled })}
                  />

                  {editing && (
                    <p className="text-xs text-default-500">
                      当前缓存 {editing.entryCount} 条 ·{" "}
                      {ago(editing.lastFetch)}
                    </p>
                  )}
                </ModalBody>
                <ModalFooter className="flex-wrap">
                  {confirmDelete ? (
                    <>
                      <p className="w-full text-sm text-danger">
                        {forceDeleteMsg || "确认删除此 CF 优选池？"}
                      </p>
                      <Button
                        variant="light"
                        onPress={() => {
                          setConfirmDelete(false);
                          setForceDeleteMsg("");
                        }}
                      >
                        取消
                      </Button>
                      <Button
                        color="danger"
                        isLoading={deleting}
                        onPress={() => remove(Boolean(forceDeleteMsg))}
                      >
                        {forceDeleteMsg ? "解绑入站并删除" : "确认删除"}
                      </Button>
                    </>
                  ) : (
                    <>
                      {draft.id && (
                        <Button
                          className="mr-auto"
                          color="danger"
                          variant="light"
                          onPress={() => setConfirmDelete(true)}
                        >
                          删除
                        </Button>
                      )}
                      <Button variant="light" onPress={onClose}>
                        取消
                      </Button>
                      <Button color="primary" isLoading={saving} onPress={save}>
                        {draft.id
                          ? "保存"
                          : draft.kind === "url"
                            ? "保存并拉取"
                            : "保存"}
                      </Button>
                    </>
                  )}
                </ModalFooter>
              </>
            )
          }
        </ModalContent>
      </Modal>
    </section>
  );
}
