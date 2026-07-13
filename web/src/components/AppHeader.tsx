import { Button, Chip } from "@heroui/react";
import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import { Link } from "react-router";

import { checkHealth, emptyHealthSnapshot } from "../api";
import {
  ActivityIcon,
  MoonIcon,
  ShieldIcon,
  SparklesIcon,
  SunIcon,
  UserIcon,
} from "../icons";
import {
  applyTheme,
  readThemePreference,
  storeThemePreference,
} from "../theme";
import type { ThemePreference } from "../theme";
import type { HealthSnapshot } from "../types";

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

  return (
    <div
      aria-label="外观主题"
      className="flex flex-wrap gap-1 rounded-xl border border-border bg-surface p-1"
      role="group"
    >
      <Button
        aria-pressed={theme === "light"}
        onPress={() => setTheme("light")}
        size="sm"
        type="button"
        variant={theme === "light" ? "primary" : "secondary"}
      >
        <SunIcon className="size-4" />
        <span className="sr-only sm:not-sr-only">浅色</span>
      </Button>
      <Button
        aria-pressed={theme === "dark"}
        onPress={() => setTheme("dark")}
        size="sm"
        type="button"
        variant={theme === "dark" ? "primary" : "secondary"}
      >
        <MoonIcon className="size-4" />
        <span className="sr-only sm:not-sr-only">深色</span>
      </Button>
      <Button
        aria-pressed={theme === "system"}
        onPress={() => setTheme("system")}
        size="sm"
        type="button"
        variant={theme === "system" ? "primary" : "secondary"}
      >
        <SparklesIcon className="size-4" />
        <span className="sr-only sm:not-sr-only">系统</span>
      </Button>
    </div>
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
      ? "检查 API"
      : health.state === "online"
        ? `API 在线 · ${health.latency}ms`
        : "API 离线";
  const color =
    health.state === "online"
      ? "success"
      : health.state === "offline"
        ? "danger"
        : "default";

  return (
    <Chip
      color={color}
      size="sm"
      title={
        health.checkedAt
          ? `上次检查：${health.checkedAt.toLocaleTimeString("zh-CN")}`
          : "正在执行首次健康检查"
      }
      variant="soft"
    >
      <Chip.Label className="flex items-center gap-1.5">
        <ActivityIcon className="size-3.5" />
        {label}
      </Chip.Label>
    </Chip>
  );
}

export default function AppHeader({
  navigation,
  username,
  isLoggingOut = false,
  onLogout,
}: {
  navigation?: ReactNode;
  username?: string;
  isLoggingOut?: boolean;
  onLogout?: () => void;
}) {
  return (
    <header className="border-b border-border bg-background/90 backdrop-blur-xl">
      <div className="mx-auto flex w-full max-w-7xl flex-col gap-4 px-4 py-4 sm:px-6 lg:flex-row lg:items-center lg:justify-between lg:px-8">
        <div className="flex min-w-0 items-center justify-between gap-4">
          <Link
            aria-label="返回解析首页"
            className="flex min-w-0 items-center gap-3 rounded-xl focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-accent"
            to="/"
          >
            <span className="grid size-10 shrink-0 place-items-center rounded-xl bg-accent text-accent-foreground">
              <SparklesIcon className="size-5" />
            </span>
            <span className="min-w-0">
              <span className="block truncate font-semibold tracking-tight">
                XHS Downloader
              </span>
              <span className="block truncate text-xs text-muted">
                Go API · HeroUI v3
              </span>
            </span>
          </Link>
          <div className="lg:hidden">
            <HealthChip />
          </div>
        </div>

        {navigation}

        <div className="flex flex-wrap items-center gap-2">
          <div className="hidden lg:block">
            <HealthChip />
          </div>
          {username && (
            <Chip size="sm" variant="soft">
              <Chip.Label className="flex items-center gap-1.5">
                <UserIcon className="size-3.5" />
                {username}
              </Chip.Label>
            </Chip>
          )}
          <ThemeControl />
          {onLogout && (
            <Button
              isDisabled={isLoggingOut}
              onPress={onLogout}
              size="sm"
              type="button"
              variant="secondary"
            >
              <ShieldIcon className="size-4" />
              {isLoggingOut ? "退出中…" : "退出"}
            </Button>
          )}
        </div>
      </div>
    </header>
  );
}
