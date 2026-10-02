// @resolveJsonModule: true
// @module: esnext
// @moduleResolution: bundler
// @filename: /src/data.json
{ "value": 1 }
// @filename: /src/main.ts
import data from "./data.json";
export const v: string = data.value;
