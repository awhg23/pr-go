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

func splitFullName(fullName string) (string, string) {
	parts := strings.SplitN(fullName, "/", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}
