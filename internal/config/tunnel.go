package config

import (
	"fmt"
	"strings"
)

// ProviderCloudflare is the only tunnel provider devrun drives. A second one
// would want an abstraction; one does not.
const ProviderCloudflare = "cloudflare"

// TunnelConfig is the tunnel: block of a devrun.yaml or services.yaml. Absent
// means devrun never looks for cloudflared at all — publishing is optional,
// and so is the dependency.
//
// Name and Hostname are separate because cloudflared keeps them separate:
// `cloudflared tunnel run --url <origin> <name>` identifies the tunnel by
// **name**, while the public **hostname** is a DNS CNAME pointing at it.
// Neither `tunnel info` nor `tunnel list` returns DNS routes, so devrun cannot
// derive one from the other. An earlier revision of the design used one field
// for both and would have passed a hostname where cloudflared wants a name.
type TunnelConfig struct {
	Provider string `yaml:"provider,omitempty" json:"provider,omitempty"`
	// Name is the cloudflared tunnel to run. Empty means a quick tunnel.
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
	// Hostname is what DNS points at that tunnel. devrun builds URLs from it
	// and never creates the record.
	Hostname string `yaml:"hostname,omitempty" json:"hostname,omitempty"`
}

// Defaults returns a copy with every unset field filled in. A nil receiver is
// valid and yields the all-defaults tunnel, which is a quick one.
func (t *TunnelConfig) Defaults() TunnelConfig {
	out := TunnelConfig{}
	if t != nil {
		out = *t
	}
	if out.Provider == "" {
		out.Provider = ProviderCloudflare
	}
	return out
}

// IsQuick reports whether this configuration asks for a quick tunnel — a
// throwaway *.trycloudflare.com hostname that changes every run and needs no
// account, no DNS and no prior login.
func (t TunnelConfig) IsQuick() bool { return t.Name == "" }

// Validate rejects a tunnel block that cannot mean anything, naming the key at
// fault.
func (t *TunnelConfig) Validate() error {
	if t == nil {
		return nil
	}
	if t.Provider != "" && t.Provider != ProviderCloudflare {
		return fmt.Errorf("tunnel.provider %q: only %q is supported", t.Provider, ProviderCloudflare)
	}

	// The two halves travel together. Encoding that here is deliberate: using
	// one value for both is the mistake this design already made once, and a
	// config that supplies half of it should say so rather than start a
	// tunnel nobody can address or print a URL nothing is listening on.
	switch {
	case t.Name == "" && t.Hostname == "":
		return nil // a quick tunnel: nothing to check
	case t.Name == "":
		return fmt.Errorf("tunnel.hostname %q needs tunnel.name beside it: "+
			"cloudflared identifies a tunnel by name, and the hostname is only the DNS record pointing at it", t.Hostname)
	case t.Hostname == "":
		return fmt.Errorf("tunnel.name %q needs tunnel.hostname beside it: "+
			"devrun cannot look the hostname up, and without it there is no URL to print", t.Name)
	}

	if err := ValidateName("tunnel", t.Name); err != nil {
		return err
	}
	return validateHostname(t.Hostname)
}

// validateHostname rejects what is plainly not a hostname. As with
// gateway.public_hostname, strictly: the failure it prevents shows up only
// once published, when whoever is debugging it is not at this machine.
func validateHostname(h string) error {
	if i := strings.IndexAny(h, "/: \t"); i >= 0 {
		return fmt.Errorf("tunnel.hostname %q is a hostname, not a URL: remove %q", h, string(h[i]))
	}
	if !strings.Contains(h, ".") {
		return fmt.Errorf("tunnel.hostname %q names no domain", h)
	}
	return nil
}

// SaveTunnel writes the tunnel block to whichever config src points at — the
// project devrun.yaml when src.IsLocal(), otherwise the global registry — and
// returns the path it wrote, so the caller can say which file it touched.
//
// This is the one place a lifecycle command writes config, and it is a
// deliberate exception: it records an answer the user has just typed at a
// prompt, rather than inferring one. `gateway expose` already writes to the
// active config the way `target add` does.
func SaveTunnel(src Source, t TunnelConfig) (string, error) {
	if src.IsLocal() {
		proj, err := LoadProject(src.Dir)
		if err != nil {
			return "", err
		}
		if proj == nil {
			return "", fmt.Errorf("no %s in %s", ProjectFileName, src.Dir)
		}
		proj.Tunnel = &t
		if err := SaveProject(src.Dir, proj); err != nil {
			return "", err
		}
		return src.Local, nil
	}

	path := RegistryPath()
	reg, err := LoadRegistry(path)
	if err != nil {
		return "", err
	}
	reg.Tunnel = &t
	if reg.Version == "" {
		reg.Version = "1"
	}
	if err := SaveRegistry(path, reg); err != nil {
		return "", err
	}
	return path, nil
}
