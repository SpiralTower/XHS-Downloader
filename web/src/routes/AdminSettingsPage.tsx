import {
  Alert,
  Button,
  Card,
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
import { CheckIcon, RefreshIcon, XIcon } from "../icons";
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
        show_popular: draft.show_popular,
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
      setSavedMessage(`已保存 · 修订 ${result.revision}`);
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 401) {
        navigate("/admin/login?next=/admin/settings", { replace: true });
        return;
      }
      setError(
        cause instanceof ApiError && cause.status === 409
          ? "设置已被其他会话修改，请重新载入"
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
    <div className="grid gap-5">
      <header className="flex items-end justify-between gap-4">
        <h1
          className="text-2xl font-semibold tracking-[-0.03em] outline-none sm:text-3xl"
          id="main-heading"
          tabIndex={-1}
        >
          系统设置
        </h1>
        {draft && (
          <p className="text-xs text-muted">修订 {draft.revision}</p>
        )}
      </header>

      {error && (
        <Alert status="danger">
          <Alert.Indicator>
            <XIcon className="size-5" />
          </Alert.Indicator>
          <Alert.Content>
            <Alert.Description>{error}</Alert.Description>
          </Alert.Content>
          <Button onPress={() => void load()} size="sm" variant="secondary">
            <RefreshIcon className="size-4" />
            重载
          </Button>
        </Alert>
      )}

      {savedMessage && (
        <Alert status="success">
          <Alert.Indicator>
            <CheckIcon className="size-5" />
          </Alert.Indicator>
          <Alert.Content>
            <Alert.Description>{savedMessage}</Alert.Description>
          </Alert.Content>
        </Alert>
      )}

      {isLoading ? (
        <Card className="glass-panel min-h-64 border border-border">
          <Card.Content className="grid min-h-64 place-items-center">
            <Spinner color="accent" size="lg" />
          </Card.Content>
        </Card>
      ) : !draft || !settings ? (
        <Card className="glass-panel min-h-64 border border-border">
          <Card.Content className="grid min-h-64 place-items-center px-6 text-center">
            <div className="max-w-sm">
              <p className="font-medium">设置未载入</p>
              <Button
                className="mt-4"
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
          className="grid gap-4"
          onSubmit={submit}
          validationBehavior="native"
        >
          <Card className="glass-panel border border-border">
            <Card.Header>
              <Card.Title>访问</Card.Title>
            </Card.Header>
            <Card.Content>
              <Fieldset className="grid gap-2">
                <Fieldset.Legend className="sr-only">访问策略</Fieldset.Legend>
                <SettingSwitch
                  description="默认关闭；开启后匿名请求受限流，并按当前保存策略处理"
                  isDisabled={isSaving}
                  isSelected={draft.public}
                  label="允许公开解析"
                  onChange={(value) =>
                    setDraft((current) =>
                      current ? { ...current, public: value } : current,
                    )
                  }
                />
                <SettingSwitch
                  description="开启后在首页展示累计与近期热门作品"
                  isDisabled={isSaving}
                  isSelected={draft.show_popular}
                  label="首页显示热门解析榜单"
                  onChange={(value) =>
                    setDraft((current) =>
                      current ? { ...current, show_popular: value } : current,
                    )
                  }
                />
              </Fieldset>
            </Card.Content>
          </Card>

          <Card className="glass-panel border border-border">
            <Card.Header>
              <Card.Title>默认连接</Card.Title>
            </Card.Header>
            <Card.Content className="grid gap-3">
              <SecretSetting
                action={cookie.action}
                description="新解析在未提供 Cookie 时继承"
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
                description="http / https 代理，凭据不回传"
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
              <Card.Title>保存</Card.Title>
            </Card.Header>
            <Card.Content>
              <Fieldset className="grid gap-2">
                <Fieldset.Legend className="sr-only">资源保存</Fieldset.Legend>
                <SettingSwitch
                  description="标题、描述、标签与互动数据"
                  isDisabled={isSaving}
                  isSelected={draft.save.text}
                  label="文案"
                  onChange={(value) => updateSave("text", value)}
                />
                <SettingSwitch
                  description="图片与实况静态帧"
                  isDisabled={isSaving}
                  isSelected={draft.save.images}
                  label="图片"
                  onChange={(value) => updateSave("images", value)}
                />
                <SettingSwitch
                  description="视频与实况视频"
                  isDisabled={isSaving}
                  isSelected={draft.save.videos}
                  label="视频"
                  onChange={(value) => updateSave("videos", value)}
                />
              </Fieldset>
            </Card.Content>
          </Card>

          <Card className="glass-panel border border-border">
            <Card.Header>
              <Card.Title>抓取</Card.Title>
            </Card.Header>
            <Card.Content>
              <SettingSwitch
                description="关闭后直接返回已有最新版本"
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

          <div className="sticky bottom-4 flex flex-wrap items-center justify-end gap-2 rounded-2xl border border-border bg-background/90 p-3 shadow-lg backdrop-blur-xl">
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
              放弃
            </Button>
            <Button isDisabled={isSaving} type="submit" variant="primary">
              {isSaving && <Spinner color="current" size="sm" />}
              {isSaving ? "保存中…" : "保存"}
            </Button>
          </div>
        </Form>
      )}
    </div>
  );
}
