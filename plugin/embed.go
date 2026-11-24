// Package plugin holds the LTS Claude Code plugin (skills + hooks). The
// marketplace installs this directory as-is; the binary embeds the same
// files so `lts skill install` / `lts skill print` stay version-matched.
package plugin

import "embed"

// Skills is skills/<name>/SKILL.md for every bundled skill.
//
//go:embed skills
var Skills embed.FS
