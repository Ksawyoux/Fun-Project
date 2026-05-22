# 🟩 Zone 2: Ingestion Subsystem — System Design Deep Dive

---

## 🎯 Zone Responsibility Reminder

Zone 2 has one sacred job:

> Reach into Zone 1, collect every signal, normalize it into one unified format, and deliver it downstream — **reliably, completely, and without coupling the rest of the system to any source**

Everything that makes the system trustworthy starts here.
A bad ingestion layer = a bad graph = wrong answers.

---

## 🧱 Subsystem Components — Full Map

```
┌─────────────────────────────────────────────────────────────────┐
│                     INGESTION SUBSYSTEM                         │
│                                                                 │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │                  CONNECTIVITY LAYER                       │  │
│  │                                                           │  │
│  │   ┌─────────────┐  ┌─────────────┐  ┌─────────────┐     │  │
│  │   │    Pull     │  │    Push     │  │   Stream    │     │  │
│  │   │ Connectors  │  │ Connectors  │  │ Connectors  │     │  │
│  │   └──────┬──────┘  └──────┬──────┘  └──────┬──────┘     │  │
│  └──────────┼────────────────┼────────────────┼────────────┘  │
│             │                │                │               │
│             └────────────────┼────────────────┘               │
│                              │                                 │
│                              ▼                                 │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │                  ORCHESTRATION LAYER                      │  │
│  │                                                           │  │
│  │   ┌─────────────────────┐   ┌─────────────────────────┐  │  │
│  │   │  Ingestor Registry  │   │  Ingestion Scheduler    │  │  │
│  │   └─────────────────────┘   └─────────────────────────┘  │  │
│  │                                                           │  │
│  │   ┌─────────────────────────────────────────────────┐    │  │
│  │   │           Ingestion DAG Engine                  │    │  │
│  │   └─────────────────────────────────────────────────┘    │  │
│  └───────────────────────────────────────────────────────────┘  │
│                              │                                 │
│                              ▼                                 │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │                   EXECUTION LAYER                         │  │
│  │                                                           │  │
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐    │  │
│  │  │   Git    │ │   AST    │ │   API    │ │ Runtime  │    │  │
│  │  │ Ingestor │ │ Ingestor │ │ Ingestor │ │ Ingestor │    │  │
│  │  └──────────┘ └──────────┘ └──────────┘ └──────────┘    │  │
│  │                                                           │  │
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐                 │  │
│  │  │Ownership │ │  Infra   │ │ Incident │                 │  │
│  │  │ Ingestor │ │ Ingestor │ │ Ingestor │                 │  │
│  │  └──────────┘ └──────────┘ └──────────┘                 │  │
│  └───────────────────────────────────────────────────────────┘  │
│                              │                                 │
│                              ▼                                 │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │               NORMALIZATION LAYER                         │  │
│  │                                                           │  │
│  │   ┌─────────────────────────────────────────────────┐    │  │
│  │   │            NIF Transformer                      │    │  │
│  │   └─────────────────────────────────────────────────┘    │  │
│  │                                                           │  │
│  │   ┌─────────────────────────────────────────────────┐    │  │
│  │   │          Schema Validator                       │    │  │
│  │   └─────────────────────────────────────────────────┘    │  │
│  └───────────────────────────────────────────────────────────┘  │
│                              │                                 │
│                              ▼                                 │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │                  DELIVERY LAYER                           │  │
│  │                                                           │  │
│  │   ┌─────────────────────────────────────────────────┐    │  │
│  │   │           Ingestion Event Bus                   │    │  │
│  │   └─────────────────────────────────────────────────┘    │  │
│  │                                                           │  │
│  │   ┌──────────────────┐   ┌──────────────────────────┐   │  │
│  │   │  Dead Letter     │   │    Backpressure           │   │  │
│  │   │  Queue           │   │    Controller             │   │  │
│  │   └──────────────────┘   └──────────────────────────┘   │  │
│  └───────────────────────────────────────────────────────────┘  │
│                              │                                 │
│                              ▼                                 │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │                 OBSERVABILITY LAYER                       │  │
│  │                                                           │  │
│  │   ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │  │
│  │   │  Ingestion   │  │  Staleness   │  │    Health    │  │  │
│  │   │   Ledger     │  │   Monitor   │  │   Dashboard  │  │  │
│  │   └──────────────┘  └──────────────┘  └──────────────┘  │  │
│  └───────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

---

## 🔌 Layer 1: Connectivity Layer

**Responsibility:** Establish and maintain connection to every source

Three connector types because sources have fundamentally different communication models.

---

### Pull Connectors

For sources we go and fetch from.

```
┌────────────────────────────────────────────┐
│              PULL CONNECTOR                │
│                                            │
│  ┌──────────────────────────────────────┐  │
│  │         Auth Manager                │  │
│  │  tokens, keys, OAuth, SSH           │  │
│  └──────────────────────────────────────┘  │
│                                            │
│  ┌──────────────────────────────────────┐  │
│  │       Checkpoint Manager            │  │
│  │  last cursor, last SHA, last hash   │  │
│  └──────────────────────────────────────┘  │
│                                            │
│  ┌──────────────────────────────────────┐  │
│  │         Rate Limiter                │  │
│  │  respect source API limits          │  │
│  └──────────────────────────────────────┘  │
│                                            │
│  ┌──────────────────────────────────────┐  │
│  │         Retry Engine                │  │
│  │  exponential backoff, max attempts  │  │
│  └──────────────────────────────────────┘  │
└────────────────────────────────────────────┘
```

Sources using Pull:
- Git repositories
- OpenAPI spec files
- IaC directories
- Package manifests
- Metrics aggregates

#### Checkpoint Manager — Critical Design:

Every pull connector must track where it left off.

Why?
- Avoid re-processing everything on each run
- Enable incremental ingestion
- Support resumption after failure

Per source, checkpoint looks different:

| Source | Checkpoint |
|--------|-----------|
| Git | Last processed commit SHA |
| OpenAPI | File content hash |
| IaC | File content hash + timestamp |
| Metrics | Last timestamp window |
| Packages | Manifest file hash |

Checkpoints are:
- Stored durably (not in memory)
- Updated only after successful processing
- Versioned (so rollback is possible)
- Source-scoped (each source has its own)

---

### Push Connectors

For sources that notify us when something happens.

```
┌────────────────────────────────────────────┐
│              PUSH CONNECTOR                │
│                                            │
│  ┌──────────────────────────────────────┐  │
│  │         Webhook Listener            │  │
│  │  HTTP endpoint, always-on           │  │
│  └──────────────────────────────────────┘  │
│                                            │
│  ┌──────────────────────────────────────┐  │
│  │       Signature Verifier            │  │
│  │  HMAC, token validation             │  │
│  └──────────────────────────────────────┘  │
│                                            │
│  ┌──────────────────────────────────────┐  │
│  │       Deduplication Buffer          │  │
│  │  idempotency key tracking           │  │
│  └──────────────────────────────────────┘  │
│                                            │
│  ┌──────────────────────────────────────┐  │
│  │         Event Queue                 │  │
│  │  buffer before processing           │  │
│  └──────────────────────────────────────┘  │
└────────────────────────────────────────────┘
```

Sources using Push:
- GitHub/GitLab webhooks (push events)
- CI/CD pipeline completion events
- Incident management webhooks
- Deployment events

#### Deduplication Buffer — Why It Matters:

Push sources can fire the same event multiple times:
- Network retry from source
- Duplicate webhook delivery
- Our own retry on failure

Without deduplication:
→ Same commit ingested multiple times
→ Graph gets duplicate edges
→ Evolution tracker sees phantom changes

Strategy:
- Every incoming event gets an idempotency key
- Key = hash(source + event_type + event_id)
- Buffer holds keys for TTL window
- Duplicate key = discard silently

---

### Stream Connectors

For high-volume, continuous sources.

```
┌────────────────────────────────────────────┐
│             STREAM CONNECTOR               │
│                                            │
│  ┌──────────────────────────────────────┐  │
│  │         Stream Consumer             │  │
│  │  Kafka, Kinesis, OTLP receiver      │  │
│  └──────────────────────────────────────┘  │
│                                            │
│  ┌──────────────────────────────────────┐  │
│  │       Sampling Controller           │  │
│  │  adaptive, priority, tail-based     │  │
│  └──────────────────────────────────────┘  │
│                                            │
│  ┌──────────────────────────────────────┐  │
│  │       Aggregation Window            │  │
│  │  time-based, count-based            │  │
│  └──────────────────────────────────────┘  │
│                                            │
│  ┌──────────────────────────────────────┐  │
│  │         Batch Emitter               │  │
│  │  emit aggregated records            │  │
│  └──────────────────────────────────────┘  │
└────────────────────────────────────────────┘
```

Sources using Stream:
- Distributed tracing (OpenTelemetry)
- Real-time metrics
- APM data streams

#### Sampling Controller — Critical Design:

We cannot ingest every span.

Sampling strategies layered:

```
Incoming Spans
      │
      ▼
┌─────────────────────┐
│  Priority Sampler   │  ← Always keep: errors, slow traces
└──────────┬──────────┘
           │
           ▼
┌─────────────────────┐
│  Adaptive Sampler   │  ← Adjust rate by traffic volume
└──────────┬──────────┘
           │
           ▼
┌─────────────────────┐
│  Aggregation Window │  ← Collapse to edge statistics
└──────────┬──────────┘
           │
           ▼
Edge-level summary:
  ServiceA → ServiceB:
    call_count: 45,000
    p50: 23ms
    p99: 187ms
    error_rate: 0.2%
    window: last 5 minutes
```

We keep statistics, not raw spans.
This reduces volume by orders of magnitude.

---

## ⚙️ Layer 2: Orchestration Layer

**Responsibility:** Know what to run, when to run it, in what order, and manage its lifecycle

---

### Ingestor Registry

A catalog of everything the system knows how to ingest.

Each registered ingestor has:

```
Registration Record:
  id:               unique identifier
  name:             human readable
  source_type:      git | ast | api | runtime | ...
  connector_type:   pull | push | stream
  version:          ingestor version
  config_schema:    what configuration it needs
  health_status:    healthy | degraded | failed
  last_run:         timestamp
  next_run:         scheduled timestamp
  enabled:          boolean
  dependencies:     other ingestors that must run first
```

Why track dependencies?

Because ingestion order matters:

```
GitIngestor must complete
    before
ASTIngestor runs
    (needs files pulled first)

ASTIngestor must complete
    before
OwnershipIngestor enriches
    (needs module structure first)
```

---

### Ingestion Scheduler

Decides when each ingestor runs.

Three trigger modes:

#### Time-based
```
GitIngestor      → every 15 minutes
OwnershipIngestor → every 24 hours
MetricsIngestor  → every 5 minutes
```

#### Event-based
```
On git push event received
  → trigger GitIngestor immediately for that repo

On deployment event received
  → trigger APIIngestor for that service
  → trigger InfraIngestor for that environment
```

#### Dependency-based
```
When GitIngestor completes for repo X
  → trigger ASTIngestor for repo X
  → trigger DependencyIngestor for repo X
```

The scheduler resolves all three trigger types and produces an **execution plan**.

---

### Ingestion DAG Engine

This is the most sophisticated part of orchestration.

Ingestion jobs form a **Directed Acyclic Graph**:

```
         GitIngestor
              │
    ┌─────────┴──────────┐
    ▼                    ▼
ASTIngestor      DependencyIngestor
    │                    │
    └─────────┬──────────┘
              ▼
      OwnershipIngestor
              │
              ▼
       EnrichmentReady
```

The DAG engine:
- Resolves execution order
- Runs independent branches in parallel
- Waits for dependencies before proceeding
- Handles partial failures gracefully
- Retries failed nodes without rerunning completed ones

#### Failure Handling in DAG:

```
GitIngestor ✅ succeeds
    │
    ├── ASTIngestor ❌ fails
    │       │
    │       └── Retry ASTIngestor (not GitIngestor)
    │           If retry fails:
    │           → Mark ASTIngestor as failed
    │           → Skip dependent nodes
    │           → Flag affected graph areas as stale
    │
    └── DependencyIngestor ✅ succeeds
```

Critical design principle:

> **A failed ingestor should never block unrelated ingestors**

Git failure doesn't stop metrics ingestion.
AST failure doesn't stop ownership ingestion.

---

## 🏭 Layer 3: Execution Layer

**Responsibility:** Actually fetch and extract raw data from sources

Every ingestor implements the same contract:

```
Ingestor Contract:

  identify()
    → returns: ingestor metadata

  validate_config(config)
    → returns: valid | invalid + reasons

  check_connectivity()
    → returns: reachable | unreachable + reason

  load_checkpoint()
    → returns: last known position

  fetch(checkpoint)
    → returns: raw records iterator

  save_checkpoint(position)
    → returns: confirmation

  report_health()
    → returns: health metrics
```

This contract is what makes the system pluggable.
A new source = implement this contract = done.
Nothing else in the system changes.

---

### Git Ingestor — Design Detail

```
┌─────────────────────────────────────────┐
│             GIT INGESTOR                │
│                                         │
│  Input: repo list + last SHA checkpoint │
│                                         │
│  ┌─────────────────────────────────┐    │
│  │       Repo Enumerator           │    │
│  │  discover all configured repos  │    │
│  └──────────────┬──────────────────┘    │
│                 │                       │
│                 ▼                       │
│  ┌─────────────────────────────────┐    │
│  │      Delta Fetcher              │    │
│  │  commits since last checkpoint  │    │
│  └──────────────┬──────────────────┘    │
│                 │                       │
│                 ▼                       │
│  ┌─────────────────────────────────┐    │
│  │    File Tree Extractor          │    │
│  │  structure, paths, sizes        │    │
│  └──────────────┬──────────────────┘    │
│                 │                       │
│                 ▼                       │
│  ┌─────────────────────────────────┐    │
│  │    Blame Extractor              │    │
│  │  per-file ownership signals     │    │
│  └──────────────┬──────────────────┘    │
│                 │                       │
│                 ▼                       │
│  ┌─────────────────────────────────┐    │
│  │    Change Frequency Analyzer    │    │
│  │  commit velocity per module     │    │
│  └──────────────┬──────────────────┘    │
│                 │                       │
│  Output: raw git records → NIF layer    │
└─────────────────────────────────────────┘
```

#### Monorepo Handling:

Monorepos need special treatment:

```
Repository: backend-monorepo
    │
    ├── /services/payments/
    ├── /services/auth/
    ├── /services/orders/
    ├── /libs/shared-utils/
    └── /libs/db-client/
```

Strategy:
- Detect service boundaries (by config or convention)
- Track changes **per sub-project**, not per repo
- Assign independent checkpoints per sub-project
- Generate separate entity records per sub-project

---

### AST Ingestor — Design Detail

```
┌─────────────────────────────────────────┐
│             AST INGESTOR                │
│                                         │
│  Input: file paths from GitIngestor     │
│                                         │
│  ┌─────────────────────────────────┐    │
│  │       Language Detector         │    │
│  │  file extension + content hints │    │
│  └──────────────┬──────────────────┘    │
│                 │                       │
│                 ▼                       │
│  ┌─────────────────────────────────┐    │
│  │       Parser Dispatcher         │    │
│  │  routes to language parser      │    │
│  └──────────────┬──────────────────┘    │
│                 │                       │
│        ┌────────┼────────┐             │
│        ▼        ▼        ▼             │
│  ┌──────────┐ ┌───────┐ ┌──────────┐  │
│  │ Python   │ │  TS   │ │   Go     │  │
│  │ Parser   │ │Parser │ │  Parser  │  │
│  └────┬─────┘ └───┬───┘ └────┬─────┘  │
│       └───────────┼───────────┘        │
│                   ▼                    │
│  ┌─────────────────────────────────┐   │
│  │    Universal AST Normalizer     │   │
│  │  all parsers output same shape  │   │
│  └──────────────┬──────────────────┘   │
│                 │                      │
│  Output: module graph, call graph,     │
│          import graph → NIF layer      │
└─────────────────────────────────────────┘
```

#### Per-file Isolation:

Each file is parsed independently.

Why?
- One file failing doesn't block others
- Partial results are better than no results
- Failed files are flagged, not silently dropped

---

### Runtime Ingestor — Design Detail

```
┌─────────────────────────────────────────┐
│           RUNTIME INGESTOR              │
│                                         │
│  Input: continuous trace stream         │
│                                         │
│  ┌─────────────────────────────────┐    │
│  │      OTLP Span Receiver         │    │
│  │  OpenTelemetry protocol         │    │
│  └──────────────┬──────────────────┘    │
│                 │                       │
│                 ▼                       │
│  ┌─────────────────────────────────┐    │
│  │      Sampling Gate              │    │
│  │  priority + adaptive sampling   │    │
│  └──────────────┬──────────────────┘    │
│                 │                       │
│                 ▼                       │
│  ┌─────────────────────────────────┐    │
│  │      Span Aggregator            │    │
│  │  collapse spans to edge stats   │    │
│  │  5-minute tumbling windows      │    │
│  └──────────────┬──────────────────┘    │
│                 │                       │
│                 ▼                       │
│  ┌─────────────────────────────────┐    │
│  │      Anomaly Detector           │    │
│  │  flag unusual latency/errors    │    │
│  └──────────────┬──────────────────┘    │
│                 │                       │
│  Output: edge statistics, anomaly       │
│          flags → NIF layer              │
└─────────────────────────────────────────┘
```

---

## 🔄 Layer 4: Normalization Layer

**Responsibility:** Transform everything into NIF

---

### NIF Schema — Full Design

Every record that exits the ingestion subsystem is one of two types:

#### Entity Record:

```
NIF Entity:
  │
  ├── id                  (deterministic, stable)
  ├── type                (Service | Module | Function | API | ...)
  ├── sub_type            (Microservice | BFF | Worker | ...)
  ├── name                (canonical name)
  ├── raw_name            (original name from source)
  ├── namespace           (org / team / domain scope)
  │
  ├── source_info
  │     ├── source_type   (git | ast | api | runtime | ...)
  │     ├── source_id     (which specific source)
  │     ├── source_ref    (original identifier in source)
  │     └── observed_at   (when this was seen)
  │
  ├── properties          (type-specific key-value bag)
  │
  ├── confidence          (0.0 - 1.0)
  ├── is_partial          (was full fetch possible?)
  └── ingestion_run_id    (which run produced this)
```

#### Relationship Record:

```
NIF Relationship:
  │
  ├── id                  (deterministic, stable)
  ├── type                (CALLS | IMPORTS | OWNS | ...)
  ├── from_entity_id      (source entity)
  ├── to_entity_id        (target entity)
  │
  ├── source_info
  │     ├── source_type
  │     ├── source_id
  │     ├── source_ref
  │     └── observed_at
  │
  ├── properties          (strength, frequency, latency, ...)
  │
  ├── confidence          (0.0 - 1.0)
  ├── is_inferred         (explicitly seen or inferred?)
  └── ingestion_run_id
```

---

### Deterministic ID Generation

This deserves focused attention.

IDs must be:
- Same every time for the same real entity
- Different for different entities
- Collision-resistant

Strategy:

```
Entity ID = hash(
  source_type +
  source_id +
  entity_type +
  canonical_name +
  namespace
)

Relationship ID = hash(
  relationship_type +
  from_entity_id +
  to_entity_id +
  source_type
)
```

Using a stable hash function (SHA-256, truncated).

Why namespace in entity ID?

Because:
```
"PaymentService" in org A ≠ "PaymentService" in org B
"utils" module in repo A ≠ "utils" module in repo B
```

Namespace scopes identity.

---

### Schema Validator

Before any NIF record exits the normalization layer:

Validates:
- Required fields present
- Types match schema
- IDs are correctly formatted
- Confidence within bounds
- Relationship endpoints exist (or are expected)

Invalid records:
- Never proceed downstream
- Go to Dead Letter Queue
- Alert raised
- Ingestion ledger updated

---

## 📨 Layer 5: Delivery Layer

**Responsibility:** Get NIF records to downstream consumers reliably

---

### Ingestion Event Bus

The bridge between Zone 2 and Zone 3.

#### Topic Design:

```
Topics:
  ingestion.entities.raw
  ingestion.relationships.raw
  ingestion.entities.deleted
  ingestion.relationships.deleted
  ingestion.errors
  ingestion.health.heartbeats
```

Entities and relationships on separate topics because:
- Different consumption rates
- Different priority
- Processing pipeline needs entities before relationships
- Allows independent scaling

#### Ordering Guarantees:

Within a single ingestion run:
- Entities published before relationships
- Relationships only reference entities in same or prior run

Why?

If relationships arrive before their entities:
→ Processing pipeline sees dangling references
→ Resolution fails
→ Relationships dropped or held

---

### Dead Letter Queue

For records that fail at any point in ingestion.

Every failed record carries:
- Original NIF record
- Failure reason
- Failure stage (connector | parsing | normalization | delivery)
- Failure timestamp
- Retry count
- Source ingestion run ID

DLQ enables:
- Investigation without data loss
- Manual reprocessing after fix
- Failure pattern analysis
- SLA tracking on data completeness

---

### Backpressure Controller

Protects downstream from being overwhelmed.

```
┌─────────────────────────────────────┐
│        BACKPRESSURE CONTROLLER      │
│                                     │
│  Monitor event bus queue depth      │
│              │                      │
│  If depth > HIGH_WATERMARK:         │
│    → Slow down pull connectors      │
│    → Increase sampling on streams   │
│    → Pause non-critical ingestors   │
│              │                      │
│  If depth > CRITICAL_WATERMARK:     │
│    → Pause all non-critical runs    │
│    → Alert operations team          │
│    → Switch to emergency mode       │
└─────────────────────────────────────┘
```

Two watermarks:
- **High watermark** → start throttling
- **Critical watermark** → stop non-essential ingestion

This prevents:
- Event bus overflow
- Processing pipeline starvation
- Cascading failures

---

## 👁️ Layer 6: Observability Layer

**Responsibility:** Make everything that happens in ingestion visible

---

### Ingestion Ledger

Append-only audit log of every ingestion activity.

Every run produces a ledger entry:

```
Ledger Entry:
  │
  ├── run_id               (unique run identifier)
  ├── ingestor_id
  ├── source_id
  ├── trigger_type         (scheduled | event | dependency)
  │
  ├── timing
  │     ├── started_at
  │     ├── completed_at
  │     └── duration_ms
  │
  ├── counts
  │     ├── records_fetched
  │     ├── records_transformed
  │     ├── records_emitted
  │     ├── records_failed
  │     └── records_skipped
  │
  ├── checkpoint
  │     ├── previous
  │     └── current
  │
  ├── errors               (list of errors encountered)
  ├── status               (success | partial | failed)
  └── downstream_ack       (confirmed by processing pipeline?)
```

---

### Staleness Monitor

Tracks freshness of every signal source.

```
For each source:
  last_successful_ingestion: timestamp
  expected_frequency: duration
  staleness_threshold: duration
  current_status: fresh | stale | critical

Alert if:
  now - last_successful_ingestion > staleness_threshold
```

Staleness is surfaced:
- On graph nodes affected by stale source
- In health dashboard
- In query responses ("Note: this data may be stale")

This is crucial.

Without staleness tracking:
→ Developers trust outdated information
→ Wrong decisions get made

---

### Health Dashboard Signals

Every component emits:

| Signal | What It Measures |
|--------|----------------|
| Ingestion rate | Records per second per source |
| Success rate | % runs completing successfully |
| Latency | Time from source change to NIF delivery |
| Error rate | Failed records per run |
| Queue depth | Event bus saturation |
| Staleness | Hours since last successful run per source |
| DLQ size | Backlog of failed records |

---

## 🔄 End-to-End Flow: One Ingestion Run

Let's trace exactly what happens when a developer pushes code:

```
1. Developer pushes to GitHub

2. GitHub fires webhook → Push Connector receives it

3. Signature verified, deduplicated

4. Event queued internally

5. Scheduler receives event
   → Triggers GitIngestor immediately for that repo

6. DAG Engine checks:
   → GitIngestor has no dependencies
   → Can run immediately

7. GitIngestor runs:
   → Loads checkpoint (last SHA)
   → Fetches commits since last SHA
   → Extracts file tree delta
   → Extracts blame for changed files
   → Saves new checkpoint

8. Raw git records → NIF Transformer
   → Each file becomes Entity record
   → Each ownership mapping becomes Relationship record
   → All get deterministic IDs

9. Schema Validator checks all records
   → Invalid records → DLQ
   → Valid records proceed

10. Records published to Event Bus
    → ingestion.entities.raw
    → ingestion.relationships.raw

11. Ledger updated with run results

12. DAG Engine sees GitIngestor complete
    → Triggers ASTIngestor for changed files
    → Triggers DependencyIngestor for changed manifests

13. Same flow repeats for AST and Dependency ingestors

14. Zone 3 (Processing Pipeline) consuming from Event Bus
```

Total latency target:
> Source change → NIF on event bus: **under 2 minutes**

---

## ⚠️ Failure Scenarios & Responses

| Scenario | Detection | Response |
|----------|-----------|----------|
| Source unreachable | Connector health check fails | Retry with backoff, mark stale |
| Partial repo access | 403 on specific paths | Ingest what's accessible, flag gaps |
| AST parse crash on file | Exception caught per-file | Skip file, log to DLQ, continue |
| Rate limit hit | 429 response | Pause, respect Retry-After header |
| NIF validation fails | Schema validator rejects | DLQ, alert, continue other records |
| Event bus full | High watermark breached | Throttle connectors, alert |
| Checkpoint corruption | Checksum mismatch | Roll back to previous checkpoint |
| DAG cycle detected | Dependency analysis | Fail fast, alert, block deployment |
| Deduplication buffer full | Memory pressure | Expand buffer, alert |

---

## 🔑 Design Decisions Summary

| Decision | Choice | Reasoning |
|----------|--------|-----------|
| Connector types | Pull / Push / Stream | Match source nature exactly |
| Ingestion ordering | DAG-based | Dependencies respected automatically |
| Internal format | NIF | Complete source isolation |
| ID generation | Deterministic hash | Idempotency guaranteed |
| Runtime data | Aggregate only | Volume manageable |
| Delivery | Event bus | Decoupling + backpressure |
| Failures | DLQ + partial success | No silent data loss |
| Observability | Ledger + staleness | Full audit trail |
| Backpressure | Watermark-based throttle | Downstream protection |

---

## 🧠 Mental Model

Think of Zone 2 as:

> **A highly disciplined customs operation at a border**

Every signal entering the system:
- Gets checked at the right checkpoint (connector)
- Gets processed by the right officer (ingestor)
- Gets translated to the official language (NIF)
- Gets stamped and recorded (ledger)
- Gets delivered to the right destination (event bus)

Nothing enters unprocessed.
Nothing enters unrecorded.
Nothing enters without a proper identity (deterministic ID).

The rest of the system speaks only NIF.
Zone 1 never bleeds into Zone 3.


