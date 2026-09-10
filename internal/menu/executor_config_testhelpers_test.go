package menu

import "github.com/ViSiON-3/vision-3-bbs/internal/config"

// The executor's hot-reloadable configs are held as atomic snapshots, so they
// cannot be set from a struct literal or mutated in place. These helpers give
// tests the same convenience a plain field offered.

// newExecutorWithServerConfig builds an executor carrying cfg, applying any
// extra setup functions to the executor first.
func newExecutorWithServerConfig(cfg config.ServerConfig, setup ...func(*MenuExecutor)) *MenuExecutor {
	e := &MenuExecutor{}
	for _, fn := range setup {
		fn(e)
	}
	e.SetServerConfig(cfg)
	return e
}

// newExecutorWithStrings builds an executor carrying cfg, applying any extra
// setup functions to the executor first.
func newExecutorWithStrings(cfg config.StringsConfig, setup ...func(*MenuExecutor)) *MenuExecutor {
	e := &MenuExecutor{}
	for _, fn := range setup {
		fn(e)
	}
	e.SetStrings(cfg)
	return e
}

// setServerField applies a mutation to a copy of the current server config and
// stores it back, standing in for what used to be a direct field assignment.
func setServerField(e *MenuExecutor, mutate func(*config.ServerConfig)) {
	cfg := e.GetServerConfig()
	mutate(&cfg)
	e.SetServerConfig(cfg)
}

// setStringsField applies a mutation to a copy of the current strings config
// and stores it back.
func setStringsField(e *MenuExecutor, mutate func(*config.StringsConfig)) {
	cfg := *e.Strings()
	mutate(&cfg)
	e.SetStrings(cfg)
}
