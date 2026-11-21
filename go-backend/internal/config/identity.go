package config

import "os"

// IdentityEnv reads a setting under its current FLUX name, falling back to
// legacy FLYX/FLVX names until existing deployments migrate. An explicitly
// empty earlier value overrides a leftover later setting.
func IdentityEnv(names ...string) string {
	for _, name := range names {
		if value, ok := os.LookupEnv(name); ok {
			return value
		}
	}
	return ""
}
