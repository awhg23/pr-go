package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/awhg23/pr-go/internal/store"
)

func (s *Server) handleInstallationEvent(ctx context.Context, event WebhookEvent) error {
	if event.Installation.ID == 0 {
		return fmt.Errorf("webhook missing installation id")
	}
	account := event.Repository.Owner.Login
	if account == "" {
		account = event.Sender.Login
	}
	installationDBID, err := s.store.UpsertInstallation(ctx, store.Installation{
		InstallationID: event.Installation.ID,
		AccountLogin:   account,
		AccountType:    "repository",
	})
	if err != nil {
		return fmt.Errorf("record installation: %w", err)
	}
	if event.Event == EventInstallationRepositories && event.Action == "removed" {
		return s.deactivateInstallationRepositories(ctx, event)
	}
	if event.Event == EventInstallation && event.Action == "deleted" {
		repoIDs, err := s.store.DeactivateRepositoriesByInstallation(ctx, installationDBID)
		if err != nil {
			return fmt.Errorf("deactivate installation repositories: %w", err)
		}
		for _, repoID := range repoIDs {
			s.auditInstallationRepositoryRemoved(ctx, event, repoID)
		}
		return nil
	}
	repos := installationRepositories(event)
	for _, repo := range repos {
		owner, name := splitFullName(repo.FullName)
		if owner == "" {
			owner = repo.Owner.Login
		}
		if name == "" {
			name = repo.Name
		}
		if owner == "" || name == "" {
			continue
		}
		repoID, err := s.store.UpsertRepository(ctx, store.Repository{
			InstallationDBID: installationDBID,
			Owner:            owner,
			Name:             name,
			FullName:         owner + "/" + name,
		})
		if err != nil {
			return fmt.Errorf("record repository installation: %w", err)
		}
		detail, _ := json.Marshal(map[string]string{"event": event.Event, "action": event.Action})
		_ = s.store.Audit(ctx, store.AuditLog{
			RepositoryID: repoID,
			Actor:        event.Actor(),
			Action:       "installation_repository_recorded",
			DetailJSON:   string(detail),
		})
	}
	return nil
}

func (s *Server) deactivateInstallationRepositories(ctx context.Context, event WebhookEvent) error {
	for _, repo := range removedInstallationRepositories(event) {
		fullName := normalizedFullName(repo)
		if fullName == "" {
			continue
		}
		repoID, ok, err := s.store.DeactivateRepository(ctx, fullName)
		if err != nil {
			return fmt.Errorf("deactivate repository installation: %w", err)
		}
		if !ok {
			continue
		}
		s.auditInstallationRepositoryRemoved(ctx, event, repoID)
	}
	return nil
}

func (s *Server) auditInstallationRepositoryRemoved(ctx context.Context, event WebhookEvent, repoID int64) {
	detail, _ := json.Marshal(map[string]string{"event": event.Event, "action": event.Action})
	_ = s.store.Audit(ctx, store.AuditLog{
		RepositoryID: repoID,
		Actor:        event.Actor(),
		Action:       "installation_repository_removed",
		DetailJSON:   string(detail),
	})
}

func installationRepositories(event WebhookEvent) []Repository {
	switch event.Event {
	case EventInstallationRepositories:
		if event.Action == "added" {
			return event.RepositoriesAdded
		}
		return nil
	default:
		return event.Repositories
	}
}

func removedInstallationRepositories(event WebhookEvent) []Repository {
	if event.Event == EventInstallationRepositories {
		return event.RepositoriesRemoved
	}
	return event.Repositories
}

func normalizedFullName(repo Repository) string {
	owner, name := splitFullName(repo.FullName)
	if owner == "" {
		owner = repo.Owner.Login
	}
	if name == "" {
		name = repo.Name
	}
	if owner == "" || name == "" {
		return ""
	}
	return owner + "/" + name
}

func splitFullName(fullName string) (string, string) {
	parts := strings.SplitN(fullName, "/", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}
