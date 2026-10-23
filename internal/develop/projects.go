package develop

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"src.solsynth.dev/solsynth/distribution/internal/service"
)

// ProjectInput is the create/update payload for a developer project.
type ProjectInput struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// CreateProject adds a project under a developer.
func (s *Service) CreateProject(developerID string, input ProjectInput) (*DevProject, error) {
	slug := strings.TrimSpace(input.Slug)
	name := strings.TrimSpace(input.Name)
	if slug == "" || name == "" {
		return nil, fmt.Errorf("%w: slug and name are required", service.ErrValidation)
	}
	if len(slug) > 1024 || len(name) > 1024 || len(input.Description) > 4096 {
		return nil, fmt.Errorf("%w: slug, name or description exceed their maximum length", service.ErrValidation)
	}
	project := &DevProject{
		ID:          newUUID(),
		Slug:        slug,
		Name:        name,
		Description: input.Description,
		DeveloperID: developerID,
	}
	if err := s.DB.Create(project).Error; err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}
	return project, nil
}

// ListProjects lists a developer's projects.
func (s *Service) ListProjects(developerID string) ([]DevProject, error) {
	var projects []DevProject
	if err := s.DB.Where("developer_id = ?", developerID).Order("created_at ASC").Find(&projects).Error; err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	if projects == nil {
		projects = []DevProject{}
	}
	return projects, nil
}

// UpdateProject patches a project. Empty fields are left unchanged.
func (s *Service) UpdateProject(developerID, projectID string, input ProjectInput) (*DevProject, error) {
	project, err := s.LoadProject(developerID, projectID)
	if err != nil {
		return nil, err
	}
	if value := strings.TrimSpace(input.Slug); value != "" {
		project.Slug = value
	}
	if value := strings.TrimSpace(input.Name); value != "" {
		project.Name = value
	}
	if input.Description != "" {
		project.Description = input.Description
	}
	if len(project.Slug) > 1024 || len(project.Name) > 1024 || len(project.Description) > 4096 {
		return nil, fmt.Errorf("%w: slug, name or description exceed their maximum length", service.ErrValidation)
	}
	if err := s.DB.Save(project).Error; err != nil {
		return nil, fmt.Errorf("update project: %w", err)
	}
	return project, nil
}

// DeleteProject soft-deletes a project and everything published under it.
func (s *Service) DeleteProject(ctx context.Context, developerID, projectID string) error {
	project, err := s.LoadProject(developerID, projectID)
	if err != nil {
		return err
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var apps []CustomApp
		if err := tx.Where("project_id = ?", project.ID).Find(&apps).Error; err != nil {
			return fmt.Errorf("list project apps: %w", err)
		}
		appIDs := make([]string, 0, len(apps))
		for _, app := range apps {
			appIDs = append(appIDs, app.ID)
		}
		if len(appIDs) > 0 {
			var productIDs []string
			if err := tx.Model(&AppProduct{}).Where("app_id IN ?", appIDs).Pluck("id", &productIDs).Error; err != nil {
				return fmt.Errorf("list project app products: %w", err)
			}
			if len(productIDs) > 0 {
				if err := tx.Where("product_id IN ?", productIDs).Delete(&AppProductState{}).Error; err != nil {
					return fmt.Errorf("delete project product states: %w", err)
				}
			}
			if err := tx.Where("app_id IN ?", appIDs).Delete(&AppProduct{}).Error; err != nil {
				return fmt.Errorf("delete project app products: %w", err)
			}
			for _, model := range []any{&CustomAppSecret{}, &BoardWidget{}} {
				if err := tx.Where("app_id IN ?", appIDs).Delete(model).Error; err != nil {
					return fmt.Errorf("delete project app children: %w", err)
				}
			}
		}
		if err := tx.Where("project_id = ?", project.ID).Delete(&MiniApp{}).Error; err != nil {
			return fmt.Errorf("delete project mini apps: %w", err)
		}
		if err := tx.Where("project_id = ?", project.ID).Delete(&BotAccount{}).Error; err != nil {
			return fmt.Errorf("delete project bots: %w", err)
		}
		if err := tx.Where("project_id = ?", project.ID).Delete(&CustomApp{}).Error; err != nil {
			return fmt.Errorf("delete project apps: %w", err)
		}
		if err := tx.Delete(project).Error; err != nil {
			return fmt.Errorf("delete project: %w", err)
		}
		return nil
	})
}
