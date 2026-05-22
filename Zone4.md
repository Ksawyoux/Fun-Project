# 🟥 Zone 4: Graph Storage — System Design Deep Dive

---

## 🎯 Zone Responsibility

Zone 4 has one sacred job:

> Be the **single source of truth** for everything the system knows about the architecture — structurally, historically, and contextually — and serve it reliably, consistently, and at scale

This is not just a database.

This is the **architectural memory** of the entire system.

Every question the system answers comes from here.
Every mutation the pipeline produces lands here.
Every query the intelligence layer asks runs against here.

Get this wrong and everything else fails.

---

## 🧠 Core Design Philosophy

Four principles govern every decision in Zone 4:

### Principle 1: The Delta Log Is the Truth
The graph database is a **projection** of the delta log.
Not the other way around.

If the graph DB corrupts:
→ Replay the delta log → graph restored

If the delta log corrupts:
→ You have lost history permanently

The delta log is sacred. The graph DB is derived.

### Principle 2: Reads Must Be Fast
Developers are waiting for answers.
Query latency must be sub-second for common paths.
Architecture decisions are made based on query results.
Slow reads = wrong decisions = system not used.

### Principle 3: Writes Must Be Consistent
A partial write is worse than no write.
An inconsistent graph produces wrong answers confidently.
Wrong confident answers are more dangerous than no answers.

### Principle 4: History Is Not Optional
The graph must answer:
- "What did the architecture look like 6 months ago?"
- "When did this dependency appear?"
- "What changed before this incident?"

History is a first-class feature, not an afterthought.

---

## 🏗️ Full Zone 4 Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                       GRAPH STORAGE                             │
│                                                                 │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │                 WRITE PATH                              │   │
│  │                                                         │   │
│  │   ┌──────────────────────────────────────────────────┐  │   │
│  │   │              Graph Mutation API                  │  │   │
│  │   │         (single, unified write entry point)      │  │   │
│  │   └──────────────────────┬───────────────────────────┘  │   │
│  │                          │                              │   │
│  │          ┌───────────────┼───────────────┐             │   │
│  │          ▼               ▼               ▼             │   │
│  │   ┌────────────┐  ┌────────────┐  ┌───────────────┐   │   │
│  │   │  Schema    │  │  Conflict  │  │  Transaction  │   │   │
│  │   │ Enforcer   │  │  Detector  │  │  Coordinator  │   │   │
│  │   └────────────┘  └────────────┘  └───────────────┘   │   │
│  └─────────────────────────────────────────────────────────┘   │
│                              │                                  │
│                              ▼                                  │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │                  STORAGE CORE                           │   │
│  │                                                         │   │
│  │  ┌──────────────────────┐  ┌──────────────────────────┐ │   │
│  │  │                      │  │                          │ │   │
│  │  │     Graph DB         │  │     Delta / Event Log    │ │   │
│  │  │   (live graph)       │  │     (evolution truth)    │ │   │
│  │  │                      │  │                          │ │   │
│  │  │  Property Graph      │  │  Append-only             │ │   │
│  │  │  Temporal edges      │  │  Ordered                 │ │   │
│  │  │  Multi-tier          │  │  Immutable               │ │   │
│  │  │                      │  │                          │ │   │
│  │  └──────────────────────┘  └──────────────────────────┘ │   │
│  │                                                         │   │
│  │  ┌──────────────────────┐  ┌──────────────────────────┐ │   │
│  │  │                      │  │                          │ │   │
│  │  │   Snapshot Store     │  │   Runtime Metrics Store  │ │   │
│  │  │  (point-in-time)     │  │   (time-series)          │ │   │
│  │  │                      │  │                          │ │   │
│  │  └──────────────────────┘  └──────────────────────────┘ │   │
│  └─────────────────────────────────────────────────────────┘   │
│                              │                                  │
│                              ▼                                  │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │                  READ PATH                              │   │
│  │                                                         │   │
│  │  ┌──────────────────────┐  ┌──────────────────────────┐ │   │
│  │  │   Search Index       │  │   Graph Cache            │ │   │
│  │  │  (entity lookup)     │  │   (hot subgraphs)        │ │   │
│  │  └──────────────────────┘  └──────────────────────────┘ │   │
│  │                                                         │   │
│  │  ┌──────────────────────────────────────────────────┐   │   │
│  │  │              Query Router                        │   │   │
│  │  │  (decides which store answers which query)       │   │   │
│  │  └──────────────────────────────────────────────────┘   │   │
│  └─────────────────────────────────────────────────────────┘   │
│                              │                                  │
│                              ▼                                  │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │              CROSS-CUTTING CONCERNS                     │   │
│  │                                                         │   │
│  │  ┌──────────────┐  ┌──────────────┐  ┌─────────────┐   │   │
│  │  │  Consistency │  │  Partition   │  │ Observability│   │   │
│  │  │  Manager     │  │  Manager     │  │  Layer       │   │   │
│  │  └──────────────┘  └──────────────┘  └─────────────┘   │   │
│  └─────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

---

## ✍️ Write Path

### Graph Mutation API

The single entry point for all writes.

Nothing writes to the graph directly.
Not the pipeline.
Not the intelligence layer.
Not the admin tools.

Everything goes through the Mutation API.

Why?

```
Benefits of single write entry point:

  Consistency enforcement:
    Every write validated before hitting storage

  Audit trail centralization:
    Every write logged in one place

  Schema enforcement:
    Impossible to write malformed data

  Event emission:
    Every write triggers downstream events

  Cache invalidation:
    Exactly the right caches cleared

  Metrics:
    Single point for write throughput, latency, errors
```

#### Mutation Operations Catalog:

```
Entity Operations:
  upsert_entity(entity, context)
    → Create if not exists, update if exists
    → Returns: entity_id, operation_type

  soft_delete_entity(entity_id, reason, context)
    → Mark inactive, preserve all history
    → Returns: confirmation, affected_relationships

  merge_entities(source_id, target_id, confidence, context)
    → Combine two entities into one canonical
    → Redirect all relationships from source to target
    → Returns: canonical_id, merged_properties

  split_entity(entity_id, split_spec, context)
    → Divide one entity into two
    → Distribute relationships per split_spec
    → Returns: [entity_id_a, entity_id_b]

  restore_entity(entity_id, context)
    → Reactivate a soft-deleted entity
    → Returns: entity_id, restored_at

Relationship Operations:
  upsert_relationship(relationship, context)
    → Create if not exists, update if exists
    → Returns: relationship_id, operation_type

  delete_relationship(relationship_id, reason, context)
    → Hard or soft delete based on policy
    → Returns: confirmation

  update_relationship_strength(relationship_id, strength, context)
    → Runtime strength updates (high frequency)
    → Optimized path (bypass full validation)
    → Returns: confirmation

Batch Operations:
  apply_mutation_batch(mutations[], context)
    → Ordered batch of mutations
    → Transactional: all succeed or compensate
    → Returns: batch_result, individual_results[]
```

---

### Schema Enforcer

Every write passes through schema enforcement before hitting storage.

```
Schema Enforcement Rules:

Entity Schema:
  ├── id: required, string, deterministic format
  ├── type: required, enum(EntityType)
  ├── sub_type: optional, enum per parent type
  ├── canonical_name: required, string, max 256
  ├── namespace: required, string
  ├── confidence: required, float, 0.0-1.0
  ├── first_seen: required, timestamp
  ├── last_seen: required, timestamp
  └── properties: optional, max 500 keys

Relationship Schema:
  ├── id: required, string, deterministic format
  ├── type: required, enum(RelationshipType)
  ├── from_id: required, must exist in graph
  ├── to_id: required, must exist in graph
  ├── confidence: required, float, 0.0-1.0
  ├── valid_from: required, timestamp
  ├── valid_to: optional, timestamp (null = active)
  └── properties: optional, max 200 keys

Violations:
  → Reject immediately
  → Return structured error with field-level detail
  → Log to mutation error store
  → Never reach storage layer
```

---

### Conflict Detector

Checks for write conflicts before committing.

```
Conflict Types:

  Concurrent write conflict:
    Two mutations targeting same entity simultaneously
    Detection: optimistic locking with version tokens
    Resolution: retry with merged state

  Semantic conflict:
    Mutation violates domain rules
    (e.g., changing entity type)
    Detection: rule evaluation pre-write
    Resolution: reject with explanation

  Temporal conflict:
    Mutation timestamps out of order
    (e.g., older observation arriving late)
    Detection: compare timestamps
    Resolution: apply with historical timestamp,
               do not override newer state

  Relationship endpoint conflict:
    Relationship references deleted entity
    Detection: entity existence check
    Resolution: hold relationship, alert
```

---

### Transaction Coordinator

Ensures multi-entity, multi-relationship mutations are atomic.

```
Transaction Protocol:

  Begin transaction
    → Acquire locks on affected entities
    → Set transaction ID

  Validate all mutations in transaction
    → Schema check
    → Conflict check
    → Semantic check

  Execute all mutations
    → Graph DB writes
    → Delta log appends (same transaction)

  Commit
    → Release locks
    → Emit mutation events
    → Invalidate caches
    → Update indexes

  On any failure:
    → Rollback all graph DB writes
    → Do NOT rollback delta log
      (append compensation record instead)
    → Release locks
    → Return structured error
```

Why not rollback delta log?

Because the delta log is append-only.
Rolling back means writing a compensation entry.
This preserves the full history including the attempt and its failure.

---

## 💾 Storage Core

### Component 1: Graph Database

The live, queryable graph.

---

#### Data Model Design

##### Node Structure:

```
Graph Node:

  Core Identity:
  ├── id                    string, primary key
  ├── type                  EntityType enum
  ├── sub_type              string
  ├── canonical_name        string
  ├── namespace             string

  Temporal:
  ├── first_seen            timestamp
  ├── last_seen             timestamp
  ├── last_updated          timestamp
  ├── valid_from            timestamp
  ├── valid_to              timestamp (null = active)

  Trust:
  ├── confidence            float
  ├── sources []            {source_type, source_id, last_contributed}
  ├── corroboration_count   int

  Status:
  ├── is_active             boolean
  ├── is_partial            boolean
  ├── lifecycle_stage       ACTIVE | DEPRECATED | SUNSET | DELETED

  Enrichment:
  ├── owner_team_id         string
  ├── owner_confidence      float
  ├── criticality           CRITICAL | HIGH | MEDIUM | LOW
  ├── maturity              MATURE | DEVELOPING | NASCENT | LEGACY
  ├── velocity              HOT | ACTIVE | STABLE | FROZEN

  Architectural Health:
  ├── architectural_smells []   {smell_type, detected_at, severity}
  ├── technical_debt_score  float
  ├── risk_score            float

  Version:
  ├── version               int (optimistic lock)
  └── properties            map<string, any>
```

##### Edge Structure:

```
Graph Edge:

  Core Identity:
  ├── id                    string, primary key
  ├── type                  RelationshipType enum
  ├── from_id               node id
  ├── to_id                 node id

  Temporal:
  ├── valid_from            timestamp
  ├── valid_to              timestamp (null = active)
  ├── first_observed        timestamp
  ├── last_observed         timestamp
  ├── last_confirmed        timestamp

  Trust:
  ├── confidence            float
  ├── sources []            {source_type, last_contributed}
  ├── is_inferred           boolean
  ├── inference_type        string (null if explicit)
  ├── inference_chain []    (for transitive inferences)

  Strength:
  ├── strength              CRITICAL | HIGH | MEDIUM | LOW | UNKNOWN
  ├── call_frequency        float (calls per day, if applicable)
  ├── data_volume           float (bytes per day, if applicable)

  Runtime:
  ├── p50_latency_ms        float
  ├── p95_latency_ms        float
  ├── p99_latency_ms        float
  ├── error_rate            float
  ├── runtime_window        timestamp (when these metrics were computed)

  Status:
  ├── is_active             boolean
  ├── is_deprecated         boolean

  Version:
  ├── version               int
  └── properties            map<string, any>
```

---

#### Entity Type Catalog

```
EntityType Enum:

  INFRASTRUCTURE:
    SERVICE               (running application)
    DEPLOYMENT            (deployed instance)
    INFRASTRUCTURE_RESOURCE (cloud resource)
    CONTAINER             (docker/k8s container)

  CODE:
    REPOSITORY            (git repo)
    MODULE                (code module/package)
    FILE                  (source file)
    FUNCTION              (code function/method)
    CLASS                 (code class)

  CONTRACT:
    API_ENDPOINT          (HTTP/gRPC/GraphQL endpoint)
    EVENT_TOPIC           (message queue topic)
    DATABASE_TABLE        (data store table)
    DATABASE_SCHEMA       (data store schema)

  ORGANIZATION:
    TEAM                  (engineering team)
    DEVELOPER             (individual contributor)
    ORGANIZATION          (company/org unit)

  CHANGE:
    COMMIT                (git commit)
    PULL_REQUEST          (code review)
    DEPLOYMENT_EVENT      (deployment record)
    INCIDENT              (production incident)

  DEPENDENCY:
    EXTERNAL_LIBRARY      (third-party package)
    EXTERNAL_SERVICE      (external API/SaaS)
```

---

#### Relationship Type Catalog

```
RelationshipType Enum:

  CODE RELATIONSHIPS:
    IMPORTS               (module → module)
    CALLS                 (function → function)
    EXTENDS               (class → class)
    IMPLEMENTS            (class → interface)
    EXPOSES               (service → api_endpoint)

  RUNTIME RELATIONSHIPS:
    RUNTIME_CALLS         (service → service, observed)
    PRODUCES              (service → event_topic)
    CONSUMES              (service → event_topic)
    READS_FROM            (service → database_table)
    WRITES_TO             (service → database_table)

  DEPENDENCY RELATIONSHIPS:
    DEPENDS_ON            (service → service, declared)
    TRANSITIVELY_DEPENDS_ON (service → service, inferred)
    USES_LIBRARY          (service → external_library)
    CALLS_EXTERNAL        (service → external_service)

  ORGANIZATIONAL RELATIONSHIPS:
    OWNS                  (team → service)
    CONTRIBUTED_TO        (developer → repository)
    REVIEWS               (developer → pull_request)
    ON_CALL_FOR           (developer → service)

  DEPLOYMENT RELATIONSHIPS:
    DEPLOYED_ON           (service → infrastructure_resource)
    DEPLOYED_IN           (service → deployment_event)
    RUNS_IN               (service → container)

  COUPLING RELATIONSHIPS:
    CHANGE_COUPLED_WITH   (file → file, temporal)
    DEPLOYMENT_COUPLED_WITH (service → service, temporal)
    DATA_COUPLED_WITH     (service → service, via shared table)
    FAILURE_CORRELATED_WITH (service → service, incidents)

  LIFECYCLE RELATIONSHIPS:
    INTRODUCED_IN         (function → commit)
    MODIFIED_IN           (file → commit)
    DEPRECATED_BY         (api_endpoint → deployment_event)
    REPLACED_BY           (service → service)
```

---

#### Graph Tiers

The graph operates in multiple tiers of granularity:

```
Tier 1: Organization Level (coarsest)
  Nodes: Organizations, Teams
  Edges: OWNS, ON_CALL_FOR
  Use: Ownership queries, team topology

Tier 2: Service Level
  Nodes: Services, External Services, Infrastructure
  Edges: RUNTIME_CALLS, DEPENDS_ON, DEPLOYED_ON
  Use: Impact analysis, blast radius, architecture overview

Tier 3: Contract Level
  Nodes: APIs, Event Topics, Database Tables
  Edges: EXPOSES, PRODUCES, CONSUMES, READS_FROM, WRITES_TO
  Use: API dependency, data flow, contract analysis

Tier 4: Code Level
  Nodes: Repositories, Modules, Files
  Edges: IMPORTS, CHANGE_COUPLED_WITH
  Use: Code ownership, change coupling, refactoring

Tier 5: Symbol Level (finest)
  Nodes: Functions, Classes
  Edges: CALLS, EXTENDS, IMPLEMENTS
  Use: Deep code analysis, function-level impact
```

Most queries operate on Tier 2-3.
Tier 5 is expensive and used sparingly.

Query router decides which tier to engage based on query type.

---

#### Temporal Design — Deep Detail

Every node and edge has temporal validity:

```
Temporal Model:

  valid_from:  When this entity/relationship first became true
  valid_to:    When it stopped being true (null = currently true)

This enables:

  Point-in-time query:
    "Show me the graph as it was on March 1st"
    → WHERE valid_from <= '2025-03-01'
      AND (valid_to IS NULL OR valid_to > '2025-03-01')

  Existence duration:
    "How long has this dependency existed?"
    → now() - valid_from

  Change detection:
    "What relationships appeared this week?"
    → WHERE valid_from >= now() - 7 days
      AND type = RELATIONSHIP_CREATED

  Dependency age:
    "Show me all dependencies older than 2 years"
    → WHERE type = DEPENDS_ON
      AND valid_from < now() - 2 years
      AND valid_to IS NULL
```

Temporal validity is maintained by:
- Pipeline writing `valid_from` on creation
- Pipeline writing `valid_to` when entity no longer observed
- Never deleting records — only closing validity windows

---

#### Graph Partitioning

At scale the graph cannot be one monolithic structure.

```
Partitioning Strategy:

  Primary partition key: namespace
    → All entities within an organization in same partition space
    → Cross-org queries are rare and can be slower

  Secondary partition: entity_type tier
    → Tier 1-2 (service level) in hot partition
    → Tier 3-4 (code level) in warm partition
    → Tier 5 (symbol level) in cold partition

  Tertiary partition: temporal
    → Active entities (valid_to IS NULL) in hot segment
    → Historical entities in cold segment

Query routing knows which partition to hit:
  → Service-level impact query → hot partition, tier 2
  → Historical architecture query → cold segment
  → Symbol-level query → cold partition, tier 5
```

---

### Component 2: Delta / Event Log

The **immutable, append-only, ordered record of every change ever made to the graph**.

This is the source of truth.

---

#### Log Entry Structure:

```
Delta Log Entry:

  Identity:
  ├── entry_id              sequential, monotonic
  ├── transaction_id        groups related mutations
  ├── pipeline_run_id       links to pipeline that produced this
  ├── ingestion_run_id      links to ingestion that sourced this

  Mutation:
  ├── mutation_type         EntityCreated | EntityUpdated | ...
  ├── entity_type           (for entity mutations)
  ├── entity_id             (for entity mutations)
  ├── relationship_id       (for relationship mutations)

  Change Detail:
  ├── before_state          full state before mutation (null for creates)
  ├── after_state           full state after mutation (null for deletes)
  ├── changed_fields []     {field_name, old_value, new_value}

  Context:
  ├── mutation_reason       why this mutation happened
  ├── initiated_by          pipeline | human | admin
  ├── confidence_at_write   confidence when written

  Temporal:
  ├── occurred_at           when the real-world change happened
  ├── recorded_at           when it was written to log
  ├── valid_from            graph validity start
  └── valid_to              graph validity end
```

Why both `occurred_at` and `recorded_at`?

```
occurred_at:
  When the change actually happened in reality
  (e.g., a commit was made 2 days ago)

recorded_at:
  When our system learned about it
  (e.g., we ingested it today)

The difference reveals:
  Ingestion lag
  Late-arriving signals
  Retroactive corrections
```

---

#### Log Access Patterns:

```
1. Sequential replay (graph reconstruction):
   Read all entries in entry_id order
   Apply to graph from scratch

2. Entity history:
   Read all entries WHERE entity_id = X
   ORDER BY occurred_at
   → Full lifecycle of one entity

3. Time-range diff:
   Read entries WHERE occurred_at BETWEEN t1 AND t2
   → What changed in a time window

4. Transaction replay:
   Read entries WHERE transaction_id = X
   → Full atomic change set

5. Pipeline audit:
   Read entries WHERE pipeline_run_id = X
   → Everything one pipeline run changed
```

---

#### Log Retention:

```
Hot tier (last 30 days):
  → In-memory + fast SSD
  → Full detail, instant access
  → Used by: recent history queries, cache rebuild

Warm tier (30 days - 2 years):
  → SSD, compressed
  → Full detail, fast access
  → Used by: evolution tracking, architecture drift

Cold tier (2+ years):
  → Object storage, heavily compressed
  → Accessed on demand
  → Used by: long-term trend analysis, compliance

Retention policy:
  → Log entries never deleted
  → Only tier transitions
  → Compliance/legal holds override tier transitions
```

---

### Component 3: Snapshot Store

Pre-computed point-in-time graph states.

---

#### Why Snapshots?

Without snapshots:
> "Show me the architecture as of January 1st"
> → Replay entire delta log from beginning to Jan 1st
> → Could take hours for large graphs

With snapshots:
> Find nearest snapshot before January 1st
> → Apply only deltas from snapshot to January 1st
> → Takes seconds

Snapshots are **query acceleration** for historical queries.

---

#### Snapshot Strategy:

```
Snapshot Schedule:
  Daily snapshots:     retained for 90 days
  Weekly snapshots:    retained for 1 year
  Monthly snapshots:   retained for 5 years
  Yearly snapshots:    retained indefinitely

Snapshot Trigger:
  Time-based (scheduled)
  OR
  Significant change threshold:
    > 10% of nodes changed → force snapshot
    > 20% of edges changed → force snapshot
```

---

#### Snapshot Content:

```
Snapshot Record:
  │
  ├── snapshot_id
  ├── snapshot_at           timestamp (point-in-time)
  ├── created_at            when snapshot was computed
  ├── last_log_entry_id     log position at snapshot time
  ├── statistics
  │     ├── node_count
  │     ├── edge_count
  │     ├── active_services
  │     └── architectural_smell_count
  │
  ├── graph_data            compressed serialized graph
  │     ├── nodes []
  │     └── edges []
  │
  └── checksum              integrity verification
```

---

#### Snapshot-Based Time Travel:

```
Query: "Architecture as of March 1st 2025"

Step 1: Find nearest snapshot before March 1st
  → Found: Feb 28th snapshot (snapshot_id: snap_2025_02_28)
  → last_log_entry_id = 4,521,334

Step 2: Load snapshot graph

Step 3: Apply delta log entries
  from entry_id 4,521,334
  to occurred_at <= March 1st 2025

Step 4: Return resulting graph state

Total time: seconds, not hours
```

---

### Component 4: Runtime Metrics Store

Stores runtime performance data linked to graph entities.

---

#### Why Separate from Graph DB?

```
Problem with storing runtime in graph DB:

  Metrics update every 5 minutes per entity
  1000 services × 288 updates/day = 288,000 writes/day
  → Graph DB optimized for structural queries, not time-series writes
  → Would create write contention
  → Would pollute graph query performance

Solution:
  Time-series store for metrics
  Graph DB stores only current summary values
  Linking layer joins at query time
```

---

#### Data Stored:

```
Per Service (linked by entity_id):
  ├── request_rate          rps, sampled every minute
  ├── error_rate            percentage, sampled every minute
  ├── p50_latency_ms        sampled every minute
  ├── p95_latency_ms        sampled every minute
  ├── p99_latency_ms        sampled every minute
  ├── cpu_utilization       percentage
  ├── memory_utilization    percentage
  └── deployment_events []  {version, deployed_at, deployed_by}

Per Edge (linked by relationship_id):
  ├── call_rate             calls/sec between services
  ├── error_rate            error rate on this specific edge
  ├── p99_latency_ms        latency on this hop
  └── data_volume_bytes     data transferred
```

---

#### Retention:

```
Raw metrics (1-minute resolution):
  Retained: 7 days
  Use: Recent performance queries

Aggregated metrics (1-hour resolution):
  Retained: 90 days
  Use: Performance trends, incident correlation

Aggregated metrics (1-day resolution):
  Retained: 2 years
  Use: Long-term performance evolution

Aggregated metrics (1-month resolution):
  Retained: indefinitely
  Use: Capacity planning, architecture ROI
```

---

#### Graph DB ↔ Metrics Store Link:

```
Graph node (Service):
  entity_id: svc_payment_001
  metrics_summary:
    p99_latency_ms: 187    ← refreshed every 5 min from metrics store
    error_rate: 0.3%       ← refreshed every 5 min
    last_metrics_update: timestamp

At query time:
  If query needs real-time metrics:
    → Fetch live from metrics store by entity_id
  If query needs historical metrics:
    → Fetch from metrics store with time range
  If query needs only current summary:
    → Use cached value on graph node
```

---

## 📖 Read Path

### Search Index

For fast entity lookup without full graph traversal.

---

#### What It Indexes:

```
Entity Search Document:
  ├── entity_id
  ├── canonical_name        full-text indexed
  ├── aliases []            full-text indexed
  ├── entity_type           facet
  ├── sub_type              facet
  ├── namespace             facet
  ├── owner_team            facet
  ├── criticality           facet
  ├── maturity              facet
  ├── velocity              facet
  ├── is_active             filter
  ├── architectural_smells  facet
  └── tags []               full-text + facet
```

#### Query Capabilities:

```
Exact lookup:
  "Find entity with name 'payment-service' in namespace 'acme'"

Full-text search:
  "Find all entities with 'payment' in name or aliases"

Faceted search:
  "Find all CRITICAL services owned by team-payments"

Combined:
  "Find active microservices with HIGH criticality
   that have architectural smells
   owned by any team in the payments domain"

Autocomplete:
  "pay..." → [payment-service, payment-gateway, payments-api]
```

---

#### Index Synchronization:

```
Write path triggers index update:

  On ENTITY_CREATED:
    → Add document to search index
    → Async, within 5 seconds

  On ENTITY_UPDATED:
    → Update document fields that changed
    → Async, within 5 seconds

  On ENTITY_SOFT_DELETED:
    → Set is_active = false in index
    → Retain in index for historical search

  On ENTITY_MERGED:
    → Update canonical document
    → Mark source document as merged
    → Update all alias references
```

---

### Graph Cache

Accelerates frequently accessed subgraphs.

---

#### Cache Layers:

```
Layer 1: Entity Cache
  What: Individual entity with all properties
  Key: entity_id
  TTL: 5 minutes
  Invalidation: On entity update

Layer 2: Neighborhood Cache
  What: Entity + 1-hop neighbors + edges
  Key: entity_id + hop_depth
  TTL: 2 minutes
  Invalidation: On entity update OR neighbor update

Layer 3: Subgraph Cache
  What: Named subgraphs (e.g., "payment domain")
  Key: subgraph_name + version
  TTL: 10 minutes
  Invalidation: On any entity in subgraph updated

Layer 4: Query Result Cache
  What: Full query results for common queries
  Key: query_hash
  TTL: 1 minute
  Invalidation: On any entity in result set updated

Layer 5: Computed Score Cache
  What: Pre-computed impact scores, risk scores
  Key: entity_id + score_type
  TTL: 15 minutes
  Invalidation: On entity update OR structural change
```

---

#### Cache Invalidation Strategy:

```
Event-driven invalidation:

  When mutation event received:
    1. Identify affected entity_ids
    2. Invalidate Layer 1 entries for each entity_id
    3. Invalidate Layer 2 entries for entity_id AND all neighbors
    4. Identify which subgraphs contain these entities
    5. Invalidate Layer 3 entries for those subgraphs
    6. Identify which cached queries include these entities
    7. Invalidate Layer 4 entries for those queries
    8. Invalidate Layer 5 entries for affected entities

  Challenge: Step 4, 6 require reverse index
    Cache maintains: entity_id → [subgraph_keys], [query_keys]
    Updated on cache write
    Used on invalidation
```

---

### Query Router

Decides which storage component answers which query.

```
Query Router Decision Logic:

  Query type: Entity lookup by name
    → Search Index (fastest)

  Query type: Entity lookup by ID
    → Graph Cache Layer 1 → Graph DB (if cache miss)

  Query type: Neighborhood traversal (1-2 hops)
    → Graph Cache Layer 2 → Graph DB (if cache miss)

  Query type: Deep traversal (3+ hops)
    → Graph DB directly (too varied for cache)

  Query type: Impact analysis
    → Graph Cache Layer 5 (pre-computed) → Graph DB (if cache miss)

  Query type: Historical query (point-in-time)
    → Snapshot Store + Delta Log

  Query type: Evolution query (what changed over time)
    → Delta Log directly

  Query type: Performance query
    → Graph node summary (if recent) → Runtime Metrics Store

  Query type: Full-text search
    → Search Index
```

---

## 🔄 Write Path — End-to-End Flow

Let's trace a mutation batch from Zone 3 arriving at Zone 4:

```
Mutation batch arrives at Graph Mutation API:
  [
    ENTITY_UPDATED: svc_payment_001 {maturity_score: 0.74}
    RELATIONSHIP_UPDATED: rel_reads_tx_001 {last_confirmed: now}
  ]

─────────────────────────────────────────────────
Step 1: Schema Enforcer

  Validates both mutations against schema
  maturity_score: float, 0.0-1.0 ✅
  last_confirmed: timestamp ✅

─────────────────────────────────────────────────
Step 2: Conflict Detector

  Load version token for svc_payment_001: v47
  Load version token for rel_reads_tx_001: v12
  No concurrent writes detected ✅

─────────────────────────────────────────────────
Step 3: Transaction Coordinator

  Begin transaction TX_9841
  Acquire locks: [svc_payment_001, rel_reads_tx_001]

─────────────────────────────────────────────────
Step 4: Graph DB Writes

  UPDATE node svc_payment_001:
    maturity_score = 0.74
    version = 48
    last_updated = now()

  UPDATE edge rel_reads_tx_001:
    last_confirmed = now()
    version = 13

─────────────────────────────────────────────────
Step 5: Delta Log Appends (same transaction)

  APPEND entry:
    mutation_type: ENTITY_UPDATED
    entity_id: svc_payment_001
    changed_fields: [{maturity_score, 0.71, 0.74}]
    transaction_id: TX_9841
    occurred_at: now()

  APPEND entry:
    mutation_type: RELATIONSHIP_UPDATED
    relationship_id: rel_reads_tx_001
    changed_fields: [{last_confirmed, old_value, now()}]
    transaction_id: TX_9841
    occurred_at: now()

─────────────────────────────────────────────────
Step 6: Commit

  Release locks
  Transaction TX_9841 committed

─────────────────────────────────────────────────
Step 7: Post-commit Events (async)

  Emit mutation events:
    → mutation.entity.updated: svc_payment_001
    → mutation.relationship.updated: rel_reads_tx_001

  Search index update triggered (async):
    → Update maturity facet for svc_payment_001

  Cache invalidation triggered (async):
    → Invalidate Layer 1: svc_payment_001
    → Invalidate Layer 2: svc_payment_001 + neighbors
    → Invalidate Layer 5: svc_payment_001 scores

─────────────────────────────────────────────────
Step 8: Return to caller

  {
    status: SUCCESS
    transaction_id: TX_9841
    mutations_applied: 2
    graph_version: 48
  }

Total write latency target: < 50ms for this batch
```

---

## 📊 Read Path — Common Query Patterns

### Pattern 1: Impact Analysis Query

```
Query: "What does payment-service affect?"

Step 1: Query Router
  → Deep traversal query → Graph DB

Step 2: Entity Lookup
  → Search Index: "payment-service" → svc_payment_001

Step 3: Traversal
  Start: svc_payment_001
  Direction: INBOUND (who depends on this?)
  Relationship types: [DEPENDS_ON, RUNTIME_CALLS, DATA_COUPLED_WITH]
  Max depth: 4 hops
  Filter: is_active = true

Step 4: Result Enrichment
  For each affected node:
    → Load criticality from node properties
    → Load runtime error_rate from metrics summary
    → Load owner from node properties

Step 5: Ranking
  Sort affected entities by:
    criticality DESC
    call_frequency DESC
    error_propagation_risk DESC

Step 6: Return
  Ranked list of affected services with:
    - Relationship path
    - Criticality
    - Runtime health
    - Ownership
    - Confidence
```

---

### Pattern 2: Historical Architecture Query

```
Query: "Show me the service dependencies as of January 1st"

Step 1: Query Router
  → Historical query → Snapshot Store + Delta Log

Step 2: Find nearest snapshot
  → Snapshot before Jan 1st: Dec 31st snapshot
  → last_log_entry_id: 3,891,204

Step 3: Load snapshot graph
  → Decompress and deserialize Dec 31st snapshot

Step 4: Apply deltas
  → Read delta log: entry_id 3,891,204 to Jan 1st midnight
  → Apply each delta to snapshot graph

Step 5: Return
  → Graph state at exactly Jan 1st midnight
```

---

### Pattern 3: Performance Diagnosis Query

```
Query: "Why is checkout-service slow?"

Step 1: Query Router
  → Combined: Graph DB + Runtime Metrics Store

Step 2: Load service neighborhood
  → checkout-service (1-hop outbound)
  → All services checkout-service calls

Step 3: Load runtime metrics for each
  → From Runtime Metrics Store
  → Last 24 hours, hourly aggregation

Step 4: Identify anomaly
  → checkout-service → inventory-service:
     p99 jumped from 45ms to 380ms at 14:00 today
  → inventory-service had deployment at 13:45 today
     (from delta log)

Step 5: Load context
  → What changed in inventory-service deployment?
  → Who deployed it?
  → Are there incidents open?

Step 6: Return
  Structured diagnosis:
    Root: inventory-service latency spike
    Trigger: deployment at 13:45
    Impact path: checkout → inventory → [database-cluster-3]
    Recommendation: rollback or investigate deployment
```

---

## 🔑 Graph DB Selection Criteria

We haven't named a specific product.
Let's specify what we need and why.

```
Requirements:

  Native graph traversal:
    Multi-hop path queries without explicit joins
    Shortest path algorithms
    Cycle detection

  Property graph model:
    Properties on both nodes and edges
    Not just node properties

  Temporal query support:
    Filter by valid_from / valid_to efficiently
    Time-indexed edges

  ACID transactions:
    Multi-node, multi-edge atomic writes
    Optimistic locking support

  Horizontal scalability:
    Partition across nodes
    Consistent reads across shards

  Index support:
    Node property indexes
    Edge property indexes
    Composite indexes

  Query language expressiveness:
    Pattern matching
    Aggregation
    Subgraph extraction
    Path filtering

  Operational maturity:
    Replication
    Backup/restore
    Monitoring integration
```

---

## ⚠️ Failure Scenarios & Responses

| Scenario | Detection | Response |
|----------|-----------|----------|
| Graph DB node failure | Health check | Failover to replica, reads continue |
| Delta log write fails | Transaction rollback | Graph write also rolled back, retry |
| Snapshot corruption | Checksum mismatch | Rebuild from previous snapshot + deltas |
| Cache stampede | High cache miss rate | Request coalescing, background refresh |
| Search index lag | Index freshness metric | Serve with staleness warning |
| Metrics store unavailable | Connection timeout | Use cached summary on graph node |
| Lock contention | Lock wait timeout | Exponential backoff retry |
| Partition split | Cross-partition query fails | Boundary node replication |
| Delta log overflow | Disk pressure | Emergency archival to cold tier |

---

## 🔑 Design Decisions Summary

| Decision | Choice | Reasoning |
|----------|--------|-----------|
| Write entry point | Single Mutation API | Consistency, auditability |
| Source of truth | Delta log | Append-only, immutable history |
| Graph DB role | Projection of delta log | Rebuildable, not primary truth |
| Temporal model | valid_from / valid_to on all | Native time-travel queries |
| Runtime metrics | Separate time-series store | Write volume, query patterns differ |
| History acceleration | Snapshot store | Fast historical queries |
| Read acceleration | Multi-layer cache | Sub-second common queries |
| Entity lookup | Dedicated search index | Full-text, faceted, fast |
| Graph granularity | 5 tiers | Right detail for right query |
| Deletes | Soft delete only | History preservation |
| Transactions | Graph DB + delta log atomic | No inconsistency between stores |

---

## 🧠 Mental Model

Zone 4 is:

> **A living, versioned, multi-layer architectural memory**

Think of it as a system with five distinct personalities:

| Store | Personality |
|-------|------------|
| Graph DB | The present — what is true right now |
| Delta Log | The memory — everything that ever happened |
| Snapshot Store | The album — pictures of the past |
| Metrics Store | The vital signs — how healthy is everything |
| Search Index + Cache | The reflexes — answer fast without thinking |

Together they answer:
- What exists now? → Graph DB
- What happened? → Delta Log
- What did it look like then? → Snapshot + Delta
- How is it performing? → Metrics Store
- What is this called? → Search Index
- What's the quick answer? → Cache

No single store can answer all questions.
Zone 4's architecture exists because different questions need different storage strategies.

---
