package pipeline

import (
	"context"
	"errors"
	"fmt"

	"archgraph/nif"
)

var (
	ErrInvalidEntity       = errors.New("invalid entity structure")
	ErrSelfReferentialRel  = errors.New("self-referential relationship")
	ErrMissingEndpoints    = errors.New("relationship refers to missing endpoints")
	ErrInvalidOwnershipRel = errors.New("only TEAMS can OWN services")
)

func (p *Pipeline) stage5ValidateEntity(ctx context.Context, e *nif.Entity) error {
	if e.Name == "" {
		return fmt.Errorf("%w: name is empty", ErrInvalidEntity)
	}
	if e.Namespace == "" {
		return fmt.Errorf("%w: namespace is empty", ErrInvalidEntity)
	}
	if e.Type == "" {
		return fmt.Errorf("%w: type is empty", ErrInvalidEntity)
	}
	return nil
}

func (p *Pipeline) stage5ValidateRelationship(ctx context.Context, r *nif.Relationship, validEntities []*nif.Entity) error {
	if r.FromEntityID == r.ToEntityID {
		return ErrSelfReferentialRel
	}

	// Verify endpoints are present in the batch or in the registry
	fromExists := false
	toExists := false

	for _, e := range validEntities {
		if e.ID == r.FromEntityID {
			fromExists = true
		}
		if e.ID == r.ToEntityID {
			toExists = true
		}
	}

	// If not in current batch, check registry
	if !fromExists {
		_, err := p.reg.GetByID(ctx, r.FromEntityID)
		if err == nil {
			fromExists = true
		}
	}
	if !toExists {
		_, err := p.reg.GetByID(ctx, r.ToEntityID)
		if err == nil {
			toExists = true
		}
	}

	if !fromExists || !toExists {
		return ErrMissingEndpoints
	}

	// Semantic constraint: Only TEAMS can OWN services
	if r.Type == nif.RelOwns {
		fromType := p.getEntityTypeByID(ctx, r.FromEntityID, validEntities)
		toType := p.getEntityTypeByID(ctx, r.ToEntityID, validEntities)
		if fromType != "" && fromType != string(nif.EntityTeam) {
			return ErrInvalidOwnershipRel
		}
		if toType != "" && toType != string(nif.EntityService) {
			return ErrInvalidOwnershipRel
		}
	}

	return nil
}

func (p *Pipeline) getEntityTypeByID(ctx context.Context, id string, validEntities []*nif.Entity) string {
	for _, e := range validEntities {
		if e.ID == id {
			return string(e.Type)
		}
	}
	regEnt, err := p.reg.GetByID(ctx, id)
	if err == nil {
		return regEnt.EntityType
	}
	return ""
}

func isCriticalValidationError(err error) bool {
	return errors.Is(err, ErrInvalidEntity)
}
