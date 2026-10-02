// @module: nodenext
// @filename: /src/package.json
{ "type": "module" }
// @filename: /src/other.cts
export const c = 1;
// @filename: /src/types.cts
export type T = number;
// @filename: /src/index.ts
import { c } from "./other.cjs";
import type { T } from "./types.cjs";
export const v: T = c;
export const later = import("./other.cjs");
