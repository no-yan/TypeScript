// @noImplicitReferences: true
// @filename: dep.ts
export const fromDep: number = "not a number";
// @filename: main.ts
import { fromDep } from "./dep";
export const value = fromDep;
