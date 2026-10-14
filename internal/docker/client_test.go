package docker

import (
	"context"
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/joostme/conflux/internal/stacks"
)

// fakeCompose records the options Up is called with. Embedding the interface
// leaves every other method nil, so calling one fails the test loudly.
type fakeCompose struct {
	api.Compose
	project *types.Project
	up      api.UpOptions
}

func (f *fakeCompose) LoadProject(context.Context, api.ProjectLoadOptions) (*types.Project, error) {
	return f.project, nil
}

func (f *fakeCompose) Up(_ context.Context, _ *types.Project, options api.UpOptions) error {
	f.up = options
	return nil
}

func TestUp_PassesProjectToStart(t *testing.T) {
	// Without the project, Compose rebuilds it from container labels for the
	// start phase, which drops configs and secrets, so nothing gets injected.
	project := &types.Project{
		Name:     "proxy",
		Configs:  types.Configs{"routes": {Content: "http: {}"}},
		Services: types.Services{"proxy": {Name: "proxy", Configs: []types.ServiceConfigObjConfig{{Source: "routes"}}}},
	}
	fake := &fakeCompose{project: project}
	client := &ComposeClient{service: fake}

	if err := client.Up(context.Background(), stacks.Stack{Name: "proxy"}, ""); err != nil {
		t.Fatalf("Up() error = %v", err)
	}
	if fake.up.Start.Project != project {
		t.Error("Up must pass the loaded project to the start phase")
	}
}
