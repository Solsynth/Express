package develop

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
	gen "src.solsynth.dev/sosys/go/proto"

	"src.solsynth.dev/solsynth/distribution/internal/service"
)

// DeveloperStats is the public per-developer counter block.
type DeveloperStats struct {
	TotalCustomApps int64 `json:"total_custom_apps"`
}

// EnrollDeveloper registers a publisher in the developer program. The caller
// must own the publisher; that check is the route's job.
func (s *Service) EnrollDeveloper(ctx context.Context, publisherName string) (*Developer, error) {
	publisher, err := s.GetPublisher(ctx, publisherName)
	if err != nil {
		return nil, err
	}
	var existing Developer
	err = s.DB.Where("publisher_id = ?", publisher.GetId()).First(&existing).Error
	if err == nil {
		return nil, fmt.Errorf("%w: publisher is already in the developer program", service.ErrConflict)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("check developer enrollment: %w", err)
	}
	developer := &Developer{ID: newUUID(), PublisherID: publisher.GetId(), Publisher: PublisherViewFromProto(publisher)}
	if err := s.DB.Create(developer).Error; err != nil {
		if isUniqueConstraint(err) {
			return nil, fmt.Errorf("%w: publisher is already in the developer program", service.ErrConflict)
		}
		return nil, fmt.Errorf("create developer: %w", err)
	}
	return developer, nil
}

// ListJoinedDevelopers lists the developers whose publishers the account
// belongs to, with the total available to the caller.
func (s *Service) ListJoinedDevelopers(ctx context.Context, accountID string) ([]Developer, int, error) {
	if s == nil || s.Publishers == nil {
		return nil, 0, fmt.Errorf("%w: publisher directory unavailable", service.ErrDependency)
	}
	publishers, err := s.Publishers.ListPublishers(ctx, accountID)
	if err != nil {
		return nil, 0, rpcErrorToService(err, "publisher")
	}
	if len(publishers) == 0 {
		return []Developer{}, 0, nil
	}
	ids := make([]string, 0, len(publishers))
	for _, publisher := range publishers {
		if id := publisher.GetId(); id != "" {
			ids = append(ids, id)
		}
	}
	var developers []Developer
	if err := s.DB.Where("publisher_id IN ?", ids).Order("id ASC").Find(&developers).Error; err != nil {
		return nil, 0, fmt.Errorf("list developers: %w", err)
	}
	byID := make(map[string]*PublisherView, len(publishers))
	for _, publisher := range publishers {
		byID[publisher.GetId()] = PublisherViewFromProto(publisher)
	}
	for index := range developers {
		developers[index].Publisher = byID[developers[index].PublisherID]
	}
	if developers == nil {
		developers = []Developer{}
	}
	return developers, len(developers), nil
}

// DeveloperStatsFor counts the custom apps published under a developer.
func (s *Service) DeveloperStatsFor(developerID string) (DeveloperStats, error) {
	var count int64
	err := s.DB.Model(&CustomApp{}).
		Joins("JOIN dev_projects ON dev_projects.id = custom_apps.project_id").
		Where("dev_projects.developer_id = ?", developerID).
		Count(&count).Error
	if err != nil {
		return DeveloperStats{}, fmt.Errorf("count developer apps: %w", err)
	}
	return DeveloperStats{TotalCustomApps: count}, nil
}

// BotQuotaRecord is one bot counted against an account's developer quota.
type BotQuotaRecord struct {
	BotID         string `json:"bot_id"`
	Slug          string `json:"slug"`
	ProjectID     string `json:"project_id"`
	ProjectName   string `json:"project_name"`
	DeveloperID   string `json:"developer_id"`
	DeveloperName string `json:"developer_name"`
}

// QuotaResponse is the tiered quota envelope the identity service returned.
type QuotaResponse[T any] struct {
	Total     int `json:"total"`
	Used      int `json:"used"`
	Remaining int `json:"remaining"`
	Level     int `json:"level"`
	PerkLevel int `json:"perk_level"`
	Records   []T `json:"records"`
}

// TieredQuota mirrors ResourceQuotaCalculator.GetTieredQuota.
func TieredQuota(level, perkLevel int) int {
	base := 0
	switch {
	case level >= 60:
		base = 3
	case level >= 30:
		base = 2
	case level >= 10:
		base = 1
	}
	return base + perkLevel
}

// DeveloperQuota reports how many bot accounts the account's developers own
// and which tier it is on.
func (s *Service) DeveloperQuota(ctx context.Context, accountID string) (QuotaResponse[BotQuotaRecord], error) {
	empty := QuotaResponse[BotQuotaRecord]{Records: []BotQuotaRecord{}}
	if s == nil || s.Publishers == nil || s.Accounts == nil {
		return empty, fmt.Errorf("%w: developer quota dependencies unavailable", service.ErrDependency)
	}
	account, err := s.Accounts.GetAccount(ctx, accountID)
	if err != nil {
		return empty, rpcErrorToService(err, "account")
	}
	level := int(account.GetProfile().GetLevel())
	perkLevel := int(account.GetPerkLevel())
	total := TieredQuota(level, perkLevel)

	records, err := s.ownedBots(ctx, accountID)
	if err != nil {
		return empty, err
	}
	remaining := total - len(records)
	if remaining < 0 {
		remaining = 0
	}
	return QuotaResponse[BotQuotaRecord]{
		Total:     total,
		Used:      len(records),
		Remaining: remaining,
		Level:     level,
		PerkLevel: perkLevel,
		Records:   records,
	}, nil
}

// ownedBots lists the bots reachable from publishers the account owns
// directly, or through a realm it owns.
func (s *Service) ownedBots(ctx context.Context, accountID string) ([]BotQuotaRecord, error) {
	var developers []Developer
	if err := s.DB.Find(&developers).Error; err != nil {
		return nil, fmt.Errorf("list developers: %w", err)
	}
	if len(developers) == 0 {
		return []BotQuotaRecord{}, nil
	}
	publisherIDs := make([]string, 0, len(developers))
	for _, developer := range developers {
		publisherIDs = append(publisherIDs, developer.PublisherID)
	}
	publishers, err := s.Publishers.GetPublisherBatch(ctx, publisherIDs)
	if err != nil {
		return nil, rpcErrorToService(err, "publisher")
	}
	byID := make(map[string]*gen.DyPublisher, len(publishers))
	for _, publisher := range publishers {
		byID[publisher.GetId()] = publisher
	}

	realmIDs := make([]string, 0, len(publishers))
	for _, publisher := range publishers {
		if publisher.GetAccountId() == "" && publisher.GetRealmId() != "" {
			realmIDs = append(realmIDs, publisher.GetRealmId())
		}
	}
	ownedRealms := map[string]bool{}
	if len(realmIDs) > 0 && s.Realms != nil {
		realms, err := s.Realms.GetRealmBatch(ctx, realmIDs)
		if err != nil {
			return nil, rpcErrorToService(err, "realm")
		}
		for _, realm := range realms {
			if realm.GetAccountId() == accountID {
				ownedRealms[realm.GetId()] = true
			}
		}
	}

	owned := map[string]string{}
	for _, developer := range developers {
		publisher, ok := byID[developer.PublisherID]
		if !ok {
			continue
		}
		direct := publisher.GetAccountId() == accountID
		viaRealm := publisher.GetRealmId() != "" && ownedRealms[publisher.GetRealmId()]
		if direct || viaRealm {
			owned[developer.ID] = publisher.GetName()
		}
	}
	if len(owned) == 0 {
		return []BotQuotaRecord{}, nil
	}

	developerIDs := make([]string, 0, len(owned))
	for id := range owned {
		developerIDs = append(developerIDs, id)
	}
	var bots []BotAccount
	if err := s.DB.Preload("Project").Where("project_id IN (?)",
		s.DB.Model(&DevProject{}).Select("id").Where("developer_id IN ?", developerIDs),
	).Order("created_at ASC").Find(&bots).Error; err != nil {
		return nil, fmt.Errorf("list quota bots: %w", err)
	}

	records := make([]BotQuotaRecord, 0, len(bots))
	for _, bot := range bots {
		record := BotQuotaRecord{BotID: bot.ID, Slug: bot.Slug, ProjectID: bot.ProjectID}
		if bot.Project != nil {
			record.ProjectName = bot.Project.Name
			record.DeveloperID = bot.Project.DeveloperID
			record.DeveloperName = owned[bot.Project.DeveloperID]
		}
		records = append(records, record)
	}
	return records, nil
}

// LoadBotsAccounts fills the account projection for a batch of bots with one
// identity-service call.
func (s *Service) LoadBotsAccounts(ctx context.Context, bots []BotAccount) (map[string]*gen.DyAccount, error) {
	if len(bots) == 0 || s == nil || s.Accounts == nil {
		return map[string]*gen.DyAccount{}, nil
	}
	ids := make([]string, 0, len(bots))
	for _, bot := range bots {
		ids = append(ids, bot.ID)
	}
	accounts, err := s.Accounts.GetBotAccountBatch(ctx, ids)
	if err != nil {
		return nil, rpcErrorToService(err, "bot account")
	}
	byID := make(map[string]*gen.DyAccount, len(accounts))
	for _, account := range accounts {
		if id := account.GetAutomatedId().GetValue(); id != "" {
			byID[id] = account
		} else if id := account.GetId(); id != "" {
			byID[id] = account
		}
	}
	return byID, nil
}

func isUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique") || strings.Contains(message, "duplicate")
}
