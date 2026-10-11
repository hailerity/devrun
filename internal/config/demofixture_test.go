package config

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// playground/devrun.yaml is the project people are told to run and the one
// assets/demo.tape records for the README's hero image. It is committed,
// nothing else loads it, and a mistake in it only shows up at the end of a
// record-and-render cycle that takes minutes and needs vhs installed — so it
// is parsed here instead.
//
// The mistake that prompted this: `search`'s command contains ": " twice, and
// as a plain scalar YAML read "search: dial tcp ..." as a nested mapping and
// refused the whole file. The recording captured `devrun up` printing a parse
// error for its full length.
func TestDemoFixture_ParsesAndShowsWhatTheHeroNeeds(t *testing.T) {
	p, err := LoadProject("../../playground")
	require.NoError(t, err, "the committed demo project must load")
	require.NotNil(t, p)

	assert.Equal(t, "acme", p.Name)

	// Enough services, in enough groups, that the sidebar has to section and
	// fold. The whole argument for grouping is a list too long to scan, so a
	// demo with four services in two groups argues against the feature it is
	// there to show.
	assert.GreaterOrEqual(t, len(p.Services), 8,
		"a short list needs no dashboard; the demo has to be long enough to earn one")

	groups := map[string]int{}
	for name, svc := range p.Services {
		assert.NotEmptyf(t, svc.Group,
			"%s inherits the project name instead of naming a sub-project", name)
		groups[svc.Group]++
	}
	assert.GreaterOrEqual(t, len(groups), 3,
		"several sub-projects, which is the shape the example is about")

	// A service in more than one target, because that is what separates a
	// target from a group and the example exists partly to show it.
	shared := 0
	for _, members := range p.Targets {
		for _, m := range members {
			if m == "postgres" {
				shared++
			}
		}
	}
	assert.GreaterOrEqual(t, shared, 2, "one service belongs to several targets")

	// Every script a command names has to be beside the file, because the tape
	// copies this directory into a throwaway project and nothing else resolves
	// the path. A missing one records as a crashed service, which looks like a
	// devrun failure rather than a broken fixture.
	for name, svc := range p.Services {
		for _, field := range strings.Fields(svc.Command) {
			switch {
			case strings.HasSuffix(field, ".py"):
				assert.FileExistsf(t, filepath.Join("../../playground", field),
					"%s runs %s, which must ship alongside devrun.yaml", name, field)
			case strings.HasPrefix(field, "./"):
				// A Go package path, run from the playground directory.
				assert.DirExistsf(t, filepath.Join("../../playground", field),
					"%s runs %s, which must exist in the playground", name, field)
			}
		}
	}

	// The crash is the point of the frame: a dashboard where everything is green
	// says less about devrun than one with a ✖ and a reason beside it.
	search := p.Services["search"]
	require.NotNil(t, search)
	assert.Contains(t, search.Command, "exit 1")
	assert.Contains(t, search.Command, "connection refused",
		"the log line a reader sees under the crashed service")
	assert.NotContains(t, search.Command, "\n", "folded to one line")
}
