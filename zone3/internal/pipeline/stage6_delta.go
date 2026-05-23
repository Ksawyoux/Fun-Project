package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"archgraph/nif"
	"archgraph/zone3/internal/z4client"
)

func (p *Pipeline) stage6DeltaCompute(ctx context.Context, entities []*nif.Entity, relationships []*nif.Relationship) ([]z4client.Mutation, error) {
	// 1. Gather all namespaces involved in the current batch
	namespaces := make(map[string]struct{})
	for _, e := range entities {
		if e.Namespace != "" {
			namespaces[e.Namespace] = struct{}{}
		}
	}


	// 2. Fetch existing entities & relationships from Zone 4 for each namespace
	existingEntities := make(map[string]*z4client.Z4Entity)
	existingRels := make(map[string]*z4client.Z4Relationship)

	for ns := range namespaces {
		listing, err := p.z4.ListNamespace(ctx, ns)
		if err != nil {
			if errors.Is(err, z4client.ErrNotFound) {
				continue
			}
			return nil, err
		}
		for _, e := range listing.Entities {
			existingEntities[e.ID] = e
		}
		for _, r := range listing.Relationships {
			existingRels[r.ID] = r
		}
	}

	var entityMutations []z4client.Mutation
	var relMutations []z4client.Mutation

	// 3. Diff Entities
	for _, e := range entities {
		z4e := mapNIFEntityToZ4(e)
		existing, exists := existingEntities[e.ID]

		if !exists {
			// Entity doesn't exist, create it
			z4e.Version = 0 // will be set to 1 by Zone 4
			entityMutations = append(entityMutations, z4client.Mutation{
				Kind:   z4client.KindUpsertEntity,
				Entity: z4e,
				Reason: "New entity detected by processing pipeline",
			})
		} else {
			// Entity exists, check for changes
			if hasEntityChanged(existing, z4e) {
				z4e.Version = existing.Version
				entityMutations = append(entityMutations, z4client.Mutation{
					Kind:   z4client.KindUpsertEntity,
					Entity: z4e,
					Reason: "Properties or metadata updated by processing pipeline",
				})
			}
		}
	}

	// 4. Diff Relationships
	for _, r := range relationships {
		z4r := mapNIFRelationshipToZ4(r)
		existing, exists := existingRels[r.ID]

		if !exists {
			// Relationship doesn't exist, create it
			z4r.Version = 0
			relMutations = append(relMutations, z4client.Mutation{
				Kind:         z4client.KindUpsertRelationship,
				Relationship: z4r,
				Reason:       "New relationship inferred/detected by processing pipeline",
			})
		} else {
			// Relationship exists, check for changes
			if hasRelationshipChanged(existing, z4r) {
				z4r.Version = existing.Version
				relMutations = append(relMutations, z4client.Mutation{
					Kind:         z4client.KindUpsertRelationship,
					Relationship: z4r,
					Reason:       "Confidence or properties updated by processing pipeline",
				})
			}
		}
	}

	// 5. Combine mutations ensuring Entities are upserted before Relationships
	allMutations := append(entityMutations, relMutations...)
	return allMutations, nil
}

func mapNIFEntityToZ4(e *nif.Entity) *z4client.Z4Entity {
	props := make(map[string]any)
	for k, v := range e.Properties {
		props[k] = v
	}
	props["source_type"] = e.Source.SourceType
	props["source_id"] = e.Source.SourceID
	props["source_ref"] = e.Source.SourceRef
	props["observed_at"] = e.Source.ObservedAt
	props["ingestion_run_id"] = e.IngestionRun
	if e.RawName != "" && e.RawName != e.Name {
		props["raw_name"] = e.RawName
	}
	if e.IsPartial {
		props["is_partial"] = true
	}

	validFrom := e.Source.ObservedAt
	if validFrom.IsZero() {
		validFrom = time.Now().UTC()
	}

	return &z4client.Z4Entity{
		ID:             e.ID,
		Type:           string(e.Type),
		SubType:        e.SubType,
		CanonicalName:  e.Name,
		Namespace:      e.Namespace,
		Confidence:     e.Confidence,
		IsActive:       true,
		LifecycleStage: "ACTIVE",
		ValidFrom:      validFrom,
		Properties:     props,
	}
}

func mapNIFRelationshipToZ4(r *nif.Relationship) *z4client.Z4Relationship {
	props := make(map[string]any)
	for k, v := range r.Properties {
		props[k] = v
	}
	props["source_type"] = r.Source.SourceType
	props["source_id"] = r.Source.SourceID
	props["source_ref"] = r.Source.SourceRef
	props["observed_at"] = r.Source.ObservedAt
	props["ingestion_run_id"] = r.IngestionRun
	if r.IsInferred {
		props["is_inferred"] = true
	}

	validFrom := r.Source.ObservedAt
	if validFrom.IsZero() {
		validFrom = time.Now().UTC()
	}

	return &z4client.Z4Relationship{
		ID:         r.ID,
		Type:       string(r.Type),
		FromID:     r.FromEntityID,
		ToID:       r.ToEntityID,
		Confidence: r.Confidence,
		IsActive:   true,
		ValidFrom:  validFrom,
		Properties: props,
	}
}

func hasEntityChanged(existing *z4client.Z4Entity, proposed *z4client.Z4Entity) bool {
	if existing.Type != proposed.Type ||
		existing.SubType != proposed.SubType ||
		existing.CanonicalName != proposed.CanonicalName ||
		existing.Namespace != proposed.Namespace ||
		existing.Confidence != proposed.Confidence ||
		existing.IsActive != proposed.IsActive ||
		existing.LifecycleStage != proposed.LifecycleStage {
		return true
	}

	return !mapsEqual(existing.Properties, proposed.Properties)
}

func hasRelationshipChanged(existing *z4client.Z4Relationship, proposed *z4client.Z4Relationship) bool {
	if existing.Type != proposed.Type ||
		existing.FromID != proposed.FromID ||
		existing.ToID != proposed.ToID ||
		existing.Confidence != proposed.Confidence ||
		existing.IsActive != proposed.IsActive {
		return true
	}

	return !mapsEqual(existing.Properties, proposed.Properties)
}

func mapsEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		// Ignore timing diffs that can vary slightly between runs
		if k == "observed_at" || k == "ingestion_run_id" {
			continue
		}
		v2, ok := b[k]
		if !ok {
			return false
		}
		// Quick check using json serialization or reflect.DeepEqual
		if !reflect.DeepEqual(v, v2) {
			// Fallback: compare marshal representation if they are complex types (like nested maps/slices)
			aJSON, _ := json.Marshal(v)
			bJSON, _ := json.Marshal(v2)
			if string(aJSON) != string(bJSON) {
				return false
			}
		}
	}
	return true
}
