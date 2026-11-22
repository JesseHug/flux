import { useCallback, useEffect, useState } from "react";
import toast from "react-hot-toast";

import {
  type APITokenItem,
  type APITokenScope,
  createApiToken,
  deleteApiToken,
  listApiTokens,
  revokeApiToken,
} from "@/api";
import { Field, SubHeading } from "@/pages/config/section";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Chip } from "@/shadcn-bridge/heroui/chip";
import { Input } from "@/shadcn-bridge/heroui/input";
import {
  Modal,
  ModalBody,
  ModalContent,
  ModalFooter,
  ModalHeader,
  useDisclosure,
} from "@/shadcn-bridge/heroui/modal";
import { Select, SelectItem } from "@/shadcn-bridge/heroui/select";

function formatTimestamp(ms?: number): string {
  if (!ms) return "-";

  return new Date(ms).toLocaleString();
}

function tokenStatus(item: APITokenItem): "active" | "revoked" | "expired" {
  if (item.revoked) return "revoked";
  if (item.expired) return "expired";

  return "active";
}

function statusChipProps(status: "active" | "revoked" | "expired"): {
  label: string;
  color: "success" | "warning" | "danger" | "default";
} {
  if (status === "active") return { label: "启用", color: "success" };
  if (status === "revoked") return { label: "已吊销", color: "danger" };
  if (status === "expired") return { label: "已过期", color: "warning" };

  return { label: status, color: "default" };
}

export function ApiTokenCard() {
  const [tokens, setTokens] = useState<APITokenItem[]>([]);
  const [loading, setLoading] = useState(false);

  const [name, setName] = useState("");
  const [scope, setScope] = useState<APITokenScope>("operator");
  const [allowedIps, setAllowedIps] = useState("");
  const [expiresDays, setExpiresDays] = useState("90");
  const [creating, setCreating] = useState(false);
  const [createdToken, setCreatedToken] = useState<string>("");
  const [busyId, setBusyId] = useState<number | null>(null);

  const createModal = useDisclosure();
  const revealModal = useDisclosure();

  const loadTokens = useCallback(async () => {
    setLoading(true);
    try {
      const res = await listApiTokens();

      if (res.code === 0 && Array.isArray(res.data)) {
        setTokens(res.data);
      }
    } catch {
      toast.error("加载 API Token 列表失败");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadTokens();
  }, [loadTokens]);

  const handleOpenCreate = () => {
    setName("");
    setScope("operator");
    setAllowedIps("");
    setExpiresDays("90");
    createModal.onOpen();
  };

  const handleCreate = async () => {
    if (!name.trim()) {
      toast.error("请输入 Token 名称");

      return;
    }

    let days: number | undefined;
    const rawDays = expiresDays.trim();

    if (rawDays !== "") {
      const parsed = Number(rawDays);

      if (!Number.isInteger(parsed) || parsed < 0 || parsed > 3650) {
        toast.error("过期天数需为 0–3650 的整数（0 或留空表示不过期）");

        return;
      }
      days = parsed;
    }

    setCreating(true);
    try {
      const res = await createApiToken({
        name: name.trim(),
        scope,
        allowedIps: allowedIps.trim(),
        expiresDays: days,
      });

      if (res.code !== 0 || !res.data?.token) {
        toast.error(res.msg || "创建失败");

        return;
      }

      createModal.onClose();
      setCreatedToken(res.data.token);
      revealModal.onOpen();
      toast.success("API Token 已创建");
      void loadTokens();
    } catch {
      toast.error("创建失败");
    } finally {
      setCreating(false);
    }
  };

  const handleCopy = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      toast.success("已复制到剪贴板");
    } catch {
      toast.error("复制失败，请手动选中复制");
    }
  };

  const handleRevoke = async (item: APITokenItem) => {
    setBusyId(item.id);
    try {
      const res = await revokeApiToken(item.id);

      if (res.code !== 0) {
        toast.error(res.msg || "吊销失败");

        return;
      }
      toast.success(`已吊销 ${item.name}`);
      void loadTokens();
    } catch {
      toast.error("吊销失败");
    } finally {
      setBusyId(null);
    }
  };

  const handleDelete = async (item: APITokenItem) => {
    setBusyId(item.id);
    try {
      const res = await deleteApiToken(item.id);

      if (res.code !== 0) {
        toast.error(res.msg || "删除失败");

        return;
      }
      toast.success(`已删除 ${item.name}`);
      void loadTokens();
    } catch {
      toast.error("删除失败");
    } finally {
      setBusyId(null);
    }
  };

  return (
    <>
      <SubHeading>API Token（AI Agent / 自动化脚本）</SubHeading>
      <p className="text-xs leading-5 text-default-500">
        使用 <code>flx_...</code> Token 通过 <code>X-API-Key</code> 或{" "}
        <code>Authorization: Bearer flx_...</code>{" "}
        调用只读或受控运维接口。出于安全隔离，任何 API Token
        均无用户、系统设置、
        备份还原、联邦、升级或宿主机节点修改权限，也不能管理 API Token 本身。
      </p>

      <div className="flex flex-wrap items-center justify-between gap-2">
        <Button color="primary" size="sm" onPress={handleOpenCreate}>
          新建 API Token
        </Button>
        <Button
          disabled={loading}
          size="sm"
          variant="flat"
          onPress={() => void loadTokens()}
        >
          {loading ? "刷新中…" : "刷新列表"}
        </Button>
      </div>

      {tokens.length === 0 ? (
        <div className="rounded-lg border border-dashed border-default-200 px-3 py-4 text-xs text-default-500">
          {loading ? "加载中…" : "暂无 API Token。"}
        </div>
      ) : (
        <div className="space-y-2">
          {tokens.map((item) => {
            const status = tokenStatus(item);
            const chip = statusChipProps(status);

            return (
              <div
                key={item.id}
                className="flex flex-col gap-2 rounded-lg border border-default-200 bg-default-50/40 p-3 sm:flex-row sm:items-center sm:justify-between"
              >
                <div className="min-w-0 space-y-1">
                  <div className="flex flex-wrap items-center gap-1.5">
                    <span className="truncate text-sm font-medium text-foreground">
                      {item.name}
                    </span>
                    <Chip size="sm" variant="flat">
                      {item.scope}
                    </Chip>
                    <Chip color={chip.color} size="sm" variant="flat">
                      {chip.label}
                    </Chip>
                    <code className="rounded bg-default-100 px-1.5 py-0.5 text-[11px] text-default-600">
                      {item.prefix}…
                    </code>
                  </div>
                  <div className="flex flex-wrap gap-x-4 gap-y-0.5 text-xs text-default-500">
                    <span>创建：{formatTimestamp(item.createdTime)}</span>
                    <span>
                      过期：
                      {item.expiresAt
                        ? formatTimestamp(item.expiresAt)
                        : "永不过期"}
                    </span>
                    <span>
                      最近使用：
                      {item.lastUsedAt
                        ? `${formatTimestamp(item.lastUsedAt)}${item.lastUsedIp ? ` (${item.lastUsedIp})` : ""}`
                        : "未使用"}
                    </span>
                    {item.allowedIps ? (
                      <span>IP 白名单：{item.allowedIps}</span>
                    ) : null}
                  </div>
                </div>

                <div className="flex shrink-0 items-center gap-2">
                  {status === "active" && (
                    <Button
                      color="warning"
                      disabled={busyId === item.id}
                      size="sm"
                      variant="flat"
                      onPress={() => void handleRevoke(item)}
                    >
                      吊销
                    </Button>
                  )}
                  <Button
                    color="danger"
                    disabled={busyId === item.id}
                    size="sm"
                    variant="light"
                    onPress={() => void handleDelete(item)}
                  >
                    删除
                  </Button>
                </div>
              </div>
            );
          })}
        </div>
      )}

      <Modal
        isOpen={createModal.isOpen}
        size="md"
        onOpenChange={createModal.onOpenChange}
      >
        <ModalContent>
          <ModalHeader>新建 API Token</ModalHeader>
          <ModalBody>
            <Field label="名称">
              <Input
                placeholder="例如：Cursor / OpenClaw Ops"
                size="sm"
                value={name}
                variant="bordered"
                onChange={(e) => setName(e.target.value)}
              />
            </Field>

            <Field
              help="readonly 仅限查询与预演；operator 可管理入站、中转、订阅及触发内核同步，仍永久封禁用户/配置/备份/升级/节点级高危路由。"
              label="权限范围（Scope）"
            >
              <Select
                selectedKeys={[scope]}
                size="sm"
                variant="bordered"
                onSelectionChange={(keys) => {
                  const selected = Array.from(keys)[0];

                  if (selected === "readonly" || selected === "operator") {
                    setScope(selected);
                  }
                }}
              >
                <SelectItem
                  key="operator"
                  textValue="operator（入站 / 订阅 / 内核同步）"
                >
                  operator（入站 / 订阅 / 内核同步）
                </SelectItem>
                <SelectItem key="readonly" textValue="readonly（只读 / 预演）">
                  readonly（只读 / 预演）
                </SelectItem>
              </Select>
            </Field>

            <Field
              help="支持单个 IP 或 CIDR，多个值用逗号或空格分隔；留空不限制来源 IP。"
              label="允许来源 IP / CIDR（可选）"
            >
              <Input
                placeholder="例如：127.0.0.1, 10.0.0.0/8"
                size="sm"
                value={allowedIps}
                variant="bordered"
                onChange={(e) => setAllowedIps(e.target.value)}
              />
            </Field>

            <Field help="填 0 或留空表示永不过期。" label="有效期（天）">
              <Input
                placeholder="90"
                size="sm"
                value={expiresDays}
                variant="bordered"
                onChange={(e) => setExpiresDays(e.target.value)}
              />
            </Field>
          </ModalBody>
          <ModalFooter>
            <Button size="sm" variant="flat" onPress={createModal.onClose}>
              取消
            </Button>
            <Button
              color="primary"
              isLoading={creating}
              size="sm"
              onPress={() => void handleCreate()}
            >
              生成 Token
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>

      <Modal
        isOpen={revealModal.isOpen}
        size="md"
        onOpenChange={revealModal.onOpenChange}
      >
        <ModalContent>
          <ModalHeader closable>保存你的 API Token</ModalHeader>
          <ModalBody>
            <p className="text-xs text-warning-600 dark:text-warning-400">
              该明文 Token
              仅在此时显示一次，后端仅存储哈希。请立即复制并妥善保存。
            </p>
            {createdToken ? (
              <div className="space-y-2 rounded-lg border border-default-200 bg-default-100/60 p-3">
                <div className="break-all font-mono text-xs text-foreground">
                  {createdToken}
                </div>
                <div className="text-xs text-default-500">
                  请求头示例：<code>X-API-Key: {createdToken}</code>
                </div>
              </div>
            ) : null}
          </ModalBody>
          <ModalFooter>
            {createdToken ? (
              <Button
                color="primary"
                size="sm"
                onPress={() => void handleCopy(createdToken)}
              >
                复制 Token
              </Button>
            ) : null}
            <Button size="sm" variant="flat" onPress={revealModal.onClose}>
              我已保存
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>
    </>
  );
}
