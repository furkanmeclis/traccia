// Package product composes implemented product services under one verified workspace policy.
package product

import (
	teams "github.com/furkanmeclis/traccia/backend/internal/modules/teams/usecase"
	workspaces "github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
)

func Registry(workspace *workspaces.Service, team *teams.Service) (*operations.Registry, error) {
	ops, e := team.Operations()
	if e != nil {
		return nil, e
	}
	return workspace.Registry(ops...)
}
