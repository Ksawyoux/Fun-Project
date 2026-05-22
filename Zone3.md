# 🟨 Zone 3: Processing Pipeline — System Design Deep Dive

---

## 🎯 Zone Responsibility

Zone 3 has one sacred job:

> Take raw, ambiguous, multi-source NIF records from Zone 2 and transform them into **confident, resolved, enriched, validated graph mutations** ready for Zone 4

This is where observations become facts.
This is where ambiguity gets resolved.
This is where the graph learns what is true.

If Zone 2 is the border crossing,
Zone 3 is the **intelligence agency** that makes sense of everything that came through.

---

## 🧠 Core Design Philosophy

Three principles govern every decision in Zone 3:

### Principle 1: Trust Must Be Earned
Nothing enters the graph without passing through confidence scoring.
Raw data is always suspect.
Confidence increases as signals corroborate each other.

### Principle 2: Ambiguity Must Be Explicit
When the pipeline cannot resolve something with certainty:
- It does not guess silently
- It does not drop the record
- It marks the uncertainty explicitly
- It routes to human review if needed

### Principle 3: The Graph Is Append-Friendly, Correction-Hostile
Getting something wrong in the graph and correcting it later is expensive.
Prevention is far cheaper than correction.
The pipeline is the prevention layer.

---

## 🏗️ Full Pipeline Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    PROCESSING PIPELINE                          │
│                                                                 │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │                  INTAKE LAYER                           │   │
│  │                                                         │   │
│  │   ┌──────────────────┐    ┌──────────────────────────┐  │   │
│  │   │  Event Bus       │    │   Intake Router          │  │   │
│  │   │  Consumer        │───▶│   (by entity type)       │  │   │
│  │   └──────────────────┘    └──────────────────────────┘  │   │
│  └─────────────────────────────────────────────────────────┘   │
│                              │                                  │
│                              ▼                                  │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │              STAGE 1: PARSE & CLASSIFY                  │   │
│  │                                                         │   │
│  │   ┌──────────────┐  ┌──────────────┐  ┌─────────────┐  │   │
│  │   │   Entity     │  │  Sub-type    │  │   Naming    │  │   │
│  │   │  Classifier  │  │  Resolver    │  │ Normalizer  │  │   │
│  │   └──────────────┘  └──────────────┘  └─────────────┘  │   │
│  │                                                         │   │
│  │   ┌──────────────────────────────────────────────────┐  │   │
│  │   │           Intent Detector                        │  │   │
│  │   │   (create | update | delete | merge hint)        │  │   │
│  │   └──────────────────────────────────────────────────┘  │   │
│  └─────────────────────────────────────────────────────────┘   │
│                              │                                  │
│                              ▼                                  │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │            STAGE 2: ENTITY RESOLUTION                   │   │
│  │                                                         │   │
│  │   ┌──────────────────────────────────────────────────┐  │   │
│  │   │              Resolution Engine                   │  │   │
│  │   │                                                  │  │   │
│  │   │  ┌────────────┐  ┌────────────┐  ┌───────────┐  │  │   │
│  │   │  │   Exact    │  │  Fuzzy     │  │ Structural│  │  │   │
│  │   │  │  Matcher   │  │  Matcher   │  │  Matcher  │  │  │   │
│  │   │  └────────────┘  └────────────┘  └───────────┘  │  │   │
│  │   │                                                  │  │   │
│  │   │  ┌────────────────────────────────────────────┐  │  │   │
│  │   │  │        Confidence Scorer                   │  │  │   │
│  │   │  └────────────────────────────────────────────┘  │  │   │
│  │   │                                                  │  │   │
│  │   │  ┌────────────────────────────────────────────┐  │  │   │
│  │   │  │        Resolution Decision Engine          │  │  │   │
│  │   │  │   merge | link | split | new | review      │  │  │   │
│  │   │  └────────────────────────────────────────────┘  │  │   │
│  │   └──────────────────────────────────────────────────┘  │   │
│  │                                                         │   │
│  │   ┌──────────────────────────────────────────────────┐  │   │
│  │   │              Entity Registry                     │  │   │
│  │   │   (canonical IDs, aliases, resolution history)   │  │   │
│  │   └──────────────────────────────────────────────────┘  │   │
│  └─────────────────────────────────────────────────────────┘   │
│                              │                                  │
│                              ▼                                  │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │          STAGE 3: RELATIONSHIP INFERENCE                │   │
│  │                                                         │   │
│  │   ┌──────────────┐  ┌──────────────┐  ┌─────────────┐  │   │
│  │   │  Structural  │  │   Runtime    │  │  Temporal   │  │   │
│  │   │  Inferrer    │  │  Inferrer    │  │  Inferrer   │  │   │
│  │   └──────────────┘  └──────────────┘  └─────────────┘  │   │
│  │                                                         │   │
│  │   ┌──────────────────────────────────────────────────┐  │   │
│  │   │         Inference Confidence Scorer              │  │   │
│  │   └──────────────────────────────────────────────────┘  │   │
│  │                                                         │   │
│  │   ┌──────────────────────────────────────────────────┐  │   │
│  │   │         Relationship Deduplicator                │  │   │
│  │   └──────────────────────────────────────────────────┘  │   │
│  └─────────────────────────────────────────────────────────┘   │
│                              │                                  │
│                              ▼                                  │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │              STAGE 4: ENRICHMENT                        │   │
│  │                                                         │   │
│  │   ┌──────────────┐  ┌──────────────┐  ┌─────────────┐  │   │
│  │   │  Ownership   │  │  Velocity    │  │ Criticality │  │   │
│  │   │  Enricher    │  │  Enricher    │  │  Enricher   │  │   │
│  │   └──────────────┘  └──────────────┘  └─────────────┘  │   │
│  │                                                         │   │
│  │   ┌──────────────┐  ┌──────────────┐                   │   │
│  │   │   Maturity   │  │  Annotation  │                   │   │
│  │   │   Enricher   │  │   Merger     │                   │   │
│  │   └──────────────┘  └──────────────┘                   │   │
│  └─────────────────────────────────────────────────────────┘   │
│                              │                                  │
│                              ▼                                  │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │        STAGE 5: VALIDATION & CONFLICT RESOLUTION        │   │
│  │                                                         │   │
│  │   ┌──────────────┐  ┌──────────────┐  ┌─────────────┐  │   │
│  │   │  Structural  │  │   Semantic   │  │ Consistency │  │   │
│  │   │  Validator   │  │  Validator   │  │  Validator  │  │   │
│  │   └──────────────┘  └──────────────┘  └─────────────┘  │   │
│  │                                                         │   │
│  │   ┌──────────────────────────────────────────────────┐  │   │
│  │   │           Conflict Resolution Engine             │  │   │
│  │   │   last-write | source-priority | confidence      │  │   │
│  │   └──────────────────────────────────────────────────┘  │   │
│  │                                                         │   │
│  │   ┌──────────────────────────────────────────────────┐  │   │
│  │   │           Human Review Queue                     │  │   │
│  │   └──────────────────────────────────────────────────┘  │   │
│  └─────────────────────────────────────────────────────────┘   │
│                              │                                  │
│                              ▼                                  │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │            STAGE 6: DELTA COMPUTATION                   │   │
│  │                                                         │   │
│  │   ┌──────────────────────────────────────────────────┐  │   │
│  │   │              State Comparator                    │  │   │
│  │   │    current graph state vs pipeline output        │  │   │
│  │   └──────────────────────────────────────────────────┘  │   │
│  │                                                         │   │
│  │   ┌──────────────────────────────────────────────────┐  │   │
│  │   │              Delta Classifier                    │  │   │
│  │   │  created | updated | deleted | merged | split    │  │   │
│  │   └──────────────────────────────────────────────────┘  │   │
│  │                                                         │   │
│  │   ┌──────────────────────────────────────────────────┐  │   │
│  │   │              Mutation Planner                    │  │   │
│  │   │    ordered, batched, compensatable               │  │   │
│  │   └──────────────────────────────────────────────────┘  │   │
│  └─────────────────────────────────────────────────────────┘   │
│                              │                                  │
│                              ▼                                  │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │              CROSS-CUTTING CONCERNS                     │   │
│  │                                                         │   │
│  │   ┌──────────────┐  ┌──────────────┐  ┌─────────────┐  │   │
│  │   │   Pipeline   │  │    Error     │  │   Pipeline  │  │   │
│  │   │   Ledger     │  │   Handler    │  │  Health     │  │   │
│  │   └──────────────┘  └──────────────┘  └─────────────┘  │   │
│  └─────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

---

## 📥 Intake Layer

**Responsibility:** Consume NIF records from event bus and route them correctly

### Event Bus Consumer

Consumes from two topics:
```
ingestion.entities.raw
ingestion.relationships.raw
```

Key design decisions:

**Consumer Group Strategy:**

Each pipeline stage is its own consumer group.
Why?
- Independent scaling per stage
- Stage failures don't affect others
- Each stage tracks its own offset
- Replay is possible per stage

**Processing Order:**

Entities must be fully processed before relationships.
Relationships reference entity IDs.
If entity doesn't exist yet → relationship resolution fails.

Enforced by:
- Entities topic consumed first
- Relationships held until entity batch confirmed
- Ordering guarantees within partitions

---

### Intake Router

Routes incoming records to appropriate processing paths.

Not all records need the same pipeline depth:

| Record Type | Pipeline Depth |
|-------------|---------------|
| New entity from single source | Full pipeline |
| Runtime metric update | Enrichment only |
| Deletion event | Validation + delta only |
| Annotation update | Enrichment + validation + delta |
| Relationship from trace | Resolution + inference + delta |

Routing saves compute.
Not everything needs every stage.

```
Intake Router Decision:

  If record.type == ENTITY and record.is_new:
    → Full pipeline (all 6 stages)

  If record.type == METRIC_UPDATE:
    → Skip to Stage 4 (Enrichment)

  If record.type == DELETION:
    → Skip to Stage 5 (Validation) → Stage 6 (Delta)

  If record.type == RELATIONSHIP and source == runtime:
    → Stage 2 (Resolution) → Stage 3 (Inference) → Stage 6 (Delta)
```

---

## 🔬 Stage 1: Parse & Classify

**Responsibility:** Understand exactly what each record is and what the pipeline should do with it

---

### Entity Classifier

Determines the precise entity type from NIF record.

Why is this needed if NIF already has a type?

Because NIF types are broad.
Classification makes them precise.

```
NIF says:     type = "Service"

Classifier determines:
  sub_type = "BFF"
  Why:
    - Exposed to external clients (from API spec)
    - Calls multiple internal services (from traces)
    - No direct DB access (from AST)
    - Frontend-facing routes (from path patterns)
```

Classification uses **multi-signal rules**:

```
Classification Rule Example:

  IF entity.type == Service
  AND entity.has_property(exposed_to_internet) == true
  AND entity.downstream_service_count > 3
  AND entity.has_property(db_direct_access) == false
  THEN sub_type = BFF
  confidence = 0.87
```

Rules are:
- Configurable
- Versioned
- Explainable (rule that fired is recorded)
- Not hardcoded logic

Why explainable?

Because when a developer asks:
> "Why is my service classified as a BFF?"

The system can answer:
> "Because it is internet-facing, calls 5 downstream services, and has no direct database access — matching rule BFF-001 with 87% confidence"

---

### Sub-type Resolver

Expands the classification taxonomy further.

```
Entity Type Taxonomy:

Service
  ├── Microservice
  │     ├── Domain Service
  │     └── Infrastructure Service
  ├── BFF
  ├── Worker
  │     ├── Scheduled Worker
  │     └── Queue Consumer
  ├── Gateway
  │     ├── API Gateway
  │     └── Service Mesh Sidecar
  └── Monolith

API
  ├── REST
  │     ├── Public API
  │     └── Internal API
  ├── GraphQL
  │     ├── Query
  │     └── Mutation
  ├── gRPC
  └── Event Handler

Dependency
  ├── Internal
  ├── External Third-party
  └── Infrastructure
```

Sub-type determines:
- Which enrichers apply
- Which validators apply
- Which inference rules apply
- How impact analysis traverses this node

---

### Naming Normalizer

Enforces canonical naming across all sources.

Problem:

```
Source          Name
─────────────────────────────
Git             payment-service
Kubernetes      payment-svc
OpenTelemetry   PaymentService
OpenAPI         payments-api
Datadog         payment_service
PagerDuty       Payments Service
```

All refer to the same service.
Without normalization → 6 graph nodes for 1 service.

Normalization pipeline:

```
Raw Name
    │
    ▼
┌──────────────────────┐
│  Case Normalization  │  PaymentService → payment-service
└──────────┬───────────┘
           │
           ▼
┌──────────────────────┐
│ Separator Unification│  payment_service → payment-service
└──────────┬───────────┘
           │
           ▼
┌──────────────────────┐
│  Suffix Stripping    │  payment-service-svc → payment-service
└──────────┬───────────┘
           │
           ▼
┌──────────────────────┐
│  Alias Lookup        │  payments-api → payment-service
│  (configured rules)  │
└──────────┬───────────┘
           │
           ▼
Canonical Name: payment-service
```

Alias lookup:
- Maintained as configuration
- Can be seeded from org conventions
- Can be learned from co-occurrence patterns
- Human-correctable

---

### Intent Detector

Determines what the pipeline should do with this record.

Four intents:

```
CREATE
  → This entity does not exist in the graph
  → Full pipeline, create new node

UPDATE
  → This entity exists, properties changed
  → Resolve to existing, update properties

DELETE
  → This entity no longer observed
  → Mark inactive, preserve for history

MERGE HINT
  → Source signals this may be same as another entity
  → Route to resolution with elevated priority
```

Intent detection uses:
- Entity Registry lookup (does this canonical ID exist?)
- Source signals (deletion events from CI/CD)
- Confidence thresholds (below threshold → treat as new)
- Temporal signals (not seen in 30 days → soft delete candidate)

---

## 🔍 Stage 2: Entity Resolution

**Responsibility:** Answer one question with certainty:

> "Is this record about an entity we already know about, or is it something new?"

This is the hardest stage.
Getting this wrong corrupts the graph.

---

### Resolution Engine Architecture

```
┌─────────────────────────────────────────────────────┐
│                RESOLUTION ENGINE                    │
│                                                     │
│  Input: classified NIF record                       │
│                                                     │
│  ┌───────────────────────────────────────────────┐  │
│  │              Candidate Generator              │  │
│  │   Find all potentially matching entities      │  │
│  │   in Entity Registry                          │  │
│  └───────────────────┬───────────────────────────┘  │
│                      │                              │
│                      ▼                              │
│  ┌───────────────────────────────────────────────┐  │
│  │              Signal Extractors                │  │
│  │                                               │  │
│  │  ┌──────────┐ ┌──────────┐ ┌──────────────┐  │  │
│  │  │  Exact   │ │  Fuzzy   │ │  Structural  │  │  │
│  │  │  Match   │ │  Match   │ │    Match     │  │  │
│  │  └──────────┘ └──────────┘ └──────────────┘  │  │
│  │                                               │  │
│  │  ┌──────────┐ ┌──────────┐                   │  │
│  │  │Co-occur  │ │Temporal  │                   │  │
│  │  │  Match   │ │  Match   │                   │  │
│  │  └──────────┘ └──────────┘                   │  │
│  └───────────────────┬───────────────────────────┘  │
│                      │                              │
│                      ▼                              │
│  ┌───────────────────────────────────────────────┐  │
│  │           Confidence Scorer                   │  │
│  │   Weighted combination of signal scores       │  │
│  └───────────────────┬───────────────────────────┘  │
│                      │                              │
│                      ▼                              │
│  ┌───────────────────────────────────────────────┐  │
│  │        Resolution Decision Engine             │  │
│  │                                               │  │
│  │  CERTAIN  → Auto merge                        │  │
│  │  LIKELY   → Auto merge + audit flag           │  │
│  │  UNCERTAIN→ Hold for human review             │  │
│  │  NO MATCH → Create new entity                 │  │
│  │  CONFLICT → Human review queue                │  │
│  └───────────────────────────────────────────────┘  │
│                                                     │
│  Output: canonical entity ID + resolution record    │
└─────────────────────────────────────────────────────┘
```

---

### Candidate Generator

First step: find candidates to compare against.

Strategy:

```
1. Exact ID lookup
   → Same deterministic ID?
   → Instant match, skip matchers

2. Canonical name lookup
   → Same normalized name in same namespace?
   → Strong candidate

3. Alias lookup
   → Known aliases in registry?
   → Strong candidate

4. Fuzzy name search
   → Similar names within namespace?
   → Weak candidates, need corroboration

5. Source cross-reference
   → Same source_ref in different source_type?
   → Medium candidate
```

Candidate set is bounded:
- Maximum 20 candidates per record
- Ranked by initial likelihood
- Only top N proceed to signal extraction

---

### Signal Extractors — Detail

#### Exact Matcher
```
Signals:
  - Same canonical name: +0.50
  - Same namespace: +0.20
  - Same entity type: +0.15
  - Same source_ref: +0.15

Total possible: 1.0
Threshold for exact: 0.85
```

#### Fuzzy Matcher
```
Signals:
  - Name similarity (edit distance): 0.0 - 0.30
  - Namespace similarity: 0.0 - 0.15
  - Common aliases: +0.20
  - Common prefixes/suffixes: +0.10

Total possible: 0.75
Used as corroboration, not standalone
```

#### Structural Matcher
```
Signals:
  - Same API endpoints exposed: +0.30 per match
  - Same database tables accessed: +0.25 per match
  - Same downstream dependencies: +0.20 per match
  - Same deployment target: +0.15

Cap at: 0.80
Strong when multiple structural signals align
```

#### Co-occurrence Matcher
```
Signals:
  - Always appear in same traces: +0.25
  - Always deployed together: +0.20
  - Always in same incidents: +0.15
  - Always in same commits: +0.15

Cap at: 0.60
Corroborating signal only
```

#### Temporal Matcher
```
Signals:
  - First seen at same time as known entity: +0.15
  - Appeared after known entity renamed: +0.25
  - Disappeared when new entity appeared: +0.20

Cap at: 0.40
Weak signal, context-dependent
```

---

### Confidence Scoring

Final confidence = weighted combination:

```
Final Score = (
  exact_score    * 0.40 +
  structural_score * 0.25 +
  fuzzy_score    * 0.15 +
  cooccurrence_score * 0.12 +
  temporal_score * 0.08
)
```

Weights are:
- Configurable per organization
- Tunable based on observed error rates
- Different weights for different entity types

---

### Resolution Decision Engine

```
Score >= 0.95  → CERTAIN MERGE
  Action: Auto-merge immediately
  Audit: Logged but no alert

0.80 <= Score < 0.95  → LIKELY MERGE
  Action: Auto-merge
  Audit: Flagged for periodic review
  Alert: None (batched daily summary)

0.60 <= Score < 0.80  → UNCERTAIN
  Action: Hold record
  Audit: Added to human review queue
  Alert: Immediate notification

Score < 0.60  → NO MATCH
  Action: Create new entity
  Audit: Logged as new entity

Multiple candidates with similar scores  → CONFLICT
  Action: Hold all candidates
  Audit: Human review queue with high priority
  Alert: Immediate notification
```

---

### Entity Registry — Design Detail

The Entity Registry is a **dedicated store** separate from the graph.

Why separate?

- Needs extremely fast lookup (milliseconds)
- Updated very frequently
- Has different access patterns than graph
- Must be consistent independent of graph state

#### Registry Data Structure:

```
Canonical Entity Record:
  │
  ├── canonical_id         (primary key)
  ├── entity_type
  ├── sub_type
  ├── canonical_name
  ├── namespace
  │
  ├── aliases []
  │     └── {name, source_type, source_id, confidence}
  │
  ├── source_contributions []
  │     └── {source_type, source_id, source_ref, last_seen}
  │
  ├── resolution_history []
  │     └── {action, candidates, score, decided_at, decided_by}
  │
  ├── graph_node_id        (link to graph DB)
  ├── confidence           (overall entity confidence)
  ├── first_seen
  └── last_confirmed
```

#### Registry Indexes:

```
Primary:    canonical_id
Secondary:  canonical_name + namespace
Secondary:  alias_name + namespace
Secondary:  source_type + source_ref
Full-text:  canonical_name (for fuzzy search)
```

Fast lookup on all access patterns.

---

## 🔗 Stage 3: Relationship Inference

**Responsibility:** Derive the complete relationship picture — both explicit and hidden

---

### Why Inference Matters

Explicit relationships (directly observed) cover maybe 60% of reality.

The remaining 40% is hidden:
- Dynamic calls not visible in AST
- Indirect dependencies through shared libraries
- Runtime coupling not declared anywhere
- Data coupling through shared databases
- Temporal coupling through shared deployments

Inference surfaces these hidden connections.

---

### Structural Inferrer

Derives relationships from code structure.

#### Direct Structural Inference:

```
Observation:
  Module A imports Module B

Direct inference:
  A → IMPORTS → B           (high confidence)
  A → DEPENDS_ON → B        (high confidence)
```

#### Transitive Structural Inference:

```
Observation:
  A imports B
  B imports C
  C calls ExternalService D

Transitive inference:
  A → TRANSITIVELY_DEPENDS_ON → D
  confidence = product of chain confidences
             = 0.90 * 0.88 * 0.75 = 0.59

Marked as:
  is_inferred = true
  inference_depth = 3
  inference_chain = [A→B, B→C, C→D]
```

Inference depth is bounded:

```
Depth 1-2: High confidence, auto-accepted
Depth 3-4: Medium confidence, marked clearly
Depth 5+:  Low confidence, stored but low weight
```

Why bound depth?

Deep transitive dependencies become noise.
Everything depends on everything transitively at depth 10.
The graph becomes useless.

#### Shared Resource Inference:

```
Observation:
  Service A writes to Table users
  Service B reads from Table users

Inference:
  A → DATA_COUPLED_WITH → B
  confidence = 0.85
  reason = "shared write/read on table users"
```

This reveals **data coupling** — one of the most dangerous hidden dependencies.

---

### Runtime Inferrer

Derives relationships from actual observed behavior.

```
Observation (from traces, aggregated over 7 days):
  ServiceA → ServiceB: 45,000 calls/day, p99=180ms
  ServiceA → ServiceB error_rate: 0.3%

Runtime inferences:

  1. RUNTIME_CALLS relationship:
     A → RUNTIME_CALLS → B
     strength = HIGH (45k calls/day)
     latency_sensitivity = MEDIUM (180ms p99)
     confidence = 0.97

  2. Failure propagation risk:
     If B fails → A failure probability = 0.73
     (based on error rate and call pattern)

  3. Latency contribution:
     B contributes ~34% of A's total latency
     (derived from trace timing breakdown)
```

Runtime inference also catches:

**Missing static relationships:**

```
If runtime shows A calls B
But AST shows no import of B
Then:
  → Dynamic call detected
  → Flag as: dynamic_dispatch_detected = true
  → No AST relationship to merge with
  → Runtime relationship stands alone
  → Marked: source = runtime_only
```

This is extremely valuable.
It surfaces what static analysis misses entirely.

---

### Temporal Inferrer

Derives relationships from change patterns over time.

#### Change Coupling:

```
Observation (from git history, 90-day window):
  FileA and FileB changed in same commit: 34 times
  FileA changed without FileB: 3 times
  FileB changed without FileA: 2 times

Change coupling score:
  = co-changes / (total changes of either)
  = 34 / (34 + 3 + 2) = 0.87

Inference:
  FileA → CHANGE_COUPLED_WITH → FileB
  coupling_strength = 0.87
  confidence = 0.91
```

Change coupling reveals:

- Hidden behavioral dependencies
- Implicit contracts between files
- Teams that must coordinate
- Refactoring candidates (high coupling = poor separation)

#### Deployment Coupling:

```
Observation:
  ServiceA and ServiceB always deployed together
  In 28 of last 30 deployments

Inference:
  A → DEPLOYMENT_COUPLED_WITH → B
  coupling_strength = 0.93
  
Implication:
  Cannot deploy A without B
  Treat as operational unit
```

#### Failure Correlation:

```
Observation (from incidents, 6 months):
  ServiceA incidents: 12 total
  ServiceB was degraded in 10 of those 12
  
Inference:
  A → FAILURE_CORRELATED_WITH → B
  correlation_strength = 0.83
  
This reveals:
  Hidden operational dependency
  Possible missing explicit dependency
  Monitoring gap
```

---

### Inference Confidence Scorer

All inferences get scored before proceeding.

```
Inference Record:
  │
  ├── relationship_type
  ├── from_entity_id
  ├── to_entity_id
  ├── inference_type        (structural | runtime | temporal)
  ├── inference_method      (transitive | shared_resource | ...)
  ├── evidence []           (what observations support this)
  ├── confidence            (0.0 - 1.0)
  ├── inference_depth       (for transitive only)
  └── inference_chain       (for transitive only)
```

Confidence thresholds per inference type:

| Inference Type | Auto-Accept | Review | Reject |
|----------------|-------------|--------|--------|
| Structural Direct | > 0.85 | 0.60-0.85 | < 0.60 |
| Structural Transitive | > 0.80 | 0.50-0.80 | < 0.50 |
| Runtime | > 0.90 | 0.70-0.90 | < 0.70 |
| Temporal Change | > 0.80 | 0.60-0.80 | < 0.60 |
| Temporal Failure | > 0.75 | 0.55-0.75 | < 0.55 |

---

### Relationship Deduplicator

Multiple sources may infer the same relationship:

```
AST says:    A → CALLS → B  (confidence 0.88)
Traces say:  A → CALLS → B  (confidence 0.95)
```

Do not create two edges.

Deduplication strategy:

```
If same relationship type + same endpoints:
  → Merge into single relationship
  → Take highest confidence
  → Record all contributing sources
  → Update strength (corroboration increases strength)

Result:
  A → CALLS → B
  confidence = 0.95 (highest)
  sources = [ast, runtime]
  corroboration_count = 2
  strength = HIGH (boosted by multi-source)
```

Multi-source corroboration = highest trust.
If AST and runtime both confirm a call → very high confidence.
If only one source sees it → lower confidence.

---

## ✨ Stage 4: Enrichment

**Responsibility:** Add contextual metadata that makes the graph useful beyond pure structure

---

### Ownership Enricher

Determines who is responsible for each entity.

```
Ownership Resolution:

Input signals (priority order):
  1. CODEOWNERS file declaration    (highest trust)
  2. git blame majority contributor
  3. CI/CD pipeline owner config
  4. Team directory mapping
  5. Historical contribution analysis (lowest trust)

Resolution:
  Primary owner = highest priority signal available
  Secondary owners = contributing teams

Output on entity:
  primary_owner: {team_id, team_name, confidence}
  secondary_owners: [{team_id, confidence}]
  ownership_confidence: overall score
  ownership_source: which signal determined this
```

#### Ownership Conflict:

```
CODEOWNERS says: Team A owns /services/payments/
git blame says:  80% of commits by Team B members

Conflict strategy:
  Primary: CODEOWNERS (declared intent wins)
  Note: "High contribution from Team B (80% commits)"
  Flag: ownership_drift = true
  Action: Surface to both teams for review
```

Ownership drift is valuable signal:
→ Declared owner ≠ actual contributor
→ Team boundary may need adjustment
→ Knowledge transfer may be needed

---

### Velocity Enricher

Measures how fast things are changing.

```
Per entity, compute:

  commit_frequency:
    commits per week over last 90 days

  churn_rate:
    lines changed per week / total lines

  contributor_count:
    distinct contributors in last 90 days

  change_volatility:
    standard deviation of change frequency
    (high = unpredictable, low = stable)

  last_significant_change:
    last commit changing > 10% of entity

Classification:
  HOT    → commit_frequency > 10/week
  ACTIVE → commit_frequency 2-10/week
  STABLE → commit_frequency < 2/week
  FROZEN → no changes in 90+ days
```

Velocity is used by:
- Impact analysis (hot files = higher risk)
- Architecture health (frozen code with dependencies = risk)
- PR review suggestions (who's been active here?)

---

### Criticality Enricher

Assigns business criticality to each entity.

This is a **computed score** from multiple signals:

```
Criticality Score Components:

  inbound_dependency_count:
    How many things depend on this?
    (more dependents = higher criticality)

  downstream_user_impact:
    Does failure affect end users directly?
    (from service topology + tracing)

  revenue_path_flag:
    Is this on a known revenue-critical path?
    (from manual annotation or path analysis)

  incident_frequency:
    How often does this cause incidents?

  sla_tier:
    Declared SLA (from annotations or config)

  data_sensitivity:
    Handles PII, payments, auth?
    (from annotations + API analysis)

Final criticality:
  CRITICAL  → score > 0.85
  HIGH      → score 0.65-0.85
  MEDIUM    → score 0.40-0.65
  LOW       → score < 0.40
```

Criticality drives:
- Impact analysis severity scoring
- Change risk assessment
- Alert routing
- Review requirements

---

### Maturity Enricher

Assesses how stable and well-understood an entity is.

```
Maturity Score Components:

  has_api_spec:           bool, weight 0.20
  has_tests:              bool, weight 0.15
  test_coverage:          float, weight 0.15
  has_documentation:      bool, weight 0.15
  has_owner:              bool, weight 0.15
  deployment_stability:   float, weight 0.10
  has_monitoring:         bool, weight 0.10

Maturity Levels:
  MATURE     → score > 0.80
  DEVELOPING → score 0.50-0.80
  NASCENT    → score 0.30-0.50
  LEGACY     → frozen + low coverage + no spec
```

Maturity surfaces technical debt.
Low maturity + high criticality = **dangerous combination**.
The system flags this explicitly.

---

### Annotation Merger

Handles human-provided metadata.

Developers and architects can annotate:
- "This service is on the payment critical path"
- "This API is deprecated, sunset date: 2025-12-01"
- "This dependency is temporary, ticket: JIRA-1234"
- "This service owner is team-payments"

Annotations:
- Come through a separate annotation API
- Have the highest trust level
- Override computed values
- Are versioned and attributed
- Are merged at enrichment stage

```
Annotation Priority:

  Manual annotation > Runtime observed > Statically inferred > Default computed
```

This ensures human knowledge always wins.

---

## ✅ Stage 5: Validation & Conflict Resolution

**Responsibility:** Ensure nothing incorrect enters the graph

---

### Structural Validator

Checks graph integrity rules:

```
Rules:

  REL-001: Relationship endpoints must exist
    If from_entity or to_entity not in registry → REJECT

  REL-002: No self-referential relationships
    If from_entity == to_entity → REJECT (unless type allows it)

  ENT-001: Required properties must be present
    entity_type, canonical_name, namespace → required
    Missing any → REJECT

  ENT-002: Property types must match schema
    confidence must be float 0.0-1.0
    observed_at must be valid timestamp
    Invalid type → REJECT

  ENT-003: Namespace must be valid
    Must exist in registry
    Unknown namespace → HOLD for review
```

---

### Semantic Validator

Checks domain logic rules:

```
Rules:

  SEM-001: Ownership type constraint
    Only Teams can OWN Services
    A Service cannot OWN another Service
    Violation → REJECT with explanation

  SEM-002: Deployment constraint
    Services deploy ON Infrastructure
    Functions do not deploy ON Infrastructure directly
    Violation → REJECT

  SEM-003: Circular dependency detection
    If A → DEPENDS_ON → B → DEPENDS_ON → A
    → Flag as CIRCULAR_DEPENDENCY smell
    → Allow but mark with architectural_smell = true
    → Alert architecture team

  SEM-004: Orphan relationship
    Relationship with no path to any Service node
    → Flag as ORPHAN
    → Hold for review

  SEM-005: Deprecated entity usage
    New DEPENDS_ON relationship pointing to deprecated entity
    → Allow but flag as DEPRECATED_DEPENDENCY
    → Alert owning team
```

---

### Consistency Validator

Checks against existing graph state:

```
Rules:

  CON-001: Entity type immutability
    Once an entity has type = Service
    It cannot become type = Function
    Type change → REJECT (requires explicit migration)

  CON-002: Confidence regression
    New data has lower confidence than existing
    Do not downgrade existing high-confidence data
    → Keep existing, log discrepancy

  CON-003: Ownership contradiction
    New source claims different owner than existing
    with lower confidence
    → Keep existing owner
    → Flag for review
    → Log contributing source

  CON-004: Relationship direction integrity
    Some relationship types are directional
    OWNS: Team → Service (not reversed)
    Reversed direction → REJECT
```

---

### Conflict Resolution Engine

When validators find conflicts:

```
Conflict Resolution Decision Tree:

  Is this a property conflict?
    ├── Is the property volatile? (metrics, timestamps)
    │     → Last-Write-Wins
    │
    └── Is the property structural? (type, name, owner)
          ├── Is new data higher confidence?
          │     → New data wins, log conflict
          │
          └── Is new data lower confidence?
                → Existing data wins, log discrepancy

  Is this a relationship conflict?
    ├── Same type, same direction, different properties?
    │     → Merge properties, take highest confidence
    │
    └── Contradictory relationships?
          → Both held, flagged, human review queue

  Is this unresolvable?
    → Human review queue
    → System uses higher-confidence version until resolved
```

---

### Human Review Queue

For conflicts and uncertainties the system cannot resolve automatically.

Each review item contains:

```
Review Item:
  │
  ├── item_id
  ├── priority           (CRITICAL | HIGH | MEDIUM | LOW)
  ├── review_type        (resolution | conflict | semantic | ownership)
  │
  ├── context
  │     ├── description  (what the conflict is, in plain English)
  │     ├── option_a     (first possibility + evidence)
  │     ├── option_b     (second possibility + evidence)
  │     └── recommendation (system's best guess)
  │
  ├── affected_entities []
  ├── pipeline_run_id
  ├── created_at
  ├── sla_deadline       (based on priority)
  └── assigned_to        (team or individual)
```

Human decisions:
- Feed back into Entity Registry
- Update resolution rules (learning)
- Improve future automatic resolution
- Are audited and versioned

---

## 📊 Stage 6: Delta Computation

**Responsibility:** Determine precisely what changed and produce the minimal set of graph mutations

---

### State Comparator

Loads current graph state for affected entities and compares:

```
Comparison Dimensions:

  Entity level:
    ├── Does this entity exist in graph?
    ├── Have properties changed?
    ├── Has confidence changed significantly?
    ├── Has ownership changed?
    └── Has sub_type changed?

  Relationship level:
    ├── Does this relationship exist?
    ├── Has strength changed?
    ├── Has confidence changed?
    ├── Has latency profile changed?
    └── Is it still active?
```

Property-level diff:

```
Not just "entity changed" but:
  "entity X property Y changed from V1 to V2"

This enables:
  - Precise graph writes (update only changed properties)
  - Precise evolution history (what exactly changed)
  - Precise cache invalidation (only affected queries)
```

---

### Delta Classifier

Classifies every detected change:

```
Entity Delta Types:
  ENTITY_CREATED          → new node needed
  ENTITY_UPDATED          → property update on existing node
  ENTITY_TYPE_MIGRATED    → sub_type changed (rare)
  ENTITY_SOFT_DELETED     → no longer observed, mark inactive
  ENTITY_RESTORED         → was inactive, now seen again
  ENTITY_MERGED           → two nodes became one
  ENTITY_SPLIT            → one node became two

Relationship Delta Types:
  RELATIONSHIP_CREATED    → new edge needed
  RELATIONSHIP_UPDATED    → properties changed
  RELATIONSHIP_DELETED    → no longer observed
  RELATIONSHIP_STRENGTH_CHANGED → frequency changed
  RELATIONSHIP_CONFIDENCE_CHANGED → new evidence changed trust
```

---

### Mutation Planner

Converts delta classifications into ordered, executable graph mutations.

**Ordering is critical:**

```
Correct Order:

  1. ENTITY_CREATED mutations first
     (relationships cannot reference non-existent nodes)

  2. ENTITY_MERGED mutations
     (consolidate before adding relationships)

  3. RELATIONSHIP_CREATED mutations
     (now safe — all entities exist)

  4. ENTITY_UPDATED mutations
     (property updates after structure is correct)

  5. RELATIONSHIP_UPDATED mutations

  6. ENTITY_SOFT_DELETED mutations last
     (relationships referencing this may need cleanup)

  7. RELATIONSHIP_DELETED mutations
```

**Batching Strategy:**

```
Mutations grouped by:
  - Transaction scope (what must succeed or fail together)
  - Entity proximity (related entities in same batch)
  - Size limit (max 500 mutations per batch)

Batch contains:
  - All mutations in the batch
  - Compensation actions (what to undo if batch fails)
  - Pipeline run ID (for audit trail)
  - Ordering guarantees
```

**Compensation Actions:**

If a batch partially fails:

```
Example:
  Batch: [Create Entity A, Create Entity B, Create Relationship A→B]

  Entity A created ✅
  Entity B created ✅
  Relationship A→B failed ❌

  Compensation:
    Option 1: Delete Entity A and B (full rollback)
    Option 2: Mark relationship as pending, retry
    
  Decision: Based on whether entities are already referenced elsewhere
```

---

## 🔄 Cross-Cutting Concerns

### Pipeline Ledger

Tracks every record through every stage:

```
Ledger Entry Per Record:
  │
  ├── record_id
  ├── pipeline_run_id
  ├── ingestion_run_id       (traceability to source)
  │
  ├── stage_results []
  │     └── {stage, status, duration_ms, output_confidence}
  │
  ├── final_status           (accepted | rejected | held | review)
  ├── final_entity_id        (canonical ID after resolution)
  ├── delta_type             (created | updated | deleted | ...)
  └── graph_mutation_id      (link to graph write)
```

Full traceability:
> "Why does this node have this property?"
> → Trace back through ledger to source signal

---

### Error Handler

Every stage can produce errors.
Error handling is:

```
Stage Failure Strategy:

  Record-level failure:
    → Skip this record
    → Log to pipeline DLQ
    → Continue processing other records
    → Never fail the whole pipeline for one record

  Stage-level failure:
    → Circuit breaker trips after threshold
    → Records accumulate in stage buffer
    → Alert operations
    → Auto-retry with backoff
    → Fall back to last known good state

  Pipeline-level failure:
    → Only if multiple stages fail simultaneously
    → Full pipeline pause
    → Alert immediately
    → Preserve all in-flight records
    → Resume from last checkpoint
```

---

### Pipeline Health

Metrics emitted per stage:

| Metric | What It Measures |
|--------|----------------|
| Stage throughput | Records per second |
| Stage latency | Time per record |
| Stage error rate | Failed records / total |
| Resolution rate | % auto-resolved vs human review |
| Inference rate | Inferences produced per entity |
| Conflict rate | Conflicts detected per run |
| Delta size | Mutations per pipeline run |
| Queue depth per stage | Backpressure indicator |

---

## 🔄 End-to-End Flow: One Record's Journey

Let's trace a single NIF record through the entire pipeline:

```
NIF Record arrives:
  type: Service
  raw_name: "PaymentSvc"
  source: kubernetes
  namespace: org-acme

─────────────────────────────────────────
STAGE 1: Parse & Classify

  Entity Classifier:
    type = Service confirmed
    sub_type = Microservice (no internet exposure detected)

  Naming Normalizer:
    "PaymentSvc" → "payment-service"

  Intent Detector:
    Check registry... exists!
    Intent = UPDATE

─────────────────────────────────────────
STAGE 2: Entity Resolution

  Candidate Generator:
    Found: canonical_id = svc_payment_001
    Match: "payment-service" in org-acme namespace

  Signal Extractors:
    Exact match: 0.85
    Structural match: 0.80 (same API endpoints)
    Total confidence: 0.91

  Decision Engine:
    LIKELY MERGE → auto-merge
    Map to canonical_id = svc_payment_001

─────────────────────────────────────────
STAGE 3: Relationship Inference

  Structural Inferrer:
    New K8s manifest shows:
    payment-service reads from table "transactions"
    Infer: payment-service → READS_FROM → transactions
    confidence: 0.88

  Runtime Inferrer:
    No new runtime signals for this record

  Deduplicator:
    READS_FROM → transactions already exists
    Same relationship, update last_confirmed timestamp

─────────────────────────────────────────
STAGE 4: Enrichment

  Ownership Enricher:
    CODEOWNERS: team-payments
    No change from existing

  Velocity Enricher:
    3 commits this week → ACTIVE (no change)

  Criticality Enricher:
    Recalculate: score = 0.82 → HIGH (unchanged)

  Maturity Enricher:
    New K8s manifest has resource limits defined
    maturity_score: 0.71 → 0.74 (slight increase)

─────────────────────────────────────────
STAGE 5: Validation

  Structural: ✅ all required fields present
  Semantic:   ✅ no rule violations
  Consistency:✅ no contradictions with existing

─────────────────────────────────────────
STAGE 6: Delta Computation

  State Comparator:
    Existing entity: maturity_score = 0.71
    New entity:      maturity_score = 0.74
    Changed: maturity_score

    Existing relationship: READS_FROM → last_confirmed = 3 days ago
    New:                   last_confirmed = now
    Changed: last_confirmed

  Delta Classifier:
    ENTITY_UPDATED (maturity_score)
    RELATIONSHIP_UPDATED (last_confirmed)

  Mutation Planner:
    Batch:
      1. UPDATE entity svc_payment_001 {maturity_score: 0.74}
      2. UPDATE relationship rel_reads_tx_001 {last_confirmed: now}

─────────────────────────────────────────
Graph Mutation API receives batch
2 precise writes → graph updated
Ledger updated
Cache invalidated for affected queries
```

Total journey: **one record, 6 stages, 2 precise graph writes**

---

## ⚠️ Failure Scenarios

| Scenario | Stage | Response |
|----------|-------|----------|
| Naming normalizer crashes | Stage 1 | Use raw name, flag as unnormalized |
| Resolution finds no candidates | Stage 2 | Create new entity |
| Circular inference chain | Stage 3 | Detect cycle, break at lowest confidence link |
| Enricher source unavailable | Stage 4 | Skip that enricher, use existing values |
| Semantic rule violation | Stage 5 | Reject record, explain violation |
| Graph state load fails | Stage 6 | Retry with backoff, hold mutations |
| Mutation batch partial fail | Output | Compensate, retry failed portion |

---

## 🔑 Design Decisions Summary

| Decision | Choice | Reasoning |
|----------|--------|-----------|
| Pipeline routing | Type-based routing | Not all records need all stages |
| Entity resolution | Multi-signal confidence | No single signal is enough |
| Inference depth | Bounded (max 5 hops) | Deeper = noise |
| Inference storage | Confidence-tagged | Inferred ≠ observed |
| Enrichment priority | Annotation > Runtime > Static | Human knowledge wins |
| Conflict resolution | Multi-strategy | Different conflict types need different strategies |
| Delta granularity | Property-level | Minimal graph writes |
| Mutation ordering | Entities before relationships | Graph integrity |
| Error handling | Per-record isolation | One bad record never blocks others |

---

## 🧠 Mental Model

Zone 3 is:

> **A refinement assembly line where raw observations are put through increasing levels of scrutiny until only confident, enriched, validated facts remain**

Every stage asks a different question:

| Stage | Question |
|-------|---------|
| Parse & Classify | What is this? |
| Entity Resolution | Have we seen this before? |
| Relationship Inference | What does this connect to? |
| Enrichment | What do we know about this? |
| Validation | Is this consistent and correct? |
| Delta Computation | What exactly changed? |

By the time a record exits Zone 3:
- Its identity is certain
- Its relationships are complete
- Its context is rich
- Its correctness is validated
- Its impact on the graph is minimal and precise

---
