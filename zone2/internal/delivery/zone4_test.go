package delivery

import (
	"testing"
	"time"

	"archgraph/nif"
)

func TestZone4MappingUsesCanonicalSchemaTypes(t *testing.T) {
	now := time.Now().UTC()
	ent := entityToZ4(&nif.Entity{
		ID: "ent_schema", Type: nif.EntityDatabaseSchema, Name: "public", Namespace: "test",
		Confidence: 1, IngestionRun: "run-1",
		Source: nif.SourceInfo{SourceType: "ast", SourceID: "src", ObservedAt: now},
	})
	if got := ent["type"]; got != "DATABASE_SCHEMA" {
		t.Fatalf("entity type = %v, want DATABASE_SCHEMA", got)
	}

	for _, tc := range []struct {
		name string
		typ  nif.RelationshipType
		want string
	}{
		{name: "contributed", typ: nif.RelContributedTo, want: "CONTRIBUTED_TO"},
		{name: "deployed", typ: nif.RelDeployedOn, want: "DEPLOYED_ON"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rel := relationshipToZ4(&nif.Relationship{
				ID: "rel_" + tc.name, Type: tc.typ, FromEntityID: "a", ToEntityID: "b",
				Confidence: 1, IngestionRun: "run-1",
				Source: nif.SourceInfo{SourceType: "ast", SourceID: "src", ObservedAt: now},
			})
			if got := rel["type"]; got != tc.want {
				t.Fatalf("relationship type = %v, want %s", got, tc.want)
			}
		})
	}
}
