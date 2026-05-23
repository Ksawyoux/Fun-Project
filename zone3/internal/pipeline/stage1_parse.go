package pipeline

import (
	"strings"

	"archgraph/zone3/internal/nif"
)

func (p *Pipeline) stage1ParseAndClassify(e *nif.Entity) (*nif.Entity, error) {
	cloned := *e
	cloned.RawName = e.Name
	cloned.Name = normalizeName(e.Name)

	// Classify service sub-types
	if cloned.Type == nif.EntityService {
		if strings.HasSuffix(cloned.Name, "-bff") || strings.Contains(cloned.Name, "gateway") || cloned.Properties["exposed_to_internet"] == true {
			cloned.SubType = "BFF"
		} else {
			cloned.SubType = "MICROSERVICE"
		}
	}

	return &cloned, nil
}

func normalizeName(name string) string {
	// Replace spaces and underscores with hyphens
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, " ", "-")

	// Convert camelCase / PascalCase to kebab-case
	var result strings.Builder
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			prev := name[i-1]
			if prev != '-' {
				result.WriteRune('-')
			}
		}
		result.WriteRune(r)
	}

	normalized := strings.ToLower(result.String())
	// Clean up duplicate hyphens
	for strings.Contains(normalized, "--") {
		normalized = strings.ReplaceAll(normalized, "--", "-")
	}
	return strings.Trim(normalized, "-")
}
