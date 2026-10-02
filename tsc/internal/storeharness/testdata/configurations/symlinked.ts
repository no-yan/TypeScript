// @module: esnext
// @moduleResolution: bundler
// @filename: /packages/real/index.ts
export const real: number = 1;
// @filename: /packages/other/index.ts
// @symlink: /app/node_modules/other/index.ts
export const other: number = 2;
// @filename: /app/main.ts
import { real } from "linked";
import { other } from "other";
export const m: string = real + other;
// @link: /packages/real -> /app/node_modules/linked
