import assert from "node:assert/strict";
import test from "node:test";

import { safeNextPath } from "../src/app/safeNextPath.ts";

const fallback = "/admin/settings";

test("accepts only the public root and admin routes", () => {
  assert.equal(safeNextPath("/", fallback), "/");
  assert.equal(safeNextPath("/?source=login#ignored", fallback), "/?source=login");
  assert.equal(safeNextPath("/admin", fallback), "/admin");
  assert.equal(
    safeNextPath("/admin/history?cursor=25#resources", fallback),
    "/admin/history?cursor=25",
  );
  assert.equal(
    safeNextPath("/admin/works/42?tab=resources#ignored", fallback),
    "/admin/works/42?tab=resources",
  );
});

test("rejects cross-origin, ambiguous, and unrelated paths", () => {
  for (const value of [
    null,
    "",
    "admin/settings",
    "//evil.example",
    "/\\evil.example",
    "https://evil.example/admin",
    "/administrator",
    "/public",
    "/admin\n/settings",
    "/admin\u0085/settings",
  ]) {
    assert.equal(safeNextPath(value, fallback), fallback, String(value));
  }
});

test("normalizes paths before applying the route allowlist", () => {
  assert.equal(safeNextPath("/admin/../", fallback), "/");
  assert.equal(safeNextPath("/admin/../../outside", fallback), fallback);
});
