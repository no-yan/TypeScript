// @useCaseSensitiveFileNames: false
// @module: esnext
// @moduleResolution: bundler
// @filename: /src/Lib.ts
export const lib = 1;
// @filename: /src/main.ts
import { lib } from "./LIB";
export const m: string = lib;
