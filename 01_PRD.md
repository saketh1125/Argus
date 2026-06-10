# Product Requirements Document (PRD)
## CodeSentinel — Autonomous Multi-Agent Bug Detection & Auto PR System
**Version:** 1.0  
**Status:** Locked  
**Hackathon:** Agentic AI Hackathon 2026 — PS-01  
**Date:** June 2026  

---

## 1. Executive Summary

CodeSentinel is an autonomous multi-agent system that analyzes GitHub repositories, detects bugs, security vulnerabilities, and code quality issues across all major programming languages, generates validated fixes, and automatically raises structured pull requests — without human intervention at any step of the pipeline.

Unlike simple static analyzers or chatbot-style code reviewers, CodeSentinel reasons like a senior engineer: it understands cross-file dependencies, generates multiple fix candidates, validates patches in an isolated sandbox before committing, and communicates findings with evidence-backed explanations.

---

## 2. Problem Statement

Modern codebases ship bugs that cost organizations millions in downtime, security breaches, and developer remediation time. Existing solutions fall into two inadequate categories:

- **Static analyzers** (ESLint, Bandit, Semgrep) — find issues but cannot fix them, generate excessive noise, and are language-specific.
- **AI pair programmers** (Copilot, Cursor) — assist humans but require human initiation, review, and execution for every change.

Neither solution is autonomous. Neither raises a PR. Neither validates that the generated fix actually works.

**The gap:** There is no fully autonomous system that goes from a repository URL to a validated, merged-ready pull request with zero human input in the loop.

CodeSentinel closes that gap.

---

## 3. Goals

### Primary Goals
- Autonomously detect bugs, security vulnerabilities, and code quality issues in any GitHub repository.
- Generate validated, working fixes for all detected Critical and High severity issues.
- Automatically raise structured pull requests with evidence-backed explanations.
- Support all major programming languages without per-language configuration.

### Secondary Goals
- Provide a real-time dashboard showing agent activity and pipeline progress.
- Generate a structured Summary Issue for Medium and Low severity findings.
- Operate within a 40 RPM API rate limit without human intervention.

### Non-Goals (v1)
- Cross-repository or multi-repo dependency analysis (v2 roadmap).
- CI/CD pipeline integration (v2 roadmap).
- Human review workflow or approval gates (v2 roadmap).
- Support for private registries or authenticated dependencies.

---

## 4. Target Users

| User | Need | How CodeSentinel Helps |
|---|---|---|
| Hackathon judges | Evaluate agentic AI system quality | Fully autonomous demo: URL in → PRs out |
| Open source maintainers | Reduce bug backlog without reviewer bandwidth | Auto-PR with validated fixes ready to merge |
| Engineering teams | Security vulnerability scanning with automatic remediation | Critical CVE fixes raised as PRs within minutes |
| Solo developers | Senior engineer code review without hiring one | Evidence-backed analysis of entire codebase |

---

## 5. Success Metrics

| Metric | Target |
|---|---|
| End-to-end pipeline completion | Repo URL → PRs raised in under 5 minutes |
| Precision (valid bugs only) | ≥ 80% of raised PRs contain a genuine issue |
| Fix validation rate | ≥ 70% of generated patches pass E2B sandbox |
| Language coverage | Python, JavaScript, TypeScript, Go, Java, C++, Rust, C |
| False positive rate | ≤ 20% of raised PRs |
| RPM compliance | Never exceeds 35 RPM on NIM API |

---

## 6. Scope

### In Scope — v1

**Detection Coverage:**
- Logic bugs (incorrect conditionals, off-by-one errors, null dereferences)
- Security vulnerabilities (injection risks, hardcoded secrets, insecure defaults, improper auth)
- Code quality issues (dead code, excessive complexity, missing error handling)
- Cross-file dependency mismatches (function signature drift across callers/callees)

**Languages Supported:**
- Python, JavaScript, TypeScript, Go, Java, C++, Rust, C, Ruby, PHP

**Output:**
- Automated Pull Requests for Critical and High severity issues (confidence ≥ 0.80)
- Structured Summary GitHub Issue for Medium and Low severity findings
- Real-time web dashboard showing pipeline status

**Constraints:**
- Repositories up to ~10,000 files (top 50 most recently modified files analyzed)
- Public GitHub repositories only (v1)
- NIM API rate limit: 35 RPM ceiling (5 RPM safety buffer below 40 RPM limit)

### Out of Scope — v1
- Private repository support
- Multi-repo cross-service analysis
- Test suite generation at scale
- Automatic merging of PRs
- IDE plugin or CLI tool

---

## 7. User Stories

**As a hackathon judge,**
I want to provide a GitHub repository URL and watch CodeSentinel autonomously find bugs, generate fixes, and raise PRs — so I can evaluate the quality of the agentic system in a live demo.

**As an open source maintainer,**
I want CodeSentinel to scan my repository, detect security vulnerabilities, and raise a PR with a validated fix — so I can merge it without spending hours debugging.

**As a solo developer,**
I want CodeSentinel to tell me exactly which line is wrong, why it's wrong, and provide a working fix with a confidence score — so I can trust the output without manually verifying every suggestion.

**As a team lead,**
I want CodeSentinel to generate a Summary Issue listing all Medium and Low priority findings — so my team has a prioritized backlog without manual code review sessions.

---

## 8. Functional Requirements

### FR-01: Repository Ingestion
- System shall accept a public GitHub repository URL as input.
- System shall clone the repository and build an AST knowledge graph using Tree-sitter.
- System shall extract function and class signatures from all supported language files.
- System shall construct an import and call graph for cross-file dependency tracking.
- System shall prioritize the 50 most recently modified files for analysis.
- System shall sanitize all file content before passing to LLM context (prompt injection prevention).

### FR-02: Issue Localization
- System shall perform two-stage retrieval: vector similarity search followed by LLM re-ranking.
- System shall narrow localization hierarchically: file → function → line range.
- System shall fall back to full file context if localization confidence falls below 0.50.
- System shall retrieve call graph dependencies for every localized function.

### FR-03: Issue Analysis
- System shall run analyst agents in parallel goroutines across file batches.
- Every analyst agent shall return a structured Issue object with mandatory fields: file, line_start, line_end, bug_type, severity, evidence (exact quoted line), explanation, confidence.
- System shall reject any analysis output missing the evidence field.
- System shall deduplicate issues by hash of (file, line_start, bug_type).

### FR-04: Fix Generation
- System shall generate 3 fix candidates per confirmed issue.
- LLM shall return the complete rewritten function only (not a diff).
- System shall programmatically produce the unified diff from original and rewritten function.
- System shall rank candidates by confidence score and select the highest-ranked.

### FR-05: Patch Validation
- System shall spin up an E2B sandbox for every fix candidate.
- System shall run syntax validation as a minimum gate.
- System shall run all existing repository tests if present.
- If no tests exist, system shall generate a minimal test for the fixed function and run it.
- Patches failing validation shall be retried once with an additional LLM call.
- Patches failing after retry shall be flagged and excluded from PR creation.

### FR-06: Pull Request Creation
- System shall create a dedicated branch per fix.
- System shall commit the validated patch to that branch.
- System shall raise a pull request with a structured body containing: issue type, severity, evidence quote and line number, natural language explanation, confidence score, and sandbox validation result.
- System shall raise PRs only for Critical and High severity issues with confidence ≥ 0.80.
- System shall compile all Medium and Low severity findings into a single structured GitHub Issue.

### FR-07: Rate Limiting
- All NIM API calls shall pass through a shared AsyncRateLimiter.
- Rate limiter ceiling shall be 35 RPM (5 RPM buffer below 40 RPM hard limit).
- Rate limiter shall be a single shared instance across all goroutines.

### FR-08: Embedding Fallback
- System shall attempt local embedding via nomic-embed-text (Ollama) first.
- On failure, system shall fall back to Jina AI API.
- On failure, system shall fall back to Google Gemini Embedding API.
- On failure, system shall fall back to all-MiniLM-L6-v2 on CPU.
- Fallback detection shall be automatic at startup with no manual configuration required.

### FR-09: Dashboard
- System shall serve a real-time web dashboard on localhost during execution.
- Dashboard shall display: pipeline stage, files processed, issues found, PRs raised, agent status per goroutine, confidence score distribution.
- Dashboard shall update via HTMX server-sent events without page reload.

---

## 9. Non-Functional Requirements

| Requirement | Target |
|---|---|
| Pipeline latency (small repo <500 files) | < 3 minutes end-to-end |
| Pipeline latency (medium repo ~5000 files) | < 8 minutes end-to-end |
| Memory usage | < 4GB RAM total |
| Binary size | < 50MB compiled Go binary |
| VRAM usage (local embeddings) | < 500MB |
| API cost per run | < $2 USD on NIM |
| Concurrent analyst goroutines | Up to 10 parallel |

---

## 10. Constraints

- NIM API: 40 RPM hard limit (system operates at 35 RPM)
- E2B: 20 concurrent sandboxes on free tier, 1-hour session limit
- GitHub API: 5,000 requests/hour on authenticated token
- Qdrant: Local instance, no persistence between runs (acceptable for v1)
- Tree-sitter: Go bindings require CGO enabled at build time

---

## 11. Known Limitations (v1)

- **Multi-repo analysis:** Bugs spanning two separate repositories are not detected. Planned for v2.
- **Dynamic analysis:** Runtime-only bugs (race conditions, memory leaks in execution) are not detected. Static + sandbox analysis only.
- **Large monorepos:** Repositories with 10,000+ files are analyzed at the top-50 file subset. This is framed as intelligent scoping, not a limitation.
- **Private repositories:** GitHub token-based private repo access not implemented in v1.

---

## 12. Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| NIM API rate limit breach | Medium | High | AsyncRateLimiter at 35 RPM |
| E2B sandbox timeout | Low | Medium | 60s timeout, retry once, flag on second failure |
| LLM hallucinated fix passes sandbox | Medium | Medium | Evidence schema + test generation |
| Go concurrency bug in agent code | Medium | High | Structured channels, no shared mutable state |
| Tree-sitter CGO build failure | Low | High | Pre-test build in CI before demo |

---

## 13. v2 Roadmap

- Multi-repo cross-service dependency analysis
- CI/CD GitHub Actions integration
- Private repository support
- Automatic PR merging with configurable approval policy
- Historical trend dashboard (bug rate over commits)
- Slack/Teams notification integration
