# 🏗️ AI Codebase Knowledge Graph

Welcome to the **AI Codebase Knowledge Graph** repository. This project is a state-of-the-art system designed to capture, store, analyze, and query structural, historical, and runtime details of a software codebase. It turns static code symbols, ownership metadata, runtime telemetry, and Git commit history into a unified, living property graph that can be queried in natural language.

---

## 🗺️ High-Level Architecture

The overall system is divided into **6 logical zones**, as detailed in the architectural documentation:

1. **[Zone 1: Signal Sources](file:///Users/MacBook/Fun_Project/Fun-Project/documentation/Zone1.md)** — Feeds raw static (Git, AST, APIs, CI/CD, Infra) and dynamic (Distributed Tracing, Metrics, APM) data.
2. **[Zone 2: Ingestion Subsystem](file:///Users/MacBook/Fun_Project/Fun-Project/documentation/Zone2.md)** — Standardizes data into the Normalized Ingestion Format (NIF) and queues it on the event bus.
3. **[Zone 3: Processing Pipeline](file:///Users/MacBook/Fun_Project/Fun-Project/documentation/Zone3.md)** — Resolves entity identities, infers hidden relationships, validates facts, and computes graph deltas.
4. **[Zone 4: Graph Storage (MVP Implemented)](file:///Users/MacBook/Fun_Project/Fun-Project/documentation/Zone4.md)** — The system's central memory. Employs the core principle: **the delta log is the source of truth, and the graph is a projection**.
5. **[Zone 5: Intelligence & Serving Layer (MVP Implemented)](file:///Users/MacBook/Fun_Project/Fun-Project/documentation/Zone5.md)** — The reasoning brain and voice. Routes queries, plans graph traversals, performs analytics, and coordinates LLM-based reasoning.
6. **[Zone 6: Consumer Interfaces](file:///Users/MacBook/Fun_Project/Fun-Project/documentation/Zone6.md)** — Exposes visual dashboards, IDE integrations, CLI tools, and chat bots.

Refer to the main **[HighLevelArchi.md](file:///Users/MacBook/Fun_Project/Fun-Project/documentation/HighLevelArchi.md)** file for a deep dive into the complete architectural blueprint.

---

## 📦 Project Layout

The repository is structured as a Go multi-module workspace containing the primary MVP components:

* **[cmd/archgraph/](file:///Users/MacBook/Fun_Project/Fun-Project/cmd/archgraph)** — Supervisor tool to coordinate local development.
* **[documentation/](file:///Users/MacBook/Fun_Project/Fun-Project/documentation)** — High-level architecture and system design specs.
* **[zone4/](file:///Users/MacBook/Fun_Project/Fun-Project/zone4)** — Zone 4 (Graph Storage) Go module.
* **[zone5/](file:///Users/MacBook/Fun_Project/Fun-Project/zone5)** — Zone 5 (Intelligence & Serving Layer) Go module.

---

## ⚡ Quick Start: Running the Entire System

A supervisor tool is provided to start all implemented zones in their correct dependency order under a single terminal command. It starts Zone 4 (Graph Storage), waits for it to become healthy, and then boots Zone 5 (Intelligence Layer) pointing to Zone 4.

### Prerequisites

- **Go** (version 1.20 or newer recommended)
- **SQLite3**

### Execution

Run the following commands from the project root:

```bash
cd cmd/archgraph
go run . -root ../..
```

#### Available Flags for the Supervisor:
* `-root` — Path to the project root containing `zone4/` and `zone5/` (default `.`)
* `-zone4-port` — Port for the Zone 4 daemon (default `8080`)
* `-zone5-port` — Port for the Zone 5 daemon (default `8081`)
* `-db` — SQLite database path passed to Zone 4 (default `zone4.db`)
* `-ready-timeout` — Max time to wait for Zone 4 to become healthy (default `30s`)

Once running, you will see prefixed logs (`[zone4]` and `[zone5]`) interleaved readably in your terminal. On termination (`Ctrl+C`), both services will be gracefully shut down.

---

## 🟥 Zone 4 — Graph Storage (MVP)

Located in **[zone4/](file:///Users/MacBook/Fun_Project/Fun-Project/zone4)**, this is a single-process, SQLite-backed implementation of the graph storage layer.

### Key Features
* **Mutation API:** A single write entry point that handles batches of mutations, enforces schema validation, and performs optimistic locking.
* **Delta Log:** An append-only, monotonic, queryable transaction ledger that preserves absolute history.
* **Graph Projection:** Live `entities` and `relationships` tables in SQLite derived from the delta log.
* **Neighborhood Queries:** Graph traversals supporting N-hop neighborhood retrievals.

### Key Endpoints (Port 8080)
* `POST /v1/mutations` — Apply a batch of entity/relationship mutations.
* `GET /v1/entities/{id}` — Retrieve an entity by canonical ID.
* `GET /v1/entities/{id}/neighborhood?depth=N` — Retrieve N-hop relationship neighborhood.
* `GET /v1/log?from_entry_id=N&limit=M` — Read the raw delta log entries.

For more details, see the **[Zone 4 README](file:///Users/MacBook/Fun_Project/Fun-Project/zone4/README.md)**.

---

## 🟪 Zone 5 — Intelligence & Serving Layer (MVP)

Located in **[zone5/](file:///Users/MacBook/Fun_Project/Fun-Project/zone5)**, this service is the reasoning brain of the system. It sits on top of Zone 4 and translates graph facts into architectural intelligence.

### Key Features
* **Query Engine:** Parses incoming natural language questions and routes them to Query Archetypes (Structural, Runtime, Temporal, Impact, Governance).
* **Context Assembler:** Fetches relevant subgraphs using weight-based PageRank pruning and formats them into a serialized structure.
* **LLM Reasoner:** Orchestrates responses using LLM prompting (currently stubbed for local testing).
* **Analytical Engines:** 
  * **Blast Radius Engine:** Computes transitive downstream impact of changes.
  * **Health Auditor:** Detects architectural smells, circular dependencies (using Tarjan's Strongly Connected Components), and shared database coupling.
  * **Evolution Tracker:** Compares codebase state over time using delta log replays.

### Key Endpoints (Port 8081)
* `POST /v1/ask` — Ask natural language questions about the codebase.
* `GET /v1/blast-radius?id=X&depth=N` — Compute blast radius of changing entity `X`.
* `GET /v1/health-audit` — Scan the graph for cycles and microservice design violations.
* `GET /v1/diff?from=N&to=M` — Diff the architecture between two log sequences.

For more details, see the **[Zone 5 README](file:///Users/MacBook/Fun_Project/Fun-Project/zone5/README.md)**.

---

## 🛠️ Testing

Both modules are fully unit-tested. You can run tests for each module individually:

**Zone 4 Tests:**
```bash
cd zone4
go test ./...
```

**Zone 5 Tests:**
```bash
cd zone5
go test ./...
```
