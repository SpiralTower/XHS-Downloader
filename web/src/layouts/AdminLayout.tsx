import { Alert, Button } from "@heroui/react";
import { useState } from "react";
import { NavLink, Outlet, useLoaderData, useNavigate } from "react-router";

import { logoutAdmin } from "../api";
import AppHeader from "../components/AppHeader";
import { XIcon } from "../icons";
import type { AdminSession } from "../types";

function AdminNavigation() {
  const className = ({ isActive }: { isActive: boolean }) =>
    [
      "rounded-xl px-3 py-2 text-sm font-medium transition-colors",
      "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent",
      isActive
        ? "bg-accent-soft text-accent-soft-foreground"
        : "text-muted hover:bg-surface-secondary hover:text-foreground",
    ].join(" ");

  return (
    <nav aria-label="管理端导航" className="flex items-center gap-1">
      <NavLink className={className} to="/admin/settings">
        系统设置
      </NavLink>
      <NavLink className={className} to="/admin/history">
        解析历史
      </NavLink>
    </nav>
  );
}

export default function AdminLayout() {
  const session = useLoaderData() as AdminSession;
  const navigate = useNavigate();
  const [isLoggingOut, setIsLoggingOut] = useState(false);
  const [logoutError, setLogoutError] = useState("");

  const logout = async () => {
    setIsLoggingOut(true);
    setLogoutError("");
    try {
      await logoutAdmin();
      navigate("/admin/login", { replace: true });
    } catch (cause) {
      setLogoutError(
        cause instanceof Error
          ? "退出请求失败，无法确认会话已清除：" + cause.message
          : "退出请求失败，无法确认会话已清除，请稍后重试。",
      );
    } finally {
      setIsLoggingOut(false);
    }
  };

  return (
    <div className="app-shell relative min-h-screen overflow-hidden bg-background text-foreground">
      <a
        className="fixed left-4 top-4 z-50 -translate-y-24 rounded-lg bg-accent px-4 py-2 text-accent-foreground transition-transform focus:translate-y-0"
        href="#main-content"
      >
        跳到主要内容
      </a>
      <div className="surface-grid pointer-events-none absolute inset-0" />
      <div className="relative">
        <AppHeader
          isLoggingOut={isLoggingOut}
          navigation={<AdminNavigation />}
          onLogout={() => void logout()}
          username={session.username ?? "管理员"}
        />
        <main
          className="mx-auto w-full max-w-7xl px-4 py-8 sm:px-6 lg:px-8 lg:py-10"
          id="main-content"
        >
          {logoutError && (
            <Alert className="mb-6" status="danger">
              <Alert.Indicator>
                <XIcon className="size-5" />
              </Alert.Indicator>
              <Alert.Content>
                <Alert.Title>未能确认退出</Alert.Title>
                <Alert.Description>{logoutError}</Alert.Description>
              </Alert.Content>
              <Button
                onPress={() => setLogoutError("")}
                size="sm"
                type="button"
                variant="secondary"
              >
                关闭
              </Button>
            </Alert>
          )}
          <Outlet />
        </main>
      </div>
    </div>
  );
}
