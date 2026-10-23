package integration

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	gen "src.solsynth.dev/sosys/go/proto"

	"src.solsynth.dev/solsynth/distribution/internal/service"
)

// SphereDirectory is the narrow control-plane contract used by DistributionCenter.
// Sphere authenticates user tokens and owns publisher membership; DistributionCenter
// never infers publisher ownership from a local account or custom app record.
type SphereDirectory struct {
	auth       gen.DyAuthServiceClient
	publishers gen.DyPublisherServiceClient
}

func NewSphereDirectory(auth gen.DyAuthServiceClient, publishers gen.DyPublisherServiceClient) *SphereDirectory {
	return &SphereDirectory{auth: auth, publishers: publishers}
}

func (s *SphereDirectory) Authenticate(ctx context.Context, token string) (string, error) {
	account, err := s.AuthenticateAccount(ctx, token)
	if err != nil {
		return "", err
	}
	return account.ID, nil
}

func (s *SphereDirectory) AuthenticateAccount(ctx context.Context, token string) (service.AuthenticatedAccount, error) {
	if s == nil || s.auth == nil {
		return service.AuthenticatedAccount{}, fmt.Errorf("sphere auth client is unavailable")
	}
	resp, err := s.auth.Authenticate(ctx, &gen.DyAuthenticateRequest{Token: token})
	if err != nil {
		return service.AuthenticatedAccount{}, err
	}
	if resp == nil || !resp.GetValid() || resp.GetSession() == nil || resp.GetSession().GetAccountId() == "" {
		return service.AuthenticatedAccount{}, fmt.Errorf("invalid authentication token")
	}
	session := resp.GetSession()
	account := session.GetAccount()
	return service.AuthenticatedAccount{
		ID:          session.GetAccountId(),
		IsSuperuser: account.GetIsSuperuser(),
		Name:        account.GetName(),
		Nick:        account.GetNick(),
		PictureURL:  account.GetPicture().GetUrl(),
	}, nil
}

func (s *SphereDirectory) GetPublisher(ctx context.Context, publisherRef string) (*gen.DyPublisher, error) {
	if s == nil || s.publishers == nil {
		return nil, fmt.Errorf("sphere publisher client is unavailable")
	}
	request := &gen.DyGetPublisherRequest{}
	if _, err := uuid.Parse(publisherRef); err == nil {
		request.Query = &gen.DyGetPublisherRequest_Id{Id: publisherRef}
	} else {
		request.Query = &gen.DyGetPublisherRequest_Name{Name: publisherRef}
	}
	resp, err := s.publishers.GetPublisher(ctx, request)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, nil
	}
	return resp.GetPublisher(), nil
}

func (s *SphereDirectory) IsPublisherMember(ctx context.Context, publisherID, accountID string, role gen.DyPublisherMemberRole) (bool, error) {
	if s == nil || s.publishers == nil {
		return false, fmt.Errorf("sphere publisher client is unavailable")
	}
	resp, err := s.publishers.IsPublisherMember(ctx, &gen.DyIsPublisherMemberRequest{PublisherId: publisherID, AccountId: accountID, Role: role})
	if err != nil {
		return false, err
	}
	return resp != nil && resp.GetValid(), nil
}

// PublisherMemberships enumerates the publishers this account belongs to. The
// listing is publisher-scoped by Sphere, so the console offers exactly the
// publishers the account can already act on; the role comes from the member
// listing because it is what decides which surfaces are useful.
func (s *SphereDirectory) PublisherMemberships(ctx context.Context, accountID string) ([]service.PublisherMembership, error) {
	if s == nil || s.publishers == nil {
		return nil, fmt.Errorf("sphere publisher client is unavailable")
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return nil, fmt.Errorf("account id is required")
	}
	resp, err := s.publishers.ListPublishers(ctx, &gen.DyListPublishersRequest{AccountId: accountID})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, nil
	}
	memberships := make([]service.PublisherMembership, 0, len(resp.GetPublishers()))
	for _, publisher := range resp.GetPublishers() {
		if publisher == nil || strings.TrimSpace(publisher.GetId()) == "" {
			continue
		}
		role := gen.DyPublisherMemberRole_DY_PUBLISHER_MEMBER_ROLE_UNSPECIFIED
		if members, err := s.publishers.ListPublisherMembers(ctx, &gen.DyListPublisherMembersRequest{PublisherId: publisher.GetId()}); err == nil && members != nil {
			for _, member := range members.GetMembers() {
				if member != nil && member.GetAccountId() == accountID {
					role = member.GetRole()
					break
				}
			}
		}
		memberships = append(memberships, service.PublisherMembership{Publisher: publisher, Role: role})
	}
	return memberships, nil
}

var _ interface {
	Authenticate(context.Context, string) (string, error)
	AuthenticateAccount(context.Context, string) (service.AuthenticatedAccount, error)
	GetPublisher(context.Context, string) (*gen.DyPublisher, error)
	IsPublisherMember(context.Context, string, string, gen.DyPublisherMemberRole) (bool, error)
	PublisherMemberships(context.Context, string) ([]service.PublisherMembership, error)
} = (*SphereDirectory)(nil)
