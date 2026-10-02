// @jsx: react-jsx
// @module: esnext
// @moduleResolution: bundler
// @filename: /node_modules/preact/jsx-runtime.d.ts
export namespace JSX {
    interface IntrinsicElements { span: { id?: string } }
}
export function jsx(type: any, props: any): any;
// @filename: /index.tsx
/** @jsxImportSource preact */
export const el = <span id={1} />;
