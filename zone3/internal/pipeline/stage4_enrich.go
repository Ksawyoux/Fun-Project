package pipeline

import (
	"context"

	"archgraph/zone3/internal/nif"
)

func (p *Pipeline) stage4EnrichEntity(ctx context.Context, e *nif.Entity, relationships []*nif.Relationship) {
	if e.Properties == nil {
		e.Properties = make(map[string]any)
	}

	// 1. Ownership Enricher
	if owner, ok := e.Properties["owner"]; ok && owner != "" {
		e.Properties["primary_owner"] = map[string]any{
			"team_name":  owner,
			"confidence": 0.95,
			"source":     "declared",
		}
	} else if author, ok := e.Properties["author"]; ok && author != "" {
		e.Properties["primary_owner"] = map[string]any{
			"team_name":  author,
			"confidence": 0.50,
			"source":     "git_history",
		}
	}

	// 2. Velocity Enricher
	commits, ok := e.Properties["commit_count"].(float64)
	if !ok {
		// try int conversion if needed
		if valInt, ok := e.Properties["commit_count"].(int); ok {
			commits = float64(valInt)
		}
	}
	velocity := "STABLE"
	if commits > 10 {
		velocity = "HOT"
	} else if commits >= 2 {
		velocity = "ACTIVE"
	}
	e.Properties["velocity"] = velocity

	// 3. Criticality Enricher
	// Count inbound dependencies in this batch
	inboundCount := 0
	for _, r := range relationships {
		if r.ToEntityID == e.ID && (r.Type == nif.RelDependsOn || r.Type == nif.RelCalls) {
			inboundCount++
		}
	}
	criticalityScore := 0.2 + 0.1*float64(inboundCount)
	if criticalityScore > 1.0 {
		criticalityScore = 1.0
	}
	criticality := "LOW"
	if criticalityScore > 0.85 {
		criticality = "CRITICAL"
	} else if criticalityScore > 0.65 {
		criticality = "HIGH"
	} else if criticalityScore > 0.40 {
		criticality = "MEDIUM"
	}
	e.Properties["criticality_score"] = criticalityScore
	e.Properties["criticality"] = criticality

	// 4. Maturity Enricher
	maturityScore := 0.1 // baseline
	if e.Properties["has_api_spec"] == true {
		maturityScore += 0.20
	}
	if e.Properties["has_tests"] == true {
		maturityScore += 0.15
	}
	if e.Properties["has_documentation"] == true {
		maturityScore += 0.15
	}
	if e.Properties["primary_owner"] != nil {
		maturityScore += 0.15
	}
	if e.Properties["has_monitoring"] == true {
		maturityScore += 0.10
	}
	if e.Properties["has_resource_limits"] == true {
		maturityScore += 0.15
	}

	maturity := "NASCENT"
	if maturityScore > 0.80 {
		maturity = "MATURE"
	} else if maturityScore > 0.50 {
		maturity = "DEVELOPING"
	}
	e.Properties["maturity_score"] = maturityScore
	e.Properties["maturity"] = maturity
}
