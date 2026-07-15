import type { LoaderFunctionArgs } from "react-router";
import { createBrowserRouter, redirect } from "react-router";

import {
  ApiError,
  getAccess,
  getAdminSession,
  getPopularWorks,
  getWorkHistory,
} from "../api";
import AdminLayout from "../layouts/AdminLayout";
import PublicLayout from "../layouts/PublicLayout";
import RootLayout from "../layouts/RootLayout";
import RouteErrorPage from "../routes/RouteErrorPage";
import type { PublicHomeData } from "../types";
import { safeNextPath } from "./safeNextPath";

async function publicAccessLoader({ request }: LoaderFunctionArgs) {
  const access = await getAccess(request.signal);
  if (!access.can_extract) {
    const url = new URL(request.url);
    throw redirect(
      `/admin/login?next=${encodeURIComponent(url.pathname + url.search)}`,
    );
  }

  let popular: PublicHomeData["popular"] = null;
  try {
    popular = await getPopularWorks(request.signal);
  } catch (error) {
    if (error instanceof DOMException && error.name === "AbortError") {
      throw error;
    }
    popular = null;
  }

  return { access, popular } satisfies PublicHomeData;
}

async function loginLoader({ request }: LoaderFunctionArgs) {
  const session = await getAdminSession(request.signal);
  if (session.authenticated) {
    const url = new URL(request.url);
    throw redirect(safeNextPath(url.searchParams.get("next"), "/admin/settings"));
  }
  return null;
}

async function requireAdminLoader({ request }: LoaderFunctionArgs) {
  const session = await getAdminSession(request.signal);
  if (!session.authenticated) {
    const url = new URL(request.url);
    throw redirect(
      `/admin/login?next=${encodeURIComponent(url.pathname + url.search)}`,
    );
  }
  return session;
}

async function workHistoryLoader({ params, request }: LoaderFunctionArgs) {
  if (!params.workId) {
    throw new Response("缺少作品 ID", { status: 400 });
  }

  try {
    return await getWorkHistory(params.workId, request.signal);
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      const url = new URL(request.url);
      throw redirect(
        `/admin/login?next=${encodeURIComponent(url.pathname + url.search)}`,
      );
    }
    throw error;
  }
}

function legacyWorkHistoryLoader({ params }: LoaderFunctionArgs) {
  if (!params.workId) {
    throw new Response("缺少作品 ID", { status: 400 });
  }

  throw redirect("/admin/works/" + encodeURIComponent(params.workId));
}

export const router = createBrowserRouter([
  {
    path: "/",
    Component: RootLayout,
    errorElement: <RouteErrorPage />,
    children: [
      {
        Component: PublicLayout,
        children: [
          {
            index: true,
            loader: publicAccessLoader,
            lazy: () =>
              import("../routes/PublicExtractPage").then((module) => ({
                Component: module.default,
              })),
          },
        ],
      },
      {
        path: "admin/login",
        loader: loginLoader,
        lazy: () =>
          import("../routes/AdminLoginPage").then((module) => ({
            Component: module.default,
          })),
      },
      {
        path: "admin",
        loader: requireAdminLoader,
        Component: AdminLayout,
        children: [
          {
            index: true,
            loader: () => redirect("/admin/settings"),
          },
          {
            path: "settings",
            lazy: () =>
              import("../routes/AdminSettingsPage").then((module) => ({
                Component: module.default,
              })),
          },
          {
            path: "works",
            lazy: () =>
              import("../routes/AdminWorksPage").then((module) => ({
                Component: module.default,
              })),
          },
          {
            path: "works/:workId",
            loader: workHistoryLoader,
            lazy: () =>
              import("../routes/WorkHistoryPage").then((module) => ({
                Component: module.default,
              })),
          },
          {
            path: "history",
            lazy: () =>
              import("../routes/AdminHistoryPage").then((module) => ({
                Component: module.default,
              })),
          },
          {
            path: "history/:workId",
            loader: legacyWorkHistoryLoader,
          },
        ],
      },
    ],
  },
]);
