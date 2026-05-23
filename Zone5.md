# 🟪 Zone 5: Intelligence & Serving Layer — System Design Deep Dive

---

## 🎯 Zone Responsibility

Zone 5 has one sacred job:

> Translate the raw structural, historical, and runtime data stored in Zone 4 into actionable, human-comprehensible architectural intelligence, and serve it reliably, securely, and at sub-second speeds to consumers.

If Zone 4 is the **architectural memory** of the system, Zone 5 is the **reasoning brain** and the **voice**. 

It is responsible for:
- Interpreting natural language developer queries.
- Planning and executing efficient graph traversals.
- Synthesizing graph data and telemetry using LLMs.
- Auditing architectural health and tracking evolution over time.
- Exposing clean, secure APIs for all downstream integrations.

---

## 🧠 Core Design Philosophy

Four core principles govern the design of Zone 5:

### Principle 1: Graph RAG Over Vector RAG
Pure vector databases match by semantic similarity, which fails when answering exact structural questions like *"Which services call this endpoint?"* or *"What is the blast radius of changing this table schema?"*. Zone 5 relies on deterministic graph query planning first, using vector embeddings only for intent classification and initial node resolution.

### Principle 2: Separate Querying from Reasoning
LLMs are notoriously unreliable at traversing graph databases directly via text prompts. Zone 5 uses a deterministic **Query Planner** to fetch structural context (subgraphs) from the database, then feeds this clean structured context to the **LLM Reasoner** solely for formatting, explanation, and synthesis.

### Principle 3: Temporal-Aware Analysis
Architecture is not static; it is a stream of changes. The intelligence layer must treat time as a first-class parameter, enabling comparative reasoning (e.g., *"What changed in our service topology between commit X and commit Y?"*).

### Principle 4: Strict Verifiability (No Hallucinations)
Every assertion made by the Serving Layer must cite its sources. If the LLM states that two services are coupled, the output must contain verifiable links to the specific graph nodes, code repositories, lines of code (from AST), or tracing spans that corroborate the claim.

---

## 🏗️ Full Zone 5 Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│              INTELLIGENCE & SERVING LAYER                       │
│                                                                 │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │                     QUERY ENGINE                          │  │
│  │                                                           │  │
│  │   ┌──────────────────┐   ┌─────────────────────────────┐  │  │
│  │   │Intent Classifier │   │    Query Planner            │  │  │
│  │   │ (Vector Router)  ├──▶│ (Cypher & Metric Generator) │  │  │
│  │   └──────────────────┘   └──────────────┬──────────────┘  │  │
│  │                                         │                 │  │
│  │                                         ▼                 │  │
│  │                          ┌─────────────────────────────┐  │  │
│  │                          │    Subgraph Retriever       │  │  │
│  │                          │    (PageRank Pruning)       │  │  │
│  │                          └──────────────┬──────────────┘  │  │
│  └─────────────────────────────────────────┼─────────────────┘  │
│                                            │                    │
│                                            ▼                    │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │                   REASONING ENGINE                        │  │
│  │                                                           │  │
│  │   ┌──────────────────┐   ┌─────────────────────────────┐  │  │
│  │   │ Context Assembler│   │      LLM Reasoner           │  │  │
│  │   │  (NIF to Text)   ├──▶│  (Chain-of-Thought Agent)   │  │  │
│  │   └──────────────────┘   └──────────────┬──────────────┘  │  │
│  │                                         │                 │  │
│  │                                         ▼                 │  │
│  │                          ┌─────────────────────────────┐  │  │
│  │                          │      Answer Formatter       │  │  │
│  │                          │   (Mermaid & Citations)     │  │  │
│  │                          └──────────────┬──────────────┘  │  │
│  └─────────────────────────────────────────┼─────────────────┘  │
│                                            │                    │
│                                            ▼                    │
│  ┌─────────────────────────────────────────┴─────────────────┐  │
│  │               SPECIALIZED ANALYTICAL ENGINES              │  │
│  │                                                           │  │
│  │  ┌──────────────────┐ ┌──────────────────┐ ┌───────────┐  │  │
│  │  │Evolution Tracker │ │  Impact Analyzer │ │  Health   │  │  │
│  │  │ (Diff Engine)    │ │  (Blast Radius)  │ │  Auditor  │  │  │
│  │  └──────────────────┘ └──────────────────┘ └───────────┘  │  │
│  └─────────────────────────────────────────┬─────────────────┘  │
│                                            │                    │
│                                            ▼                    │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │                    PUBLIC SERVING LAYER                   │  │
│  │                                                           │  │
│  │   ┌──────────────────┐ ┌──────────────────┐ ┌───────────┐  │  │
│  │   │    REST API      │ │   GraphQL API    │ │ WebSockets│  │  │
│  │   └──────────────────┘ └──────────────────┘ └───────────┘  │  │
│  │                                                           │  │
│  │   ┌─────────────────────────────────────────────────────┐  │  │
│  │   │          Caching & Rate Limiting Guard              │  │  │
│  │   └─────────────────────────────────────────────────────┘  │  │
│  └───────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

---

## 🔍 Subsystem 1: Query Engine

The Query Engine parses requests, generates database execution plans, and retrieves the minimal set of facts needed to answer a query.

### 1. Intent Classifier

Before generating queries, the Intent Classifier routes incoming natural language requests into one of five predefined **Query Archetypes**.

```
Incoming Query ──▶ [Embedding Generator] ──▶ [Vector Matcher] ──▶ Archetype Route
```

| Archetype | Question Example | Routing Target | Required Parameters |
|:---|:---|:---|:---|
| **Structural** | *"What are the dependencies of PaymentService?"* | Live Graph DB | `target_node_id`, `traversal_depth` |
| **Runtime** | *"Which endpoint of AuthSvc is slowest?"* | Graph DB + Metric Store | `target_node_id`, `metric_window` |
| **Temporal** | *"How did billing change in the last month?"* | Delta Log + Snapshots | `t1`, `t2`, `namespace` |
| **Impact** | *"What breaks if I delete UserTable?"* | Impact Analyzer | `origin_node_id`, `propagation_type` |
| **Governance** | *"Do we have any circular dependencies?"* | Health Auditor | `smell_type`, `namespace` |

---

### 2. Query Planner

The Query Planner translates the classified intent and its extracted parameters into execution plans. It targets two distinct systems: Cypher (for graph traversals in Zone 4's Graph DB) and SQL/Time-series queries (for runtime telemetry).

#### Example: Resolving a "Structural + Runtime" Intent
*Natural Query:* *"Find all services calling `payment-service` that have p99 latency > 200ms."*

The planner decomposes this into a Cypher query referencing temporal relationships:

```cypher
MATCH (caller:Service)-[edge:RUNTIME_CALLS]->(target:Service {canonical_name: 'payment-service'})
WHERE edge.is_active = true 
  AND edge.p99_latency_ms > 200
RETURN caller.canonical_name AS caller_service, 
       edge.p99_latency_ms AS latency, 
       edge.error_rate AS error_rate, 
       caller.owner_team_id AS owner_team
ORDER BY edge.p99_latency_ms DESC
LIMIT 50
```

For complex metrics that have drifted out of the Graph DB's hot partition, the Planner coordinates a federated retrieval:
1. **Graph Step:** Query Graph DB for the immediate caller topology.
2. **Metrics Step:** Fetch time-series aggregates for those edges from the Runtime Metrics Store for the requested time frame.
3. **Join Step:** Merge the telemetry series back onto the fetched subgraph edges.

---

### 3. Subgraph Retriever & Context Pruner

Code graphs are dense. Querying a popular service with a traversal depth of `3` can easily fetch 5,000+ nodes (monoliths, shared modules, libraries, tables), which would overflow the LLM's context window.

The **Context Pruner** implements a weighted PageRank-like propagation algorithm to select only the most relevant subgraphs.

```
                  [Origin Node]
                       │
       ┌───────────────┴───────────────┐
       ▼ (weight: 1.0)                 ▼ (weight: 0.2)
[Direct Import]                 [Unused Util Module]
       │                               │
       ▼ (weight: 0.8)                 ▼ (weight: 0.05)
[API Endpoint]                  [Transitive Library]
```

#### Pruning Weight Formulas:
The score of a retrieved entity $E$ in relation to an origin node $O$ is defined as:

$$Score(E) = \prod_{edge \in path(O \to E)} Weight(edge.type) \times Confidence(E)$$

Where default edge type weights are configured as:

| Edge Type | Weight | Rationale |
|:---|:---|:---|
| `EXPOSES` | **1.0** | Crucial interface boundaries |
| `RUNTIME_CALLS` | **0.9** | Active runtime dependencies |
| `IMPORTS` | **0.7** | Static dependency structure |
| `OWNS` | **0.6** | Ownership metadata |
| `DEPENDS_ON` | **0.5** | Declared service boundaries |
| `CHANGE_COUPLED_WITH` | **0.4** | Co-evolution indicator |
| `CALLS` (symbol-level) | **0.2** | Too noisy, heavily pruned |

#### Context Safety Guard:
If the accumulated subgraph tokens exceed **64,000 tokens** (configurable threshold):
1. The pruner decreases the traversal depth parameter by `1`.
2. It drops nodes with $Score(E) < 0.15$.
3. It collapses fine-grained entities (symbol-level files/functions) into service-level summaries (e.g., *"and 45 other internal files"*).

---

## 🧠 Subsystem 2: Reasoning Engine

Once clean, pruned subgraphs are retrieved, they are processed by the Reasoning Engine to format context and generate final answers.

```
Subgraphs ──▶ [Context Assembler] ──▶ Prompt Payload ──▶ [LLM Reasoner] ──▶ [Answer Formatter] ──▶ Final Answer
```

### 1. Context Assembler

The Context Assembler serializes Graph DB node/edge collections into a clean, hierarchical Markdown format optimized for LLM comprehension.

#### Example Serialized Context Output:
```markdown
### TARGET ENTITY
- **ID:** `svc-payment-db71a`
- **Type:** Service (Microservice)
- **Canonical Name:** `payment-service`
- **Owner:** `team-billing` (Confidence: 0.95)
- **Criticality:** CRITICAL
- **Risk Score:** 0.18

### ACTIVE RUNTIME CALLERS
1. **Entity:** `order-service` (`svc-order-912bc`)
   - **Relationship:** RUNTIME_CALLS
   - **Latency:** p50: 12ms | p99: 210ms (Anomaly detected)
   - **Call Frequency:** 14,200/day
   - **Trace Verification:** [OTel-Span-91bc2](file:///traces/91bc2)

### SYSTEM CONTRACTS EXPOSED
1. **Endpoint:** `POST /v1/charges` (`api-charge-e221f`)
   - **Spec:** OpenAPI 3.0 [payment_spec.yaml](file:///Users/MacBook/Fun_Project/Fun-Project/specs/payment_spec.yaml#L45)
   - **Consumers:** `order-service`, `subscription-service`
```

---

### 2. LLM Reasoner

The Reasoner orchestrates the LLM call using a system prompt configured for architectural reasoning.

#### System Prompt Template:
```
You are an expert Software Architect analyzing a live AI Codebase Knowledge Graph.
Your task is to answer user queries using ONLY the provided serialized context.

Rules:
1. Citations: You MUST cite the source of every fact. Use markdown links: [Name](file:///path/to/source#line) or node IDs [node-id].
2. Code references: When referencing files or schemas, use the exact file URLs provided in the context.
3. Hallucinations: If the context does not contain enough information to resolve a query, state explicitly: "I do not have the telemetry/code context to answer this."
4. Mermaid diagrams: When explaining structures, generate a clean Mermaid.js diagram to visualize nodes and edges.
5. Analytical Tone: Keep the analysis highly objective and technical. Focus on structural boundaries, failure modes, and coupling.
```

---

### 3. Answer Formatter

The Answer Formatter post-processes the raw text returned by the LLM:
- **Mermaid Validator:** Validates that the Mermaid syntax compiles. If invalid, it strips the code block or routes to a fallback generator.
- **Citation Enhancer:** Rewrites absolute local workspace paths into clickable markdown IDE links (e.g., standardizing `file:///absolute/path` links).

---

## 📊 Subsystem 3: Specialized Analytical Engines

These engines bypass the LLM and perform high-performance analytical calculations directly on the Graph Storage.

```
           [Specialized Analytics Query]
                         │
         ┌───────────────┼───────────────┐
         ▼               ▼               ▼
 [Evolution Tracker] [Impact Analyzer] [Health Auditor]
```

### 1. Evolution Tracker (Diff Engine)

The Evolution Tracker calculates changes in the codebase architecture over time by comparing two snapshots or replaying the Delta Log.

```
Snapshot A (t1) ──┐
                  ├──▶ [Diff Engine] ──▶ Graph Delta Report
Snapshot B (t2) ──┘
```

#### Diff Algorithm:
Given two graphs $G_{t1} = (V_{t1}, E_{t1})$ and $G_{t2} = (V_{t2}, E_{t2})$:

1. **Entity Additions ($\Delta V^+$):**
   $$\Delta V^+ = \{v \in V_{t2} \mid v.id \notin V_{t1}.ids\}$$

2. **Entity Removals ($\Delta V^-$):**
   $$\Delta V^- = \{v \in V_{t1} \mid v.id \notin V_{t2}.ids\}$$

3. **Relationship Mutations ($\Delta E^{\Delta}$):**
   Detect changes in weights, latency, or ownership:
   $$\Delta E^{\Delta} = \{ (e_{t1}, e_{t2}) \in E_{t1} \times E_{t2} \mid e_{t1}.id = e_{t2}.id \wedge e_{t1}.properties \neq e_{t2}.properties \}$$

#### Example Output Report:
```json
{
  "diff_summary": {
    "nodes_added": 3,
    "nodes_removed": 1,
    "edges_modified": 12
  },
  "drift_alerts": [
    {
      "severity": "HIGH",
      "message": "Service 'notification-service' has bypassed the Gateway. Direct RUNTIME_CALLS from 'order-service' appeared.",
      "introduced_in": "commit-91a2bc8",
      "author": "dev-alice@company.com"
    }
  ]
}
```

---

### 2. Impact Analyzer (Blast Radius Engine)

Computes the downstream blast radius if an entity is modified, deprecated, or fails.

```
[Origin Node] ──▶ Transitive Callers ──▶ Shared Databases ──▶ Affected Teams
```

#### Blast Radius Propagation Algorithm:
```python
def calculate_blast_radius(origin_id: str, max_depth: int = 5) -> dict:
    visited = {}
    queue = [(origin_id, 0, 1.0)]  # (node_id, current_depth, impact_probability)
    affected_nodes = []
    
    while queue:
        node_id, depth, prob = queue.pop(0)
        
        if node_id in visited and visited[node_id] >= prob:
            continue
        visited[node_id] = prob
        
        node = graph_db.get_node(node_id)
        affected_nodes.append({
            "node_id": node_id,
            "type": node.type,
            "depth": depth,
            "impact_probability": prob
        })
        
        if depth >= max_depth:
            continue
            
        # Traverse incoming edges (callers/dependents of the target)
        incoming_edges = graph_db.get_incoming_edges(node_id)
        for edge in incoming_edges:
            # Impact decays as traversal gets further from origin
            decay = get_propagation_decay(edge.type)
            next_prob = prob * decay * edge.confidence
            
            if next_prob > 0.10:  # Pruning threshold
                queue.append((edge.from_id, depth + 1, next_prob))
                
    return sort_by_impact(affected_nodes)
```

Where `get_propagation_decay()` is defined by the coupling severity:
- `IMPORTS` (static): **0.95** (Strong coupling; code breaks on build if API changes)
- `RUNTIME_CALLS` (dynamic): **0.80** (High runtime impact; handled by retry logic/timeouts)
- `DATA_COUPLED_WITH`: **0.70** (Shared schema reliance)
- `DEPENDS_ON` (implicit): **0.50**

---

### 3. Architecture Health Auditor

The Health Auditor evaluates the live graph against structural rules to identify **architectural smells** and technical debt.

#### Standard Smells Evaluated:

##### A. Circular Dependency Detection
- **Rule:** Detect cycles of length $\ge 2$ in service or module import trees.
- **Algorithm:** Tarjan's Strongly Connected Components (SCC).
- **Smell Severity:** CRITICAL (breaks independent deployment, causes circular build sequences).

##### B. Shared Database Coupling
- **Rule:** Two distinct domain services reading/writing to the same physical database table.
- **Query Pattern:**
  ```cypher
  MATCH (s1:Service)-[:READS_FROM|WRITES_TO]->(t:DatabaseTable)<-[:READS_FROM|WRITES_TO]-(s2:Service)
  WHERE s1 <> s2 AND s1.namespace = s2.namespace
  RETURN s1, s2, t
  ```
- **Smell Severity:** HIGH (violates microservice boundary isolation, risks runtime database locking).

##### C. Hub-and-Spoke Bottleneck (Supernode)
- **Rule:** A single node with in-degree / out-degree counts deviating by $> 4$ standard deviations from the workspace mean.
- **Smell Severity:** MEDIUM (indicates single points of failure or monolithic helper modules).

---

## 🔌 Subsystem 4: Public API & Serving Layer

This layer handles downstream security, authentication, isolation, and query acceleration.

### 1. Schema Specifications

#### A. GraphQL Schema excerpt
```graphql
type Query {
  entity(id: ID!): Entity
  searchEntities(query: String!, limit: Int): [Entity!]!
  blastRadius(originId: ID!, maxDepth: Int): BlastRadiusReport!
  architectureDiff(t1: String!, t2: String!): DiffReport!
  ask(question: String!): AIAnswer!
}

type Entity {
  id: ID!
  canonicalName: String!
  type: String!
  namespace: String!
  criticality: String!
  healthScore: Float!
  owners: [Team!]!
  dependencies(direction: Direction): [Relationship!]!
}

type AIAnswer {
  text: String!
  mermaidDiagram: String
  citations: [Citation!]!
  confidence: Float!
}

type Citation {
  targetId: ID!
  sourceUrl: String!
  evidenceType: String!
}
```

#### B. WebSocket Real-time API
Used by IDE plugins for active workspace linting.
- **Connection URL:** `wss://api.archgraph.internal/v1/workspace/stream`
- **Payload Structure:**
  ```json
  // Client updates server on active file changes
  {
    "event": "editor_focus",
    "payload": {
      "file_path": "/Users/MacBook/Fun_Project/Fun-Project/services/payment.py",
      "cursor_line": 128
    }
  }

  // Server responds with immediate local coupling hints
  {
    "event": "context_insights",
    "payload": {
      "file": "payment.py",
      "connected_services": [
        { "name": "order-service", "coupling": "high", "p99_latency_ms": 210 }
      ],
      "active_architectural_smells": [
        { "type": "circular_dependency", "nodes": ["payment.py", "auth.py", "payment.py"] }
      ]
    }
  }
  ```

---

### 2. Serving Guardrails & Scalability Policies

#### A. Multi-tenant Isolation
To prevent data leaks between distinct organizations, the public serving APIs enforce isolation using a tenant header validation middleware:
- Every query compiles with an implicit filter: `WHERE tenant_id = context.tenant_id`.
- Graph traversals cannot traverse across namespaces unless explicit cross-tenant federations are registered.

#### B. Cache Strategy
Graph reads are accelerated through a two-tiered caching mechanism:
1. **Tier 1 (Edge Cache):** Redis cluster caches completed GraphQL query results and resolved subgraphs with a short TTL (10 seconds for runtime-heavy structures, 10 minutes for static-only structures).
2. **Tier 2 (Graph Memory Cache):** The Serving Layer maintains the structural layout of Tier 1-2 (Organization and Service relationships) in-memory for instant blast radius computations.

#### C. Cache Invalidation Triggers:
When the mutation writer in Zone 4 appends a change to the Delta Log:
- It publishes an invalidation event matching the modified entity ID.
- The cache cluster invalidates all keys referencing that ID or any of its immediate (1-hop) neighbors.

---

## 📊 Serving Complexity & SLA Goals

| API Endpoint | Target Latency | P99 SLA | Bottleneck Source | Mitigation Strategy |
|:---|:---|:---|:---|:---|
| `/searchEntities` | 15ms | 50ms | DB Index Lookup | Elasticsearch replication |
| `/blastRadius` | 45ms | 120ms | Graph traversal depth | In-memory structural caching |
| `/architectureDiff`| 150ms | 400ms | Multi-snapshot comparison | Pre-computed daily snapshot differentials |
| `/ask` (AI Q&A) | 1,200ms | 3,500ms | LLM Generation time | Streaming WebSocket tokens & prompt caching |
