// @allowJs: true
// @noCheck: true
// @skipLibCheck: true
// @filename: decl.d.ts
export declare const d: number;
// @filename: a.js
export const a = 1;
// @filename: main.ts
import { d } from "./decl";
import { a } from "./a";
export const m: string = d + a;
