package pipeline

import (
	"context"
	"fmt"

	"archgraph/nif"
	"archgraph/zone3/internal/registry"
	"archgraph/zone3/internal/z4client"
)

type Pipeline struct {
	reg *registry.Registry
	z4  *z4client.Client
}

func New(reg *registry.Registry, z4 *z4client.Client) *Pipeline {
	return &Pipeline{
		reg: reg,
		z4:  z4,
	}
}

func (p *Pipeline) Process(ctx context.Context, batch *nif.Batch) ([]z4client.Mutation, error) {
	if batch == nil {
		return nil, nil
	}

	idMap := make(map[string]string) // original_id -> resolved canonical_id

	// 1. Stage 1 & 2: Parse, Classify, Normalize & Resolve Entities
	resolvedEntities := make([]*nif.Entity, 0, len(batch.Entities))
	for _, e := range batch.Entities {
		originalID := e.ID

		// Stage 1: Parse & Classify & Normalize
		pe, err := p.stage1ParseAndClassify(e)
		if err != nil {
			return nil, fmt.Errorf("stage 1 failed for entity %s: %w", e.Name, err)
		}

		// Stage 2: Entity Resolution
		re, err := p.stage2Resolve(ctx, pe)
		if err != nil {
			return nil, fmt.Errorf("stage 2 failed for entity %s: %w", e.Name, err)
		}

		resolvedEntities = append(resolvedEntities, re)

		if originalID != "" {
			idMap[originalID] = re.ID
		}
	}

	// Deduplicate resolved entities
	resolvedEntities = deduplicateEntities(resolvedEntities)

	// 2. Stage 3: Relationship Inference & Deduplication
	resolvedRelationships, err := p.stage3InferAndDeduplicate(ctx, batch.Relationships, resolvedEntities, idMap)
	if err != nil {
		return nil, fmt.Errorf("stage 3 failed: %w", err)
	}

	// 3. Stage 4: Enrichment
	for _, e := range resolvedEntities {
		p.stage4EnrichEntity(ctx, e, resolvedRelationships)
	}

	// 4. Stage 5: Validation
	var validEntities []*nif.Entity
	for _, e := range resolvedEntities {
		if err := p.stage5ValidateEntity(ctx, e); err != nil {
			if isCriticalValidationError(err) {
				continue // Skip invalid entities
			}
		}
		validEntities = append(validEntities, e)
	}

	var validRelationships []*nif.Relationship
	for _, r := range resolvedRelationships {
		if err := p.stage5ValidateRelationship(ctx, r, validEntities); err != nil {
			continue // Skip invalid relationships
		}
		validRelationships = append(validRelationships, r)
	}

	// 5. Stage 6: Delta Computation
	mutations, err := p.stage6DeltaCompute(ctx, validEntities, validRelationships)
	if err != nil {
		return nil, fmt.Errorf("stage 6 failed: %w", err)
	}

	return mutations, nil
}

func deduplicateEntities(entities []*nif.Entity) []*nif.Entity {
	seen := make(map[string]*nif.Entity)
	var ordered []*nif.Entity

	for _, e := range entities {
		if e.ID == "" {
			continue
		}
		if existing, ok := seen[e.ID]; ok {
			if e.Confidence > existing.Confidence {
				existing.Confidence = e.Confidence
				existing.Source = e.Source
				existing.SubType = e.SubType
				existing.Name = e.Name
				existing.RawName = e.RawName
			}
			if existing.Properties == nil {
				existing.Properties = make(map[string]any)
			}
			for k, v := range e.Properties {
				existing.Properties[k] = v
			}
		} else {
			cloned := *e
			cloned.Properties = make(map[string]any)
			for k, v := range e.Properties {
				cloned.Properties[k] = v
			}
			seen[e.ID] = &cloned
			ordered = append(ordered, &cloned)
		}
	}
	return ordered
}
