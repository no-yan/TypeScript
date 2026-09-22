import { fileURLToPath } from "node:url";
import generateEncoder from "./generate-encoder.ts";
import generateGoAST from "./generate-go-ast.ts";
import generateGoStore from "./generate-go-store.ts";
import generateTSAST from "./generate-ts-ast.ts";

export default function generate() {
    generateEncoder();
    generateGoAST();
    generateGoStore();
    generateTSAST();
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
    generate();
}
