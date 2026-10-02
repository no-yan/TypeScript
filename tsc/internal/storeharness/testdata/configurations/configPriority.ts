// @strict: true
// @checkJs: true
// @filename: /proj/tsconfig.json
{
    "compilerOptions": { "strict": false, "newLine": "lf", "allowJs": true, "module": "esnext", "moduleResolution": "bundler", "target": "es2017" },
    "files": ["a.ts", "b.js"]
}
// @filename: /proj/a.ts
export function f(x) { return x; }
// @filename: /proj/b.js
/** @type {number} */
export const n = "s";
// @filename: /proj/other.ts
export const unused = 1;
