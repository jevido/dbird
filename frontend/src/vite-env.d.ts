/// <reference types="svelte" />
/// <reference types="vite/client" />

// Monarch definitions shipped by monaco-editor without type declarations.
declare module 'monaco-editor/languages/definitions/*.js' {
  export const language: { keywords: string[]; operators: string[]; builtinFunctions: string[]; [key: string]: unknown };
  export const conf: unknown;
}
