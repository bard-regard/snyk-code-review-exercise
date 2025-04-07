# Review

## Ideas
1. Linear dependencyResolve.
2. Logging, Context/Tracing, Timeouts.
3. Global/root scoped LRU cache with trie search and compressed values and maybe disk usage.
4. Signature verification on external calls.

## Problems
1. Circular Dependencies
   - There is no detection or handling of circular dependencies in the recursive resolver. 
   - This can result in infinite loops, stack overflows, and runtime crashes. 
   - Fix: Track visited packages during recursion and break cycles, use `_circular` to contain fields.
   - tap 0.4.0 &rarr; deep-equal 0.0.0 &rarr; tap 0.4.0 !
2. Unbounded Recursion & Memory Usage
   - The resolver is implemented recursively, which can cause stack overflow for deeply nested trees. 
   - Each call to resolveDependencies creates a new call frame and builds a new map — expensive at scale. 
   - Fix: Iterative & queue, or add a max-depth safeguard.
3. Redundant Requests (Double Fetch)
   - Each dependency resolution causes two fetches per package:
     - One for metadata (fetchPackageMeta).
     - One for dependencies of the chosen version (fetchPackage).
     - The first response already includes dependencies; the second is usually redundant.
     - Fix: Extract dependencies from the abbreviated metadata to eliminate the second request.
4. No Caching
   - There is no local or global caching of package metadata or resolved versions. 
   - This results in repeated network calls for identical packages across requests — a major performance bottleneck. 
   - Note: Caching was part of the acceptance criteria for this task. 
   - Fix: Add an in-memory cache (e.g., `map[string]*npmPackageMetaResponse`) or use `sync.Map/golang-lru`.
5. Inconsistent Error Handling
   - Errors are not consistently wrapped with context. 
   - Some errors are logged with `println()` rather than a structured logger. 
   - Many return paths silently ignore JSON parsing errors (`_ = json.Unmarshal(...)`). 
   - Fix: Use `log.Printf()` or a structured logger, and wrap errors with context using `fmt.Errorf("...: %w", err)`.
6. Missing Input Validation
   - User inputs (pkgName, pkgVersion) are not validated or sanitized. 
   - Fix: Use regex validation or apply constraints based on npm naming conventions.
7. Missing HTTP Response Handling
   - `http.Get` calls do not check for `resp.StatusCode != http.StatusOK`. 
   - This can cause unexpected behavior when receiving `404/500` responses from npm.
8. Redundant Types 
   - `NpmPackageVersion` and `npmPackageResponse` share almost identical fields, and would also become redundant with a refactor. 
   - Naming (`pkg, versionConstraint, versions`) is sometimes inconsistent or overly verbose.
9. General Quality Issues
   - Unwrapped errors 
   - Shadowed variables (`err := ...` before `if err := ...`)
   - Redundant type casts (`[]byte(body)` on something already a `[]byte`)
   - Unchecked `w.Write()` return values 
   - No gzip or compression headers 
   - No timeout or retry logic on external calls 
   - No metrics or structured logs (low observability)
   - HTTP (not HTTPS) server in main.go (though this is technically Acceptance Criteria #1)

---

## Tests
- There is one test
- No code coverage
- No integration tests
- No supporting make file

---

### Code Coverage
We should aim to maintain a meaningful level of code coverage.

While hitting an exact percentage target can be misleading, having some coverage is far better than none. Coverage alone doesn't prove quality — but it does help reveal untested paths, and acts as a safety net during refactoring.

Rather than chasing numbers, we should:
- Cover code we expect to run 
- Assert both success and failure paths 
- Handle edge cases explicitly 
- Ensure tests are expressive, not just present

---

### Makefile
We should also include a Makefile so that we can standardise development commands.

---

### Observability
We should aim to make the system observable - not just functional. 
This includes adding structured logging, metrics, and potentially tracing, so we can understand what's happening at runtime, especially when something breaks.

---

### Containerization
We should containerise the service using Docker to ensure consistent builds and deployment across environments. 
This allows the app to run identically in local dev, CI, staging, and production.

For orchestration and scaling, Kubernetes can be used to manage deployment, health checks, and horizontal scaling of the service.

Together, containerisation ensures that the service is portable, resilient, and ready for production deployment with observability and resource control built in.

---

### Unit Tests

#### function packageHandler
1. Successful request
   - Given a valid package name and version range, returns the dependency tree with HTTP 200 and correct JSON
2. Invalid input
   - Invalid package name
     - package name length should be greater than zero
     - all the characters in the package name must be lowercase i.e., no uppercase or mixed case names are allowed
     - package name can consist of hyphens
     - package name must not contain any non-url-safe characters (since name ends up being part of a URL)
     - package name should not start with . or _
     - package name should not contain any spaces
     - package name should not contain any of the following characters: ~)('!*
     - package name cannot be the same as a node.js/io.js core module nor a reserved/blacklisted name. For example, the following names are invalid:
       - node_modules
       - favicon.ico
     - package name length cannot exceed 214 
   - Invalid package version
     - Not a valid semantic version 
   - Nonexistent package 
   - Nonexistent version 
   - Valid package with no matching versions
3. Edge cases
   - Valid package, circular dependency, should detect and break the loop gracefully 
   - Package with deep dependency tree 100+?, should complete without stack overflow or timeout
4. Output correctness
   - JSON is valid, contains all dependencies 
   - Structure matches NpmPackageVersion (no nil pointers, no missing nested deps)
---
#### function resolveDependencies
   - Resolves all transitive dependencies recursively
   - Handles packages with no dependencies
   - Handles packages with optional or dev dependencies (even if ignored for install, they matter for scans)
   - Circular references (Self or separated)
---
#### function fetchPackageMeta
  - 200 OK &rarr; returns full parsed metadata 
  - 404 &rarr; error
  - 500 &rarr; error
  - Invalid JSON &rarr; error
  - Response with Content-Encoding: gzip (if added) &rarr; correctly handled 
  - Abbreviated metadata properly parsed &rarr; `Versions {Dependencies ...}`
---
#### function highestCompatibleVersion
  - Handles exact match constraint like "1.2.3"
  - Handles range constraint like "^1.2.0" and returns highest match 
  - Handles "~2" `"nopt": "~2"` or "~0.3" `"mkdirp": "~0.3"`
  - Handles "*" `"slide": "*"` and returns highest version 
  - Handles invalid semver constraint string &rarr; returns error 
  - Returns error if no versions match constraint
---
#### function fetchPackage
- Correctly parses single-version responses 
- Returns error on 404, 500, invalid JSON 
- Response with missing fields (e.g., no dependencies) handled safely
---
#### Security Test Cases
- Input sanitization: ensure no package or version input can cause path traversal, SSRF, header injection, or log forging
- Rate limiting / abuse control: (if added later)
- Timeout or cancellation of slow fetches (especially large packages like npm, aws-sdk) (with appropriate error messaging)
- JSON payload limits enforced (avoid unbounded io.ReadAll or large recursion trees) (consider buffer flushing)
- Proper logging with no sensitive info leak
---
#### Performance & Resource Usage
- Response time under load (e.g. 100 requests/sec for shallow deps)
- Response time with large dependency trees (e.g. webpack, aws-sdk)
- P90, P99
- Memory use of recursive resolution does not exceed bounds
- Avoids fetching the same version metadata twice (consider caching in-memory during a single request)
---
#### Additional Feature Tests (if extended)
  - gzip compression on response when Accept-Encoding: gzip 
  - Support for version pinning via resolutions or overrides in future 
  - API versioning (e.g., /v1/...) routing works as expected
---


### Integration Tests
  - Ensure the HTTP handler (packageHandler) processes real package/version inputs 
  - Ensure resolution logic integrates correctly with live or mocked npm registry responses 
  - Validate full JSON output for expected structure 
  - Catch regressions, edge cases, and real-world failure modes

#### Suggested Integration Test Cases
1. Basic resolution
   - `GET /package/lodash/^4.17.0`
   - &rarr; returns resolved dependency tree with root version 4.17.x, HTTP 200, correct structure
2. Exact version
   - `GET /package/lodash/4.17.15`
   - &rarr; returns only that version with its dependencies
3. Nonexistent package
   - `GET /package/nonexistentpackageforsureyesplease/1.0.0`
   - &rarr; returns HTTP 404 or structured error response
4. Nonexistent version
   - `GET /package/lodash/999.999.999`
   - &rarr; returns 404 or JSON error with no compatible versions found
5. Malformed semver input
   - `GET /package/lodash/not-a-semver`
   - &rarr; returns 400 Bad Request
6. Invalid package name
   - `GET /package/../../../etc/passwd/^1.0.0`
   - &rarr; returns 400 or sanitized failure
7. Circular dependency package
   - Use a mock or custom package tarball with circular deps
   - &rarr; test that recursion stops gracefully, doesn’t crash
8. Large dependency tree
   - `GET /package/webpack/latest`
   - &rarr; returns nested structure, no timeout or panic
9. Security-sensitive package
   - `GET /package/fsevents/latest`
   - &rarr; validates behavior with optional deps / platform-specific logic
10. Giant response handling
    - Simulate response > 5MB (e.g., npm metadata) → ensure capped reading or graceful failure if limit is hit
11. Rate limiting or DoS resistance (if implemented)
    - Rapid repeated requests
    - &rarr; ensure proper throttling or graceful degradation