import { useEffect } from "react";
import { Outlet, ScrollRestoration, useLocation } from "react-router";

export default function RootLayout() {
  const location = useLocation();

  useEffect(() => {
    window.requestAnimationFrame(() => {
      document.querySelector<HTMLElement>("#main-heading")?.focus();
    });
  }, [location.pathname]);

  return (
    <>
      <Outlet />
      <ScrollRestoration />
    </>
  );
}
