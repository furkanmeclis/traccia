// Package product composes implemented product services under one verified workspace policy.
package product

import (
	projects "github.com/furkanmeclis/traccia/backend/internal/modules/projects/usecase"
	teams "github.com/furkanmeclis/traccia/backend/internal/modules/teams/usecase"
	workspaces "github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
)

func Registry(workspace *workspaces.Service, team *teams.Service, project ...*projects.Service) (*operations.Registry, error) {
	ops, e := team.Operations()
	if e != nil {
		return nil, e
	}
	for _, service := range project {
		extra, err := service.Operations()
		if err != nil {
			return nil, err
		}
		ops = append(ops, extra...)
	}
	return workspace.Registry(ops...)
}
