import { Alert, Button } from "@heroui/react";
import { useState } from "react";
import { NavLink, Outlet, useNavigate } from "react-router";

import { logoutAdmin } from "../api";
import { adminNavigationItems } from "../app/adminNavigation";
import AppHeader from "../components/AppHeader";
import { XIcon } from "../icons";

const adminNavLinkClassName = ({ isActive }: { isActive: boolean }) =>
  [
    "rounded-lg px-3 py-1.5 text-sm font-medium transition-colors",
    "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent",
    isActive
      ? "bg-accent-soft text-accent-soft-foreground"
      : "text-muted hover:bg-surface-secondary hover:text-foreground",
  ].join(" ");

function AdminNavigation({ className }: { className: string }) {
  return (
    <nav aria-label="管理端导航" className={className}>
      {adminNavigationItems.map((item) => (
        <NavLink className={adminNavLinkClassName} key={item.to} to={item.to}>
          {item.label}
        </NavLink>
      ))}
    </nav>
  );
}

export default function AdminLayout() {
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
          ? "退出失败：" + cause.message
          : "退出失败，请稍后重试",
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
      <div className="relative flex min-h-screen flex-col">
        <AppHeader
          isLoggingOut={isLoggingOut}
          navigation={
            <AdminNavigation className="hidden items-center gap-1 sm:flex" />
          }
          onLogout={() => void logout()}
        />
        <main
          className="mx-auto w-full max-w-6xl flex-1 px-4 py-6 sm:px-6 sm:py-8 lg:px-8"
          id="main-content"
        >
          <AdminNavigation className="mb-5 flex items-center gap-1 sm:hidden" />
          {logoutError && (
            <Alert className="mb-5" status="danger">
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
