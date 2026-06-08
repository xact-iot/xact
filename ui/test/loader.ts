// Test Loader - Dynamically loads and runs test files
import { clearTests, runAll, onTestResult, onTestComplete, getResults, type SuiteResult } from './framework';

export interface TestRunInfo {
  totalSuites: number;
  totalTests: number;
  passedSuites: number;
  failedSuites: number;
  passedTests: number;
  failedTests: number;
  duration: number;
}

// Import test files here
// Add new test files to this array
const testModules = [
  () => import('./store.test'),
  // Add more test files here: () => import('./other.test'),
];

export async function loadAndRunTests(
  onResult?: (suite: SuiteResult) => void,
  onComplete?: (info: TestRunInfo) => void
) {
  clearTests();
  
  // Set up callbacks
  if (onResult) {
    onTestResult(onResult);
  }
  
  const startTime = performance.now();
  
  // Load all test modules
  for (const loadTest of testModules) {
    try {
      await loadTest();
    } catch (error) {
      console.error('Failed to load test module:', error);
    }
  }
  
  // Set up completion callback
  if (onComplete) {
    onTestComplete(() => {
      const results = getResults();
      const totalSuites = results.length;
      const passedSuites = results.filter(r => r.passed).length;
      const failedSuites = totalSuites - passedSuites;
      const totalTests = results.reduce((sum, r) => sum + r.tests.length, 0);
      const passedTests = results.reduce((sum, r) => sum + r.tests.filter(t => t.passed).length, 0);
      const failedTests = totalTests - passedTests;
      const duration = performance.now() - startTime;
      
      onComplete({
        totalSuites,
        totalTests,
        passedSuites,
        failedSuites,
        passedTests,
        failedTests,
        duration
      });
    });
  }
  
  // Run all tests
  await runAll();
}

export { getResults };
