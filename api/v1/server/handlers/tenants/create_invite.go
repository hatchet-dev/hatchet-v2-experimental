package tenants

import (
	"context"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/hatchet-dev/hatchet/api/v1/server/oas/apierrors"
	"github.com/hatchet-dev/hatchet/api/v1/server/oas/gen"
	"github.com/hatchet-dev/hatchet/api/v1/server/oas/transformers"
	"github.com/hatchet-dev/hatchet/internal/integrations/email"
	"github.com/hatchet-dev/hatchet/pkg/repository"
	"github.com/hatchet-dev/hatchet/pkg/repository/prisma/db"
)

func (t *TenantService) TenantInviteCreate(ctx echo.Context, request gen.TenantInviteCreateRequestObject) (gen.TenantInviteCreateResponseObject, error) {
	user := ctx.Get("user").(*db.UserModel)
	tenant := ctx.Get("tenant").(*db.TenantModel)
	tenantMember := ctx.Get("tenant-member").(*db.TenantMemberModel)
	if !t.config.Runtime.AllowInvites {
		t.config.Logger.Warn().Msg("tenant invites are disabled")
		return gen.TenantInviteCreate400JSONResponse(
			apierrors.NewAPIErrors("tenant invites are disabled"),
		), nil
	}

	// validate the request
	if apiErrors, err := t.config.Validator.ValidateAPI(request.Body); err != nil {
		return nil, err
	} else if apiErrors != nil {
		t.config.Logger.Warn().Msg("invalid request")
		return gen.TenantInviteCreate400JSONResponse(*apiErrors), nil
	}

	// ensure that this user isn't already a member of the tenant
	if _, err := t.config.APIRepository.Tenant().GetTenantMemberByEmail(tenant.ID, request.Body.Email); err == nil {
		t.config.Logger.Warn().Msg("this user is already a member of this tenant")
		return gen.TenantInviteCreate400JSONResponse(
			apierrors.NewAPIErrors("this user is already a member of this tenant"),
		), nil
	}

	// if user is not an owner, they cannot change a role to owner
	if tenantMember.Role != db.TenantMemberRoleOwner && request.Body.Role == gen.OWNER {
		t.config.Logger.Warn().Msg("only an owner can change a role to owner")
		return gen.TenantInviteCreate400JSONResponse(
			apierrors.NewAPIErrors("only an owner can change a role to owner"),
		), nil
	}

	// construct the database query
	createOpts := &repository.CreateTenantInviteOpts{
		InviteeEmail: request.Body.Email,
		InviterEmail: user.Email,
		ExpiresAt:    time.Now().Add(7 * 24 * time.Hour), // 1 week expiration
		Role:         string(request.Body.Role),
		MaxPending:   t.config.Runtime.MaxPendingInvites,
	}

	// create the invite
	invite, err := t.config.APIRepository.TenantInvite().CreateTenantInvite(tenant.ID, createOpts)

	if err != nil {
		t.config.Logger.Err(err).Msg("could not create tenant invite")

		return gen.TenantInviteCreate403JSONResponse{
			Description: err.Error(),
		}, nil
	}

	// send an email
	go func() {
		emailCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		name := user.Email

		if userName, ok := user.Name(); ok && userName != "" {
			name = userName
		}

		if err := t.config.Email.SendTenantInviteEmail(emailCtx, invite.InviteeEmail, email.TenantInviteEmailData{
			InviteSenderName: name,
			TenantName:       tenant.Name,
			ActionURL:        t.config.Runtime.ServerURL,
		}); err != nil {
			t.config.Logger.Err(err).Msg("could not send tenant invite email")
		}
	}()

	t.config.Analytics.Enqueue("user-invite:create",
		user.ID,
		&invite.TenantID,
		nil,
	)

	return gen.TenantInviteCreate201JSONResponse(
		*transformers.ToTenantInviteLink(invite),
	), nil
}
