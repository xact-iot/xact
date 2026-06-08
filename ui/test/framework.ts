// Browser Test Framework - TypeScript version
// Provides describe/it/expect API similar to Vitest

export interface TestResult {
  name: string;
  passed: boolean;
  error?: string;
  duration: number;
}

export interface SuiteResult {
  name: string;
  tests: TestResult[];
  passed: boolean;
}

interface TestDefinition {
  name: string;
  fn: () => Promise<void> | void;
}

interface SuiteDefinition {
  name: string;
  tests: TestDefinition[];
  beforeAll?: () => Promise<void>;
  afterAll?: () => Promise<void>;
}

class TestRunner {
  private suites: Map<string, SuiteDefinition> = new Map();
  private currentSuite: string | null = null;
  results: SuiteResult[] = [];
  onResult?: (suite: SuiteResult) => void;
  onComplete?: () => void;

  describe(name: string, fn: () => void) {
    this.currentSuite = name;
    this.suites.set(name, { name, tests: [] });
    fn();
    this.currentSuite = null;
  }

  it(name: string, fn: () => Promise<void> | void) {
    if (!this.currentSuite) {
      throw new Error('it() must be called inside describe()');
    }
    const suite = this.suites.get(this.currentSuite)!;
    suite.tests.push({ name, fn });
  }

  beforeAll(fn: () => Promise<void> | void) {
    if (!this.currentSuite) {
      throw new Error('beforeAll() must be called inside describe()');
    }
    const suite = this.suites.get(this.currentSuite)!;
    suite.beforeAll = async () => fn();
  }

  afterAll(fn: () => Promise<void> | void) {
    if (!this.currentSuite) {
      throw new Error('afterAll() must be called inside describe()');
    }
    const suite = this.suites.get(this.currentSuite)!;
    suite.afterAll = async () => fn();
  }

  async runAll() {
    this.results = [];
    for (const [name, suite] of this.suites) {
      const suiteResult = await this.runSuite(suite);
      this.results.push(suiteResult);
      this.onResult?.(suiteResult);
    }
    this.onComplete?.();
  }

  async runSuite(suite: SuiteDefinition): Promise<SuiteResult> {
    const suiteResult: SuiteResult = { name: suite.name, tests: [], passed: true };
    
    try {
      if (suite.beforeAll) {
        await suite.beforeAll();
      }
      
      for (const test of suite.tests) {
        const result = await this.runTest(test);
        suiteResult.tests.push(result);
        if (!result.passed) {
          suiteResult.passed = false;
        }
      }
      
      if (suite.afterAll) {
        await suite.afterAll();
      }
    } catch (error: any) {
      suiteResult.passed = false;
    }
    
    return suiteResult;
  }

  async runTest(test: TestDefinition): Promise<TestResult> {
    const start = performance.now();
    try {
      await test.fn();
      const duration = performance.now() - start;
      return { name: test.name, passed: true, duration };
    } catch (error: any) {
      const duration = performance.now() - start;
      return { name: test.name, passed: false, error: error?.message || String(error), duration };
    }
  }

  async runSingleTest(suiteName: string, testName: string): Promise<TestResult | null> {
    const suite = this.suites.get(suiteName);
    if (!suite) return null;
    
    const test = suite.tests.find(t => t.name === testName);
    if (!test) return null;

    // Run beforeAll if exists
    if (suite.beforeAll) {
      await suite.beforeAll();
    }

    const result = await this.runTest(test);

    // Run afterAll if exists
    if (suite.afterAll) {
      await suite.afterAll();
    }

    return result;
  }

  getSuite(suiteName: string): SuiteDefinition | undefined {
    return this.suites.get(suiteName);
  }

  clear() {
    this.suites.clear();
    this.results = [];
  }
}

const runner = new TestRunner();

export const describe = runner.describe.bind(runner);
export const it = runner.it.bind(runner);
export const beforeAll = runner.beforeAll.bind(runner);
export const afterAll = runner.afterAll.bind(runner);
export const runAll = runner.runAll.bind(runner);
export const clearTests = runner.clear.bind(runner);
export const getResults = () => runner.results;
export const onTestResult = (cb: (suite: SuiteResult) => void) => { runner.onResult = cb; };
export const onTestComplete = (cb: () => void) => { runner.onComplete = cb; };
export const runSingleTest = (suiteName: string, testName: string) => runner.runSingleTest(suiteName, testName);
export const getSuite = (suiteName: string) => runner.getSuite(suiteName);

// Expect API
class Expectation {
  private value: any;
  private negated: boolean = false;

  constructor(value: any) {
    this.value = value;
  }

  get not() {
    this.negated = !this.negated;
    return this;
  }

  private check(condition: boolean, message: string) {
    const result = this.negated ? !condition : condition;
    if (!result) {
      throw new Error(message);
    }
  }

  toBe(expected: any) {
    this.check(
      Object.is(this.value, expected),
      `Expected ${JSON.stringify(expected)} but got ${JSON.stringify(this.value)}`
    );
  }

  toEqual(expected: any) {
    this.check(
      JSON.stringify(this.value) === JSON.stringify(expected),
      `Expected ${JSON.stringify(expected)} but got ${JSON.stringify(this.value)}`
    );
  }

  toBeDefined() {
    this.check(
      this.value !== undefined,
      `Expected value to be defined but got undefined`
    );
  }

  toBeUndefined() {
    this.check(
      this.value === undefined,
      `Expected value to be undefined but got ${JSON.stringify(this.value)}`
    );
  }

  toBeNull() {
    this.check(
      this.value === null,
      `Expected value to be null but got ${JSON.stringify(this.value)}`
    );
  }

  toBeTruthy() {
    this.check(
      !!this.value,
      `Expected value to be truthy but got ${JSON.stringify(this.value)}`
    );
  }

  toBeFalsy() {
    this.check(
      !this.value,
      `Expected value to be falsy but got ${JSON.stringify(this.value)}`
    );
  }

  toBeGreaterThan(expected: number) {
    this.check(
      this.value > expected,
      `Expected ${this.value} to be greater than ${expected}`
    );
  }

  toBeGreaterThanOrEqual(expected: number) {
    this.check(
      this.value >= expected,
      `Expected ${this.value} to be greater than or equal to ${expected}`
    );
  }

  toBeLessThan(expected: number) {
    this.check(
      this.value < expected,
      `Expected ${this.value} to be less than ${expected}`
    );
  }

  toBeLessThanOrEqual(expected: number) {
    this.check(
      this.value <= expected,
      `Expected ${this.value} to be less than or equal to ${expected}`
    );
  }

  toContain(item: any) {
    this.check(
      this.value && this.value.includes && this.value.includes(item),
      `Expected ${JSON.stringify(this.value)} to contain ${JSON.stringify(item)}`
    );
  }

  toHaveLength(expected: number) {
    this.check(
      this.value && this.value.length === expected,
      `Expected length ${expected} but got ${this.value?.length}`
    );
  }

  toThrow() {
    let threw = false;
    try {
      if (typeof this.value === 'function') {
        this.value();
      }
    } catch (e) {
      threw = true;
    }
    this.check(
      threw,
      `Expected function to throw but it did not`
    );
  }

  toThrowError(expectedMessage?: string) {
    let threw = false;
    let message = '';
    try {
      if (typeof this.value === 'function') {
        this.value();
      }
    } catch (e: any) {
      threw = true;
      message = e?.message || String(e);
    }
    this.check(
      threw,
      `Expected function to throw but it did not`
    );
    if (expectedMessage && !this.negated) {
      if (!message.includes(expectedMessage)) {
        throw new Error(`Expected error message to include "${expectedMessage}" but got "${message}"`);
      }
    }
  }
}

export function expect(value: any): Expectation {
  return new Expectation(value);
}
