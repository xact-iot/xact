# Browser Test Framework

A browser-based testing framework with hot reloading for testing the XACT UI code.

## Running Tests

Start the dev server:
```bash
npm run dev
```

Then navigate to:
- Main app: http://localhost:3000
- **Tests: http://localhost:3000/test/**

The test page will automatically run all tests on load.

## Hot Reloading

The test framework supports hot reloading:
- Edit any test file (`.test.ts`) in the `test/` directory
- Tests will automatically re-run
- Edit source code in `src/` 
- Tests will automatically re-run

## Adding New Tests

1. Create a new test file in the `test/` directory:
   ```typescript
   // test/my-feature.test.ts
   import { describe, it, beforeAll, afterAll, expect } from './framework';
   
   describe('My Feature', () => {
     it('should do something', () => {
       expect(true).toBe(true);
     });
   });
   ```

2. Add the test file to `test/loader.ts`:
   ```typescript
   const testModules = [
     () => import('./store.test'),
     () => import('./my-feature.test'), // Add here
   ];
   ```

3. Tests will automatically load and run

## Testing Guidelines

**DO:**
- Test the public API of classes
- Pass configuration (URLs, bucket names) to the store
- Test through the store's interface only

**DON'T:**
- Import NATS directly in tests
- Access private methods or internal state
- Create separate NATS connections

## Available APIs

### describe(name, fn)
Define a test suite.

### it(name, fn)
Define a test case.

### beforeAll(fn)
Run setup code once before all tests in a suite.

### afterAll(fn)
Run cleanup code once after all tests in a suite.

### expect(value)
Create an assertion. Supports:
- `toBe(expected)` - Strict equality
- `toEqual(expected)` - Deep equality
- `toBeDefined()` - Check not undefined
- `toBeUndefined()` - Check is undefined
- `toBeNull()` - Check is null
- `toBeTruthy()` - Check truthy
- `toBeFalsy()` - Check falsy
- `toBeGreaterThan(n)` - Numeric comparison
- `toBeGreaterThanOrEqual(n)` - Numeric comparison
- `toBeLessThan(n)` - Numeric comparison
- `toBeLessThanOrEqual(n)` - Numeric comparison
- `toContain(item)` - Array/string contains
- `toHaveLength(n)` - Array length
- `toThrow()` - Function throws
- `toThrowError(msg)` - Function throws with message
- `.not` - Negate any assertion

## Example Test

```typescript
import { describe, it, beforeAll, afterAll, expect } from './framework';
import { MirrorStore } from '../src/store/store';

describe('MirrorStore', () => {
  let store: MirrorStore;

  beforeAll(async () => {
    store = new MirrorStore();
    // Only pass URL and bucket name - NATS is handled internally
    await store.storeConnectNats('ws://localhost:9222', 'rtdb');
  });

  afterAll(async () => {
    await store.storeDisconnectNats();
  });

  it('should connect successfully', () => {
    // Test the store through its public API only
    expect(store).toBeDefined();
  });
});
```
