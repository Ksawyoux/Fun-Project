# 🟦 Zone 1: Signal Sources — Deep Dive

---

## 🎯 Zone Responsibility

Zone 1 is **not** an active part of the system.

This is a critical distinction:

> Zone 1 is the **external world** the system observes.
> Zone 2 (Ingestion) reaches into Zone 1 to pull signals.

Zone 1 itself does nothing.
But understanding it deeply is essential because:

- It determines **what we can know**
- It determines **how fresh our knowledge is**
- It determines **how complex ingestion must be**
- It determines **what gaps exist in the graph**

---

## 🗂️ Signal Taxonomy

Before listing sources, let's establish a taxonomy.

Every signal has four dimensions:

### Dimension 1: Nature
| Nature | Meaning |
|--------|---------|
| Static | Represents structure at a point in time |
| Dynamic | Represents behavior as it happens |

### Dimension 2: Trigger
| Trigger | Meaning |
|---------|---------|
| Event-driven | Changes when something happens |
| Continuous | Always flowing |
| On-demand | Pulled when needed |

### Dimension 3: Fidelity
| Fidelity | Meaning |
|----------|---------|
| High | Precise, authoritative, complete |
| Medium | Mostly accurate, some gaps |
| Low | Approximation, needs corroboration |

### Dimension 4: Freshness
| Freshness | Meaning |
|-----------|---------|
| Real-time | Seconds old |
| Near real-time | Minutes old |
| Periodic | Hours to days old |
| Historical | Archived, immutable |

---

## 📡 Signal Sources — Full Breakdown

---

### 🔹 Source 1: Git Repositories

**What it tells us:**
> The structural and historical truth of the codebase

#### Signals Available:

| Signal | What It Reveals |
|--------|----------------|
| File tree | Module structure, boundaries |
| Commit history | What changed, when, by whom |
| Commit messages | Intent behind changes |
| Branches | Active development streams |
| Tags / Releases | Deployment markers |
| Git blame | Who wrote what |
| Diff history | How files evolved |
| Merge patterns | How teams collaborate |
| CODEOWNERS | Declared ownership |

#### Characteristics:
| Dimension | Value |
|-----------|-------|
| Nature | Static |
| Trigger | Event-driven (on push) |
| Fidelity | High |
| Freshness | Near real-time |

#### Key Challenges:

**Monorepo vs Polyrepo:**

Monorepo:
- One repo, many services
- Need boundary detection
- Ownership per subdirectory
- Independent change tracking per service

Polyrepo:
- Many repos, one service each
- Need cross-repo relationship tracking
- Dependency references across repo boundaries

**Scale:**
- Large orgs have thousands of repos
- Commit volume can be enormous
- Need selective, incremental processing

**What It Cannot Tell Us:**
- How code actually behaves at runtime
- Which paths are actually executed
- Performance characteristics
- What actually calls what dynamically

---

### 🔹 Source 2: AST (Abstract Syntax Trees)

**What it tells us:**
> The structural meaning of code, beyond raw text

#### What AST Reveals:

| Signal | What It Reveals |
|--------|----------------|
| Import statements | Explicit dependencies |
| Function signatures | Interface contracts |
| Function calls | Static call graph |
| Class hierarchies | Inheritance relationships |
| Variable types | Data flow hints |
| Annotations / Decorators | Framework metadata |
| Module exports | Public API surface |
| Error handling | Resilience patterns |

#### Characteristics:
| Dimension | Value |
|-----------|-------|
| Nature | Static |
| Trigger | On-demand (after git pull) |
| Fidelity | High for static, low for dynamic |
| Freshness | Near real-time |

#### Key Challenges:

**Multi-language reality:**

Every language has its own AST shape:

| Language | Specific Challenges |
|----------|-------------------|
| Python | Dynamic typing, runtime imports |
| TypeScript | Type erasure, complex generics |
| Go | Interface satisfaction implicit |
| Java | Reflection, Spring magic |
| Ruby | Metaprogramming everywhere |
| Scala | Complex type system |

No universal AST parser exists.
Need language-specific parsers that all output the same NIF.

**Dynamic Language Problem:**

```python
# Static analysis cannot resolve this
service = get_service(config["service_name"])
service.call()
```

AST sees a call but cannot know the target.
Runtime signals must fill this gap.

**Framework Magic:**

Spring, Rails, Django — inject dependencies at runtime.
AST alone misses these entirely.

**What It Cannot Tell Us:**
- Runtime behavior
- Dynamic dispatch targets
- Actual execution frequency
- Performance characteristics

---

### 🔹 Source 3: API Specifications

**What it tells us:**
> The declared contracts between services

#### Spec Types:

| Format | Used For |
|--------|---------|
| OpenAPI / Swagger | REST APIs |
| GraphQL Schema | GraphQL APIs |
| Proto files | gRPC services |
| AsyncAPI | Event-driven APIs |
| WSDL | Legacy SOAP |

#### Signals Available:

| Signal | What It Reveals |
|--------|----------------|
| Endpoints | What a service exposes |
| Request schemas | What consumers must send |
| Response schemas | What producers return |
| Authentication | Security requirements |
| Versioning | API evolution |
| Deprecation markers | What's going away |
| Error definitions | Failure contracts |
| Rate limits | Capacity signals |

#### Characteristics:
| Dimension | Value |
|-----------|-------|
| Nature | Static |
| Trigger | Event-driven (on deploy) |
| Fidelity | High (when kept updated) |
| Freshness | Periodic |

#### Key Challenges:

**Spec Drift:**

The most painful challenge:

> The spec says one thing.
> The code does another.
> Reality is a third thing.

Specs get outdated.
Teams forget to update them.
Auto-generation helps but isn't universal.

**Missing Specs:**

Many services — especially legacy — have no spec at all.
The system must infer API surface from:
- Code analysis
- Runtime traces
- Network observation

**Undocumented Behavior:**

Error responses, edge cases, implicit contracts — often not in specs.

**What It Cannot Tell Us:**
- Actual runtime behavior
- Which consumers actually use which endpoints
- Performance characteristics
- Whether the spec is accurate

---

### 🔹 Source 4: Distributed Tracing

**What it tells us:**
> How services actually talk to each other at runtime

This is the **most powerful dynamic signal**.

#### What Traces Reveal:

| Signal | What It Reveals |
|--------|----------------|
| Service-to-service calls | Actual runtime dependencies |
| Call latency per hop | Where time is spent |
| Call frequency | Dependency strength |
| Error rates per hop | Failure propagation |
| Trace depth | Dependency chain length |
| Retry patterns | Resilience behavior |
| Async boundaries | Queue/event relationships |
| Database calls | Data access patterns |

#### Characteristics:
| Dimension | Value |
|-----------|-------|
| Nature | Dynamic |
| Trigger | Continuous |
| Fidelity | High (for what's traced) |
| Freshness | Real-time |

#### Key Challenges:

**Volume:**

A system handling 10,000 RPS generates:
- Millions of spans per minute
- Terabytes per day

We cannot ingest everything.
Sampling is mandatory.

**Sampling Strategy:**

| Strategy | When to Use |
|----------|------------|
| Head-based sampling | Simple, cheap, loses rare events |
| Tail-based sampling | Captures errors and slow traces |
| Adaptive sampling | Adjusts to traffic patterns |
| Priority sampling | Always capture critical paths |

For our purposes:
- We care about **relationships**, not individual traces
- Aggregate spans into **edge-level statistics**
- Keep: call graph, latency distributions, error rates
- Discard: individual span payloads

**Incomplete Instrumentation:**

Not all services are instrumented.
Legacy services, third-party services — invisible.

Gaps in traces = gaps in graph.
Must be marked as such.

**What It Cannot Tell Us:**
- Why a dependency exists (need code for that)
- Ownership
- Historical structure (traces are ephemeral)
- Future change impact

---

### 🔹 Source 5: Metrics & APM

**What it tells us:**
> The health and performance of every service

#### Signals Available:

| Signal | What It Reveals |
|--------|----------------|
| Request rate | Traffic volume |
| Error rate | Reliability |
| Latency (p50, p95, p99) | Performance |
| Saturation | Resource pressure |
| CPU / Memory usage | Capacity |
| DB query performance | Data layer health |
| Cache hit rates | Efficiency |
| Queue depths | Backpressure |
| Deployment markers | Change correlation |

#### Characteristics:
| Dimension | Value |
|-----------|-------|
| Nature | Dynamic |
| Trigger | Continuous |
| Fidelity | High |
| Freshness | Real-time |

#### Key Challenges:

**Volume:**

Metrics are extremely high volume.
We don't store raw metrics in the graph.

Strategy:
- Store statistical summaries on graph nodes
- Link to time-series store for detail
- Refresh summaries periodically

**Metric Naming Chaos:**

Different teams name the same thing differently:
```
payment_service_latency_ms
payments.latency
svc_payment_response_time
```

Need metric normalization mapping.

**Coverage:**

Not all services emit the same metrics.
Some emit too many (noise).
Some emit too few (blind spots).

**What It Cannot Tell Us:**
- Why something is slow (need traces + code)
- Ownership
- Structural relationships
- Historical code changes

---

### 🔹 Source 6: CI/CD Pipelines

**What it tells us:**
> How software is built, tested, and deployed

#### Signals Available:

| Signal | What It Reveals |
|--------|----------------|
| Build dependencies | What needs what to build |
| Test results | Reliability trends |
| Deployment frequency | Team velocity |
| Deployment targets | Service-to-infra mapping |
| Rollback events | Instability signals |
| Build duration | Complexity proxy |
| Test coverage | Quality signal |
| Pipeline failures | Risk areas |
| Artifact dependencies | Runtime dependency hints |

#### Characteristics:
| Dimension | Value |
|-----------|-------|
| Nature | Static + Dynamic |
| Trigger | Event-driven |
| Fidelity | High |
| Freshness | Near real-time |

#### Key Challenges:

**Pipeline Diversity:**

| Tool | Notes |
|------|-------|
| GitHub Actions | YAML-based |
| GitLab CI | YAML-based |
| Jenkins | Groovy-based |
| CircleCI | YAML-based |
| ArgoCD | GitOps |
| Tekton | Kubernetes-native |

Each has its own data model.
Need tool-specific connectors.

**Build vs Deploy:**

Build pipeline ≠ Deploy pipeline
Need to track both separately and link them.

**What It Cannot Tell Us:**
- Runtime behavior
- Code structure
- Ownership (sometimes)

---

### 🔹 Source 7: Infrastructure as Code

**What it tells us:**
> Where services live and what they depend on infrastructurally

#### Signals Available:

| Signal | What It Reveals |
|--------|----------------|
| Service definitions | What runs where |
| Resource dependencies | Infra-level coupling |
| Environment topology | Staging, prod, etc. |
| Scaling configuration | Capacity design |
| Network policies | Communication rules |
| Secret references | Security dependencies |
| Database provisioning | Data infrastructure |
| Queue definitions | Async infrastructure |

#### Formats:

| Format | Tool |
|--------|------|
| HCL | Terraform |
| YAML | Kubernetes manifests |
| YAML | Helm charts |
| JSON/YAML | CloudFormation |
| Python | CDK |
| YAML | Docker Compose |

#### Characteristics:
| Dimension | Value |
|-----------|-------|
| Nature | Static |
| Trigger | Event-driven (on change) |
| Fidelity | High (when kept current) |
| Freshness | Periodic |

#### Key Challenges:

**Drift:**

Same as API spec drift.
IaC may not reflect actual deployed state.
Actual cloud state may differ.

**Multi-cloud:**

AWS + GCP + Azure simultaneously.
Different resource models.
Need abstraction layer.

**What It Cannot Tell Us:**
- Code-level relationships
- Runtime behavior
- Business ownership

---

### 🔹 Source 8: Incident & Change Data

**What it tells us:**
> What has gone wrong and what changes preceded failures

#### Signals Available:

| Signal | What It Reveals |
|--------|----------------|
| Incident reports | Which services fail together |
| Post-mortems | Root cause chains |
| Change events | What changed before failure |
| Alert history | Recurring problems |
| On-call assignments | Implicit ownership |
| Escalation paths | Team relationships |

#### Sources:

- PagerDuty
- OpsGenie
- Jira
- Linear
- Statuspage

#### Characteristics:
| Dimension | Value |
|-----------|-------|
| Nature | Dynamic |
| Trigger | Event-driven |
| Fidelity | Medium |
| Freshness | Near real-time |

#### Key Value:

This source enables a unique capability:

> **Failure correlation**

Services that fail together likely depend on each other.
This reveals hidden dependencies that nothing else shows.

#### Key Challenges:

**Unstructured data:**

Post-mortems are free text.
Incident descriptions are narrative.
Need NLP to extract structured signals.

**Lag:**

Incidents are declared after the fact.
Not useful for real-time detection.
Very useful for historical pattern learning.

---

## 📊 Signal Source Comparison Matrix

| Source | Nature | Trigger | Fidelity | Freshness | Unique Value |
|--------|--------|---------|----------|-----------|-------------|
| Git Repos | Static | Event | High | Near RT | History, ownership |
| AST | Static | On-demand | High | Near RT | Code structure |
| API Specs | Static | Event | Medium | Periodic | Contracts |
| Tracing | Dynamic | Continuous | High | Real-time | Actual runtime calls |
| Metrics/APM | Dynamic | Continuous | High | Real-time | Performance signals |
| CI/CD | Both | Event | High | Near RT | Build topology |
| Infra as Code | Static | Event | High | Periodic | Deployment mapping |
| Incidents | Dynamic | Event | Medium | Near RT | Failure patterns |

---

## 🧩 Signal Complementarity

This is the most important insight about Zone 1:

> **No single source gives complete knowledge**
> **The graph becomes accurate through signal combination**

```
AST alone:
  Sees imports but misses dynamic calls

Tracing alone:
  Sees calls but misses why they exist

Git alone:
  Sees changes but misses runtime impact

Metrics alone:
  Sees performance but misses structure

Combined:
  Complete picture emerges
```

Examples of cross-signal insight:

| Question | Signals Needed |
|----------|---------------|
| "What calls PaymentService?" | AST + Tracing |
| "Who owns PaymentService?" | Git + CI/CD |
| "Why is it slow?" | Tracing + Metrics + Git |
| "What breaks if it changes?" | AST + Tracing + Incidents |
| "When did this dependency appear?" | Git + Tracing history |
| "Is this API still used?" | AST + Tracing + Specs |

---

## ⚠️ Zone 1 Gaps & Blind Spots

Every signal landscape has gaps.
The system must acknowledge and surface them.

| Gap | Cause | Impact |
|-----|-------|--------|
| Uninstrumented services | No tracing | Missing runtime edges |
| Outdated specs | Spec drift | Wrong contract data |
| Dark dependencies | Dynamic languages | Missing graph edges |
| Shadow IT | Unregistered services | Invisible services |
| Third-party services | No access | External boundary only |
| Legacy systems | No modern tooling | Opaque nodes |

These gaps must:
- Be **visible** in the graph (not silently missing)
- Have **confidence markers** indicating uncertainty
- Be **surfaced** to developers querying affected areas

---

## 🧠 Mental Model for Zone 1

> Think of Zone 1 as a city being observed by different sensors:

| Sensor | Like |
|--------|------|
| Git | City blueprints and building permits |
| AST | Architectural floor plans |
| API Specs | Declared roads and entrances |
| Tracing | GPS tracking of actual traffic |
| Metrics | Traffic flow sensors |
| CI/CD | Construction and renovation activity |
| IaC | Zoning and infrastructure maps |
| Incidents | Accident and emergency reports |

No single sensor sees the whole city.
But combined, you know everything about it.

---
