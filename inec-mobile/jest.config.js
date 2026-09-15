// Minimal REAL unit-test layer (R5-116): pure logic modules under src/lib
// are tested against fixtures in a node environment — no RN runtime and no
// mock-the-world. Modules that import Expo/RN stay out of scope here by
// design (they are covered by `npm run typecheck`).
module.exports = {
  preset: 'ts-jest',
  testEnvironment: 'node',
  roots: ['<rootDir>/src'],
  testMatch: ['**/__tests__/**/*.test.ts'],
  transform: {
    '^.+\\.tsx?$': [
      'ts-jest',
      {
        tsconfig: {
          // Self-contained compiler options for the pure-logic tests.
          target: 'ES2020',
          module: 'commonjs',
          moduleResolution: 'node',
          esModuleInterop: true,
          strict: true,
          skipLibCheck: true,
          types: ['jest', 'node'],
        },
        diagnostics: true,
      },
    ],
  },
};
