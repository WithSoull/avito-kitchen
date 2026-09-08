package service

import (
	"context"
	"fmt"

	"github.com/WithSoull/avito-kitchen/internal/venue/domain"
)

type MenuRepository interface {
	CurrentMenu(context.Context) (domain.MenuSnapshot, error)
}

type PartnerCatalog interface {
	ReplaceMenu(context.Context, domain.MenuSnapshot) error
	UpdateAvailability(context.Context, domain.Availability) error
}

type MenuPublisher struct {
	repository MenuRepository
	partner    PartnerCatalog
}

func NewMenuPublisher(repository MenuRepository, partner PartnerCatalog) *MenuPublisher {
	return &MenuPublisher{repository: repository, partner: partner}
}

func (publisher *MenuPublisher) Publish(ctx context.Context) error {
	snapshot, err := publisher.repository.CurrentMenu(ctx)
	if err != nil {
		return fmt.Errorf("load current menu: %w", err)
	}
	if err := publisher.partner.ReplaceMenu(ctx, snapshot); err != nil {
		return fmt.Errorf("publish menu: %w", err)
	}
	if err := publisher.partner.UpdateAvailability(ctx, domain.Availability{IsAcceptingOrders: true}); err != nil {
		return fmt.Errorf("publish availability: %w", err)
	}
	return nil
}
