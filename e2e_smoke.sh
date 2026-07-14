#!/bin/bash
set -e

echo "Running E2E Smoke Tests..."
cd /home/ubuntu/inec/e2e

# Install dependencies if needed
if [ ! -d "node_modules" ]; then
  npm install
  npx playwright install chromium
fi

# Run playwright tests
npx playwright test
