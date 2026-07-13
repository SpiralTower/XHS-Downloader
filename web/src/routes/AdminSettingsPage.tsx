import {
  Alert,
  Button,
  Card,
  Description,
  Fieldset,
  Form,
  Spinner,
} from "@heroui/react";
import { useCallback, useEffect, useState } from "react";
import type { FormEvent } from "react";
import { useNavigate } from "react-router";

import {
  ApiError,
  getAdminSettings,
  updateAdminSettings,
} from "../api";
import SecretSetting from "../features/settings/SecretSetting";
import SettingSwitch from "../features/settings/SettingSwitch";
import { CheckIcon, RefreshIcon, ShieldIcon, XIcon } from "../icons";
import type {
  AdminSettings,
  SecretAction,
} from "../types";

interface SecretDraft {
  action: SecretAction;
  value: string;
}

export default function AdminSettingsPage() {
  const navigate = useNavigate();
  const [settings, setSettings] = useState<AdminSettings | null>(null);
  const [draft, setDraft] = useState<AdminSettings | null>(null);
  const [cookie, setCookie] = useState<SecretDraft>({
    action: "keep",
    value: "",
  });
  const [proxy, setProxy] = useState<SecretDraft>({
    action: "keep",
    value: "",
  });
  const [isLoading, setIsLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);
  const [error, setError] = useState("");
  const [savedMessage, setSavedMessage] = useState("");

  const load = useCallback(async (signal?: AbortSignal) => {
    setIsLoading(true);
    setError("");
    try {
      const result = await getAdminSettings(signal);
      setSettings(result);
      setDraft(result);
      setCookie({ action: "keep", value: "" });
      setProxy({ action: "keep", value: "" });
    } catch (cause) {
      if (cause instanceof DOMException && cause.name === "AbortError") return;
      if (cause instanceof ApiError && cause.status === 401) {
        navigate("/admin/login?next=/admin/settings", { replace: true });
        return;
      }
      setError(cause instanceof Error ? cause.message : "读取设置失败");
    } finally {
      if (!signal?.aborted) setIsLoading(false);
    }
  }, [navigate]);

  useEffect(() => {
    document.title = "系统设置 · XHS Downloader";
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!draft) return;

    setIsSaving(true);
    setError("");
    setSavedMessage("");
    try {
      const result = await updateAdminSettings({
        revision: draft.revision,
        public: draft.public,
        save: draft.save,
        refetch: draft.refetch,
        default_cookie: {
          action: cookie.action,
          ...(cookie.action === "replace" ? { value: cookie.value.trim() } : {}),
        },
        default_proxy: {
          action: proxy.action,
          ...(proxy.action === "replace" ? { value: proxy.value.trim() } : {}),
        },
      });
      setSettings(result);
      setDraft(result);
      setCookie({ action: "keep", value: "" });
      setProxy({ action: "keep", value: "" });
      setSavedMessage(`设置已保存，当前修订号为 ${result.revision}`);
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 401) {
        navigate("/admin/login?next=/admin/settings", { replace: true });
        return;
      }
      setError(
        cause instanceof ApiError && cause.status === 409
          ? "设置已被其他会话修改。请重新载入后再保存。"
          : cause instanceof Error
            ? cause.message
            : "保存设置失败",
      );
    } finally {
      setIsSaving(false);
    }
  };

  const updateSave = (key: keyof AdminSettings["save"], value: boolean) => {
    setDraft((current) =>
      current
        ? { ...current, save: { ...current.save, [key]: value } }
        : current,
    );
  };

  return (
    <div className="grid gap-6">
      <header>
        <h1
          className="text-3xl font-bold tracking-[-0.035em] outline-none"
          id="main-heading"
          tabIndex={-1}
        >
          系统设置
        </h1>
        <p className="mt-2 max-w-3xl text-sm leading-6 text-muted">
          控制匿名访问、默认连接、资源保存和历史作品的重新抓取策略。
        </p>
      </header>

      {error && (
        <Alert status="danger">
          <Alert.Indicator>
            <XIcon className="size-5" />
          </Alert.Indicator>
          <Alert.Content>
            <Alert.Title>设置操作失败</Alert.Title>
            <Alert.Description>{error}</Alert.Description>
          </Alert.Content>
          <Button onPress={() => void load()} size="sm" variant="secondary">
            <RefreshIcon className="size-4" />
            重新载入
          </Button>
        </Alert>
      )}

      {savedMessage && (
        <Alert status="success">
          <Alert.Indicator>
            <CheckIcon className="size-5" />
          </Alert.Indicator>
          <Alert.Content>
            <Alert.Title>保存成功</Alert.Title>
            <Alert.Description>{savedMessage}</Alert.Description>
          </Alert.Content>
        </Alert>
      )}

      {isLoading ? (
        <Card className="glass-panel min-h-72 border border-border">
          <Card.Content className="grid min-h-72 place-items-center text-center">
            <div role="status">
              <Spinner color="accent" size="lg" />
              <p className="mt-4 font-medium">正在读取系统设置…</p>
            </div>
          </Card.Content>
        </Card>
      ) : !draft || !settings ? (
        <Card className="glass-panel min-h-72 border border-border">
          <Card.Content className="grid min-h-72 place-items-center px-6 text-center">
            <div className="max-w-md">
              <XIcon className="mx-auto size-7 text-danger" />
              <h2 className="mt-4 text-lg font-semibold">系统设置尚未载入</h2>
              <p className="mt-2 text-sm leading-6 text-muted">
                本次读取失败，页面没有可供编辑的设置。请重新载入后再继续。
              </p>
              <Button
                className="mt-5"
                onPress={() => void load()}
                variant="secondary"
              >
                <RefreshIcon className="size-4" />
                重新载入
              </Button>
            </div>
          </Card.Content>
        </Card>
      ) : (
        <Form
          aria-label="系统设置表单"
          className="grid gap-6"
          onSubmit={submit}
          validationBehavior="native"
        >
          <Card className="glass-panel border border-border">
            <Card.Header>
              <div>
                <Card.Title>访问策略</Card.Title>
                <Card.Description>
                  决定未登录访问者是否能够使用用户解析页。
                </Card.Description>
              </div>
            </Card.Header>
            <Card.Content>
              <Fieldset>
                <Fieldset.Legend className="sr-only">访问策略</Fieldset.Legend>
                <SettingSwitch
                  description="关闭后，匿名访问解析接口会收到 401，并转到管理端登录。"
                  isDisabled={isSaving}
                  isSelected={draft.public}
                  label="允许匿名公开解析"
                  onChange={(value) =>
                    setDraft((current) =>
                      current ? { ...current, public: value } : current,
                    )
                  }
                />
              </Fieldset>
            </Card.Content>
          </Card>

          <Card className="glass-panel border border-border">
            <Card.Header>
              <div>
                <Card.Title>默认连接</Card.Title>
                <Card.Description>
                  仅服务器持有真实值；浏览器只能查看是否已配置。
                </Card.Description>
              </div>
            </Card.Header>
            <Card.Content className="grid gap-4">
              <SecretSetting
                action={cookie.action}
                description="保存后，新解析请求在未提供本次 Cookie 时继承它。"
                fieldName="default_cookie"
                isDisabled={isSaving}
                label="默认 Cookie"
                onActionChange={(action) =>
                  setCookie((current) => ({ ...current, action }))
                }
                onValueChange={(value) =>
                  setCookie((current) => ({ ...current, value }))
                }
                summary={settings.default_cookie}
                value={cookie.value}
              />
              <SecretSetting
                action={proxy.action}
                description="使用 http 或 https URL；代理凭据不会返回浏览器。"
                fieldName="default_proxy"
                isDisabled={isSaving}
                label="默认代理"
                onActionChange={(action) =>
                  setProxy((current) => ({ ...current, action }))
                }
                onValueChange={(value) =>
                  setProxy((current) => ({ ...current, value }))
                }
                summary={settings.default_proxy}
                value={proxy.value}
              />
            </Card.Content>
          </Card>

          <Card className="glass-panel border border-border">
            <Card.Header>
              <div>
                <Card.Title>保存策略</Card.Title>
                <Card.Description>
                  每类资源独立决定是否保存到服务端卷。
                </Card.Description>
              </div>
            </Card.Header>
            <Card.Content>
              <Fieldset className="grid gap-3">
                <Fieldset.Legend className="sr-only">资源保存策略</Fieldset.Legend>
                <SettingSwitch
                  description="保存标题、描述、标签和互动数据等结构化文案。"
                  isDisabled={isSaving}
                  isSelected={draft.save.text}
                  label="保存文案"
                  onChange={(value) => updateSave("text", value)}
                />
                <SettingSwitch
                  description="保存图片及实况照片的静态图片资源。"
                  isDisabled={isSaving}
                  isSelected={draft.save.images}
                  label="保存图片"
                  onChange={(value) => updateSave("images", value)}
                />
                <SettingSwitch
                  description="保存视频及实况照片对应的视频资源。"
                  isDisabled={isSaving}
                  isSelected={draft.save.videos}
                  label="保存视频"
                  onChange={(value) => updateSave("videos", value)}
                />
              </Fieldset>
            </Card.Content>
          </Card>

          <Card className="glass-panel border border-border">
            <Card.Header>
              <div>
                <Card.Title>已有记录处理</Card.Title>
                <Card.Description>
                  控制相同作品再次提交时是否访问上游并创建新快照。
                </Card.Description>
              </div>
            </Card.Header>
            <Card.Content>
              <SettingSwitch
                description="开启后每次重新抓取；关闭后直接返回已有的最新版本。"
                isDisabled={isSaving}
                isSelected={draft.refetch}
                label="已有记录时重新抓取"
                onChange={(value) =>
                  setDraft((current) =>
                    current ? { ...current, refetch: value } : current,
                  )
                }
              />
            </Card.Content>
          </Card>

          <div className="sticky bottom-4 flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-border bg-background/90 p-4 shadow-lg backdrop-blur-xl">
            <p className="flex items-center gap-2 text-xs text-muted">
              <ShieldIcon className="size-4" />
              当前修订号：{draft.revision}
            </p>
            <div className="flex gap-3">
              <Button
                isDisabled={isSaving}
                onPress={() => {
                  setDraft(settings);
                  setCookie({ action: "keep", value: "" });
                  setProxy({ action: "keep", value: "" });
                }}
                type="button"
                variant="secondary"
              >
                放弃修改
              </Button>
              <Button
                isDisabled={isSaving}
                type="submit"
                variant="primary"
              >
                {isSaving && <Spinner color="current" size="sm" />}
                {isSaving ? "保存中…" : "保存设置"}
              </Button>
            </div>
          </div>
        </Form>
      )}
    </div>
  );
}
