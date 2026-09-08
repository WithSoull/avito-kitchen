package auth

import "context"

type PartnerPrincipal struct {
	VenueID string
}

type principalKey struct{}

func WithPartnerPrincipal(ctx context.Context, principal PartnerPrincipal) context.Context {
	return context.WithValue(ctx, principalKey{}, principal)
}

func PartnerPrincipalFromContext(ctx context.Context) (PartnerPrincipal, bool) {
	principal, ok := ctx.Value(principalKey{}).(PartnerPrincipal)
	return principal, ok
}
