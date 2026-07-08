import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const repoRoot = path.resolve(__dirname, '..');

// Helper to check if path should be ignored
function shouldIgnore(filePath) {
  const segments = filePath.split(path.sep);
  return (
    segments.includes('.git') ||
    segments.includes('node_modules') ||
    segments.includes('dist') ||
    segments.includes('.next') ||
    segments.includes('.gemini') ||
    segments.includes('data')
  );
}

// Set of allowed files and directories containing the word 'canonical'
// conforming to the naming policy (artifact invariants, validation, spec bytes, SHA, ordering)
const allowedPatterns = [
  // UI components representing the detail/registry views of the approved plan/run of record
  /^apps\/web\/src\/components\/relay\/RelayCanonicalPlanDetail(\.test)?\.tsx$/,
  /^apps\/web\/src\/components\/relay\/RelayCanonicalPlanPassDetail(\.test)?\.tsx$/,
  /^apps\/web\/src\/components\/relay\/RelayCanonicalPlansRegistry\.tsx$/,
  /^apps\/web\/src\/components\/relay\/RelayCanonicalRunWorkbench(\.test)?\.tsx$/,
  /^apps\/web\/src\/components\/relay\/RelayCanonicalRunsRegistry\.tsx$/,
  // Tests validating canonical properties or classification
  /^apps\/web\/src\/features\/relay-navigation\/pipeline\.canonicalClassification\.property\.test\.ts$/,
  /^apps\/web\/src\/features\/relay-navigation\/useShellData\.canonical\.test\.ts$/,
  // HTTP routes & handlers for canonical artifacts submission and validation
  /^internal\/api\/canonical\/[a-zA-Z0-9_-]+\.go$/,
  // MCP coordinator mapping responses for canonical validations
  /^internal\/mcp\/canonical_application\.go$/,
  // Integration tests for canonical transport parity
  /^internal\/mcp\/canonical_transport_parity_test\.go$/,
  // Test data representing non-canonical spec bytes
  /^internal\/speccompiler\/testdata\/noncanonical\.execution-spec\.json$/,
  // This naming checker script itself
  /^scripts\/check-workflow-naming\.mjs$/
];

function isPathAllowed(relPath) {
  const normalized = relPath.replace(/\\/g, '/');
  return allowedPatterns.some((pattern) => pattern.test(normalized));
}

let exitCode = 0;

function checkFile(absPath, relPath) {
  // Check if filename contains 'canonical' violating the naming normalization policy
  if (relPath.toLowerCase().includes('canonical')) {
    if (!isPathAllowed(relPath)) {
      console.error(`Violation: File name contains 'canonical' outside naming policy exceptions: ${relPath}`);
      exitCode = 1;
    }
  }

  // Check Go files package names
  if (absPath.endsWith('.go')) {
    try {
      const content = fs.readFileSync(absPath, 'utf8');
      const lines = content.split('\n');
      for (const line of lines) {
        const trimmed = line.trim();
        if (trimmed.startsWith('package ')) {
          const pkgName = trimmed.substring(8).trim();
          if (pkgName.includes('canonical')) {
            const relDir = path.dirname(relPath).replace(/\\/g, '/');
            if (relDir !== 'internal/api/canonical') {
              console.error(`Violation: Go package contains 'canonical' in unauthorized directory: ${relPath} (package ${pkgName})`);
              exitCode = 1;
            }
          }
          break;
        }
      }
    } catch (err) {
      console.error(`Error reading ${relPath}: ${err.message}`);
    }
  }
}

function checkDir(absPath, relPath) {
  if (relPath && relPath.toLowerCase().includes('canonical')) {
    // Check if the directory itself is allowed
    const normalized = relPath.replace(/\\/g, '/');
    const isAllowedDir = normalized === 'internal/api/canonical';
    if (!isAllowedDir) {
      console.error(`Violation: Directory name contains 'canonical' outside naming policy exceptions: ${relPath}`);
      exitCode = 1;
    }
  }
}

function walk(dir) {
  const entries = fs.readdirSync(dir, { withFileTypes: true });
  for (const entry of entries) {
    const absPath = path.join(dir, entry.name);
    const relPath = path.relative(repoRoot, absPath);

    if (shouldIgnore(relPath)) {
      continue;
    }

    if (entry.isDirectory()) {
      checkDir(absPath, relPath);
      walk(absPath);
    } else if (entry.isFile()) {
      checkFile(absPath, relPath);
    }
  }
}

walk(repoRoot);

if (exitCode === 0) {
  console.log('Naming normalization check passed successfully.');
} else {
  console.error('Naming normalization check failed with violations.');
}

process.exit(exitCode);
