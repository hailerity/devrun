package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestServiceConfigValidate(t *testing.T) {
	assert.NoError(t, (&ServiceConfig{Name: "web", Command: "yarn dev"}).Validate())

	assert.Error(t, (*ServiceConfig)(nil).Validate())
	assert.Error(t, (&ServiceConfig{Name: "web"}).Validate())
	assert.Error(t, (&ServiceConfig{Name: "web", Command: "   \t\n"}).Validate())
}

func TestProjectServiceConfigValidate(t *testing.T) {
	assert.NoError(t, (&ProjectServiceConfig{Command: "go run ."}).Validate())

	assert.Error(t, (*ProjectServiceConfig)(nil).Validate())
	assert.Error(t, (&ProjectServiceConfig{}).Validate())
	assert.Error(t, (&ProjectServiceConfig{Command: "  "}).Validate())
}
