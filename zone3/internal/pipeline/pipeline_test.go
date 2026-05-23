package pipeline

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"archgraph/zone3/internal/nif"
	"archgraph/zone3/internal/registry"
	"archgraph/zone3/internal/z4client"
)

func TestPipelineProcessingFlow(t *testing.T) {
	ctx := context.Background()

	// 1. Set up a mock Zone 4 HTTP Server
	var receivedMutations []z4client.Mutation
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodGet && r.URL.Path == "/v1/entities" {
			// Mock ListNamespace
			resp := z4client.NamespaceListing{
				Namespace:     r.URL.Query().Get("namespace"),
				Entities:      []*z4client.Z4Entity{},
				Relationships: []*z4client.Z4Relationship{},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		if r.Method == http.MethodPost && r.URL.Path == "/v1/mutations" {
			// Mock ApplyBatch
			var req struct {
				Mutations []z4client.Mutation `json:"mutations"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
				receivedMutations = req.Mutations
			}

			resp := map[string]any{
				"transaction_id": "txn_test",
				"status":         "SUCCESS",
				"results":        []any{},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	// 2. Open registry and z4client
	reg, err := registry.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open registry: %v", err)
	}
	defer reg.Close()

	z4 := z4client.New(ts.URL)
	pl := New(reg, z4)

	// 3. Construct a mock NIF batch
	now := time.Now().UTC()
	batch := &nif.Batch{
		Entities: []*nif.Entity{
			{
				ID:        "original_ent_id",
				Type:      nif.EntityService,
				Name:      "PaymentService",
				Namespace: "acme-prod",
				Source: nif.SourceInfo{
					SourceType: "git",
					SourceID:   "repo-x",
					SourceRef:  "main",
					ObservedAt: now,
				},
				Confidence: 0.9,
				Properties: map[string]any{
					"has_tests":            true,
					"has_documentation":    true,
					"has_resource_limits":  true,
					"exposed_to_internet": true,
				},
			},
			{
				ID:        "module_ent_id",
				Type:      nif.EntityModule,
				Name:      "auth_module",
				Namespace: "acme-prod",
				Source: nif.SourceInfo{
					SourceType: "ast",
					SourceID:   "repo-x",
					SourceRef:  "main",
					ObservedAt: now,
				},
				Confidence: 0.85,
			},
		},
		Relationships: []*nif.Relationship{
			{
				Type:         nif.RelImports,
				FromEntityID: "original_ent_id",
				ToEntityID:   "module_ent_id",
				Source: nif.SourceInfo{
					SourceType: "ast",
					SourceID:   "repo-x",
					SourceRef:  "main",
					ObservedAt: now,
				},
				Confidence: 0.95,
			},
		},
	}

	// 4. Process Batch through the Pipeline
	mutations, err := pl.Process(ctx, batch)
	if err != nil {
		t.Fatalf("pipeline processing failed: %v", err)
	}

	// Verify generated mutations
	if len(mutations) == 0 {
		t.Fatal("expected mutations to be generated, got 0")
	}

	var hasService, hasModule, hasDependsOn, hasImports bool
	for _, m := range mutations {
		if m.Kind == z4client.KindUpsertEntity && m.Entity != nil {
			if m.Entity.CanonicalName == "payment-service" {
				hasService = true
				// Check sub_type classification (BFF because exposed_to_internet = true)
				if m.Entity.SubType != "BFF" {
					t.Errorf("expected SubType BFF, got %q", m.Entity.SubType)
				}
				// Check maturity enrichment: tests(15) + doc(15) + monitoring(0) + resource_limits(15) + base(10) + api(0) + owner(0) = 55 -> DEVELOPING
				maturity := m.Entity.Properties["maturity"].(string)
				if maturity != "DEVELOPING" {
					t.Errorf("expected maturity DEVELOPING, got %q", maturity)
				}
			}
			if m.Entity.CanonicalName == "auth-module" {
				hasModule = true
			}
		}
		if m.Kind == z4client.KindUpsertRelationship && m.Relationship != nil {
			if m.Relationship.Type == string(nif.RelDependsOn) {
				hasDependsOn = true
			}
			if m.Relationship.Type == string(nif.RelImports) {
				hasImports = true
			}
		}
	}

	if !hasService {
		t.Error("missing payment-service entity mutation")
	}
	if !hasModule {
		t.Error("missing auth-module entity mutation")
	}
	if !hasImports {
		t.Error("missing original imports relationship mutation")
	}
	// Verify structural inference: IMPORTS -> DEPENDS_ON worked
	if !hasDependsOn {
		t.Error("missing inferred depends_on relationship mutation")
	}

	// 5. Test applying to mock server
	err = z4.ApplyBatch(ctx, mutations)
	if err != nil {
		t.Fatalf("failed to apply batch: %v", err)
	}

	if len(receivedMutations) != len(mutations) {
		t.Errorf("expected mock server to receive %d mutations, got %d", len(mutations), len(receivedMutations))
	}
}
