package pipeline

import (
	"testing"
	"time"

	"archgraph/nif"
	z4schema "archgraph/zone4/schema"
)

func TestNIFToZone4SchemaContract(t *testing.T) {
	// 1. Validate every EntityType defined in NIF passes Zone 4 validation
	nifEntityTypes := []nif.EntityType{
		nif.EntityService,
		nif.EntityModule,
		nif.EntityFunction,
		nif.EntityAPIEndpoint,
		nif.EntityDatabaseTable,
		nif.EntityTeam,
		nif.EntityDatabaseSchema,
	}

	for _, et := range nifEntityTypes {
		t.Run("Entity_"+string(et), func(t *testing.T) {
			ne := &nif.Entity{
				ID:        "ent_test_id",
				Type:      et,
				Name:      "test-canonical-name",
				Namespace: "test-namespace",
				Source: nif.SourceInfo{
					SourceType: "git",
					SourceID:   "repo1",
					SourceRef:  "main",
					ObservedAt: time.Now(),
				},
				Confidence: 1.0,
			}

			// Map to Z4 Entity
			z4e := mapNIFEntityToZ4(ne)

			// Convert z4client.Z4Entity to z4schema.Entity
			se := &z4schema.Entity{
				ID:             z4e.ID,
				Type:           z4schema.EntityType(z4e.Type),
				SubType:        z4e.SubType,
				CanonicalName:  z4e.CanonicalName,
				Namespace:      z4e.Namespace,
				Confidence:     z4e.Confidence,
				IsActive:       z4e.IsActive,
				LifecycleStage: z4schema.LifecycleStage(z4e.LifecycleStage),
				ValidFrom:      z4e.ValidFrom,
				Properties:     z4e.Properties,
			}

			// Validate against Zone 4 schema rules
			if err := z4schema.ValidateEntity(se); err != nil {
				t.Errorf("Zone 4 rejected mapped NIF entity type %q: %v", et, err)
			}
		})
	}

	// 2. Validate every RelationshipType defined in NIF passes Zone 4 validation
	nifRelTypes := []nif.RelationshipType{
		nif.RelDependsOn,
		nif.RelCalls,
		nif.RelImports,
		nif.RelOwns,
		nif.RelExposes,
		nif.RelRuntimeCalls,
		nif.RelReadsFrom,
		nif.RelWritesTo,
		nif.RelChangeCoupledWith,
		nif.RelContributedTo,
		nif.RelDeployedOn,
	}

	for _, rt := range nifRelTypes {
		t.Run("Relationship_"+string(rt), func(t *testing.T) {
			nr := &nif.Relationship{
				ID:           "rel_test_id",
				Type:         rt,
				FromEntityID: "ent_source_id",
				ToEntityID:   "ent_target_id",
				Source: nif.SourceInfo{
					SourceType: "git",
					SourceID:   "repo1",
					SourceRef:  "main",
					ObservedAt: time.Now(),
				},
				Confidence: 1.0,
			}

			// Map to Z4 Relationship
			z4r := mapNIFRelationshipToZ4(nr)

			// Convert z4client.Z4Relationship to z4schema.Relationship
			sr := &z4schema.Relationship{
				ID:         z4r.ID,
				Type:       z4schema.RelationshipType(z4r.Type),
				FromID:     z4r.FromID,
				ToID:       z4r.ToID,
				Confidence: z4r.Confidence,
				IsActive:   z4r.IsActive,
				ValidFrom:  z4r.ValidFrom,
				Properties: z4r.Properties,
			}

			// Validate against Zone 4 schema rules
			if err := z4schema.ValidateRelationship(sr); err != nil {
				t.Errorf("Zone 4 rejected mapped NIF relationship type %q: %v", rt, err)
			}
		})
	}
}

func TestLegacyNIFToZone4SchemaTranslationContract(t *testing.T) {
	// 1. Validate that legacy "SCHEMA" entity type translates to DATABASE_SCHEMA and is accepted
	t.Run("Legacy_SCHEMA", func(t *testing.T) {
		ne := &nif.Entity{
			ID:        "ent_test_id",
			Type:      "SCHEMA", // legacy string
			Name:      "test-schema",
			Namespace: "test-namespace",
			Source: nif.SourceInfo{
				SourceType: "git",
				SourceID:   "repo1",
				ObservedAt: time.Now(),
			},
			Confidence: 1.0,
		}
		z4e := mapNIFEntityToZ4(ne)
		se := &z4schema.Entity{
			ID:             z4e.ID,
			Type:           z4schema.EntityType(z4e.Type),
			CanonicalName:  z4e.CanonicalName,
			Namespace:      z4e.Namespace,
			Confidence:     z4e.Confidence,
			IsActive:       z4e.IsActive,
			LifecycleStage: z4schema.LifecycleStage(z4e.LifecycleStage),
			ValidFrom:      z4e.ValidFrom,
			Properties:     z4e.Properties,
		}
		if err := z4schema.ValidateEntity(se); err != nil {
			t.Errorf("Zone 4 rejected translated legacy SCHEMA: %v", err)
		}
		if se.Type != z4schema.EntityDatabaseSchema {
			t.Errorf("expected translation to DATABASE_SCHEMA, got %q", se.Type)
		}
	})

	// 2. Validate that legacy "AUTHORED_BY" and "DEPLOYED_TO" relationships translate and are accepted
	legacyRels := []struct {
		legacyType string
		expected   z4schema.RelationshipType
	}{
		{"AUTHORED_BY", z4schema.RelContributedTo},
		{"DEPLOYED_TO", z4schema.RelDeployedOn},
	}

	for _, tc := range legacyRels {
		t.Run("Legacy_"+tc.legacyType, func(t *testing.T) {
			nr := &nif.Relationship{
				ID:           "rel_test_id",
				Type:         nif.RelationshipType(tc.legacyType),
				FromEntityID: "ent_source_id",
				ToEntityID:   "ent_target_id",
				Source: nif.SourceInfo{
					SourceType: "git",
					SourceID:   "repo1",
					ObservedAt: time.Now(),
				},
				Confidence: 1.0,
			}
			z4r := mapNIFRelationshipToZ4(nr)
			sr := &z4schema.Relationship{
				ID:         z4r.ID,
				Type:       z4schema.RelationshipType(z4r.Type),
				FromID:     z4r.FromID,
				ToID:       z4r.ToID,
				Confidence: z4r.Confidence,
				IsActive:   z4r.IsActive,
				ValidFrom:  z4r.ValidFrom,
				Properties: z4r.Properties,
			}
			if err := z4schema.ValidateRelationship(sr); err != nil {
				t.Errorf("Zone 4 rejected translated legacy %q: %v", tc.legacyType, err)
			}
			if sr.Type != tc.expected {
				t.Errorf("expected translation to %q, got %q", tc.expected, sr.Type)
			}
		})
	}
}
