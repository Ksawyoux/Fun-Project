# 🏗️ AI Codebase Knowledge Graph

Welcome to the **AI Codebase Knowledge Graph** repository. This project is a state-of-the-art system designed to capture, store, analyze, and query structural, historical, and runtime details of a software codebase. It turns static code symbols, ownership metadata, runtime telemetry, and Git commit history into a unified, living property graph that can be queried in natural language.

---

## 🗺️ High-Level Architecture (The 6 Zones)

The system is organized into **6 logical zones**, running from signal capture all the way to user interaction interfaces. Here is how they connect:

```mermaid
flowchart TD
    subgraph Zone1 ["Zone 1: Signal Sources"]
        Git["Git Repositories"]
        AST["AST Code Parsers"]
        APIs["API Specifications"]
        Trace["Distributed Tracing"]
        Metrics["Metrics & APM"]
    end

    subgraph Zone2 ["Zone 2: Ingestion Subsystem"]
        Connectors["Source Connectors"]
        Ingestors["Ingestor Pool"]
        NIF["NIF Normalizer"]
        Ledger["Ingestion Ledger & Event Bus"]
    end

    subgraph Zone3 ["Zone 3: Processing Pipeline"]
        Parse["Parse & Classify"]
        Resolve["Entity ID Resolver"]
        Infer["Relationship Inferrer"]
        Enrich["Context Enricher"]
        Delta["Delta Compute"]
    end

    subgraph Zone4 ["Zone 4: Graph Storage"]
        MutationAPI["Mutation API"]
        GraphDB["Live Graph DB (SQLite)"]
        DeltaLog["Delta Log (Source of Truth)"]
    end

    subgraph Zone5 ["Zone 5: Intelligence & Serving"]
        QueryEngine["Query Engine"]
        Analytics["Specialized Analytics (Blast Radius & Health SCC)"]
        LLMReason["LLM Reasoner & Context Assembler"]
        PublicAPI["Public API (REST)"]
    end

    subgraph Zone6 ["Zone 6: Consumer Interfaces"]
        IDE["IDE Plugins"]
        WebDash["Web Dashboard"]
        CLI["CLI Tool & CI Gates"]
    end

    %% Data flow connections
    Git & AST & APIs & Trace & Metrics --> Connectors
    Connectors --> Ingestors --> NIF --> Ledger
    Ledger --> Parse --> Resolve --> Infer --> Enrich --> Delta
    Delta --> MutationAPI
    MutationAPI --> GraphDB & DeltaLog
    GraphDB & DeltaLog --> QueryEngine & Analytics & LLMReason
    QueryEngine & Analytics & LLMReason --> PublicAPI
    PublicAPI --> IDE & WebDash & CLI

    %% Styling
    style Zone1 fill:#e3f2fd,stroke:#1565c0,stroke-width:2px,color:#0d47a1
    style Zone2 fill:#e8f5e9,stroke:#2e7d32,stroke-width:2px,color:#1b5e20
    style Zone3 fill:#fffde7,stroke:#fbc02d,stroke-width:2px,color:#f57f17
    style Zone4 fill:#ffebee,stroke:#c62828,stroke-width:2px,color:#b71c1c
    style Zone5 fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,color:#4a148c
    style Zone6 fill:#efebe9,stroke:#4e342e,stroke-width:2px,color:#3e2723

    classDef default fill:#ffffff,stroke:#333,stroke-width:1px;
```

---

## 🔄 End-to-End Data Flow

The sequence below illustrates how a change in the codebase propagates through the architecture to update the serving layer:

```mermaid
sequenceDiagram
    autonumber
    actor Dev as Developer / Codebase
    participant Z1 as Zone 1: Signal Sources
    participant Z2 as Zone 2: Ingestion
    participant Z3 as Zone 3: Pipeline
    participant Z4 as Zone 4: Graph Storage
    participant Z5 as Zone 5: Intelligence

    Dev->>Z1: Code Push / API change / Trace hit
    Z1->>Z2: Emit raw event
    Z2->>Z2: Normalize to NIF (Unified Format)
    Z2->>Z3: Publish to Event Bus
    Z3->>Z3: Parse, Resolve Entity & Infer Relationships
    Z3->>Z3: Validate and Compute Delta Mutation
    Z3->>Z4: POST /v1/mutations (Batch)
    Z4->>Z4: Write to Delta Log (Truth) & project to SQLite (Graph)
    Dev->>Z5: Natural Language Query (e.g., "What depends on X?")
    Z5->>Z4: Query entities/neighborhood
    Z4-->>Z5: Return structural subgraphs
    Z5->>Z5: LLM Context Assembly & Analysis (Blast Radius/Cycles)
    Z5-->>Dev: Formatted answer with Mermaid & Citations
```

---

## 📦 Project Layout

The repository is structured as a Go multi-module workspace containing the primary MVP components:

* **[cmd/archgraph/](file:///Users/MacBook/Fun_Project/Fun-Project/cmd/archgraph)** — Supervisor tool to coordinate local development.
* **[documentation/](file:///Users/MacBook/Fun_Project/Fun-Project/documentation)** — High-level architecture and system design specs.
  * **[HighLevelArchi.md](file:///Users/MacBook/Fun_Project/Fun-Project/documentation/HighLevelArchi.md)** — Architectural Overview.
  * **[Zone 1 Specifications](file:///Users/MacBook/Fun_Project/Fun-Project/documentation/Zone1.md)** | **[Zone 2 Specifications](file:///Users/MacBook/Fun_Project/Fun-Project/documentation/Zone2.md)** | **[Zone 3 Specifications](file:///Users/MacBook/Fun_Project/Fun-Project/documentation/Zone3.md)**
  * **[Zone 4 Specifications](file:///Users/MacBook/Fun_Project/Fun-Project/documentation/Zone4.md)** | **[Zone 5 Specifications](file:///Users/MacBook/Fun_Project/Fun-Project/documentation/Zone5.md)** | **[Zone 6 Specifications](file:///Users/MacBook/Fun_Project/Fun-Project/documentation/Zone6.md)**
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
  * **Health Auditor:** Detects circular dependencies (using Tarjan's Strongly Connected Components) and shared database coupling.
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
