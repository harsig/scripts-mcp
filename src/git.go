package main

import (
	"context"
	"log"
)

// RequireRepos fails, without touching the network, if a git source has not been cloned yet.
func (c *Config) RequireRepos(configPath string) error {
	for _, src := range c.Sources {
		if src.Git != nil {
			if err := src.Git.Require("scripts-mcp --setup " + configPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// UpdateRepos clones missing git sources and fetches and checks out the configured ref of existing ones.
func (c *Config) UpdateRepos(ctx context.Context) error {
	for _, src := range c.Sources {
		if src.Git == nil {
			continue
		}
		log.Printf("updating %s", src.Git)
		if err := src.Git.Sync(ctx); err != nil {
			return err
		}
	}
	return nil
}
