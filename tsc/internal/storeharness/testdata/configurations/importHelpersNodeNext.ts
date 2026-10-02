// @importHelpers: true
// @target: es2017
// @module: nodenext
// @filename: /node_modules/tslib/package.json
{ "name": "tslib", "version": "1.0.0", "types": "tslib.d.ts" }
// @filename: /node_modules/tslib/tslib.d.ts
export declare function __rest(s: any, e: string[]): any;
// @filename: /a.mts
const a = { x: 1, y: 2 };
export const { x, ...rest } = a;
// @filename: /b.cts
const b = { x: 1, y: 2 };
export const { x, ...rest } = b;
