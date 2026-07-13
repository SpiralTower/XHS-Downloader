import { Outlet } from "react-router";

import AppHeader from "../components/AppHeader";

export default function PublicLayout() {
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
        <AppHeader />
        <main
          className="mx-auto w-full max-w-7xl px-4 py-8 sm:px-6 lg:px-8 lg:py-12"
          id="main-content"
        >
          <Outlet />
        </main>
      </div>
    </div>
  );
}
