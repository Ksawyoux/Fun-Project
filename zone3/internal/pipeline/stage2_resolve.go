package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"archgraph/nif"
	"archgraph/zone3/internal/registry"
)

func (p *Pipeline) stage2Resolve(ctx context.Context, e *nif.Entity) (*nif.Entity, error) {
	var regEnt *registry.RegistryEntity
	var err error

	// 1. Look up by ID
	if e.ID != "" {
		regEnt, err = p.reg.GetByID(ctx, e.ID)
		if err != nil && !errors.Is(err, registry.ErrNotFound) {
			return nil, err
		}
	}

	// 2. Look up by Canonical Name & Namespace
	if regEnt == nil {
		regEnt, err = p.reg.GetByCanonicalName(ctx, e.Name, e.Namespace)
		if err != nil && !errors.Is(err, registry.ErrNotFound) {
			return nil, err
		}
	}

	// 3. Look up by Alias & Namespace
	if regEnt == nil {
		regEnt, err = p.reg.GetByAlias(ctx, e.Name, e.Namespace)
		if err != nil && !errors.Is(err, registry.ErrNotFound) {
			return nil, err
		}
	}

	// 4. Look up by RawName Alias if provided
	if regEnt == nil && e.RawName != "" {
		regEnt, err = p.reg.GetByAlias(ctx, e.RawName, e.Namespace)
		if err != nil && !errors.Is(err, registry.ErrNotFound) {
			return nil, err
		}
	}

	now := time.Now().UTC()

	if regEnt == nil {
		// New entity registry record
		canonicalID := e.ID
		if canonicalID == "" {
			canonicalID = generateDeterministicID(string(e.Type), e.Name, e.Namespace)
		}

		regEnt = &registry.RegistryEntity{
			CanonicalID:   canonicalID,
			EntityType:    string(e.Type),
			SubType:       e.SubType,
			CanonicalName: e.Name,
			Namespace:     e.Namespace,
			Confidence:    e.Confidence,
			FirstSeen:     now,
			LastConfirmed: now,
		}

		if e.RawName != "" && e.RawName != e.Name {
			regEnt.Aliases = append(regEnt.Aliases, registry.AliasInfo{
				Name:       e.RawName,
				SourceType: e.Source.SourceType,
				SourceID:   e.Source.SourceID,
				Confidence: e.Confidence,
				AddedAt:    now,
			})
		}
	} else {
		// Existing entity registry record
		regEnt.LastConfirmed = now
		// Add alias if raw name differs from canonical name and is not already registered
		if e.RawName != "" && e.RawName != regEnt.CanonicalName {
			aliasExists := false
			for _, a := range regEnt.Aliases {
				if a.Name == e.RawName {
					aliasExists = true
					break
				}
			}
			if !aliasExists {
				regEnt.Aliases = append(regEnt.Aliases, registry.AliasInfo{
					Name:       e.RawName,
					SourceType: e.Source.SourceType,
					SourceID:   e.Source.SourceID,
					Confidence: e.Confidence,
					AddedAt:    now,
				})
			}
		}
	}

	// Track source contribution
	contribExists := false
	for i, c := range regEnt.SourceContributions {
		if c.SourceType == e.Source.SourceType && c.SourceID == e.Source.SourceID && c.SourceRef == e.Source.SourceRef {
			regEnt.SourceContributions[i].LastSeen = now
			contribExists = true
			break
		}
	}
	if !contribExists {
		regEnt.SourceContributions = append(regEnt.SourceContributions, registry.SourceContribution{
			SourceType: e.Source.SourceType,
			SourceID:   e.Source.SourceID,
			SourceRef:  e.Source.SourceRef,
			LastSeen:   now,
		})
	}

	// Update local SQLite entity registry
	if err := p.reg.Upsert(ctx, regEnt); err != nil {
		return nil, fmt.Errorf("registry update: %w", err)
	}

	// Update NIF entity properties with canonical values
	e.ID = regEnt.CanonicalID
	e.Name = regEnt.CanonicalName
	e.SubType = regEnt.SubType

	return e, nil
}

func generateDeterministicID(entityType, name, namespace string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s", entityType, name, namespace)
	return "ent_" + hex.EncodeToString(h.Sum(nil))[:24]
}
