import { Button, Chip } from "@heroui/react";
import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import { Link } from "react-router";

import { checkHealth, emptyHealthSnapshot } from "../api";
import {
  ActivityIcon,
  MonitorIcon,
  MoonIcon,
  SparklesIcon,
  SunIcon,
} from "../icons";
import {
  applyTheme,
  readThemePreference,
  storeThemePreference,
} from "../theme";
import type { ThemePreference } from "../theme";
import type { HealthSnapshot } from "../types";

const themeCycle: ThemePreference[] = ["light", "dark", "system"];

const themeMeta: Record<
  ThemePreference,
  { label: string; Icon: typeof SunIcon }
> = {
  light: { label: "浅色", Icon: SunIcon },
  dark: { label: "深色", Icon: MoonIcon },
  system: { label: "系统", Icon: MonitorIcon },
};

function ThemeControl() {
  const [theme, setTheme] = useState<ThemePreference>(readThemePreference);

  useEffect(() => {
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const syncTheme = () => applyTheme(theme);

    storeThemePreference(theme);
    syncTheme();
    if (theme !== "system") return;

    media.addEventListener("change", syncTheme);
    return () => media.removeEventListener("change", syncTheme);
  }, [theme]);

  const { label, Icon } = themeMeta[theme];
  const nextTheme =
    themeCycle[(themeCycle.indexOf(theme) + 1) % themeCycle.length];

  return (
    <Button
      aria-label={`外观主题：${label}，点击切换为${themeMeta[nextTheme].label}`}
      isIconOnly
      onPress={() => setTheme(nextTheme)}
      size="sm"
      type="button"
      variant="secondary"
    >
      <Icon className="size-4" />
    </Button>
  );
}

function HealthChip() {
  const [health, setHealth] = useState<HealthSnapshot>(emptyHealthSnapshot);

  useEffect(() => {
    let controller: AbortController | null = null;
    const run = async () => {
      controller?.abort();
      controller = new AbortController();
      setHealth((current) => ({ ...current, state: "checking" }));
      try {
        const latency = await checkHealth(controller.signal);
        setHealth({ state: "online", latency, checkedAt: new Date() });
      } catch (error) {
        if (error instanceof DOMException && error.name === "AbortError") return;
        setHealth({ state: "offline", latency: null, checkedAt: new Date() });
      }
    };

    void run();
    const timer = window.setInterval(() => void run(), 30_000);
    return () => {
      window.clearInterval(timer);
      controller?.abort();
    };
  }, []);

  const label =
    health.state === "checking"
      ? "检查中"
      : health.state === "online"
        ? `在线 ${health.latency}ms`
        : "离线";
  const color =
    health.state === "online"
      ? "success"
      : health.state === "offline"
        ? "danger"
        : "default";
  const detail = health.checkedAt
    ? `上次检查：${health.checkedAt.toLocaleTimeString("zh-CN")}`
    : "正在执行首次健康检查";

  return (
    <Chip
      aria-label={`服务状态：${label}`}
      className="size-9 justify-center px-0 md:size-8"
      color={color}
      size="sm"
      title={`${label} · ${detail}`}
      variant="soft"
    >
      <Chip.Label className="flex items-center justify-center">
        <ActivityIcon className="size-4" />
      </Chip.Label>
    </Chip>
  );
}

export default function AppHeader({
  navigation,
  isLoggingOut = false,
  onLogout,
}: {
  navigation?: ReactNode;
  isLoggingOut?: boolean;
  onLogout?: () => void;
}) {
  return (
    <header className="border-b border-border/70 bg-background/80 backdrop-blur-xl">
      <div className="mx-auto flex w-full max-w-6xl items-center justify-between gap-4 px-4 py-3 sm:px-6 lg:px-8">
        <div className="flex min-w-0 items-center gap-4">
          <Link
            aria-label="返回首页"
            className="flex min-w-0 items-center gap-2.5 rounded-xl focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-accent"
            to="/"
          >
            <span className="grid size-9 shrink-0 place-items-center rounded-xl bg-accent text-accent-foreground">
              <SparklesIcon className="size-4" />
            </span>
            <span className="truncate text-sm font-semibold tracking-tight sm:text-base">
              XHS Downloader
            </span>
          </Link>
          {navigation}
        </div>

        <div className="flex h-9 shrink-0 items-center gap-2 md:h-8">
          <div className="hidden sm:block">
            <HealthChip />
          </div>
          <ThemeControl />
          {onLogout && (
            <Button
              className="h-9 px-3 md:h-8"
              isDisabled={isLoggingOut}
              onPress={onLogout}
              size="sm"
              type="button"
              variant="secondary"
            >
              {isLoggingOut ? "退出中" : "退出"}
            </Button>
          )}
        </div>
      </div>
    </header>
  );
}
