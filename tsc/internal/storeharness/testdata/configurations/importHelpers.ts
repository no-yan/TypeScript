// @importHelpers: true
// @target: es2017
// @module: esnext
// @moduleResolution: bundler
// @filename: /node_modules/tslib/package.json
{ "name": "tslib", "version": "1.0.0", "types": "tslib.d.ts" }
// @filename: /node_modules/tslib/tslib.d.ts
export declare function __rest(s: any, e: string[]): any;
// @filename: /index.ts
const a = { x: 1, y: 2 };
export const { x, ...rest } = a;
