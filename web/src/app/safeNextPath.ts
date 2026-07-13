const nextPathBaseURL = new URL("https://xhs-downloader.invalid");
const controlCharacterPattern = /[\u0000-\u001f\u007f-\u009f]/;

export function safeNextPath(value: string | null, fallback: string): string {
  if (
    !value ||
    !value.startsWith("/") ||
    value.includes("\\") ||
    controlCharacterPattern.test(value)
  ) {
    return fallback;
  }

  try {
    const target = new URL(value, nextPathBaseURL);
    const allowedPath =
      target.pathname === "/" ||
      target.pathname === "/admin" ||
      target.pathname.startsWith("/admin/");

    if (target.origin !== nextPathBaseURL.origin || !allowedPath) {
      return fallback;
    }
    return target.pathname + target.search;
  } catch {
    return fallback;
  }
}
