// Package gh integrates the GitHub CLI. Entirely optional: LTS works
// without gh installed; GitHub surfaces appear only when it's present and
// authenticated, following the same feature-detect pattern as tmux and
// package managers. All auth is gh's problem — LTS never sees a credential.
package gh

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// Available reports whether the GitHub CLI is installed.
func Available() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

// Authed reports whether gh has a usable token (local check, no network).
func Authed() bool {
	return Available() && exec.Command("gh", "auth", "token").Run() == nil
}

// Version returns the gh version (e.g. "2.89.0") for diagnostics.
func Version() (string, bool) {
	out, err := exec.Command("gh", "--version").Output()
	if err != nil {
		return "", false
	}
	fields := strings.Fields(strings.SplitN(string(out), "\n", 2)[0])
	for _, f := range fields {
		if f[0] >= '0' && f[0] <= '9' {
			return f, true
		}
	}
	return "", false
}

// Login returns the authenticated user's GitHub login. Network-bound —
// call once from a command goroutine and cache.
func Login() (string, error) {
	out, err := exec.Command("gh", "api", "user", "--jq", ".login").Output()
	if err != nil {
		return "", fmt.Errorf("gh api user failed")
	}
	return strings.TrimSpace(string(out)), nil
}

// Repo is a cloneable repository visible to the authenticated user.
type Repo struct {
	NameWithOwner string    `json:"nameWithOwner"`
	Description   string    `json:"description"`
	IsPrivate     bool      `json:"isPrivate"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

const repoListLimit = "200"

// RepoList returns the user's repos plus every org's repos, newest activity
// first. Network-bound — call from a command goroutine.
func RepoList() ([]Repo, error) {
	repos, err := listRepos("")
	if err != nil {
		return nil, err
	}
	if orgsOut, err := exec.Command("gh", "api", "user/orgs", "--jq", ".[].login").Output(); err == nil {
		for _, org := range strings.Fields(string(orgsOut)) {
			if orgRepos, err := listRepos(org); err == nil {
				repos = append(repos, orgRepos...)
			}
		}
	}
	// Dedupe (a repo can appear via user and org visibility) and sort by recency
	seen := make(map[string]bool)
	out := repos[:0]
	for _, r := range repos {
		if !seen[r.NameWithOwner] {
			seen[r.NameWithOwner] = true
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].NameWithOwner < out[j].NameWithOwner
	})
	return out, nil
}

func listRepos(owner string) ([]Repo, error) {
	args := []string{"repo", "list"}
	if owner != "" {
		args = append(args, owner)
	}
	args = append(args, "--json", "nameWithOwner,description,isPrivate,updatedAt", "--limit", repoListLimit)
	out, err := exec.Command("gh", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("gh repo list failed — is gh authenticated? (gh auth login)")
	}
	var repos []Repo
	if err := json.Unmarshal(out, &repos); err != nil {
		return nil, err
	}
	return repos, nil
}

// Clone clones nameWithOwner into destDir (the LTS working directory) and
// returns the created repo directory name.
func Clone(nameWithOwner, destDir string) (string, error) {
	cmd := exec.Command("gh", "repo", "clone", nameWithOwner)
	cmd.Dir = destDir
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return "", fmt.Errorf("clone failed: %s", msg)
	}
	parts := strings.SplitN(nameWithOwner, "/", 2)
	return parts[len(parts)-1], nil
}

// PRCreateWeb opens the browser's PR-creation page for the worktree's
// current branch against base (prefilled compare view). When a PR already
// exists for the branch, gh errors — fall through to opening that PR.
func PRCreateWeb(wtPath, base string) error {
	create := exec.Command("gh", "pr", "create", "--web", "--base", base)
	create.Dir = wtPath
	if out, err := create.CombinedOutput(); err != nil {
		if strings.Contains(string(out), "already exists") {
			view := exec.Command("gh", "pr", "view", "--web")
			view.Dir = wtPath
			if view.Run() == nil {
				return nil
			}
		}
		msg := strings.TrimSpace(string(out))
		if len(msg) > 160 {
			msg = msg[:160]
		}
		return fmt.Errorf("gh pr create failed: %s", msg)
	}
	return nil
}

// RemoteIsGitHub reports whether a repo's origin points at github.com —
// GitHub surfaces are per-repo, not global (a GitLab repo next door
// shouldn't grow PR buttons).
func RemoteIsGitHub(repoPath string) bool {
	out, err := exec.Command("git", "-C", repoPath, "remote", "get-url", "origin").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "github.com")
}
