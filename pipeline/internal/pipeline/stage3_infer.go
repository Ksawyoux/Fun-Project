package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"archgraph/nif"
)

func (p *Pipeline) stage3InferAndDeduplicate(ctx context.Context, relationships []*nif.Relationship, resolvedEntities []*nif.Entity, idMap map[string]string) ([]*nif.Relationship, error) {
	// 1. Map all relationship endpoints to their resolved canonical IDs
	for _, r := range relationships {
		if canonicalFrom, ok := idMap[r.FromEntityID]; ok {
			r.FromEntityID = canonicalFrom
		} else {
			r.FromEntityID = p.resolveID(ctx, r.FromEntityID)
		}

		if canonicalTo, ok := idMap[r.ToEntityID]; ok {
			r.ToEntityID = canonicalTo
		} else {
			r.ToEntityID = p.resolveID(ctx, r.ToEntityID)
		}
	}

	var allRels []*nif.Relationship
	allRels = append(allRels, relationships...)

	// 2. Structural Inference: IMPORTS -> DEPENDS_ON
	for _, r := range relationships {
		if r.Type == nif.RelImports {
			inferredDep := &nif.Relationship{
				Type:         nif.RelDependsOn,
				FromEntityID: r.FromEntityID,
				ToEntityID:   r.ToEntityID,
				Source:       r.Source,
				Confidence:   r.Confidence * 0.90,
				IsInferred:   true,
				IngestionRun: r.IngestionRun,
				Properties:   map[string]any{"inferred_from": "imports"},
			}
			allRels = append(allRels, inferredDep)
		}
	}

	// 3. Shared Resource Inference: (A writes X) & (B reads X) -> B change_coupled_with A
	// Track writers and readers per database table
	writers := make(map[string][]string) // tableID -> list of serviceIDs
	readers := make(map[string][]string) // tableID -> list of serviceIDs

	for _, r := range relationships {
		if r.Type == nif.RelWritesTo {
			writers[r.ToEntityID] = append(writers[r.ToEntityID], r.FromEntityID)
		} else if r.Type == nif.RelReadsFrom {
			readers[r.ToEntityID] = append(readers[r.ToEntityID], r.FromEntityID)
		}
	}

	for tableID, tableWriters := range writers {
		if tableReaders, ok := readers[tableID]; ok {
			for _, writer := range tableWriters {
				for _, reader := range tableReaders {
					if writer != reader {
						inferredCoupling := &nif.Relationship{
							Type:         nif.RelChangeCoupledWith,
							FromEntityID: reader,
							ToEntityID:   writer,
							Confidence:   0.85,
							IsInferred:   true,
							Properties: map[string]any{
								"inferred_from": "shared_resource",
								"shared_table":   tableID,
							},
						}
						allRels = append(allRels, inferredCoupling)
					}
				}
			}
		}
	}

	// 4. Deduplicate relationships
	type key struct {
		from string
		to   string
		rel  nif.RelationshipType
	}

	groups := make(map[key][]*nif.Relationship)
	for _, r := range allRels {
		if r.FromEntityID == "" || r.ToEntityID == "" {
			continue
		}
		k := key{from: r.FromEntityID, to: r.ToEntityID, rel: r.Type}
		groups[k] = append(groups[k], r)
	}

	var deduplicated []*nif.Relationship
	for k, group := range groups {
		var best *nif.Relationship
		var sources []string
		isInferred := true

		for _, r := range group {
			if !r.IsInferred {
				isInferred = false
			}
			if r.Source.SourceType != "" {
				sources = append(sources, r.Source.SourceType)
			}
			if best == nil || r.Confidence > best.Confidence {
				best = r
			}
		}

		// Create merged relationship
		merged := &nif.Relationship{
			Type:         k.rel,
			FromEntityID: k.from,
			ToEntityID:   k.to,
			Source:       best.Source,
			Confidence:   best.Confidence,
			IsInferred:   isInferred,
			IngestionRun: best.IngestionRun,
			Properties:   make(map[string]any),
		}

		// Copy properties
		for kp, vp := range best.Properties {
			merged.Properties[kp] = vp
		}

		if len(sources) > 0 {
			merged.Properties["sources"] = sources
		}
		merged.Properties["corroboration_count"] = len(group)

		// Generate canonical ID
		sourceType := merged.Source.SourceType
		if sourceType == "" {
			sourceType = "inferred"
		}
		merged.ID = generateRelationshipID(string(merged.Type), merged.FromEntityID, merged.ToEntityID, sourceType)

		deduplicated = append(deduplicated, merged)
	}

	return deduplicated, nil
}

func (p *Pipeline) resolveID(ctx context.Context, id string) string {
	if id == "" {
		return ""
	}
	regEnt, err := p.reg.GetByID(ctx, id)
	if err == nil {
		return regEnt.CanonicalID
	}
	return id
}

func generateRelationshipID(relType, fromID, toID, sourceType string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s|%s", relType, fromID, toID, sourceType)
	return "rel_" + hex.EncodeToString(h.Sum(nil))[:24]
}
