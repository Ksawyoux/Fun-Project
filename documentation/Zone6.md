# 🟫 Zone 6: Consumer Interfaces — System Design Deep Dive

---

## 🎯 Zone Responsibility

Zone 6 has one sacred job:

> Bridge the gap between the complex Knowledge Graph backend (Zones 4 & 5) and the developer's daily workflow, presenting architectural intelligence when and where it is needed — via AI agents, IDEs, CLI tools, web dashboards, and CI/CD pipelines.

If Zone 5 is the **reasoning brain**, Zone 6 is the **sensory-motor interface** that actually touches the developer. 

It is designed to:
- Expose the graph directly to AI agents using the **Model Context Protocol (MCP)**.
- Provide real-time, inline architectural linting and Q&A inside the IDE.
- Empower developers with an interactive 3D Web Dashboard.
- Enforce architectural quality gates in local git hooks and central CI/CD pipelines.
- Automate pull request impact reviews.

---

## 🧠 Core Design Philosophy

Four core principles govern the design of Zone 6:

### Principle 1: MCP-First for Agentic Workflows
Modern software engineering relies heavily on AI-assisted coding. Exposing the architecture graph as an MCP server allows third-party AI agents (e.g., Cursor, Claude Desktop, Gemini) to natively inspect codebases, run impact analyses, and debug system designs autonomously.

### Principle 2: Zero Context Disruption
Developers shouldn't leave their environment to look up architecture definitions. Insights, warnings, and code-coupling metrics must overlay directly inside the IDE (via inline decorations and compiler diagnostic channels) or trigger automatically inside PR reviews.

### Principle 3: Shared Configuration, Pluggable Clients
Whether running in a local CLI, a GitHub Action, or the IDE plugin, all client configurations (such as quality gate rules, allowed coupling profiles, and naming policies) are retrieved from a centralized governance file (`.archgraph.yaml`) checked into the codebase.

### Principle 4: Lean and Offline-First Local Operations
To maintain IDE responsiveness, clients must cache the high-level structural map (Tiers 1 & 2) locally. The IDE uses WebSocket feeds for active changes and drops back to query-only mode when disconnected, ensuring local indexing doesn't saturate system resources.

---

## 🏗️ Full Consumer Interfaces Map

The following map illustrates how Zone 6 interfaces coordinate with the Serving Layer (Zone 5) and AI clients:

```
                  ┌──────────────────────────────┐
                  │          AI CLIENTS          │
                  │   (Claude, Cursor, Gemini)   │
                  └──────────────┬───────────────┘
                                 │ (MCP Protocol)
                                 ▼
┌─────────────────────────────────────────────────────────────────┐
│                      INGESTION & SERVING                        │
│                                                                 │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │                    MCP SERVER                           │   │
│   │      Tools      │     Resources     │      Prompts      │   │
│   └────────────────────────────┬────────────────────────────┘   │
│                                │                                │
│        ┌───────────────────────┼───────────────────────┐        │
│        ▼                       ▼                       ▼        │
│  ┌──────────┐            ┌───────────┐           ┌──────────┐   │
│  │   IDE    │            │    Web    │           │   CLI    │   │
│  │  Plugin  │            │ Dashboard │           │   Tool   │   │
│  │ (VSCode/ │            │ (3D viz/  │           │(Git hooks│   │
│  │ IntelliJ)│            │ timeline) │           │  CI/CD)  │   │
│  └─────┬────┘            └───────────┘           └────┬─────┘   │
└────────┼──────────────────────────────────────────────┼─────────┘
         │ (Active edits)                               │ (Run audits)
         ▼                                              ▼
┌──────────────────┐                            ┌─────────────────┐
│ LOCAL WORKSPACE  │                            │  CI/CD PIPELINE │
│  Active Files &  │                            │  PR Analyzers & │
│  Diagnostics     │                            │  Quality Gates  │
└──────────────────┘                            └─────────────────┘
```

---

## 🔍 Subsystem 1: Model Context Protocol (MCP) Server

The MCP Server implements the official Model Context Protocol, exposing the query planner and analytical engines as tools, resources, and prompt templates to AI agents. It operates over a `stdio` transport.

### 1. MCP Tools Specification

The server exposes the following tools to the AI client. When the LLM calls these tools, they execute corresponding operations on Zone 5's serving layer.

#### A. Tool: `get_blast_radius`
Retrieves a list of downstream dependencies impacted if the target entity is modified or deprecated.
```json
{
  "name": "get_blast_radius",
  "description": "Calculate the downstream blast radius and impacted teams if an entity is modified or deprecated.",
  "inputSchema": {
    "type": "OBJECT",
    "properties": {
      "entityId": { "type": "STRING", "description": "The canonical ID of the entity (e.g. svc-billing-91a)." },
      "maxDepth": { "type": "NUMBER", "description": "Traversal depth (default: 3)." }
    },
    "required": ["entityId"]
  }
}
```

#### B. Tool: `query_architecture`
Executes an arbitrary read-only Cypher query against the Graph Database.
```json
{
  "name": "query_architecture",
  "description": "Query the live architecture graph using Cypher. Useful for tracing structural dependencies and entity properties.",
  "inputSchema": {
    "type": "OBJECT",
    "properties": {
      "query": { "type": "STRING", "description": "A valid Cypher read query." }
    },
    "required": ["query"]
  }
}
```

#### C. Tool: `get_architecture_diff`
Compares the architecture between two timestamps, commits, or releases.
```json
{
  "name": "get_architecture_diff",
  "description": "Retrieve structural changes (nodes/edges added, removed, or modified) between two reference points.",
  "inputSchema": {
    "type": "OBJECT",
    "properties": {
      "referenceA": { "type": "STRING", "description": "Git commit SHA, release tag, or ISO timestamp." },
      "referenceB": { "type": "STRING", "description": "Git commit SHA, release tag, or ISO timestamp." }
    },
    "required": ["referenceA", "referenceB"]
  }
}
```

#### D. Tool: `scan_architectural_smells`
Scans a namespace for circular dependencies, modularity violations, or high-coupling technical debt.
```json
{
  "name": "scan_architectural_smells",
  "description": "Scan a codebase namespace for architectural smells and return structural violations.",
  "inputSchema": {
    "type": "OBJECT",
    "properties": {
      "namespace": { "type": "STRING", "description": "The codebase namespace/directory filter." }
    },
    "required": ["namespace"]
  }
}
```

---

### 2. MCP Resources Specification

Resources are read-only data sources exposed by the MCP server.

- **`archgraph://schema`**: Exposes the complete NIF (Normalized Ingestion Format) schema definitions and the entity/relationship catalogs. This helps AI agents understand the structure of the graph database before crafting Cypher queries.
- **`archgraph://health/summary`**: Returns real-time health metrics, counting total circular dependencies, dangling endpoints, and undocumented schemas across namespaces.
- **`archgraph://drift/log`**: Returns a log of recent architectural changes appended to the Delta Log in Zone 4.

---

### 3. MCP Prompts Specification

Prompts are predefined templates that developers or AI clients can invoke to structure architectural reasoning sessions.

#### A. Prompt: `audit-refactoring-risk`
Sets up the LLM to analyze the risk of a proposed refactoring path.
```json
{
  "name": "audit-refactoring-risk",
  "description": "Prepares the agent to analyze structural and runtime risks associated with a proposed code refactoring.",
  "arguments": [
    { "name": "targetEntity", "description": "The service or module to refactor.", "required": true }
  ]
}
```
*Prompt Template Output:*
> "You are an expert software architect. A developer wants to refactor the entity `{{targetEntity}}`. First, call the tool `get_blast_radius` with `entityId: '{{targetEntity}}'` to identify all downstream callers. Next, call the tool `scan_architectural_smells` with the namespace of `{{targetEntity}}` to check for circular dependencies. Finally, compile a risk report outlining structural dependencies, runtime latency risks, and a recommended step-by-step migration plan."

---

## 🔌 Subsystem 2: IDE Plugin (VS Code / JetBrains)

The IDE plugin translates the workspace context into real-time feedback for developers as they write code.

```
Editor Action (Focus/Edit) ──▶ [WebSocket Client] ──▶ Zone 5 API ──▶ Real-time Diagnostics
```

### 1. Sidebar Architecture Assistant
An in-editor chat panel integrated with the local IDE's active document context.
- **Context Injection:** When the developer asks, *"How does this file fit into the system?"*, the plugin automatically extracts the active file name, maps it to its canonical entity ID via the IDE's local file mapping cache, and queries Zone 5's Query Engine.
- **Mermaid Render:** The sidebar features a native SVG renderer for compiling `mermaid.js` diagrams generated by the reasoning engine.

### 2. Inline Code Decorators
Overlays structural metadata directly onto class, method, or function signatures.
- **CodeLens Overlays:** Displays caller counts and owner indicators above functions:
  ```typescript
  // 👥 Owned by team-billing | 🔗 Called by 5 services (p99: 145ms)
  export async function processPayment(paymentPayload: Payload) { ... }
  ```
- **Hot-spot Visualizer:** Colors functions or imports that have high change-coupling scores (from the Change Coupled relationship data) in red to warn developers that changing this line historically breaks adjacent modules.

### 3. Diagnostic Provider (Linter Integration)
Maps architectural smells returned from Zone 5 directly onto the IDE's editor window as wiggly warning lines.

```typescript
import { window, languages, Diagnostic, DiagnosticSeverity, Range } from 'vscode';

// Example: Registering VS Code diagnostic listener for shared-database smells
export function updateDiagnostics(document: TextDocument, collection: DiagnosticCollection) {
    const canonicalId = mapFileToCanonicalId(document.fileName);
    const smells = localCache.getSmellsFor(canonicalId);
    
    const diagnostics: Diagnostic[] = smells.map(smell => {
        const range = new Range(smell.line_start, 0, smell.line_end, 99);
        return new Diagnostic(
            range,
            `[Architectural Smell] Circular coupling detected: ${smell.description}`,
            DiagnosticSeverity.Warning
        );
    });
    
    collection.set(document.uri, diagnostics);
}
```

---

## 🖥️ Subsystem 3: Web Dashboard

The Web Dashboard is the central graphical console for mapping, searching, and managing the architecture graph.

```
WebGL Render Loop ──▶ [Force Graph Engine] ──▶ Mouse Events (Zoom/Filter) ──▶ Tier Focus
```

### 1. 3D Force-Directed Graph Viewer
Built using **WebGL (Three.js / React-Force-Graph-3D)** to render dense multi-tiered codebases.
- **Layered viewports:** Developers can collapse Tier 5 (symbols) and Tier 4 (files) to look exclusively at Tier 2 (Service topologies).
- **Metric heatmaps:** Overlays latency or error rate parameters onto relationship edges. Critical edges glow red or thicken as throughput values increase.

### 2. Timeline Evolution Slider
An interactive UI slider that steps through historical snapshots from Zone 4.
- As the developer drags the slider, the interface plays back the Delta Log.
- New nodes flash green, removed nodes turn red and fade, and shifting dependencies slide dynamically to represent restructuring over time.

---

## 💻 Subsystem 4: CLI Tool (`archgraph`) & Git Hooks

A compiled local binary (written in Go or Rust) used for local terminal operations and CI scripts.

### 1. CLI Commands Catalog

- **`archgraph query "<Cypher>"`**: Run direct read queries against the local tenant namespace.
- **`archgraph diff <commit_sha_1> <commit_sha_2>`**: Compares two workspace commits and lists architectural additions, deletions, or structural modifications.
- **`archgraph impact --file <path> --line <number>`**: Computes the local blast radius of a specific block of code without opening the Web UI.
- **`archgraph validate`**: Validates the local codebase structure against rules defined in `.archgraph.yaml`.

---

### 2. Pre-Commit Hooks
Developers can register `archgraph validate` as a Git pre-commit hook:

```bash
#!/bin/sh
# .git/hooks/pre-commit

echo "Checking architecture boundaries..."
# Scan only modified files in the staging area
staged_files=$(git diff --cached --name-only)

if ! archgraph validate --files "$staged_files"; then
    echo "❌ Commit rejected: Your changes introduce critical architectural smells."
    echo "Run 'archgraph validate --detail' to view the violations."
    exit 1
fi

echo "✅ Architecture validation passed."
```

---

## ⚙️ Subsystem 5: CI/CD PR Analyzer

Automates architectural governance and risk assessment directly inside continuous integration pipelines.

```
Git Push ──▶ CI Trigger ──▶ [PR Analyzer Runner] ──▶ [Zone 5 API] ──▶ PR Comment / Check Gate
```

### 1. Automated PR Blast-Radius Commenter
Upon creation of a pull request, the CI Runner triggers a script that:
1. Calculates the diff between the source branch and the target branch.
2. Identifies all modified code files.
3. Requests a blast radius report for those modified entities from Zone 5.
4. Generates and posts a Markdown summary comment to the pull request.

#### Example PR Comment Generated:
> ### 🛡️ ArchGraph Impact Analysis Report
> 
> This PR modifies the file [user_service.py](file:///services/user/user_service.py).
> 
> #### Downstream Blast Radius:
> - **Directly Impacted Services:** `auth-service` (p99 latency: 45ms), `billing-service` (Critical)
> - **Transitively Impacted Teams:** `team-finance` (billing owner), `team-security` (auth owner)
> 
> #### Structural Violations Identified:
> - ⚠️ **Warning:** Modifying `user_service.py` increases data coupling on table `user_credentials`.
> - ❌ **Error:** Introduces a circular dependency: `user_service` ──▶ `auth_service` ──▶ `user_service`.
> 
> **Status:** Quality gates failed. Please break the dependency cycle before merging.

---

### 2. CI Quality Gate Policies (`.archgraph.yaml`)
Pipeline checks are governed by rules configured in the root of the project:

```yaml
version: "1.0"
namespace: "payment-platform"

governance:
  rules:
    # Circular dependencies fail the build
    - id: "RULE-01"
      name: "no-circular-dependencies"
      severity: "FAIL"
      scope: "Tiers[2..4]"
      
    # Services must have declared owners
    - id: "RULE-02"
      name: "require-service-owners"
      severity: "FAIL"
      scope: "Tier[2]"
      
    # Shared database tables produce warnings, but do not block merge
    - id: "RULE-03"
      name: "shared-database-table"
      severity: "WARN"
      scope: "Tier[3]"

  allow_list:
    # Legacy exclusions to be refactored
    - rule_id: "RULE-01"
      exclude_paths:
        - "libs/legacy-shared/**"
```

---

## 📊 Client Metrics & Performance SLA Targets

To maintain a frictionless developer experience, all clients must respect the following SLA thresholds:

| Client Action | Trigger Context | Target Latency | Client-Side Mitigation |
|:---|:---|:---|:---|
| **Linter Diagnostics** | IDE File Save / Focus | **< 10ms** | Debounce check loop, query local indexing cache |
| **Tool Execution** | MCP Agent Call | **< 150ms** | Pre-compute common path traversals in Zone 5 |
| **Visual Render Loop** | Dashboard Navigation | **60 FPS** | WebGL vertex buffering, instanced geometry |
| **Pre-Commit Hook** | Git Commit Invocation | **< 800ms** | Filter analysis only to files present in git staging |
| **CI Gate Validation** | PR Build Pipeline | **< 15s** | Cache intermediate graph runs between pipelines |
