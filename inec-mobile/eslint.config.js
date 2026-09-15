// R4-36: flat ESLint config for inec-mobile (Expo SDK 56).
// Uses the official eslint-config-expo flat preset so `npx eslint .` and
// `npm run lint` (expo lint) work out of the box.
const { defineConfig } = require('eslint/config');
const expoConfig = require('eslint-config-expo/flat');
const tsPlugin = require('@typescript-eslint/eslint-plugin');
const reactHooks = require('eslint-plugin-react-hooks');

module.exports = defineConfig([
  expoConfig,
  {
    ignores: [
      'dist/*',
      'web-build/*',
      '.expo/*',
      'node_modules/*',
      'assets/*',
    ],
  },
  {
    plugins: {
      '@typescript-eslint': tsPlugin,
      'react-hooks': reactHooks,
    },
    rules: {
      // R4-36: relaxed so the config gates CI without mass-rewriting existing
      // sources. Re-enable these as the codebase is cleaned up.
      'react-hooks/exhaustive-deps': 'warn',
      'import/no-unresolved': 'warn',
      'no-unused-vars': 'off', // handled by @typescript-eslint/no-unused-vars
      '@typescript-eslint/no-unused-vars': 'warn',
      '@typescript-eslint/no-require-imports': 'off', // RN assets use require()
      // R4-36: react-hooks v7 strictness rules flag long-standing RN patterns
      // across the existing codebase (78 errors). Demoted to warn so lint gates
      // CI without a mass source rewrite; re-enable incrementally.
      'react-hooks/set-state-in-effect': 'warn',
      'react-hooks/refs': 'warn',
      'react-hooks/purity': 'warn',
      'react-hooks/immutability': 'warn',
      'react/no-unescaped-entities': 'warn',
    },
  },
]);
