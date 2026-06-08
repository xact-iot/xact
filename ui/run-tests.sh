#!/bin/bash

# XACT UI Test Runner
# This script runs all tests for the UI store

set -e

echo "==================================="
echo "XACT UI Store Test Runner"
echo "==================================="
echo ""

# Color codes
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Change to UI directory
cd "$(dirname "$0")"

# Run unit tests (doesn't require backend)
echo -e "${YELLOW}Running unit tests...${NC}"
npm test run ui-mirror-store.unit.test.ts
UNIT_RESULT=$?

if [ $UNIT_RESULT -eq 0 ]; then
    echo -e "${GREEN}✓ Unit tests passed${NC}"
else
    echo -e "${RED}✗ Unit tests failed${NC}"
fi

echo ""

# Check if backend is running
echo -e "${YELLOW}Checking if backend is running...${NC}"
if curl -s http://localhost:8080/health > /dev/null 2>&1; then
    echo -e "${GREEN}✓ Backend is running${NC}"
    BACKEND_RUNNING=true
else
    echo -e "${YELLOW}⚠ Backend is not running${NC}"
    BACKEND_RUNNING=false
fi

echo ""

# Run integration tests if backend is running
if [ "$BACKEND_RUNNING" = true ]; then
    echo -e "${YELLOW}Running integration tests...${NC}"
    echo -e "${YELLOW}Note: Integration tests may be skipped due to WebSocket limitations in test environment${NC}"
    npm test run ui-mirror-store.integration.test.ts 2>&1 || true
else
    echo -e "${YELLOW}Skipping integration tests (backend not running)${NC}"
fi

echo ""

# Build check
echo -e "${YELLOW}Checking build...${NC}"
npm run build > /dev/null 2>&1
BUILD_RESULT=$?

if [ $BUILD_RESULT -eq 0 ]; then
    echo -e "${GREEN}✓ Build successful${NC}"
else
    echo -e "${RED}✗ Build failed${NC}"
fi

echo ""
echo "==================================="
echo "Test Summary"
echo "==================================="
echo -e "Unit Tests: $([ $UNIT_RESULT -eq 0 ] && echo "${GREEN}PASSED${NC}" || echo "${RED}FAILED${NC}")"
echo -e "Build: $([ $BUILD_RESULT -eq 0 ] && echo "${GREEN}PASSED${NC}" || echo "${RED}FAILED${NC}")"

if [ "$BACKEND_RUNNING" = true ]; then
    echo ""
    echo "For full WebSocket integration testing, open:"
    echo "  http://localhost:3000/test.html"
    echo ""
    echo "Or run:"
    echo "  npm run dev"
fi

echo ""

# Exit with appropriate code
if [ $UNIT_RESULT -eq 0 ] && [ $BUILD_RESULT -eq 0 ]; then
    exit 0
else
    exit 1
fi
