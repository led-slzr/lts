#!/bin/bash

# ============================================================================
#  Led Salazar's Tree Script (LTS) - Git Worktree Management Tool
#  A comprehensive tool for managing git worktrees with IDE integration
# ============================================================================

# Note: We intentionally don't use `set -e` because many git commands return
# non-zero exit codes for expected conditions (e.g., git diff --quiet returns 1
# when there ARE differences, which is not an error for our purposes).

# Get the directory where this script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Configuration
LTS_CONF="$SCRIPT_DIR/lts.conf"
DEFAULT_PACKAGE_MANAGER="pnpm"
DEFAULT_IDE_COMMAND="windsurf"
DEFAULT_BASIS_BRANCH="main"
DEFAULT_TOOLS_CHECKED="false"
DEFAULT_PACKAGE_MANAGER_CHECKED="false"
DEFAULT_LAST_REFRESH="0"
DEFAULT_WIDTH="62"
DEFAULT_HEIGHT="56"
PACKAGE_MANAGER=""
IDE_COMMAND=""
BASIS_BRANCH=""
TOOLS_CHECKED=""
PACKAGE_MANAGER_CHECKED=""
LAST_REFRESH=""
WIDTH=""
HEIGHT=""

# Display dimensions (computed after config is loaded)
INNER_WIDTH=""

# Track background processes for cleanup
BACKGROUND_PIDS=()

# Cleanup function for graceful exit
cleanup() {
    local exit_code=$?

    # Kill any background processes we started
    for pid in "${BACKGROUND_PIDS[@]}"; do
        if kill -0 "$pid" 2>/dev/null; then
            kill "$pid" 2>/dev/null || true
        fi
    done

    # Restore cursor if hidden
    printf "\e[?25h"

    # Return to script directory
    cd "$SCRIPT_DIR" 2>/dev/null || true

    # Only show warning for actual interruptions (Ctrl+C=130, TERM=143)
    if [[ $exit_code -eq 130 ]] || [[ $exit_code -eq 143 ]]; then
        echo ""
        echo -e "\033[0;33m⚠ Script interrupted. Some operations may be incomplete.\033[0m"
        echo -e "\033[0;34mℹ Run 'git worktree prune' in affected repos if needed.\033[0m"
    fi

    exit $exit_code
}

# Set up trap for various signals
trap cleanup EXIT
trap 'exit 130' INT   # Ctrl+C
trap 'exit 143' TERM  # kill command

# ============================================================================
#  COLORS & STYLING
# ============================================================================

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
MAGENTA='\033[0;35m'
CYAN='\033[0;36m'
WHITE='\033[1;37m'
DIM='\033[2m'
BOLD='\033[1m'
NC='\033[0m' # No Color

# ============================================================================
#  UTILITY FUNCTIONS
# ============================================================================

repeat_char() {
    local char="$1"
    local count="$2"
    printf "%0.s${char}" $(seq 1 "$count")
}

print_header() {
    local text="  $1"
    local width=$INNER_WIDTH
    local text_len=${#text}
    local padding=$((width - text_len))
    local spaces=""

    # Handle overflow: truncate if text too long
    if [[ $padding -lt 0 ]]; then
        text="${text:0:$((INNER_WIDTH - 3))}..."
        padding=0
    fi

    for ((i=0; i<padding; i++)); do spaces+=" "; done

    echo ""
    echo -e "${CYAN}╔$(repeat_char '═' $INNER_WIDTH)╗${NC}"
    echo -e "${CYAN}║${NC}${BOLD}${WHITE}${text}${NC}${spaces}${CYAN}║${NC}"
    echo -e "${CYAN}╚$(repeat_char '═' $INNER_WIDTH)╝${NC}"
    echo ""
}

print_subheader() {
    echo ""
    echo -e "${MAGENTA}━━━ $1 ━━━${NC}"
    echo ""
}

print_success() {
    echo -e "${GREEN}✓${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}⚠${NC} $1"
}

print_error() {
    echo -e "${RED}✗${NC} $1"
}

print_info() {
    echo -e "${BLUE}ℹ${NC} $1"
}

print_step() {
    echo -e "${CYAN}→${NC} $1"
}

# Confirm with Y/n (default Y)
confirm() {
    local prompt="$1"
    local response

    echo -ne "${YELLOW}?${NC} $prompt ${DIM}[Y/n]${NC}: " > /dev/tty
    read -r response < /dev/tty
    response=${response:-Y}

    [[ "$response" =~ ^[Yy]$ ]]
}

# Confirm with y/N (default N)
confirm_no() {
    local prompt="$1"
    local response

    echo -ne "${YELLOW}?${NC} $prompt ${DIM}[y/N]${NC}: " > /dev/tty
    read -r response < /dev/tty
    response=${response:-N}

    [[ "$response" =~ ^[Yy]$ ]]
}

# Spinner for long operations
spinner() {
    local pid=$1
    local message=$2
    local spin='⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏'
    local i=0

    while kill -0 "$pid" 2>/dev/null; do
        i=$(( (i+1) % ${#spin} ))
        printf "\r${CYAN}${spin:$i:1}${NC} %s" "$message"
        sleep 0.1
    done
    printf "\r"
}

# Extract suffix from branch name (fix/idv-hotfix -> idv-hotfix)
extract_suffix() {
    local branch="$1"
    echo "$branch" | sed 's|.*/||'
}

# Sanitize string for use in folder/file names
# Removes/replaces characters that are problematic in file systems or shell
sanitize_for_filename() {
    local input="$1"
    echo "$input" | \
        sed 's/[[:space:]]/-/g' | \
        sed 's/[@#$%^&*()!+=\[\]{}|\\:;"'"'"'<>,?]/-/g' | \
        sed 's/--*/-/g' | \
        sed 's/^-//' | \
        sed 's/-$//'
}

# Validate branch name input
# Returns 0 if valid, 1 if invalid
validate_branch_name() {
    local branch="$1"

    # Check for empty
    if [[ -z "$branch" || "$branch" =~ ^[[:space:]]*$ ]]; then
        print_error "Branch name cannot be empty"
        return 1
    fi

    # Check for problematic characters that git itself doesn't allow
    if [[ "$branch" =~ [[:space:]] ]]; then
        print_error "Branch name cannot contain spaces"
        return 1
    fi

    # STRICT PROTECTION: Never allow main/master as branch names for worktrees
    if [[ "$branch" == "main" || "$branch" == "master" ]]; then
        print_error "Cannot create worktree with '$branch' branch"
        print_error "This is the primary branch and should not be used for worktrees"
        return 1
    fi

    # Warn about characters that will be sanitized in folder names
    local suffix=$(extract_suffix "$branch")
    local sanitized=$(sanitize_for_filename "$suffix")

    if [[ "$suffix" != "$sanitized" ]]; then
        print_warning "Branch suffix '$suffix' contains special characters"
        print_info "Folder name will be sanitized to: $sanitized"
    fi

    return 0
}

# Generate unique worktree name, handling collisions
# Usage: generate_unique_worktree_name "repo-name" "branch-suffix" "lts-path"
generate_unique_worktree_name() {
    local repo_name="$1"
    local suffix="$2"
    local lts_path="$3"

    # Sanitize the suffix for safe folder naming
    local safe_suffix=$(sanitize_for_filename "$suffix")

    local base_name="${repo_name}-${safe_suffix}"
    local worktree_name="$base_name"
    local counter=2

    # Check if name already exists (as folder), if so add numeric suffix
    while [[ -d "$lts_path/$worktree_name" ]]; do
        worktree_name="${base_name}-${counter}"
        ((counter++))
    done

    echo "$worktree_name"
}

# Check for duplicate suffix in the current batch of branch names
# Returns 0 if duplicates found, 1 if all unique
check_duplicate_suffixes() {
    local -a branches=("$@")
    local -a suffixes=()

    for branch in "${branches[@]}"; do
        local suffix=$(extract_suffix "$branch")
        for existing in "${suffixes[@]}"; do
            if [[ "$existing" == "$suffix" ]]; then
                print_warning "Duplicate suffix detected: '$suffix'"
                print_warning "Branches '${branches[*]}' would create conflicting folder names"
                return 0
            fi
        done
        suffixes+=("$suffix")
    done

    return 1
}

# Get input with prompt
get_input() {
    local prompt="$1"
    local response

    # Clear any buffered input from terminal
    read -r -t 0.1 -n 10000 discard < /dev/tty 2>/dev/null || true

    echo -ne "${YELLOW}?${NC} $prompt: " > /dev/tty
    read -r response < /dev/tty
    echo "$response"
}

# ============================================================================
#  CONFIGURATION MANAGEMENT
# ============================================================================

create_default_config() {
    cat > "$LTS_CONF" << 'EOF'
# LTS Configuration File
# Generated automatically - edit values as needed

# IDE command to open workspaces (cursor, code, windsurf, zed)
IDE_COMMAND="windsurf"

# Package manager for installing dependencies (pnpm, npm, yarn, bun)
PACKAGE_MANAGER="pnpm"

# Basis branch for worktree creation (main, master, develop, etc.)
BASIS_BRANCH="main"

# Whether core tools (brew/git/fzf) have been checked (true/false)
# Set to false to force a full tools check on next run
TOOLS_CHECKED="false"

# Whether package manager has been checked (true/false)
# Set to false to force a package manager check on next run
PACKAGE_MANAGER_CHECKED="false"

# Unix timestamp of last refresh (0 = never)
LAST_REFRESH="0"

# Terminal width (total columns including borders)
WIDTH="62"

# Terminal height (total rows)
HEIGHT="42"
EOF
    print_success "Created default configuration: lts.conf"
}

load_config() {
    if [[ -f "$LTS_CONF" ]]; then
        source "$LTS_CONF"
    fi
    PACKAGE_MANAGER="${PACKAGE_MANAGER:-$DEFAULT_PACKAGE_MANAGER}"
    IDE_COMMAND="${IDE_COMMAND:-$DEFAULT_IDE_COMMAND}"
    BASIS_BRANCH="${BASIS_BRANCH:-$DEFAULT_BASIS_BRANCH}"

    # Backward-compatible migration: inherit from old PREREQUISITES_CHECKED if new flags don't exist
    if [[ -n "$PREREQUISITES_CHECKED" && -z "$TOOLS_CHECKED" && -z "$PACKAGE_MANAGER_CHECKED" ]]; then
        TOOLS_CHECKED="$PREREQUISITES_CHECKED"
        PACKAGE_MANAGER_CHECKED="$PREREQUISITES_CHECKED"
        unset PREREQUISITES_CHECKED
        save_config
    fi

    TOOLS_CHECKED="${TOOLS_CHECKED:-$DEFAULT_TOOLS_CHECKED}"
    PACKAGE_MANAGER_CHECKED="${PACKAGE_MANAGER_CHECKED:-$DEFAULT_PACKAGE_MANAGER_CHECKED}"
    LAST_REFRESH="${LAST_REFRESH:-$DEFAULT_LAST_REFRESH}"
    WIDTH="${WIDTH:-$DEFAULT_WIDTH}"
    HEIGHT="${HEIGHT:-$DEFAULT_HEIGHT}"
    INNER_WIDTH=$((WIDTH - 2))
}

save_config() {
    cat > "$LTS_CONF" << EOF
# LTS Configuration File
# Generated automatically - edit values as needed

# IDE command to open workspaces (cursor, code, windsurf, zed)
IDE_COMMAND="$IDE_COMMAND"

# Package manager for installing dependencies (pnpm, npm, yarn, bun)
PACKAGE_MANAGER="$PACKAGE_MANAGER"

# Basis branch for worktree creation (main, master, develop, etc.)
BASIS_BRANCH="$BASIS_BRANCH"

# Whether core tools (brew/git/fzf) have been checked (true/false)
# Set to false to force a full tools check on next run
TOOLS_CHECKED="$TOOLS_CHECKED"

# Whether package manager has been checked (true/false)
# Set to false to force a package manager check on next run
PACKAGE_MANAGER_CHECKED="$PACKAGE_MANAGER_CHECKED"

# Unix timestamp of last refresh (0 = never)
LAST_REFRESH="$LAST_REFRESH"

# Terminal width (total columns including borders)
WIDTH="$WIDTH"

# Terminal height (total rows)
HEIGHT="$HEIGHT"
EOF
}

check_auto_refresh() {
    local now
    now=$(date +%s)
    local elapsed=$(( now - LAST_REFRESH ))

    if [[ $elapsed -ge 86400 ]]; then
        print_info "Repos haven't been refreshed in over 24 hours"
        print_step "Auto-refreshing overview..."
        mode_refresh_overview
        LAST_REFRESH=$(date +%s)
        save_config
    fi
}

# ============================================================================
#  PREREQUISITES CHECK
# ============================================================================

check_brew() {
    if ! command -v brew &>/dev/null; then
        print_warning "Homebrew is not installed."
        if confirm "Would you like to install Homebrew?"; then
            print_step "Installing Homebrew..."
            /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"

            # Add brew to PATH for Apple Silicon Macs
            if [[ -f /opt/homebrew/bin/brew ]]; then
                eval "$(/opt/homebrew/bin/brew shellenv)"
            fi

            if ! command -v brew &>/dev/null; then
                print_error "Homebrew installation failed or requires shell restart."
                exit 1
            fi
            print_success "Homebrew installed successfully."
        else
            print_error "Homebrew is required. Exiting."
            exit 1
        fi
    else
        print_success "Homebrew is installed"
    fi
}

check_git() {
    if ! command -v git &>/dev/null; then
        print_warning "Git is not installed."
        if confirm "Would you like to install git via Homebrew?"; then
            print_step "Installing git..."
            brew install git
            if ! command -v git &>/dev/null; then
                print_error "Git installation failed."
                exit 1
            fi
            print_success "Git installed successfully."
        else
            print_error "Git is required. Exiting."
            exit 1
        fi
    else
        print_success "Git is installed"
    fi
}

check_fzf() {
    if ! command -v fzf &>/dev/null; then
        print_warning "fzf is not installed."
        if confirm "Would you like to install fzf via Homebrew?"; then
            print_step "Installing fzf..."
            brew install fzf
            if ! command -v fzf &>/dev/null; then
                print_error "fzf installation failed."
                exit 1
            fi
            print_success "fzf installed successfully."
        else
            print_error "fzf is required. Exiting."
            exit 1
        fi
    else
        print_success "fzf is installed"
    fi
}

check_ssh_auth() {
    # Test SSH connection to GitHub
    print_step "Testing GitHub SSH connection..."
    local ssh_output
    ssh_output=$(ssh -T git@github.com 2>&1 || true)

    if echo "$ssh_output" | grep -q "successfully authenticated"; then
        GIT_SSH_AUTHENTICATED=true
        print_success "GitHub SSH authentication working"
    else
        GIT_SSH_AUTHENTICATED=false
        print_warning "GitHub SSH authentication not configured or failed"
        print_info "Remote branch operations will be skipped"
        print_info "To set up SSH: https://docs.github.com/en/authentication/connecting-to-github-with-ssh"
    fi
}

check_package_manager() {
    if ! command -v "$PACKAGE_MANAGER" &>/dev/null; then
        print_warning "$PACKAGE_MANAGER is not installed."
        if confirm "Would you like to install $PACKAGE_MANAGER via Homebrew?"; then
            print_step "Installing $PACKAGE_MANAGER..."
            brew install "$PACKAGE_MANAGER"
            if ! command -v "$PACKAGE_MANAGER" &>/dev/null; then
                print_error "$PACKAGE_MANAGER installation failed."
                exit 1
            fi
            print_success "$PACKAGE_MANAGER installed successfully."
        else
            print_warning "$PACKAGE_MANAGER is optional but recommended for auto-installing dependencies."
        fi
    else
        print_success "$PACKAGE_MANAGER is installed"
    fi
}

# Global flag for git SSH auth status
GIT_SSH_AUTHENTICATED=false

check_prerequisites() {
    local ran_full_check=false

    # Stage 1: Core tools (brew, git, fzf)
    if [[ "$TOOLS_CHECKED" == "true" ]]; then
        # Silent verify: reset flag if tools disappeared
        if ! command -v git &>/dev/null || ! command -v fzf &>/dev/null; then
            TOOLS_CHECKED="false"
        fi
    fi
    if [[ "$TOOLS_CHECKED" != "true" ]]; then
        ran_full_check=true
        print_header "Checking Prerequisites"
        check_brew
        check_git
        check_fzf
        TOOLS_CHECKED="true"
    fi

    # Stage 2: SSH auth (always — network-dependent)
    local ssh_output
    ssh_output=$(ssh -T git@github.com 2>&1 || true)
    if echo "$ssh_output" | grep -q "successfully authenticated"; then
        GIT_SSH_AUTHENTICATED=true
    else
        GIT_SSH_AUTHENTICATED=false
        if [[ "$ran_full_check" == true ]]; then
            print_warning "GitHub SSH authentication not configured or failed"
            print_info "Remote branch operations will be skipped"
            print_info "To set up SSH: https://docs.github.com/en/authentication/connecting-to-github-with-ssh"
        fi
    fi

    # Stage 3: Package manager
    if [[ "$PACKAGE_MANAGER_CHECKED" == "true" ]]; then
        # Silent verify: reset flag if package manager disappeared
        if ! command -v "$PACKAGE_MANAGER" &>/dev/null; then
            PACKAGE_MANAGER_CHECKED="false"
        fi
    fi
    if [[ "$PACKAGE_MANAGER_CHECKED" != "true" ]]; then
        if [[ "$ran_full_check" != true ]]; then
            print_header "Checking Prerequisites"
        fi
        ran_full_check=true
        check_package_manager
        PACKAGE_MANAGER_CHECKED="true"
    fi

    if [[ "$ran_full_check" == true ]]; then
        echo ""
        print_success "All prerequisites checked!"
    fi

    save_config

    # Stage 4: Repos on main branch (always — transient state)
    check_repos_on_main_branch
}

check_repos_on_main_branch() {
    local repos
    repos=$(get_git_repos)
    if [[ -z "$repos" ]]; then
        return
    fi

    local switched_repos=()
    local failed_repos=()

    while IFS= read -r repo_name; do
        local repo_path="$SCRIPT_DIR/$repo_name"

        # Get main branch (silently skip if none found)
        local main_branch
        main_branch=$(get_main_branch "$repo_path" "true")
        if [[ -z "$main_branch" ]]; then
            continue
        fi

        # Get current branch
        local current_branch
        current_branch=$(cd "$repo_path" && git rev-parse --abbrev-ref HEAD 2>/dev/null)
        if [[ -z "$current_branch" ]]; then
            continue
        fi

        # Already on main — skip
        if [[ "$current_branch" == "$main_branch" ]]; then
            continue
        fi

        # Not on main — need to switch
        local is_dirty=false
        local stashed=false
        local original_branch="$current_branch"

        # Check for uncommitted changes
        if ! (cd "$repo_path" && git diff --quiet 2>/dev/null && git diff --cached --quiet 2>/dev/null); then
            is_dirty=true
        fi

        if [[ "$is_dirty" == true ]]; then
            # Try to stash changes
            if ! (cd "$repo_path" && git stash push -m "lts-auto-stash: switching to $main_branch" 2>/dev/null); then
                failed_repos+=("$repo_name")
                continue
            fi
            stashed=true
        fi

        # Try to checkout main
        if ! (cd "$repo_path" && git checkout "$main_branch" 2>/dev/null); then
            # Checkout failed — revert
            if [[ "$stashed" == true ]]; then
                (cd "$repo_path" && git stash pop 2>/dev/null) || true
            fi
            failed_repos+=("$repo_name")
            continue
        fi

        if [[ "$stashed" == true ]]; then
            # Try to pop stash
            if ! (cd "$repo_path" && git stash pop 2>/dev/null); then
                # Stash pop failed (conflict) — revert everything
                (cd "$repo_path" && git checkout -- . 2>/dev/null) || true
                (cd "$repo_path" && git checkout "$original_branch" 2>/dev/null) || true
                # Re-stash the changes on the original branch
                (cd "$repo_path" && git stash pop 2>/dev/null) || true
                failed_repos+=("$repo_name")
                continue
            fi
        fi

        switched_repos+=("$repo_name (was on $original_branch)")
    done <<< "$repos"

    # Print info about switched repos
    for info in "${switched_repos[@]}"; do
        print_info "Switched to $main_branch: $info"
    done

    # Abort if any repos failed
    if [[ ${#failed_repos[@]} -gt 0 ]]; then
        echo ""
        print_error "Aborted. Please manually change to your main branch for the repositories:"
        for repo in "${failed_repos[@]}"; do
            print_error "  - $repo"
        done
        exit 1
    fi
}

# ============================================================================
#  CORE FUNCTIONS
# ============================================================================

# Get list of git repositories (exclude worktrees and -lts directories)
get_git_repos() {
    local repos=()

    for dir in "$SCRIPT_DIR"/*/; do
        if [[ -d "$dir" ]]; then
            local dirname=$(basename "$dir")

            # Skip -lts directories
            if [[ "$dirname" == *-lts ]]; then
                continue
            fi

            # Check if it's a git repo (not a worktree)
            if [[ -d "$dir/.git" ]] && [[ -f "$dir/.git/config" ]]; then
                # It's a main repo, not a worktree
                repos+=("$dirname")
            fi
        fi
    done

    printf '%s\n' "${repos[@]}"
}

# Get list of -lts directories
get_lts_dirs() {
    local dirs=()

    for dir in "$SCRIPT_DIR"/*-lts/; do
        if [[ -d "$dir" ]]; then
            dirs+=("$(basename "$dir")")
        fi
    done

    printf '%s\n' "${dirs[@]}"
}

# Get worktrees within an -lts directory
get_worktrees_in_lts() {
    local lts_dir="$1"
    local worktrees=()

    for dir in "$SCRIPT_DIR/$lts_dir"/*/; do
        if [[ -d "$dir" ]]; then
            local dirname=$(basename "$dir")
            # Check if it's a worktree (has .git file, not directory)
            if [[ -f "$dir/.git" ]]; then
                worktrees+=("$dirname")
            fi
            # Also check one level deeper (branch-group subdirectories)
            for subdir in "$dir"/*/; do
                if [[ -d "$subdir" ]] && [[ -f "$subdir/.git" ]]; then
                    local subdirname=$(basename "$subdir")
                    worktrees+=("$dirname/$subdirname")
                fi
            done
        fi
    done

    printf '%s\n' "${worktrees[@]}"
}

# ============================================================================
#  MONOREPO LTS HELPERS
# ============================================================================

# Get the type of an LTS directory: "erp", "monorepo", or "single"
get_lts_type() {
    local lts_dir="$1"
    # Prefer explicit metadata
    if [[ -f "$SCRIPT_DIR/$lts_dir/.lts-type" ]]; then
        cat "$SCRIPT_DIR/$lts_dir/.lts-type"
        return
    fi
    # Backward compat: detect from old naming conventions
    if [[ -f "$SCRIPT_DIR/$lts_dir/.lts-repos" ]]; then
        local repo_name="${lts_dir%-lts}"
        if [[ "$repo_name" == "erp" ]]; then
            echo "erp"
        else
            echo "monorepo"
        fi
    else
        echo "single"
    fi
}

# Get the repos associated with an LTS directory
get_lts_repos() {
    local lts_dir="$1"
    local lts_type
    lts_type=$(get_lts_type "$lts_dir")

    if [[ -f "$SCRIPT_DIR/$lts_dir/.lts-repos" ]]; then
        cat "$SCRIPT_DIR/$lts_dir/.lts-repos"
    elif [[ "$lts_type" == "erp" ]]; then
        # Backward compat for old erp-lts without .lts-repos
        printf '%s\n' "gorocky-erp" "erp-ui"
    else
        # Single repo: derive from dir name
        echo "${lts_dir%-lts}"
    fi
}

# Check if an LTS directory is a monorepo (has .lts-repos file)
is_monorepo_lts() {
    local lts_dir="$1"
    [[ -f "$SCRIPT_DIR/$lts_dir/.lts-repos" ]]
}

# Get the list of repos in a monorepo LTS directory (one per line)
get_monorepo_repos() {
    local lts_dir="$1"
    cat "$SCRIPT_DIR/$lts_dir/.lts-repos"
}

# Given a worktree name and a list of repos, return which repo it belongs to
# Sorts repos by name length descending to solve prefix ambiguity
# e.g., "core-api-fix" matches "core-api" before "core"
get_repo_for_worktree() {
    local wt_name="$1"
    shift
    local repos=("$@")

    # Use basename to handle nested paths (e.g., "erp-hotfix/gorocky-erp-hotfix")
    local wt_basename
    wt_basename=$(basename "$wt_name")

    # Sort repos by length descending (longest first)
    local sorted_repos
    sorted_repos=$(printf '%s\n' "${repos[@]}" | awk '{ print length, $0 }' | sort -rn | cut -d' ' -f2-)

    while IFS= read -r repo; do
        [[ -z "$repo" ]] && continue
        if [[ "$wt_basename" == "${repo}-"* ]]; then
            echo "$repo"
            return 0
        fi
    done <<< "$sorted_repos"

    return 1
}

# Generate a monorepo LTS directory name from a list of repos
# Sorts alphabetically, joins with '-', appends '-lts'
# e.g., ("erp-ui" "core") → "core-erp-ui-lts"
generate_monorepo_lts_name() {
    local repos=("$@")
    local sorted
    sorted=$(printf '%s\n' "${repos[@]}" | LC_ALL=C sort)
    local joined=""
    while IFS= read -r repo; do
        [[ -z "$repo" ]] && continue
        if [[ -n "$joined" ]]; then
            joined+="-${repo}"
        else
            joined="$repo"
        fi
    done <<< "$sorted"
    echo "${joined}-lts"
}

# Find all monorepo LTS directories that contain a given repo
# Returns matching LTS dir names (one per line)
get_monorepo_lts_dirs_for_repo() {
    local target_repo="$1"
    local results=()

    for dir in "$SCRIPT_DIR"/*-lts/; do
        if [[ -d "$dir" ]]; then
            local dirname
            dirname=$(basename "$dir")
            if [[ -f "$dir/.lts-repos" ]]; then
                if grep -qxF "$target_repo" "$dir/.lts-repos" 2>/dev/null; then
                    results+=("$dirname")
                fi
            fi
        fi
    done

    printf '%s\n' "${results[@]}"
}

show_existing_branches() {
    local repo_path="$1"
    local repo_name
    repo_name=$(basename "$repo_path")

    cd "$repo_path" 2>/dev/null || return

    # Get local branches excluding main/master/HEAD
    local local_branches
    local_branches=$(git branch --format='%(refname:short)' 2>/dev/null | grep -vE '^(main|master)$')

    # Get remote-only branches (exist on origin but not locally)
    local remote_only_branches
    remote_only_branches=$(git branch -r --format='%(refname:short)' 2>/dev/null \
        | grep -E '^origin/' | grep -vE '/(main|master|HEAD)$' | sed 's|^origin/||' \
        | while IFS= read -r rb; do
            if ! git show-ref --verify --quiet "refs/heads/$rb" 2>/dev/null; then
                echo "$rb"
            fi
        done)

    cd "$SCRIPT_DIR"

    if [[ -z "$local_branches" && -z "$remote_only_branches" ]]; then
        return
    fi

    echo -e "${DIM}Existing branches in ${repo_name}:${NC}"

    if [[ -n "$local_branches" ]]; then
        echo "$local_branches" | while IFS= read -r b; do
            echo -e "  ${DIM}${b}${NC}"
        done
    fi

    if [[ -n "$remote_only_branches" ]]; then
        echo "$remote_only_branches" | while IFS= read -r b; do
            echo -e "  ${DIM}${b} (remote)${NC}"
        done
    fi

    echo ""
}

# Prune orphaned worktree entries from a repository
# Call this before worktree operations to clean up stale entries
prune_worktrees() {
    local repo_path="$1"

    if [[ -d "$repo_path" ]]; then
        cd "$repo_path"
        local pruned
        pruned=$(git worktree prune -v 2>&1 || true)
        if [[ -n "$pruned" ]] && ! echo "$pruned" | grep -q "^$"; then
            print_info "Pruned orphaned worktree entries in $(basename "$repo_path")"
        fi
        cd "$SCRIPT_DIR"
    fi
}

# Check for ongoing git operations (rebase, merge, cherry-pick, etc.)
# Returns 0 if operation in progress, 1 if clean
# Works for both main repos (.git is a directory) and worktrees (.git is a file)
check_ongoing_operations() {
    local repo_path="$1"

    cd "$repo_path"

    local has_operation=false
    local operation_type=""
    local git_dir=""

    # Determine the actual .git directory (different for worktrees vs main repos)
    if [[ -f ".git" ]]; then
        # This is a worktree - .git is a file pointing to the actual git dir
        git_dir=$(cat ".git" | sed 's/gitdir: //')
    elif [[ -d ".git" ]]; then
        # This is a main repo
        git_dir=".git"
    else
        # Not a git repo
        cd "$SCRIPT_DIR"
        return 1
    fi

    if [[ -d "$git_dir/rebase-merge" ]] || [[ -d "$git_dir/rebase-apply" ]]; then
        has_operation=true
        operation_type="rebase"
    elif [[ -f "$git_dir/MERGE_HEAD" ]]; then
        has_operation=true
        operation_type="merge"
    elif [[ -f "$git_dir/CHERRY_PICK_HEAD" ]]; then
        has_operation=true
        operation_type="cherry-pick"
    elif [[ -f "$git_dir/REVERT_HEAD" ]]; then
        has_operation=true
        operation_type="revert"
    elif [[ -f "$git_dir/index.lock" ]]; then
        # Check if the lock file is stale (no git process running)
        if check_stale_index_lock "$repo_path"; then
            # Lock was stale and removed, continue
            has_operation=false
        else
            has_operation=true
            operation_type="locked index (another git process may be running)"
        fi
    fi

    cd "$SCRIPT_DIR"

    if [[ "$has_operation" == true ]]; then
        print_error "Ongoing $operation_type detected in $(basename "$repo_path")"
        print_info "Please complete or abort the $operation_type before continuing"
        return 0
    fi

    return 1
}

# Check if index.lock is stale (no git process holding it)
# Returns 0 if stale and removed, 1 if active or user declined removal
# Works for both main repos and worktrees
check_stale_index_lock() {
    local repo_path="$1"
    local git_dir=""
    local lock_file=""

    cd "$repo_path" 2>/dev/null || return 1

    # Determine the actual .git directory
    if [[ -f ".git" ]]; then
        # Worktree - .git is a file
        git_dir=$(cat ".git" | sed 's/gitdir: //')
    elif [[ -d ".git" ]]; then
        # Main repo
        git_dir=".git"
    else
        cd "$SCRIPT_DIR"
        return 1
    fi

    lock_file="$git_dir/index.lock"

    if [[ ! -f "$lock_file" ]]; then
        cd "$SCRIPT_DIR"
        return 1
    fi

    # Check if any git process is running for this repo
    local git_pids
    git_pids=$(pgrep -f "git.*$(basename "$repo_path")" 2>/dev/null || true)

    # Also check for any git process in general that might hold the lock
    if [[ -z "$git_pids" ]]; then
        git_pids=$(pgrep -x "git" 2>/dev/null || true)
    fi

    if [[ -z "$git_pids" ]]; then
        # No git process found - lock is likely stale
        local lock_age
        lock_age=$(( $(date +%s) - $(stat -f %m "$lock_file" 2>/dev/null || stat -c %Y "$lock_file" 2>/dev/null || echo "0") ))

        print_warning "Stale index.lock found in $(basename "$repo_path") (age: ${lock_age}s)"
        print_info "No git process appears to be running"

        if confirm "Remove stale lock file?"; then
            rm -f "$lock_file"
            print_success "Removed stale index.lock"
            cd "$SCRIPT_DIR"
            return 0
        fi
    fi

    cd "$SCRIPT_DIR"
    return 1
}

# Check if a branch is protected (main/master)
# Returns 0 if protected, 1 if not
is_protected_branch() {
    local branch="$1"

    case "$branch" in
        main|master|develop|development|staging|production|release|release/*)
            return 0
            ;;
        *)
            return 1
            ;;
    esac
}

# Prompt user to choose how to handle branch deletion (local/remote/both/skip)
# Returns via global BRANCH_DELETE_ACTION: "skip", "local", "remote", "both"
BRANCH_DELETE_ACTION=""

prompt_branch_deletion() {
    local branch_name="$1"

    BRANCH_DELETE_ACTION="skip"

    # Check if remote branch exists
    local remote_exists=""
    if [[ "$GIT_SSH_AUTHENTICATED" == true ]] && ! is_protected_branch "$branch_name"; then
        remote_exists=$(git ls-remote --heads origin "$branch_name" 2>/dev/null)
    fi

    # Build options dynamically
    local options="Leave as is"
    options+=$'\n'"Remove local branch"
    if [[ -n "$remote_exists" ]]; then
        options+=$'\n'"Remove remote branch"
        options+=$'\n'"Remove local AND remote branch"
    fi

    local fzf_height=8
    [[ -z "$remote_exists" ]] && fzf_height=6

    # Print blank lines to ensure fzf has room to render all options
    printf '\n%.0s' {1..10}
    echo -e "${DIM}(press esc to skip)${NC}"
    local choice
    choice=$(echo "$options" | fzf --height=$fzf_height --reverse --prompt="Branch $branch_name: ")

    case "$choice" in
        "Leave as is"|"")
            BRANCH_DELETE_ACTION="skip"
            ;;
        "Remove local branch")
            BRANCH_DELETE_ACTION="local"
            ;;
        "Remove remote branch")
            BRANCH_DELETE_ACTION="remote"
            ;;
        "Remove local AND remote branch")
            BRANCH_DELETE_ACTION="both"
            ;;
    esac
}

# Execute branch deletion based on BRANCH_DELETE_ACTION
execute_branch_deletion() {
    local branch_name="$1"

    local delete_local=false
    local delete_remote=false

    case "$BRANCH_DELETE_ACTION" in
        "local") delete_local=true ;;
        "remote") delete_remote=true ;;
        "both") delete_local=true; delete_remote=true ;;
        *) return ;;
    esac

    # Delete local branch
    if [[ "$delete_local" == true ]]; then
        if git branch -d "$branch_name" 2>/dev/null; then
            print_success "Deleted local branch: $branch_name"
        else
            print_warning "Branch $branch_name has unmerged changes"
            if confirm "Force delete local branch (git branch -D)?"; then
                if git branch -D "$branch_name" 2>/dev/null; then
                    print_success "Force deleted local branch: $branch_name"
                else
                    print_error "Failed to delete branch: $branch_name"
                fi
            fi
        fi
    fi

    # Delete remote branch
    if [[ "$delete_remote" == true ]]; then
        local git_output
        git_output=$(git push origin --delete "$branch_name" 2>&1)
        if [[ $? -eq 0 ]]; then
            print_success "Deleted remote branch: $branch_name"
        else
            if echo "$git_output" | grep -qi "Could not read from remote"; then
                print_warning "Could not connect to remote (check SSH keys)"
            elif echo "$git_output" | grep -qi "refusing to delete"; then
                print_warning "Remote refused deletion (may be protected on GitHub)"
            else
                print_warning "Failed to delete remote branch: $git_output"
            fi
        fi
    fi
}

# Check if branch has unpushed commits
# Returns 0 if has unpushed commits, 1 if all pushed (or no unique commits)
has_unpushed_commits() {
    local branch="$1"

    # Check if remote tracking branch exists
    if ! git rev-parse --verify "origin/$branch" &>/dev/null; then
        # No remote tracking - count commits unique to this branch (not in main)
        # This avoids counting inherited commits from main as "unpushed"
        local main_branch=""

        # Try to find basis/main/master branch to compare against
        local _candidates=("$BASIS_BRANCH")
        [[ "$BASIS_BRANCH" != "main" ]] && _candidates+=("main")
        [[ "$BASIS_BRANCH" != "master" ]] && _candidates+=("master")
        for _c in "${_candidates[@]}"; do
            if git show-ref --verify --quiet "refs/heads/$_c" 2>/dev/null; then
                main_branch="$_c"
                break
            fi
        done

        if [[ -n "$main_branch" ]]; then
            # Count commits in branch that are NOT in main (unique to this branch)
            local commit_count
            commit_count=$(git rev-list --count "$main_branch..$branch" 2>/dev/null || echo "0")
            if [[ "$commit_count" -gt 0 ]]; then
                echo "$commit_count unique commit(s) (branch never pushed to remote)"
                return 0
            fi
        fi
        # No unique commits or can't determine - safe to delete
        return 1
    fi

    # Count commits ahead of remote
    local ahead
    ahead=$(git rev-list --count "origin/$branch..$branch" 2>/dev/null || echo "0")

    if [[ "$ahead" -gt 0 ]]; then
        echo "$ahead unpushed commit(s)"
        return 0
    fi

    return 1
}

# Get worktree display info (name + branch info for display)
# Usage: get_worktree_display_info "lts_dir" "worktree_name"
# Returns formatted string like: "repo-branch (2 unpushed commits)"
get_worktree_display_info() {
    local lts_dir="$1"
    local wt_name="$2"
    local wt_path="$SCRIPT_DIR/$lts_dir/$wt_name"

    if [[ ! -d "$wt_path" ]]; then
        echo "$wt_name"
        return
    fi

    cd "$wt_path" 2>/dev/null || { echo "$wt_name"; return; }

    # Get current branch
    local branch
    branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null)

    if [[ -z "$branch" || "$branch" == "HEAD" ]]; then
        cd "$SCRIPT_DIR"
        echo "$wt_name"
        return
    fi

    # Get commit info
    local info=""
    local commit_info
    commit_info=$(has_unpushed_commits "$branch" 2>/dev/null)

    if [[ $? -eq 0 ]] && [[ -n "$commit_info" ]]; then
        info=" ${YELLOW}⚠ $commit_info${NC}"
    fi

    cd "$SCRIPT_DIR"
    echo -e "$wt_name$info"
}

# Detect main branch (main or master)
# Returns empty string if no main branch found (fresh repo)
get_main_branch() {
    local repo_path="$1"
    local silent="${2:-false}"  # Optional: suppress warnings

    cd "$repo_path"

    local main_branch=""

    # Build candidate list: BASIS_BRANCH first, then main/master as fallbacks
    local candidates=("$BASIS_BRANCH")
    [[ "$BASIS_BRANCH" != "main" ]] && candidates+=("main")
    [[ "$BASIS_BRANCH" != "master" ]] && candidates+=("master")

    # Check locally first, then remote
    for candidate in "${candidates[@]}"; do
        if git show-ref --verify --quiet "refs/heads/$candidate" 2>/dev/null; then
            main_branch="$candidate"
            break
        fi
    done

    if [[ -z "$main_branch" ]]; then
        for candidate in "${candidates[@]}"; do
            if git show-ref --verify --quiet "refs/remotes/origin/$candidate" 2>/dev/null; then
                main_branch="$candidate"
                break
            fi
        done
    fi

    cd "$SCRIPT_DIR"

    if [[ -z "$main_branch" ]]; then
        if [[ "$silent" != "true" ]]; then
            print_warning "No $BASIS_BRANCH/main/master branch found in $(basename "$repo_path")"
            print_info "This may be a fresh repository or use a different default branch"
        fi
        # Try to get the default branch from git config
        cd "$repo_path"
        local default_branch
        default_branch=$(git config --get init.defaultBranch 2>/dev/null || echo "")
        if [[ -n "$default_branch" ]] && git show-ref --verify --quiet "refs/heads/$default_branch" 2>/dev/null; then
            main_branch="$default_branch"
        fi
        cd "$SCRIPT_DIR"
    fi

    echo "$main_branch"
}

# Get the remote URL and extract owner/repo
get_repo_info() {
    local repo_path="$1"
    local remote_url

    cd "$repo_path"
    remote_url=$(git remote get-url origin 2>/dev/null || echo "")
    cd "$SCRIPT_DIR"

    if [[ -z "$remote_url" ]]; then
        echo ""
        return
    fi

    # Extract owner/repo from various URL formats
    # git@github.com:Owner/Repo.git
    # https://github.com/Owner/Repo.git
    local owner_repo
    owner_repo=$(echo "$remote_url" | sed -E 's|.*[:/]([^/]+/[^/]+)\.git$|\1|' | sed -E 's|.*[:/]([^/]+/[^/]+)$|\1|')
    echo "$owner_repo"
}

# Ensure repo is on clean main branch with latest changes
ensure_clean_main() {
    local repo_path="$1"
    local main_branch
    local stashed=false
    local original_branch

    cd "$repo_path"

    # Get current branch before any operations
    original_branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")

    # Get main branch
    main_branch=$(get_main_branch "$repo_path")

    # Handle fresh repo with no main branch
    if [[ -z "$main_branch" ]]; then
        print_error "Cannot determine main branch for $(basename "$repo_path")"

        # Check if there are any branches at all
        local branch_count
        branch_count=$(git branch --list 2>/dev/null | wc -l | tr -d ' ')

        if [[ "$branch_count" -eq 0 ]]; then
            print_error "Repository has no branches (fresh repo with no commits?)"
            print_info "Please make an initial commit first"
            cd "$SCRIPT_DIR"
            return 1
        fi

        # Ask user to specify the base branch
        print_info "Available branches:"
        git branch --list 2>/dev/null | head -10
        echo ""

        local user_branch
        user_branch=$(get_input "Enter the base branch name to use")

        if git show-ref --verify --quiet "refs/heads/$user_branch" 2>/dev/null; then
            main_branch="$user_branch"
            print_success "Using '$main_branch' as base branch"
        else
            print_error "Branch '$user_branch' not found"
            cd "$SCRIPT_DIR"
            return 1
        fi
    fi

    # Check for uncommitted changes
    if ! git diff --quiet HEAD 2>/dev/null || ! git diff --cached --quiet 2>/dev/null; then
        print_warning "Uncommitted changes detected in $(basename "$repo_path")"
        if confirm "Would you like to stash them?"; then
            local stash_output
            stash_output=$(git stash push -m "LTS auto-stash $(date '+%Y-%m-%d %H:%M:%S')" 2>&1)
            local stash_exit=$?

            if [[ $stash_exit -eq 0 ]] && ! echo "$stash_output" | grep -q "No local changes"; then
                stashed=true
                print_success "Changes stashed"
            else
                print_warning "Stash may have failed: $stash_output"
                # Check if we still have uncommitted changes
                if ! git diff --quiet HEAD 2>/dev/null || ! git diff --cached --quiet 2>/dev/null; then
                    print_error "Uncommitted changes still present after stash attempt"
                    if ! confirm "Continue anyway? (changes may block checkout)"; then
                        cd "$SCRIPT_DIR"
                        return 1
                    fi
                fi
            fi
        else
            print_warning "Proceeding with uncommitted changes"
        fi
    fi

    # Get current branch
    local current_branch
    current_branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")

    # Switch to main if needed
    if [[ "$current_branch" != "$main_branch" ]]; then
        print_step "Switching to $main_branch branch..."
        local checkout_output
        checkout_output=$(git checkout "$main_branch" 2>&1)
        local checkout_exit=$?

        if [[ $checkout_exit -ne 0 ]]; then
            print_error "Failed to switch to $main_branch: $checkout_output"

            # If we stashed, offer to restore on original branch
            if [[ "$stashed" == true ]]; then
                print_warning "Your changes were stashed but checkout failed"
                print_info "You can recover them with: cd $(basename "$repo_path") && git stash pop"
            fi

            # Try to stay on original branch
            if [[ -n "$original_branch" ]] && [[ "$original_branch" != "HEAD" ]]; then
                git checkout "$original_branch" 2>/dev/null || true
            fi

            cd "$SCRIPT_DIR"
            return 1
        fi
    fi

    # Pull latest changes
    print_step "Pulling latest changes from origin/$main_branch..."
    local pull_output
    pull_output=$(git pull origin "$main_branch" 2>&1)
    local pull_exit=$?

    if [[ $pull_exit -ne 0 ]]; then
        if echo "$pull_output" | grep -qi "Could not read from remote"; then
            print_warning "Cannot reach remote (network issue or SSH not configured)"
        elif echo "$pull_output" | grep -qi "There is no tracking information"; then
            print_info "No upstream configured, skipping pull"
        else
            print_warning "Pull failed: $pull_output"
        fi
        print_info "Continuing with local state..."
    fi

    cd "$SCRIPT_DIR"

    if [[ "$stashed" == true ]]; then
        print_info "Remember: Your changes are stashed in $(basename "$repo_path")"
    fi

    return 0
}

# Copy .env files recursively
copy_env_files() {
    local source_path="$1"
    local dest_path="$2"

    print_step "Copying .env files..."

    # Find all .env* files (but not in node_modules, .git, etc.)
    local env_files
    env_files=$(find "$source_path" \
        -name ".env*" \
        -type f \
        ! -path "**/node_modules/*" \
        ! -path "**/.git/*" \
        ! -path "**/dist/*" \
        ! -path "**/build/*" \
        2>/dev/null || true)

    if [[ -z "$env_files" ]]; then
        print_warning "No .env files found"
        return
    fi

    local count=0
    while IFS= read -r env_file; do
        if [[ -n "$env_file" ]]; then
            # Get relative path
            local rel_path="${env_file#$source_path/}"
            local dest_file="$dest_path/$rel_path"
            local dest_dir=$(dirname "$dest_file")

            # Create directory if needed
            mkdir -p "$dest_dir"

            # Copy file
            cp "$env_file" "$dest_file"
            ((count++))
        fi
    done <<< "$env_files"

    print_success "Copied $count .env file(s)"
}

# Run package manager install
run_package_install() {
    local worktree_path="$1"

    if ! command -v "$PACKAGE_MANAGER" &>/dev/null; then
        print_warning "$PACKAGE_MANAGER not installed, skipping dependency installation"
        return
    fi

    if [[ ! -f "$worktree_path/package.json" ]]; then
        print_info "No package.json found, skipping $PACKAGE_MANAGER install"
        return
    fi

    print_step "Running $PACKAGE_MANAGER install in $(basename "$worktree_path")..."

    cd "$worktree_path"

    # Determine install command based on package manager
    local install_cmd=""
    case "$PACKAGE_MANAGER" in
        pnpm) install_cmd="pnpm install --silent" ;;
        npm)  install_cmd="npm install --silent" ;;
        yarn) install_cmd="yarn install --silent" ;;
        bun)  install_cmd="bun install --silent" ;;
        *)    install_cmd="$PACKAGE_MANAGER install" ;;
    esac

    # Run install in background and show spinner
    $install_cmd 2>/dev/null &
    local pid=$!
    BACKGROUND_PIDS+=("$pid")  # Track for cleanup on interrupt
    spinner $pid "Installing dependencies..."
    wait $pid
    local exit_code=$?

    # Remove from tracking array
    BACKGROUND_PIDS=("${BACKGROUND_PIDS[@]/$pid/}")

    cd "$SCRIPT_DIR"

    if [[ $exit_code -eq 0 ]]; then
        print_success "Dependencies installed"
    else
        print_warning "$PACKAGE_MANAGER install completed with warnings"
    fi
}

# Generate individual .code-workspace file
generate_individual_workspace() {
    local lts_dir="$1"
    local worktree_name="$2"
    local workspace_file="$SCRIPT_DIR/$lts_dir/${worktree_name}.code-workspace"
    local PM_UPPER
    PM_UPPER=$(echo "$PACKAGE_MANAGER" | tr '[:lower:]' '[:upper:]')

    cat > "$workspace_file" << 'EOF'
{
  "folders": [
    {
      "path": "WORKTREE_NAME_PLACEHOLDER"
    }
  ],
  "settings": {
    "terminal.integrated.defaultProfile.osx": "zsh"
  },
  "launch": {
    "version": "0.2.0",
    "configurations": []
  },
  "tasks": {
    "version": "2.0.0",
    "tasks": [
      {
        "label": "Claude",
        "type": "shell",
        "command": "claude",
        "isBackground": true,
        "presentation": {
          "reveal": "always",
          "panel": "new",
          "focus": true
        },
        "runOptions": {
          "runOn": "folderOpen"
        },
        "problemMatcher": []
      },
      {
        "label": "PM_LABEL_PLACEHOLDER",
        "type": "shell",
        "command": "echo ''; echo '📦 PM_LABEL_PLACEHOLDER Terminal Ready'; echo ''; exec $SHELL",
        "isBackground": true,
        "presentation": {
          "reveal": "always",
          "panel": "dedicated",
          "group": "lts"
        },
        "runOptions": {
          "runOn": "folderOpen"
        },
        "problemMatcher": []
      },
      {
        "label": "Git",
        "type": "shell",
        "command": "git status; echo ''; echo '📂 Git Terminal Ready'; echo ''; exec $SHELL",
        "isBackground": true,
        "presentation": {
          "reveal": "always",
          "panel": "dedicated",
          "group": "lts"
        },
        "runOptions": {
          "runOn": "folderOpen"
        },
        "problemMatcher": []
      }
    ]
  }
}
EOF

    # Replace placeholders with actual values
    sed -i '' "s|WORKTREE_NAME_PLACEHOLDER|${worktree_name}|g" "$workspace_file"
    sed -i '' "s|PM_LABEL_PLACEHOLDER|${PM_UPPER}|g" "$workspace_file"

    print_success "Created ${worktree_name}.code-workspace"
}

# Generate combined .code-workspace file
generate_combined_workspace() {
    local lts_dir="$1"
    local repo_name="$2"
    shift 2
    local worktrees=("$@")

    local workspace_file="$SCRIPT_DIR/$lts_dir/${repo_name}-combined.code-workspace"
    local PM_UPPER
    PM_UPPER=$(echo "$PACKAGE_MANAGER" | tr '[:lower:]' '[:upper:]')

    # Build folders JSON
    local folders_json=""
    for wt in "${worktrees[@]}"; do
        if [[ -n "$folders_json" ]]; then
            folders_json+=","
        fi
        folders_json+="
    {
      \"path\": \"${wt}\"
    }"
    done

    # Build tasks JSON: one Claude + per-worktree PM and Git
    local tasks_json=""

    # Single Claude terminal
    tasks_json+="
      {
        \"label\": \"Claude\",
        \"type\": \"shell\",
        \"command\": \"claude\",
        \"isBackground\": true,
        \"presentation\": {
          \"reveal\": \"always\",
          \"panel\": \"new\",
          \"focus\": true
        },
        \"runOptions\": {
          \"runOn\": \"folderOpen\"
        },
        \"problemMatcher\": []
      }"

    # Per-worktree PM and Git terminals
    for wt in "${worktrees[@]}"; do
        local suffix="${wt#*-}"
        suffix="${suffix#*-}"  # Remove second part if exists (e.g., core-idv-hotfix -> idv-hotfix)

        tasks_json+=",
      {
        \"label\": \"${PM_UPPER} [${suffix}]\",
        \"type\": \"shell\",
        \"command\": \"echo '' && echo '📦 ${PM_UPPER} Terminal Ready' && echo '' && exec \$SHELL\",
        \"options\": {
          \"cwd\": \"\\\${workspaceFolder:${wt}}\"
        },
        \"isBackground\": true,
        \"presentation\": {
          \"reveal\": \"always\",
          \"panel\": \"dedicated\",
          \"group\": \"lts\"
        },
        \"runOptions\": {
          \"runOn\": \"folderOpen\"
        },
        \"problemMatcher\": []
      },
      {
        \"label\": \"Git [${suffix}]\",
        \"type\": \"shell\",
        \"command\": \"git status && echo '' && echo '📂 Git Terminal Ready' && echo '' && exec \$SHELL\",
        \"options\": {
          \"cwd\": \"\\\${workspaceFolder:${wt}}\"
        },
        \"isBackground\": true,
        \"presentation\": {
          \"reveal\": \"always\",
          \"panel\": \"dedicated\",
          \"group\": \"lts\"
        },
        \"runOptions\": {
          \"runOn\": \"folderOpen\"
        },
        \"problemMatcher\": []
      }"
    done

    cat > "$workspace_file" << EOF
{
  "folders": [${folders_json}
  ],
  "settings": {
    "terminal.integrated.defaultProfile.osx": "zsh"
  },
  "launch": {
    "version": "0.2.0",
    "configurations": []
  },
  "tasks": {
    "version": "2.0.0",
    "tasks": [${tasks_json}
    ]
  }
}
EOF

    print_success "Created ${repo_name}-combined.code-workspace"
}

# Generate ERP combined workspace (for both repos)
# Uses unique workspace filename derived from worktree names to avoid collisions
generate_erp_workspace() {
    local base_dir="$1"  # Can be lts_dir or lts_dir/subdir
    local suffix="$2"
    local backend_wt="$3"
    local frontend_wt="$4"

    # Derive unique suffix from backend worktree name to avoid collisions
    # e.g., gorocky-erp-hotfix-2 -> hotfix-2
    local unique_suffix="${backend_wt#gorocky-erp-}"
    local workspace_file="$SCRIPT_DIR/$base_dir/erp-${unique_suffix}.code-workspace"
    local PM_UPPER
    PM_UPPER=$(echo "$PACKAGE_MANAGER" | tr '[:lower:]' '[:upper:]')

    cat > "$workspace_file" << 'WORKSPACE_EOF'
{
  "folders": [
    {
      "name": "ERP Backend - SUFFIX_PLACEHOLDER",
      "path": "BACKEND_WT_PLACEHOLDER"
    },
    {
      "name": "ERP UI - SUFFIX_PLACEHOLDER",
      "path": "FRONTEND_WT_PLACEHOLDER"
    }
  ],
  "settings": {
    "terminal.integrated.defaultProfile.osx": "zsh"
  },
  "launch": {
    "version": "0.2.0",
    "configurations": []
  },
  "tasks": {
    "version": "2.0.0",
    "tasks": [
      {
        "label": "Claude",
        "type": "shell",
        "command": "claude",
        "isBackground": true,
        "presentation": {
          "reveal": "always",
          "panel": "new",
          "focus": true
        },
        "runOptions": {
          "runOn": "folderOpen"
        },
        "problemMatcher": []
      },
      {
        "label": "Backend",
        "type": "shell",
        "command": "echo '' && echo '🔧 Backend Terminal (SUFFIX_PLACEHOLDER)' && echo '' && exec $SHELL",
        "options": {
          "cwd": "${workspaceFolder:ERP Backend - SUFFIX_PLACEHOLDER}"
        },
        "isBackground": true,
        "presentation": {
          "reveal": "always",
          "panel": "dedicated",
          "group": "lts"
        },
        "runOptions": {
          "runOn": "folderOpen"
        },
        "problemMatcher": []
      },
      {
        "label": "Frontend",
        "type": "shell",
        "command": "echo '' && echo '🎨 Frontend Terminal (SUFFIX_PLACEHOLDER)' && echo '' && exec $SHELL",
        "options": {
          "cwd": "${workspaceFolder:ERP UI - SUFFIX_PLACEHOLDER}"
        },
        "isBackground": true,
        "presentation": {
          "reveal": "always",
          "panel": "dedicated",
          "group": "lts"
        },
        "runOptions": {
          "runOn": "folderOpen"
        },
        "problemMatcher": []
      }
    ]
  }
}
WORKSPACE_EOF

    # Replace placeholders with actual values
    sed -i '' "s|SUFFIX_PLACEHOLDER|${suffix}|g" "$workspace_file"
    sed -i '' "s|BACKEND_WT_PLACEHOLDER|${backend_wt}|g" "$workspace_file"
    sed -i '' "s|FRONTEND_WT_PLACEHOLDER|${frontend_wt}|g" "$workspace_file"
    sed -i '' "s|PM_LABEL_PLACEHOLDER|${PM_UPPER}|g" "$workspace_file"

    print_success "Created erp-${unique_suffix}.code-workspace"
}

# Generate monorepo workspace file for N repos
# Args: lts_dir suffix "repo1:wt1" "repo2:wt2" ...
generate_monorepo_workspace() {
    local lts_dir="$1"
    local suffix="$2"
    shift 2
    local repo_wt_pairs=("$@")

    local workspace_file="$SCRIPT_DIR/$lts_dir/monorepo-${suffix}.code-workspace"
    local PM_UPPER
    PM_UPPER=$(echo "$PACKAGE_MANAGER" | tr '[:lower:]' '[:upper:]')

    # Build folders JSON
    local folders_json=""
    for pair in "${repo_wt_pairs[@]}"; do
        local repo="${pair%%:*}"
        local wt="${pair#*:}"
        if [[ -n "$folders_json" ]]; then
            folders_json+=","
        fi
        folders_json+="
    {
      \"name\": \"${repo} - ${suffix}\",
      \"path\": \"${wt}\"
    }"
    done

    # Build tasks JSON: one Claude + one terminal per repo
    local tasks_json=""

    # Single Claude terminal at parent directory
    tasks_json+="
      {
        \"label\": \"Claude\",
        \"type\": \"shell\",
        \"command\": \"claude\",
        \"isBackground\": true,
        \"presentation\": {
          \"reveal\": \"always\",
          \"panel\": \"new\",
          \"focus\": true
        },
        \"runOptions\": {
          \"runOn\": \"folderOpen\"
        },
        \"problemMatcher\": []
      }"

    # One terminal per repo
    for pair in "${repo_wt_pairs[@]}"; do
        local repo="${pair%%:*}"
        local wt="${pair#*:}"
        local folder_name="${repo} - ${suffix}"

        tasks_json+=",
      {
        \"label\": \"${repo}\",
        \"type\": \"shell\",
        \"command\": \"echo '' && echo '📂 ${repo} Terminal (${suffix})' && echo '' && exec \$SHELL\",
        \"options\": {
          \"cwd\": \"\\\${workspaceFolder:${folder_name}}\"
        },
        \"isBackground\": true,
        \"presentation\": {
          \"reveal\": \"always\",
          \"panel\": \"dedicated\",
          \"group\": \"lts\"
        },
        \"runOptions\": {
          \"runOn\": \"folderOpen\"
        },
        \"problemMatcher\": []
      }"
    done

    cat > "$workspace_file" << EOF
{
  "folders": [${folders_json}
  ],
  "settings": {
    "terminal.integrated.defaultProfile.osx": "zsh"
  },
  "launch": {
    "version": "0.2.0",
    "configurations": []
  },
  "tasks": {
    "version": "2.0.0",
    "tasks": [${tasks_json}
    ]
  }
}
EOF

    print_success "Created monorepo-${suffix}.code-workspace"
}

# ============================================================================
#  MODE 1: CREATE WORKTREE/S
# ============================================================================

mode_create_worktrees() {
    print_header "Create Worktree/s"

    # Get list of repositories
    local repos
    repos=$(get_git_repos)

    if [[ -z "$repos" ]]; then
        print_error "No git repositories found in $SCRIPT_DIR"
        return 1
    fi

    # Count repos
    local repo_count
    repo_count=$(echo "$repos" | wc -l | tr -d ' ')

    local selected_repo

    # Step 1: Select repository (always show selector for Go Back option)
    print_subheader "Step 1: Select Repository"
    if [[ "$repo_count" -eq 1 ]]; then
        print_info "Only one repository available"
    fi
    echo -e "${DIM}(press esc to cancel)${NC}"

    local repo_options="← Go Back
$repos"
    selected_repo=$(echo "$repo_options" | fzf --height=15 --reverse --prompt="Select repository: ")

    if [[ -z "$selected_repo" ]]; then
        print_warning "Cancelled."
        return 1
    fi

    if [[ "$selected_repo" == "← Go Back" ]]; then
        return 0
    fi

    print_success "Selected: $selected_repo"

    # Select number of worktrees
    while true; do
        print_subheader "Step 2: How Many Worktrees?"
        echo -e "${DIM}Hanggang tatlo lang baka mabaliw ka hehe${NC}"
        echo -e "${DIM}(press esc to cancel)${NC}"
        echo ""

        local count_options="← Go Back
1
2
3"
        local worktree_count
        worktree_count=$(echo "$count_options" | fzf --height=7 --reverse --prompt="Number of worktrees: ")

        if [[ -z "$worktree_count" ]]; then
            print_warning "Cancelled."
            return 1
        fi

        if [[ "$worktree_count" == "← Go Back" ]]; then
            return 0
        fi

        print_success "Creating $worktree_count worktree(s)"
        break
    done

    # Get branch names for each worktree
    print_subheader "Step 3: Enter Branch Names"
    echo -e "${DIM}You'll be prompted for each branch name one at a time.${NC}"
    echo -e "${DIM}A new branch will be created from main/master for each worktree.${NC}"
    echo ""
    echo -e "${DIM}Format:    <type>/<description>  (e.g., fix/login-bug)${NC}"
    echo -e "${DIM}Examples:  fix/login-bug   feature/new-dashboard   hotfix/payment-issue${NC}"
    echo ""
    echo -e "${DIM}The part after '/' becomes the folder name:${NC}"
    echo -e "${DIM}  fix/login-bug  →  ${selected_repo}-login-bug${NC}"
    echo ""
    echo -e "${DIM}(leave empty to cancel)${NC}"
    echo ""
    show_existing_branches "$SCRIPT_DIR/$selected_repo"
    local branch_names=()

    for ((i=1; i<=worktree_count; i++)); do
        local branch_name
        branch_name=$(get_input "Branch name for worktree $i (or empty to cancel)")

        # Allow empty input to cancel
        if [[ -z "$branch_name" || "$branch_name" =~ ^[[:space:]]*$ ]]; then
            print_warning "Cancelled."
            return 0
        fi

        if ! validate_branch_name "$branch_name"; then
            return 1
        fi

        branch_names+=("$branch_name")
    done

    # Check for duplicate suffixes in user input
    if check_duplicate_suffixes "${branch_names[@]}"; then
        print_error "Please use unique branch suffixes (the part after the last /)"
        return 1
    fi

    # Define paths early for confirmation display
    local repo_path="$SCRIPT_DIR/$selected_repo"
    local lts_dir="${selected_repo}-lts"
    local lts_path="$SCRIPT_DIR/$lts_dir"

    # Confirm
    print_subheader "Step 4: Confirmation"
    echo -e "Repository: ${BOLD}$selected_repo${NC}"
    echo -e "Worktrees to create:"
    for branch in "${branch_names[@]}"; do
        local suffix=$(extract_suffix "$branch")
        local wt_name=$(generate_unique_worktree_name "$selected_repo" "$suffix" "$lts_path")
        if [[ "$wt_name" != "${selected_repo}-${suffix}" ]]; then
            echo -e "  - Branch: ${CYAN}$branch${NC} -> Folder: ${YELLOW}${wt_name}${NC} (renamed to avoid collision)"
        else
            echo -e "  - Branch: ${CYAN}$branch${NC} -> Folder: ${CYAN}${wt_name}${NC}"
        fi
    done
    echo ""

    if ! confirm "Proceed with creation?"; then
        print_warning "Cancelled."
        return 1
    fi

    # Execute
    print_subheader "Step 5: Creating Worktrees"

    # Create -lts directory
    if [[ ! -d "$lts_path" ]]; then
        mkdir -p "$lts_path"
        print_success "Created $lts_dir directory"
    fi

    # Check for ongoing operations that would block us
    if check_ongoing_operations "$repo_path"; then
        return 1
    fi

    # Prune any orphaned worktree entries
    prune_worktrees "$repo_path"

    # Ensure clean main
    if ! ensure_clean_main "$repo_path"; then
        print_error "Failed to prepare repository"
        return 1
    fi

    # After ensure_clean_main succeeds, we're guaranteed to be on the base branch
    # Get it directly rather than calling get_main_branch again (which might not find custom branches)
    cd "$repo_path"
    local main_branch
    main_branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")

    # Fetch all remote refs so we can detect remote-only branches
    print_step "Fetching remote branches..."
    git fetch origin 2>/dev/null || true

    cd "$SCRIPT_DIR"

    # Safety check - if still no main branch, fail gracefully
    if [[ -z "$main_branch" ]]; then
        print_error "Could not determine base branch for worktree creation"
        print_info "ensure_clean_main should have resolved this - possible bug"
        return 1
    fi

    # Create worktrees
    local created_worktrees=()

    for branch in "${branch_names[@]}"; do
        local suffix=$(extract_suffix "$branch")
        local worktree_name=$(generate_unique_worktree_name "$selected_repo" "$suffix" "$lts_path")
        local worktree_path="$lts_path/$worktree_name"

        echo ""
        print_step "Creating worktree for branch: $branch"

        # Inform if name was auto-adjusted for uniqueness
        if [[ "$worktree_name" != "${selected_repo}-${suffix}" ]]; then
            print_info "Using unique folder name: $worktree_name (to avoid collision)"
        fi

        cd "$repo_path"

        # Check if branch already exists (local or remote)
        if git show-ref --verify --quiet "refs/heads/$branch" 2>/dev/null; then
            print_warning "Branch $branch already exists locally"

            # Check if branch is already checked out in another worktree
            local checked_out_at
            checked_out_at=$(git worktree list 2>/dev/null | grep -E "\[$branch\]" | awk '{print $1}' || echo "")

            if [[ -n "$checked_out_at" ]]; then
                print_error "Branch $branch is already checked out at: $checked_out_at"
                print_info "You must either:"
                print_info "  1. Use a different branch name"
                print_info "  2. Remove the existing worktree first (Mode 2)"
                print_info "  3. Switch that worktree to a different branch"
                cd "$SCRIPT_DIR"
                continue
            fi

            if confirm "Checkout existing branch instead of creating new?"; then
                local add_output
                add_output=$(git worktree add "$worktree_path" "$branch" 2>&1)
                if [[ $? -ne 0 ]]; then
                    if echo "$add_output" | grep -q "already checked out"; then
                        print_error "Branch is checked out elsewhere: $add_output"
                    else
                        print_error "Failed to add worktree: $add_output"
                    fi
                    cd "$SCRIPT_DIR"
                    continue
                fi
            else
                print_warning "Skipping $branch"
                cd "$SCRIPT_DIR"
                continue
            fi
        elif git show-ref --verify --quiet "refs/remotes/origin/$branch" 2>/dev/null; then
            print_warning "Branch $branch exists on remote"

            if confirm "Checkout remote branch instead of creating new?"; then
                local add_output
                add_output=$(git worktree add --track -b "$branch" "$worktree_path" "origin/$branch" 2>&1)
                if [[ $? -ne 0 ]]; then
                    print_error "Failed to add worktree: $add_output"
                    cd "$SCRIPT_DIR"
                    continue
                fi
            else
                print_warning "Skipping $branch"
                cd "$SCRIPT_DIR"
                continue
            fi
        else
            # Create new branch and worktree
            local add_output
            add_output=$(git worktree add -b "$branch" "$worktree_path" "$main_branch" 2>&1)
            if [[ $? -ne 0 ]]; then
                print_error "Failed to create worktree: $add_output"
                cd "$SCRIPT_DIR"
                continue
            fi
        fi

        cd "$SCRIPT_DIR"

        print_success "Created worktree: $worktree_name"
        created_worktrees+=("$worktree_name")

        # Copy .env files
        copy_env_files "$repo_path" "$worktree_path"

        # Install dependencies
        run_package_install "$worktree_path"

        # Generate individual workspace
        generate_individual_workspace "$lts_dir" "$worktree_name"
    done

    # Get ALL worktrees in the -lts directory (including previously created ones)
    local all_worktrees=()
    while IFS= read -r wt; do
        [[ -n "$wt" ]] && all_worktrees+=("$wt")
    done <<< "$(get_worktrees_in_lts "$lts_dir")"

    # Generate/regenerate combined workspace if more than one worktree exists
    if [[ ${#all_worktrees[@]} -gt 1 ]]; then
        print_step "Regenerating combined workspace with all ${#all_worktrees[@]} worktrees..."
        generate_combined_workspace "$lts_dir" "$selected_repo" "${all_worktrees[@]}"
    elif [[ ${#all_worktrees[@]} -eq 1 ]]; then
        # Remove combined workspace if only 1 worktree remains
        local combined_ws="$SCRIPT_DIR/$lts_dir/${selected_repo}-combined.code-workspace"
        if [[ -f "$combined_ws" ]]; then
            rm "$combined_ws"
            print_info "Removed combined workspace (only 1 worktree exists)"
        fi
    fi

    # Summary
    print_subheader "Summary"
    print_success "Created ${#created_worktrees[@]} new worktree(s) in $lts_dir/"
    echo ""
    echo -e "All worktrees in $lts_dir/:"
    for wt in "${all_worktrees[@]}"; do
        # Mark newly created ones
        local is_new=""
        for new_wt in "${created_worktrees[@]}"; do
            if [[ "$wt" == "$new_wt" ]]; then
                is_new=" ${GREEN}(new)${NC}"
                break
            fi
        done
        echo -e "  ${CYAN}$wt${NC}$is_new"
    done
    echo ""
    echo -e "Workspaces:"
    for wt in "${all_worktrees[@]}"; do
        echo -e "  ${CYAN}$lts_dir/${wt}.code-workspace${NC}"
    done
    if [[ ${#all_worktrees[@]} -gt 1 ]]; then
        echo -e "  ${CYAN}$lts_dir/${selected_repo}-combined.code-workspace${NC} ${DIM}(all worktrees)${NC}"
    fi
    echo ""

    # Offer to open workspace in IDE
    if [[ ${#created_worktrees[@]} -gt 0 ]]; then
        local workspace_to_open=""

        if [[ ${#all_worktrees[@]} -eq 1 ]]; then
            # Only 1 worktree total - simple confirm
            workspace_to_open="$SCRIPT_DIR/$lts_dir/${all_worktrees[0]}.code-workspace"
            if [[ -f "$workspace_to_open" ]]; then
                if confirm "Open workspace in $IDE_COMMAND?"; then
                    "$IDE_COMMAND" "$workspace_to_open" &
                    print_success "Opening $IDE_COMMAND..."
                fi
            fi
        else
            # Multiple worktrees - offer options
            local open_options="Open combined workspace (all worktrees)
Open individual workspace
Don't open, I'll open them"
            echo -e "${DIM}(press esc to skip)${NC}"
            local open_choice
            open_choice=$(echo "$open_options" | fzf --height=6 --reverse --prompt="Open workspace: ")

            if [[ -z "$open_choice" ]] || [[ "$open_choice" == "Don't open, I'll open them" ]]; then
                print_info "You can open workspaces from: $lts_dir/"
            elif [[ "$open_choice" == "Open combined workspace (all worktrees)" ]]; then
                workspace_to_open="$SCRIPT_DIR/$lts_dir/${selected_repo}-combined.code-workspace"
                if [[ -f "$workspace_to_open" ]]; then
                    "$IDE_COMMAND" "$workspace_to_open" &
                    print_success "Opening $IDE_COMMAND..."
                fi
            elif [[ "$open_choice" == "Open individual workspace" ]]; then
                # Let user select which individual workspace to open
                local ws_list=""
                for wt in "${all_worktrees[@]}"; do
                    ws_list+="${wt}.code-workspace"$'\n'
                done
                ws_list="${ws_list%$'\n'}"

                echo -e "${DIM}(press esc to skip, TAB to select multiple)${NC}"
                local selected_ws
                selected_ws=$(echo "$ws_list" | fzf --height=10 --reverse --multi --prompt="Select workspace(s): ")

                if [[ -n "$selected_ws" ]]; then
                    while IFS= read -r ws; do
                        if [[ -n "$ws" ]]; then
                            workspace_to_open="$SCRIPT_DIR/$lts_dir/$ws"
                            if [[ -f "$workspace_to_open" ]]; then
                                "$IDE_COMMAND" "$workspace_to_open" &
                                print_success "Opening: $ws"
                            fi
                        fi
                    done <<< "$selected_ws"
                fi
            fi
        fi
    fi
}

# ============================================================================
#  MODE 2: CLEANUP WORKTREE/S
# ============================================================================

mode_cleanup_worktrees() {
    print_header "Cleanup Worktree/s"

    # Get list of -lts directories
    local lts_dirs
    lts_dirs=$(get_lts_dirs)

    if [[ -z "$lts_dirs" ]]; then
        print_error "No LTS worktree directories found"
        return 1
    fi

    # Count LTS directories
    local lts_count
    lts_count=$(echo "$lts_dirs" | wc -l | tr -d ' ')

    local lts_list=()

    # Step 1: Select LTS directories (always show selector for Go Back option)
    print_subheader "Step 1: Select LTS Directory"

    if [[ "$lts_count" -eq 1 ]]; then
        print_info "Only one LTS directory available"
    fi

    # Build options - include "All" only if multiple directories
    local lts_options="← Go Back"
    if [[ "$lts_count" -gt 1 ]]; then
        lts_options+=$'\n'"All LTS directories"
    fi
    lts_options+=$'\n'"$lts_dirs"

    echo -e "${DIM}(press esc to cancel, TAB to select multiple)${NC}"
    local selected_lts
    selected_lts=$(echo "$lts_options" | fzf --height=15 --reverse --multi --prompt="Select LTS directory(s): ")

    if [[ -z "$selected_lts" ]]; then
        print_warning "Cancelled."
        return 1
    fi

    # Check if Go Back was selected
    if echo "$selected_lts" | grep -q "← Go Back"; then
        return 0
    fi

    # Check if All was selected
    if echo "$selected_lts" | grep -q "All LTS directories"; then
        while IFS= read -r dir; do
            [[ -n "$dir" ]] && lts_list+=("$dir")
        done <<< "$lts_dirs"
        print_info "Selected all LTS directories"
    else
        # Add selected LTS directories (filter out special options)
        while IFS= read -r dir; do
            # Skip empty, Go Back, and All options
            [[ -z "$dir" ]] && continue
            [[ "$dir" == "← Go Back" ]] && continue
            [[ "$dir" == "All LTS directories" ]] && continue
            lts_list+=("$dir")
        done <<< "$selected_lts"
        print_success "Selected ${#lts_list[@]} LTS directory(s)"
    fi

    # Step 2: Gather all worktrees from selected LTS directories
    print_subheader "Step 2: Select Worktrees to Clean"
    print_step "Checking worktree status..."

    # Build combined list: "lts_dir/worktree_name [status]"
    local all_worktrees_display=""
    local all_worktrees_raw=""  # Format: lts_dir:worktree_name

    for lts_dir in "${lts_list[@]}"; do
        local worktrees
        worktrees=$(get_worktrees_in_lts "$lts_dir")

        if [[ -z "$worktrees" ]]; then
            continue
        fi

        while IFS= read -r wt; do
            if [[ -n "$wt" ]]; then
                local wt_path="$SCRIPT_DIR/$lts_dir/$wt"
                local wt_status
                wt_status=$(get_worktree_status "$wt_path")

                # Color the status like the overview
                local status_color="$GREEN"
                case "$wt_status" in
                    *missing*) status_color="$RED" ;;
                    *diverged*) status_color="$RED" ;;
                    *changed*) status_color="$CYAN" ;;
                    *cleanable*) status_color="$GREEN" ;;
                    *"to push"*|*"to pull"*) status_color="$YELLOW" ;;
                    *"no remote"*) status_color="$DIM" ;;
                    *new*) status_color="$BLUE" ;;
                esac

                all_worktrees_display+="${lts_dir}/${wt} (${status_color}${wt_status}${NC})"$'\n'
                all_worktrees_raw+="${lts_dir}:${wt}"$'\n'
            fi
        done <<< "$worktrees"
    done

    # Remove trailing newlines
    all_worktrees_display="${all_worktrees_display%$'\n'}"
    all_worktrees_raw="${all_worktrees_raw%$'\n'}"

    if [[ -z "$all_worktrees_display" ]]; then
        print_warning "No worktrees found in selected LTS directories"
        return 0
    fi

    # Count total worktrees
    local total_wt_count
    total_wt_count=$(echo "$all_worktrees_raw" | wc -l | tr -d ' ')

    # Show all worktrees
    echo -e "Found $total_wt_count worktree(s):"
    echo -e "$all_worktrees_display" | while read -r wt; do
        echo -e "  - $wt"
    done
    echo ""

    # Ask: All or Select specific
    local scope_options="← Go Back
All worktrees ($total_wt_count)
Select specific"

    echo -e "${DIM}(press esc to cancel)${NC}"
    local scope
    scope=$(echo "$scope_options" | fzf --height=6 --reverse --prompt="Clean up: ")

    if [[ -z "$scope" ]]; then
        print_warning "Cancelled."
        return 1
    fi

    if [[ "$scope" == "← Go Back" ]]; then
        return 0
    fi

    # Build the final list of worktrees to clean (format: lts_dir:worktree_name)
    local selected_worktrees=""

    if [[ "$scope" == "All worktrees ($total_wt_count)" ]]; then
        selected_worktrees="$all_worktrees_raw"
        print_info "Selected all $total_wt_count worktree(s)"
    else
        # Multi-select from all worktrees
        local wt_options="← Go Back
$all_worktrees_display"
        echo -e "${DIM}(press esc to cancel, TAB to select multiple, ENTER to confirm)${NC}"
        local selected_display
        selected_display=$(echo -e "$wt_options" | fzf --height=20 --reverse --multi --ansi --prompt="Select worktree(s) to clean: ")

        if [[ -z "$selected_display" ]]; then
            print_warning "Cancelled."
            return 1
        fi

        # Check if Go Back was selected
        if echo "$selected_display" | grep -q "← Go Back"; then
            return 0
        fi

        # Parse selected items back to lts_dir:worktree format
        while IFS= read -r line; do
            # Skip empty lines and Go Back option
            [[ -z "$line" ]] && continue
            [[ "$line" == "← Go Back" ]] && continue

            # Format is: lts_dir/worktree_name [optional status]
            # Extract lts_dir/worktree_name (before any space/color code)
            local clean_line
            clean_line=$(echo "$line" | sed 's/ .*//' | sed 's/\x1b\[[0-9;]*m//g')
            # Convert lts_dir/worktree_name to lts_dir:worktree_name
            local lts_part="${clean_line%%/*}"
            local wt_part="${clean_line#*/}"
            selected_worktrees+="${lts_part}:${wt_part}"$'\n'
        done <<< "$selected_display"
        selected_worktrees="${selected_worktrees%$'\n'}"

        local selected_count
        selected_count=$(echo "$selected_worktrees" | wc -l | tr -d ' ')
        print_success "Selected $selected_count worktree(s)"
    fi

    if [[ -z "$selected_worktrees" ]]; then
        print_warning "No worktrees selected"
        return 0
    fi

    # Show confirmation summary
    print_subheader "Cleanup Confirmation"
    echo -e "${YELLOW}The following will be cleaned up:${NC}"
    echo ""

    while IFS= read -r entry; do
        [[ -z "$entry" ]] && continue
        local entry_lts="${entry%%:*}"
        local entry_wt="${entry#*:}"
        local wt_path="$SCRIPT_DIR/$entry_lts/$entry_wt"

        local branch_name=""
        if [[ -f "$wt_path/.git" ]]; then
            cd "$wt_path"
            branch_name=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
            cd "$SCRIPT_DIR"
        fi

        if [[ -n "$branch_name" ]]; then
            echo -e "  ${DIM}-${NC} ${CYAN}$entry_wt${NC} ${DIM}(branch: $branch_name)${NC}"
        else
            echo -e "  ${DIM}-${NC} ${CYAN}$entry_wt${NC}"
        fi
    done <<< "$selected_worktrees"

    echo ""
    echo -e "${YELLOW}This will:${NC}"
    echo -e "  ${DIM}- Remove worktree directories and files${NC}"
    echo -e "  ${DIM}- Prompt to delete local/remote branches${NC}"
    echo -e "  ${DIM}- Remove workspace files${NC}"
    echo ""

    if ! confirm "Proceed with cleanup?"; then
        print_warning "Cancelled."
        return 0
    fi

    # Get unique LTS directories from selected worktrees
    local unique_lts_dirs
    unique_lts_dirs=$(echo "$selected_worktrees" | cut -d':' -f1 | sort -u)

    # Track affected repos across all LTS dirs for final pull
    local affected_repos=()

    # Process each LTS directory
    while IFS= read -r lts_dir; do
        [[ -z "$lts_dir" ]] && continue

        print_subheader "Processing: $lts_dir"

        # Get worktrees for this LTS directory
        local worktrees_to_clean=()
        while IFS= read -r entry; do
            if [[ -n "$entry" ]]; then
                local entry_lts="${entry%%:*}"
                local entry_wt="${entry#*:}"
                if [[ "$entry_lts" == "$lts_dir" ]]; then
                    worktrees_to_clean+=("$entry_wt")
                fi
            fi
        done <<< "$selected_worktrees"

        # Detect LTS directory type via metadata
        local lts_type
        lts_type=$(get_lts_type "$lts_dir")
        local repo_name="${lts_dir%-lts}"
        local is_erp_lts=false
        local is_monorepo_lts_flag=false
        local monorepo_repos=()

        if [[ "$lts_type" == "erp" ]]; then
            is_erp_lts=true
            print_info "ERP LTS directory detected - will handle gorocky-erp and erp-ui repos"

            # Read repos from metadata (backward compat: defaults to gorocky-erp + erp-ui)
            local erp_repos=()
            while IFS= read -r r; do
                [[ -n "$r" ]] && erp_repos+=("$r")
            done <<< "$(get_lts_repos "$lts_dir")"

            # Track affected repos (before potential dir removal)
            for r in "${erp_repos[@]}"; do
                [[ ! " ${affected_repos[*]} " =~ " $r " ]] && affected_repos+=("$r")
            done

            # Prune and pull main for each ERP repo
            for r in "${erp_repos[@]}"; do
                if [[ -d "$SCRIPT_DIR/$r" ]]; then
                    prune_worktrees "$SCRIPT_DIR/$r"
                    print_step "Pulling main in $r..."
                    cd "$SCRIPT_DIR/$r"
                    local main_branch
                    main_branch=$(get_main_branch "$SCRIPT_DIR/$r")
                    git checkout "$main_branch" 2>/dev/null || true
                    git pull origin "$main_branch" 2>/dev/null || true
                    cd "$SCRIPT_DIR"
                fi
            done
        elif [[ "$lts_type" == "monorepo" ]]; then
            is_monorepo_lts_flag=true
            while IFS= read -r mr; do
                [[ -n "$mr" ]] && monorepo_repos+=("$mr")
            done <<< "$(get_monorepo_repos "$lts_dir")"
            print_info "Monorepo LTS directory detected - repos: ${monorepo_repos[*]}"

            # Track affected repos (before potential dir removal)
            for mr in "${monorepo_repos[@]}"; do
                [[ ! " ${affected_repos[*]} " =~ " $mr " ]] && affected_repos+=("$mr")
            done

            # Prune and pull main for each repo
            for mr in "${monorepo_repos[@]}"; do
                if [[ -d "$SCRIPT_DIR/$mr" ]]; then
                    prune_worktrees "$SCRIPT_DIR/$mr"
                    print_step "Pulling main in $mr..."
                    cd "$SCRIPT_DIR/$mr"
                    local main_branch
                    main_branch=$(get_main_branch "$SCRIPT_DIR/$mr")
                    git checkout "$main_branch" 2>/dev/null || true
                    git pull origin "$main_branch" 2>/dev/null || true
                    cd "$SCRIPT_DIR"
                fi
            done
        else
            local repo_path="$SCRIPT_DIR/$repo_name"

            # Track affected repo (before potential dir removal)
            [[ ! " ${affected_repos[*]} " =~ " $repo_name " ]] && affected_repos+=("$repo_name")

            # Prune orphaned worktree entries first
            prune_worktrees "$repo_path"

            # Verify main repo exists
            if [[ ! -d "$repo_path" ]]; then
                print_error "Main repository $repo_name not found"
                continue
            fi

            # Pull main first (so branch -d can detect merged branches)
            print_step "Pulling main branch in $repo_name..."
            cd "$repo_path"
            local main_branch
            main_branch=$(get_main_branch "$repo_path")
            git checkout "$main_branch" 2>/dev/null || true
            git pull origin "$main_branch" 2>/dev/null || true
            cd "$SCRIPT_DIR"
        fi

        # Process each worktree
        for wt in "${worktrees_to_clean[@]}"; do
            echo ""
            print_step "Cleaning up: $wt"

            local wt_path="$SCRIPT_DIR/$lts_dir/$wt"
            local wt_base
            wt_base=$(basename "$wt")

            # Determine the correct repo for this worktree
            local wt_repo_path=""

            if [[ "$is_erp_lts" == true ]]; then
                # For erp-lts, determine repo based on worktree name prefix
                if [[ "$wt_base" == gorocky-erp-* ]]; then
                    wt_repo_path="$SCRIPT_DIR/gorocky-erp"
                elif [[ "$wt_base" == erp-ui-* ]]; then
                    wt_repo_path="$SCRIPT_DIR/erp-ui"
                else
                    print_warning "Cannot determine repo for worktree: $wt"
                    wt_repo_path=""
                fi
            elif [[ "$is_monorepo_lts_flag" == true ]]; then
                local matched_repo
                matched_repo=$(get_repo_for_worktree "$wt_base" "${monorepo_repos[@]}")
                if [[ -n "$matched_repo" ]]; then
                    wt_repo_path="$SCRIPT_DIR/$matched_repo"
                else
                    print_warning "Cannot determine repo for worktree: $wt"
                    wt_repo_path=""
                fi
            else
                wt_repo_path="$SCRIPT_DIR/$repo_name"
            fi

            # Get the branch name for this worktree
            local branch_name=""
            if [[ -f "$wt_path/.git" ]]; then
                cd "$wt_path"
                branch_name=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
                cd "$SCRIPT_DIR"
            fi

            # Check for ongoing operations in the worktree itself
            if [[ -d "$wt_path" ]]; then
                if check_ongoing_operations "$wt_path"; then
                    print_warning "Worktree $wt has an ongoing git operation"
                    if ! confirm "Force remove worktree anyway? (may corrupt git state)"; then
                        print_warning "Skipping $wt"
                        continue
                    fi
                    print_warning "Proceeding with force removal..."
                fi
            fi

            # Check for uncommitted changes
            if [[ -d "$wt_path" ]]; then
                cd "$wt_path"
                if ! git diff --quiet HEAD 2>/dev/null || ! git diff --cached --quiet 2>/dev/null; then
                    print_warning "Uncommitted changes in $wt"
                    if ! confirm "Discard changes and continue?"; then
                        print_warning "Skipping $wt"
                        cd "$SCRIPT_DIR"
                        continue
                    fi
                fi
                cd "$SCRIPT_DIR"
            fi

            # Remove worktree (need to be in the main repo)
            if [[ -n "$wt_repo_path" && -d "$wt_repo_path" ]]; then
                cd "$wt_repo_path"
                if git worktree remove "$wt_path" --force 2>/dev/null; then
                    print_success "Removed worktree: $wt"
                else
                    print_warning "Could not remove worktree cleanly, forcing..."
                    rm -rf "$wt_path" 2>/dev/null || true
                    git worktree prune 2>/dev/null || true
                    print_success "Force removed worktree: $wt"
                fi
                cd "$SCRIPT_DIR"
            else
                # Just remove the directory if we can't find the repo
                print_warning "Main repo not found, removing directory directly..."
                rm -rf "$wt_path" 2>/dev/null || true
                print_success "Removed worktree directory: $wt"
            fi

            # Delete local branch (must be in the repo directory)
            if [[ -n "$branch_name" && "$branch_name" != "HEAD" && -n "$wt_repo_path" && -d "$wt_repo_path" ]]; then
                cd "$wt_repo_path"

                # STRICT PROTECTION: Never allow deletion of main/master branches (no choice given)
                if [[ "$branch_name" == "main" || "$branch_name" == "master" ]]; then
                    print_error "CRITICAL: Cannot delete '$branch_name' branch"
                    print_error "This is the primary branch and deletion is strictly forbidden"
                    print_info "Skipping branch deletion - worktree was removed but branch preserved"
                    cd "$SCRIPT_DIR"
                    # Still continue with workspace cleanup
                    local workspace_file="$(dirname "$SCRIPT_DIR/$lts_dir/$wt")/${wt_base}.code-workspace"
                    if [[ -f "$workspace_file" ]]; then
                        rm "$workspace_file"
                        print_success "Removed workspace file: ${wt}.code-workspace"
                    fi
                    continue
                fi

                # Check for other protected branches (develop, staging, production, etc.)
                if is_protected_branch "$branch_name"; then
                    print_error "PROTECTED BRANCH: $branch_name"
                    print_warning "This is a protected branch and should not be deleted"
                    print_info "Protected branches: develop, development, staging, production, release/*"

                    if ! confirm_no "Are you ABSOLUTELY SURE you want to delete '$branch_name'?"; then
                        print_info "Skipping deletion of protected branch: $branch_name"
                        cd "$SCRIPT_DIR"
                        # Still continue with workspace cleanup
                        local workspace_file="$(dirname "$SCRIPT_DIR/$lts_dir/$wt")/${wt_base}.code-workspace"
                        if [[ -f "$workspace_file" ]]; then
                            rm "$workspace_file"
                            print_success "Removed workspace file: ${wt}.code-workspace"
                        fi
                        continue
                    fi
                    print_warning "Proceeding with protected branch deletion..."
                fi

                # Check for unpushed commits before deleting
                local unpushed_info
                unpushed_info=$(has_unpushed_commits "$branch_name")
                if [[ $? -eq 0 ]]; then
                    print_warning "Branch $branch_name has $unpushed_info"
                    if ! confirm "Delete branch anyway? (commits will be LOST)"; then
                        print_info "Skipping branch deletion for: $branch_name"
                        cd "$SCRIPT_DIR"
                        # Still continue with workspace cleanup
                        local workspace_file="$(dirname "$SCRIPT_DIR/$lts_dir/$wt")/${wt_base}.code-workspace"
                        if [[ -f "$workspace_file" ]]; then
                            rm "$workspace_file"
                            print_success "Removed workspace file: ${wt}.code-workspace"
                        fi
                        continue
                    fi
                fi

                # Prompt and execute branch deletion
                prompt_branch_deletion "$branch_name"
                if [[ "$BRANCH_DELETE_ACTION" == "skip" ]]; then
                    print_info "Kept branch: $branch_name"
                else
                    execute_branch_deletion "$branch_name"
                fi

                cd "$SCRIPT_DIR"
            fi

            # Remove workspace file
            local workspace_file="$(dirname "$SCRIPT_DIR/$lts_dir/$wt")/${wt_base}.code-workspace"
            if [[ -f "$workspace_file" ]]; then
                rm "$workspace_file"
                print_success "Removed workspace file: ${wt}.code-workspace"
            fi

            # Determine the subdirectory containing this worktree (for nested paths)
            local wt_subdir_path
            wt_subdir_path=$(dirname "$SCRIPT_DIR/$lts_dir/$wt")

            # For ERP worktrees, check if the paired workspace is now orphaned
            if [[ "$is_erp_lts" == true ]]; then
                local wt_suffix=""
                if [[ "$wt_base" == gorocky-erp-* ]]; then
                    wt_suffix="${wt_base#gorocky-erp-}"
                elif [[ "$wt_base" == erp-ui-* ]]; then
                    wt_suffix="${wt_base#erp-ui-}"
                fi

                if [[ -n "$wt_suffix" ]]; then
                    local backend_exists=false
                    local frontend_exists=false

                    # Search in the same subdirectory (or flat in lts_dir for backward compat)
                    for check_dir in "$wt_subdir_path"/gorocky-erp-"$wt_suffix"*/; do
                        if [[ -d "$check_dir" ]] && [[ -f "$check_dir/.git" ]]; then
                            backend_exists=true
                            break
                        fi
                    done

                    for check_dir in "$wt_subdir_path"/erp-ui-"$wt_suffix"*/; do
                        if [[ -d "$check_dir" ]] && [[ -f "$check_dir/.git" ]]; then
                            frontend_exists=true
                            break
                        fi
                    done

                    if [[ "$backend_exists" == false ]] || [[ "$frontend_exists" == false ]]; then
                        # Look for workspace in the subdirectory
                        local erp_workspace="$wt_subdir_path/erp-${wt_suffix}.code-workspace"
                        if [[ -f "$erp_workspace" ]]; then
                            rm "$erp_workspace"
                            print_success "Removed orphaned ERP workspace: erp-${wt_suffix}.code-workspace"
                        fi
                    fi

                    # Clean up empty subdirectory
                    if [[ "$wt_subdir_path" != "$SCRIPT_DIR/$lts_dir" ]] && [[ -d "$wt_subdir_path" ]]; then
                        local remaining_in_subdir
                        remaining_in_subdir=$(find "$wt_subdir_path" -maxdepth 1 -mindepth 1 -type d 2>/dev/null | head -1)
                        if [[ -z "$remaining_in_subdir" ]]; then
                            rm -rf "$wt_subdir_path"
                            print_info "Removed empty subdirectory: $(basename "$wt_subdir_path")"
                        fi
                    fi
                fi
            fi

            # For monorepo worktrees, check if the workspace is now orphaned
            if [[ "$is_monorepo_lts_flag" == true ]]; then
                local matched_repo_for_suffix
                matched_repo_for_suffix=$(get_repo_for_worktree "$wt_base" "${monorepo_repos[@]}")
                if [[ -n "$matched_repo_for_suffix" ]]; then
                    local wt_suffix="${wt_base#${matched_repo_for_suffix}-}"
                    if [[ -n "$wt_suffix" ]]; then
                        local all_present=true
                        for check_repo in "${monorepo_repos[@]}"; do
                            local found=false
                            for check_dir in "$wt_subdir_path"/"${check_repo}-${wt_suffix}"*/; do
                                if [[ -d "$check_dir" ]] && [[ -f "$check_dir/.git" ]]; then
                                    found=true
                                    break
                                fi
                            done
                            if [[ "$found" == false ]]; then
                                all_present=false
                                break
                            fi
                        done

                        if [[ "$all_present" == false ]]; then
                            local mono_workspace="$wt_subdir_path/monorepo-${wt_suffix}.code-workspace"
                            if [[ -f "$mono_workspace" ]]; then
                                rm "$mono_workspace"
                                print_success "Removed orphaned monorepo workspace: monorepo-${wt_suffix}.code-workspace"
                            fi
                        fi

                        # Clean up empty subdirectory
                        if [[ "$wt_subdir_path" != "$SCRIPT_DIR/$lts_dir" ]] && [[ -d "$wt_subdir_path" ]]; then
                            local remaining_in_subdir
                            remaining_in_subdir=$(find "$wt_subdir_path" -maxdepth 1 -mindepth 1 -type d 2>/dev/null | head -1)
                            if [[ -z "$remaining_in_subdir" ]]; then
                                rm -rf "$wt_subdir_path"
                                print_info "Removed empty subdirectory: $(basename "$wt_subdir_path")"
                            fi
                        fi
                    fi
                fi
            fi
        done

        # Check remaining worktrees and handle combined workspace
        local remaining_worktrees
        remaining_worktrees=$(get_worktrees_in_lts "$lts_dir")

        if [[ -z "$remaining_worktrees" ]]; then
            # No worktrees left - remove combined workspace and optionally the directory
            if [[ "$is_erp_lts" == true ]]; then
                # Remove all erp-*.code-workspace files
                rm -f "$SCRIPT_DIR/$lts_dir"/erp-*.code-workspace 2>/dev/null || true
            elif [[ "$is_monorepo_lts_flag" == true ]]; then
                # Remove all monorepo-*.code-workspace files
                rm -f "$SCRIPT_DIR/$lts_dir"/monorepo-*.code-workspace 2>/dev/null || true
            else
                local combined_ws="$SCRIPT_DIR/$lts_dir/${repo_name}-combined.code-workspace"
                [[ -f "$combined_ws" ]] && rm "$combined_ws"
            fi

            # Auto-remove empty -lts directory without prompting
            rm -rf "$SCRIPT_DIR/$lts_dir"
            print_success "Removed empty $lts_dir directory"
        else
            # Worktrees remain - regenerate combined workspace
            if [[ "$is_erp_lts" == true ]]; then
                # For ERP, we don't have a single combined workspace
                # Each erp-*.code-workspace is for a specific branch pair
                # Just inform the user about remaining worktrees
                print_info "Remaining ERP worktrees in $lts_dir:"
                echo "$remaining_worktrees" | while read -r wt; do
                    echo -e "  ${CYAN}$wt${NC}"
                done
            elif [[ "$is_monorepo_lts_flag" == true ]]; then
                # For monorepo, inform about remaining worktrees
                print_info "Remaining monorepo worktrees in $lts_dir:"
                echo "$remaining_worktrees" | while read -r wt; do
                    echo -e "  ${CYAN}$wt${NC}"
                done
            else
                # Regenerate combined workspace with remaining worktrees
                local remaining_array=()
                while IFS= read -r wt; do
                    [[ -n "$wt" ]] && remaining_array+=("$wt")
                done <<< "$remaining_worktrees"

                if [[ ${#remaining_array[@]} -gt 1 ]]; then
                    print_step "Regenerating combined workspace with ${#remaining_array[@]} remaining worktrees..."
                    generate_combined_workspace "$lts_dir" "$repo_name" "${remaining_array[@]}"
                elif [[ ${#remaining_array[@]} -eq 1 ]]; then
                    # Only 1 worktree left, remove combined workspace
                    local combined_ws="$SCRIPT_DIR/$lts_dir/${repo_name}-combined.code-workspace"
                    if [[ -f "$combined_ws" ]]; then
                        rm "$combined_ws"
                        print_info "Removed combined workspace (only 1 worktree remains)"
                    fi
                fi
            fi
        fi
    done <<< "$unique_lts_dirs"

    # Final pull from main for all affected repos to guarantee updatedness
    # (affected_repos was populated during the processing loop above, before dirs were removed)
    print_subheader "Finalizing"

    for repo in "${affected_repos[@]}"; do
        local repo_path="$SCRIPT_DIR/$repo"
        if [[ -d "$repo_path" ]]; then
            print_step "Pulling latest main in $repo..."
            cd "$repo_path"
            local main_branch
            main_branch=$(get_main_branch "$repo_path")
            git checkout "$main_branch" 2>/dev/null || true
            git pull origin "$main_branch" 2>/dev/null || true
            cd "$SCRIPT_DIR"
        fi
    done

    print_subheader "Cleanup Complete"
    print_success "Worktree cleanup finished!"
}

mode_cleanup_merged_cleanables() {
    print_header "Cleanup Merged Cleanables"

    # Get all LTS directories
    local lts_dirs
    lts_dirs=$(get_lts_dirs)

    if [[ -z "$lts_dirs" ]]; then
        print_error "No LTS worktree directories found"
        return 1
    fi

    print_step "Scanning for merged/cleanable worktrees..."

    # Gather all cleanable worktrees across all LTS dirs
    local cleanable_display=""
    local cleanable_raw=""  # Format: lts_dir:worktree_name

    while IFS= read -r lts_dir; do
        [[ -z "$lts_dir" ]] && continue

        local worktrees
        worktrees=$(get_worktrees_in_lts "$lts_dir")

        if [[ -z "$worktrees" ]]; then
            continue
        fi

        while IFS= read -r wt; do
            [[ -z "$wt" ]] && continue

            local wt_path="$SCRIPT_DIR/$lts_dir/$wt"
            local status
            status=$(get_worktree_status "$wt_path")

            # Check if status indicates merged/cleanable
            if echo "$status" | grep -q "cleanable\|^merged"; then
                cleanable_display+="${lts_dir}/${wt} (${status})"$'\n'
                cleanable_raw+="${lts_dir}:${wt}"$'\n'
            fi
        done <<< "$worktrees"
    done <<< "$lts_dirs"

    # Remove trailing newlines
    cleanable_display="${cleanable_display%$'\n'}"
    cleanable_raw="${cleanable_raw%$'\n'}"

    if [[ -z "$cleanable_raw" ]]; then
        print_info "No merged/cleanable worktrees found"
        return 0
    fi

    # Count cleanable worktrees
    local cleanable_count
    cleanable_count=$(echo "$cleanable_raw" | wc -l | tr -d ' ')

    echo ""
    echo -e "Found $cleanable_count merged/cleanable worktree(s):"
    echo -e "$cleanable_display" | while read -r line; do
        echo -e "  ${GREEN}-${NC} $line"
    done
    echo ""

    # Ask: All or Select specific
    local scope_options="← Go Back
All merged cleanables ($cleanable_count)
Select specific"

    echo -e "${DIM}(press esc to cancel)${NC}"
    local scope
    scope=$(echo "$scope_options" | fzf --height=6 --reverse --prompt="Clean up: ")

    if [[ -z "$scope" ]]; then
        print_warning "Cancelled."
        return 1
    fi

    if [[ "$scope" == "← Go Back" ]]; then
        return 0
    fi

    local selected_worktrees=""

    if [[ "$scope" == "All merged cleanables ($cleanable_count)" ]]; then
        selected_worktrees="$cleanable_raw"
        print_info "Selected all $cleanable_count merged cleanable(s)"
    else
        # Multi-select from cleanable worktrees
        local wt_options="← Go Back
$cleanable_display"
        echo -e "${DIM}(press esc to cancel, TAB to select multiple, ENTER to confirm)${NC}"
        local selected_display
        selected_display=$(echo "$wt_options" | fzf --height=20 --reverse --multi --prompt="Select worktree(s) to clean: ")

        if [[ -z "$selected_display" ]]; then
            print_warning "Cancelled."
            return 1
        fi

        if echo "$selected_display" | grep -q "← Go Back"; then
            return 0
        fi

        # Parse selected items back to lts_dir:worktree format
        while IFS= read -r line; do
            [[ -z "$line" ]] && continue
            [[ "$line" == "← Go Back" ]] && continue

            # Format is: lts_dir/worktree_name (status)
            # Extract lts_dir/worktree_name (before " (")
            local clean_line="${line%% (*}"
            local lts_part="${clean_line%%/*}"
            local wt_part="${clean_line#*/}"
            selected_worktrees+="${lts_part}:${wt_part}"$'\n'
        done <<< "$selected_display"
        selected_worktrees="${selected_worktrees%$'\n'}"

        local selected_count
        selected_count=$(echo "$selected_worktrees" | wc -l | tr -d ' ')
        print_success "Selected $selected_count worktree(s)"
    fi

    if [[ -z "$selected_worktrees" ]]; then
        print_warning "No worktrees selected"
        return 0
    fi

    # Show confirmation summary
    print_subheader "Cleanup Confirmation"
    echo -e "${YELLOW}The following merged worktrees will be cleaned up:${NC}"
    echo ""

    while IFS= read -r entry; do
        [[ -z "$entry" ]] && continue
        local entry_lts="${entry%%:*}"
        local entry_wt="${entry#*:}"
        local wt_path="$SCRIPT_DIR/$entry_lts/$entry_wt"

        local branch_name=""
        if [[ -f "$wt_path/.git" ]]; then
            cd "$wt_path"
            branch_name=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
            cd "$SCRIPT_DIR"
        fi

        if [[ -n "$branch_name" ]]; then
            echo -e "  ${DIM}-${NC} ${CYAN}$entry_wt${NC} ${DIM}(branch: $branch_name)${NC}"
        else
            echo -e "  ${DIM}-${NC} ${CYAN}$entry_wt${NC}"
        fi
    done <<< "$selected_worktrees"

    echo ""
    echo -e "${YELLOW}This will:${NC}"
    echo -e "  ${DIM}- Remove worktree directories and files${NC}"
    echo -e "  ${DIM}- Prompt to delete local/remote branches${NC}"
    echo -e "  ${DIM}- Remove workspace files${NC}"
    echo ""

    if ! confirm "Proceed with cleanup?"; then
        print_warning "Cancelled."
        return 0
    fi

    # Get unique LTS directories from selected worktrees
    local unique_lts_dirs
    unique_lts_dirs=$(echo "$selected_worktrees" | cut -d':' -f1 | sort -u)

    # Track affected repos across all LTS dirs for final pull
    local affected_repos=()

    # Process each LTS directory
    while IFS= read -r lts_dir; do
        [[ -z "$lts_dir" ]] && continue

        print_subheader "Processing: $lts_dir"

        # Get worktrees for this LTS directory
        local worktrees_to_clean=()
        while IFS= read -r entry; do
            if [[ -n "$entry" ]]; then
                local entry_lts="${entry%%:*}"
                local entry_wt="${entry#*:}"
                if [[ "$entry_lts" == "$lts_dir" ]]; then
                    worktrees_to_clean+=("$entry_wt")
                fi
            fi
        done <<< "$selected_worktrees"

        # Detect LTS directory type via metadata
        local lts_type
        lts_type=$(get_lts_type "$lts_dir")
        local repo_name="${lts_dir%-lts}"
        local is_erp_lts=false
        local is_monorepo_lts_flag=false
        local monorepo_repos=()

        if [[ "$lts_type" == "erp" ]]; then
            is_erp_lts=true

            local erp_repos=()
            while IFS= read -r r; do
                [[ -n "$r" ]] && erp_repos+=("$r")
            done <<< "$(get_lts_repos "$lts_dir")"

            for r in "${erp_repos[@]}"; do
                [[ ! " ${affected_repos[*]} " =~ " $r " ]] && affected_repos+=("$r")
            done

            for r in "${erp_repos[@]}"; do
                if [[ -d "$SCRIPT_DIR/$r" ]]; then
                    prune_worktrees "$SCRIPT_DIR/$r"
                    print_step "Pulling main in $r..."
                    cd "$SCRIPT_DIR/$r"
                    local main_branch
                    main_branch=$(get_main_branch "$SCRIPT_DIR/$r")
                    git checkout "$main_branch" 2>/dev/null || true
                    git pull origin "$main_branch" 2>/dev/null || true
                    cd "$SCRIPT_DIR"
                fi
            done
        elif [[ "$lts_type" == "monorepo" ]]; then
            is_monorepo_lts_flag=true
            while IFS= read -r mr; do
                [[ -n "$mr" ]] && monorepo_repos+=("$mr")
            done <<< "$(get_monorepo_repos "$lts_dir")"

            for mr in "${monorepo_repos[@]}"; do
                [[ ! " ${affected_repos[*]} " =~ " $mr " ]] && affected_repos+=("$mr")
            done

            for mr in "${monorepo_repos[@]}"; do
                if [[ -d "$SCRIPT_DIR/$mr" ]]; then
                    prune_worktrees "$SCRIPT_DIR/$mr"
                    print_step "Pulling main in $mr..."
                    cd "$SCRIPT_DIR/$mr"
                    local main_branch
                    main_branch=$(get_main_branch "$SCRIPT_DIR/$mr")
                    git checkout "$main_branch" 2>/dev/null || true
                    git pull origin "$main_branch" 2>/dev/null || true
                    cd "$SCRIPT_DIR"
                fi
            done
        else
            local repo_path="$SCRIPT_DIR/$repo_name"

            [[ ! " ${affected_repos[*]} " =~ " $repo_name " ]] && affected_repos+=("$repo_name")

            prune_worktrees "$repo_path"

            if [[ ! -d "$repo_path" ]]; then
                print_error "Main repository $repo_name not found"
                continue
            fi

            print_step "Pulling main branch in $repo_name..."
            cd "$repo_path"
            local main_branch
            main_branch=$(get_main_branch "$repo_path")
            git checkout "$main_branch" 2>/dev/null || true
            git pull origin "$main_branch" 2>/dev/null || true
            cd "$SCRIPT_DIR"
        fi

        # Process each worktree
        for wt in "${worktrees_to_clean[@]}"; do
            echo ""
            print_step "Cleaning up: $wt"

            local wt_path="$SCRIPT_DIR/$lts_dir/$wt"
            local wt_base
            wt_base=$(basename "$wt")

            # Determine the correct repo for this worktree
            local wt_repo_path=""

            if [[ "$is_erp_lts" == true ]]; then
                if [[ "$wt_base" == gorocky-erp-* ]]; then
                    wt_repo_path="$SCRIPT_DIR/gorocky-erp"
                elif [[ "$wt_base" == erp-ui-* ]]; then
                    wt_repo_path="$SCRIPT_DIR/erp-ui"
                else
                    print_warning "Cannot determine repo for worktree: $wt"
                    wt_repo_path=""
                fi
            elif [[ "$is_monorepo_lts_flag" == true ]]; then
                local matched_repo
                matched_repo=$(get_repo_for_worktree "$wt_base" "${monorepo_repos[@]}")
                if [[ -n "$matched_repo" ]]; then
                    wt_repo_path="$SCRIPT_DIR/$matched_repo"
                else
                    print_warning "Cannot determine repo for worktree: $wt"
                    wt_repo_path=""
                fi
            else
                wt_repo_path="$SCRIPT_DIR/$repo_name"
            fi

            # Get the branch name for this worktree
            local branch_name=""
            if [[ -f "$wt_path/.git" ]]; then
                cd "$wt_path"
                branch_name=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
                cd "$SCRIPT_DIR"
            fi

            # Remove worktree
            if [[ -n "$wt_repo_path" && -d "$wt_repo_path" ]]; then
                cd "$wt_repo_path"
                if git worktree remove "$wt_path" --force 2>/dev/null; then
                    print_success "Removed worktree: $wt"
                else
                    print_warning "Could not remove worktree cleanly, forcing..."
                    rm -rf "$wt_path" 2>/dev/null || true
                    git worktree prune 2>/dev/null || true
                    print_success "Force removed worktree: $wt"
                fi
                cd "$SCRIPT_DIR"
            else
                print_warning "Main repo not found, removing directory directly..."
                rm -rf "$wt_path" 2>/dev/null || true
                print_success "Removed worktree directory: $wt"
            fi

            # Delete local branch
            if [[ -n "$branch_name" && "$branch_name" != "HEAD" && -n "$wt_repo_path" && -d "$wt_repo_path" ]]; then
                cd "$wt_repo_path"

                if [[ "$branch_name" == "main" || "$branch_name" == "master" ]]; then
                    print_error "CRITICAL: Cannot delete '$branch_name' branch"
                    print_info "Skipping branch deletion - worktree was removed but branch preserved"
                    cd "$SCRIPT_DIR"
                    local workspace_file="$(dirname "$SCRIPT_DIR/$lts_dir/$wt")/${wt_base}.code-workspace"
                    if [[ -f "$workspace_file" ]]; then
                        rm "$workspace_file"
                        print_success "Removed workspace file: ${wt}.code-workspace"
                    fi
                    continue
                fi

                if is_protected_branch "$branch_name"; then
                    print_error "PROTECTED BRANCH: $branch_name"
                    print_warning "This is a protected branch and should not be deleted"

                    if ! confirm_no "Are you ABSOLUTELY SURE you want to delete '$branch_name'?"; then
                        print_info "Skipping deletion of protected branch: $branch_name"
                        cd "$SCRIPT_DIR"
                        local workspace_file="$(dirname "$SCRIPT_DIR/$lts_dir/$wt")/${wt_base}.code-workspace"
                        if [[ -f "$workspace_file" ]]; then
                            rm "$workspace_file"
                            print_success "Removed workspace file: ${wt}.code-workspace"
                        fi
                        continue
                    fi
                    print_warning "Proceeding with protected branch deletion..."
                fi

                # Check for unpushed commits
                local unpushed_info
                unpushed_info=$(has_unpushed_commits "$branch_name")
                if [[ $? -eq 0 ]]; then
                    print_warning "Branch $branch_name has $unpushed_info"
                    if ! confirm "Delete branch anyway? (commits will be LOST)"; then
                        print_info "Skipping branch deletion for: $branch_name"
                        cd "$SCRIPT_DIR"
                        local workspace_file="$(dirname "$SCRIPT_DIR/$lts_dir/$wt")/${wt_base}.code-workspace"
                        if [[ -f "$workspace_file" ]]; then
                            rm "$workspace_file"
                            print_success "Removed workspace file: ${wt}.code-workspace"
                        fi
                        continue
                    fi
                fi

                # Prompt and execute branch deletion
                prompt_branch_deletion "$branch_name"
                if [[ "$BRANCH_DELETE_ACTION" == "skip" ]]; then
                    print_info "Kept branch: $branch_name"
                else
                    execute_branch_deletion "$branch_name"
                fi

                cd "$SCRIPT_DIR"
            fi

            # Remove workspace file
            local workspace_file="$(dirname "$SCRIPT_DIR/$lts_dir/$wt")/${wt_base}.code-workspace"
            if [[ -f "$workspace_file" ]]; then
                rm "$workspace_file"
                print_success "Removed workspace file: ${wt}.code-workspace"
            fi

            # Determine the subdirectory containing this worktree (for nested paths)
            local wt_subdir_path
            wt_subdir_path=$(dirname "$SCRIPT_DIR/$lts_dir/$wt")

            # Handle ERP orphaned workspaces
            if [[ "$is_erp_lts" == true ]]; then
                local wt_suffix=""
                if [[ "$wt_base" == gorocky-erp-* ]]; then
                    wt_suffix="${wt_base#gorocky-erp-}"
                elif [[ "$wt_base" == erp-ui-* ]]; then
                    wt_suffix="${wt_base#erp-ui-}"
                fi

                if [[ -n "$wt_suffix" ]]; then
                    local backend_exists=false
                    local frontend_exists=false

                    for check_dir in "$wt_subdir_path"/gorocky-erp-"$wt_suffix"*/; do
                        if [[ -d "$check_dir" ]] && [[ -f "$check_dir/.git" ]]; then
                            backend_exists=true
                            break
                        fi
                    done

                    for check_dir in "$wt_subdir_path"/erp-ui-"$wt_suffix"*/; do
                        if [[ -d "$check_dir" ]] && [[ -f "$check_dir/.git" ]]; then
                            frontend_exists=true
                            break
                        fi
                    done

                    if [[ "$backend_exists" == false ]] || [[ "$frontend_exists" == false ]]; then
                        local erp_workspace="$wt_subdir_path/erp-${wt_suffix}.code-workspace"
                        if [[ -f "$erp_workspace" ]]; then
                            rm "$erp_workspace"
                            print_success "Removed orphaned ERP workspace: erp-${wt_suffix}.code-workspace"
                        fi
                    fi

                    # Clean up empty subdirectory
                    if [[ "$wt_subdir_path" != "$SCRIPT_DIR/$lts_dir" ]] && [[ -d "$wt_subdir_path" ]]; then
                        local remaining_in_subdir
                        remaining_in_subdir=$(find "$wt_subdir_path" -maxdepth 1 -mindepth 1 -type d 2>/dev/null | head -1)
                        if [[ -z "$remaining_in_subdir" ]]; then
                            rm -rf "$wt_subdir_path"
                            print_info "Removed empty subdirectory: $(basename "$wt_subdir_path")"
                        fi
                    fi
                fi
            fi

            # Handle monorepo orphaned workspaces
            if [[ "$is_monorepo_lts_flag" == true ]]; then
                local matched_repo_for_suffix
                matched_repo_for_suffix=$(get_repo_for_worktree "$wt_base" "${monorepo_repos[@]}")
                if [[ -n "$matched_repo_for_suffix" ]]; then
                    local wt_suffix="${wt_base#${matched_repo_for_suffix}-}"
                    if [[ -n "$wt_suffix" ]]; then
                        local all_present=true
                        for check_repo in "${monorepo_repos[@]}"; do
                            local found=false
                            for check_dir in "$wt_subdir_path"/"${check_repo}-${wt_suffix}"*/; do
                                if [[ -d "$check_dir" ]] && [[ -f "$check_dir/.git" ]]; then
                                    found=true
                                    break
                                fi
                            done
                            if [[ "$found" == false ]]; then
                                all_present=false
                                break
                            fi
                        done

                        if [[ "$all_present" == false ]]; then
                            local mono_workspace="$wt_subdir_path/monorepo-${wt_suffix}.code-workspace"
                            if [[ -f "$mono_workspace" ]]; then
                                rm "$mono_workspace"
                                print_success "Removed orphaned monorepo workspace: monorepo-${wt_suffix}.code-workspace"
                            fi
                        fi

                        # Clean up empty subdirectory
                        if [[ "$wt_subdir_path" != "$SCRIPT_DIR/$lts_dir" ]] && [[ -d "$wt_subdir_path" ]]; then
                            local remaining_in_subdir
                            remaining_in_subdir=$(find "$wt_subdir_path" -maxdepth 1 -mindepth 1 -type d 2>/dev/null | head -1)
                            if [[ -z "$remaining_in_subdir" ]]; then
                                rm -rf "$wt_subdir_path"
                                print_info "Removed empty subdirectory: $(basename "$wt_subdir_path")"
                            fi
                        fi
                    fi
                fi
            fi
        done

        # Check remaining worktrees and handle combined workspace
        local remaining_worktrees
        remaining_worktrees=$(get_worktrees_in_lts "$lts_dir")

        if [[ -z "$remaining_worktrees" ]]; then
            if [[ "$is_erp_lts" == true ]]; then
                rm -f "$SCRIPT_DIR/$lts_dir"/erp-*.code-workspace 2>/dev/null || true
            elif [[ "$is_monorepo_lts_flag" == true ]]; then
                rm -f "$SCRIPT_DIR/$lts_dir"/monorepo-*.code-workspace 2>/dev/null || true
            else
                local combined_ws="$SCRIPT_DIR/$lts_dir/${repo_name}-combined.code-workspace"
                [[ -f "$combined_ws" ]] && rm "$combined_ws"
            fi

            rm -rf "$SCRIPT_DIR/$lts_dir"
            print_success "Removed empty $lts_dir directory"
        else
            if [[ "$is_erp_lts" == true ]]; then
                print_info "Remaining ERP worktrees in $lts_dir:"
                echo "$remaining_worktrees" | while read -r wt; do
                    echo -e "  ${CYAN}$wt${NC}"
                done
            elif [[ "$is_monorepo_lts_flag" == true ]]; then
                print_info "Remaining monorepo worktrees in $lts_dir:"
                echo "$remaining_worktrees" | while read -r wt; do
                    echo -e "  ${CYAN}$wt${NC}"
                done
            else
                local remaining_array=()
                while IFS= read -r wt; do
                    [[ -n "$wt" ]] && remaining_array+=("$wt")
                done <<< "$remaining_worktrees"

                if [[ ${#remaining_array[@]} -gt 1 ]]; then
                    print_step "Regenerating combined workspace with ${#remaining_array[@]} remaining worktrees..."
                    generate_combined_workspace "$lts_dir" "$repo_name" "${remaining_array[@]}"
                elif [[ ${#remaining_array[@]} -eq 1 ]]; then
                    local combined_ws="$SCRIPT_DIR/$lts_dir/${repo_name}-combined.code-workspace"
                    if [[ -f "$combined_ws" ]]; then
                        rm "$combined_ws"
                        print_info "Removed combined workspace (only 1 worktree remains)"
                    fi
                fi
            fi
        fi
    done <<< "$unique_lts_dirs"

    # Final pull from main for all affected repos
    print_subheader "Finalizing"

    for repo in "${affected_repos[@]}"; do
        local repo_path="$SCRIPT_DIR/$repo"
        if [[ -d "$repo_path" ]]; then
            print_step "Pulling latest main in $repo..."
            cd "$repo_path"
            local main_branch
            main_branch=$(get_main_branch "$repo_path")
            git checkout "$main_branch" 2>/dev/null || true
            git pull origin "$main_branch" 2>/dev/null || true
            cd "$SCRIPT_DIR"
        fi
    done

    print_subheader "Cleanup Complete"
    print_success "Merged cleanables cleanup finished!"
}

# ============================================================================
#  MODE 3: UPDATE WORKTREE/S
# ============================================================================

mode_update_worktrees() {
    print_header "Update Worktree/s (Rebase from Main)"

    # Get list of -lts directories
    local lts_dirs
    lts_dirs=$(get_lts_dirs)

    if [[ -z "$lts_dirs" ]]; then
        print_error "No LTS worktree directories found"
        return 1
    fi

    # Count LTS directories
    local lts_count
    lts_count=$(echo "$lts_dirs" | wc -l | tr -d ' ')

    local lts_list=()

    # Step 1: Select LTS directories (always show selector for Go Back option)
    print_subheader "Step 1: Select LTS Directory"

    if [[ "$lts_count" -eq 1 ]]; then
        print_info "Only one LTS directory available"
    fi

    # Build options - include "All" only if multiple directories
    local lts_options="← Go Back"
    if [[ "$lts_count" -gt 1 ]]; then
        lts_options+=$'\n'"All LTS directories"
    fi
    lts_options+=$'\n'"$lts_dirs"

    echo -e "${DIM}(press esc to cancel, TAB to select multiple)${NC}"
    local selected_lts
    selected_lts=$(echo "$lts_options" | fzf --height=15 --reverse --multi --prompt="Select LTS directory(s) to update: ")

    if [[ -z "$selected_lts" ]]; then
        print_warning "Cancelled."
        return 1
    fi

    # Check if Go Back was selected
    if echo "$selected_lts" | grep -q "← Go Back"; then
        return 0
    fi

    # Check if All was selected
    if echo "$selected_lts" | grep -q "All LTS directories"; then
        while IFS= read -r dir; do
            [[ -n "$dir" ]] && lts_list+=("$dir")
        done <<< "$lts_dirs"
        print_info "Selected all LTS directories"
    else
        # Add selected LTS directories (filter out special options)
        while IFS= read -r dir; do
            # Skip empty, Go Back, and All options
            [[ -z "$dir" ]] && continue
            [[ "$dir" == "← Go Back" ]] && continue
            [[ "$dir" == "All LTS directories" ]] && continue
            lts_list+=("$dir")
        done <<< "$selected_lts"
        print_success "Selected ${#lts_list[@]} LTS directory(s)"
    fi

    # Step 2: Gather all worktrees from selected LTS directories
    print_subheader "Step 2: Select Worktrees to Update"

    # Build combined list: "lts_dir/worktree_name"
    local all_worktrees_display=""
    local all_worktrees_raw=""  # Format: lts_dir:worktree_name

    for lts_dir in "${lts_list[@]}"; do
        local worktrees
        worktrees=$(get_worktrees_in_lts "$lts_dir")

        if [[ -z "$worktrees" ]]; then
            continue
        fi

        while IFS= read -r wt; do
            if [[ -n "$wt" ]]; then
                all_worktrees_display+="${lts_dir}/${wt}"$'\n'
                all_worktrees_raw+="${lts_dir}:${wt}"$'\n'
            fi
        done <<< "$worktrees"
    done

    # Remove trailing newlines
    all_worktrees_display="${all_worktrees_display%$'\n'}"
    all_worktrees_raw="${all_worktrees_raw%$'\n'}"

    if [[ -z "$all_worktrees_display" ]]; then
        print_warning "No worktrees found in selected LTS directories"
        return 0
    fi

    # Count total worktrees
    local total_wt_count
    total_wt_count=$(echo "$all_worktrees_raw" | wc -l | tr -d ' ')

    # Show all worktrees
    echo -e "Found $total_wt_count worktree(s):"
    echo "$all_worktrees_display" | while read -r wt; do
        echo -e "  - ${CYAN}$wt${NC}"
    done
    echo ""

    # Ask: All or Select specific
    local scope_options="← Go Back
All worktrees ($total_wt_count)
Select specific"

    echo -e "${DIM}(press esc to cancel)${NC}"
    local scope
    scope=$(echo "$scope_options" | fzf --height=6 --reverse --prompt="Update: ")

    if [[ -z "$scope" ]]; then
        print_warning "Cancelled."
        return 1
    fi

    if [[ "$scope" == "← Go Back" ]]; then
        return 0
    fi

    # Build the final list of worktrees to update (format: lts_dir:worktree_name)
    local selected_worktrees=""

    if [[ "$scope" == "All worktrees ($total_wt_count)" ]]; then
        selected_worktrees="$all_worktrees_raw"
        print_info "Selected all $total_wt_count worktree(s)"
    else
        # Multi-select from all worktrees
        local wt_options="← Go Back
$all_worktrees_display"
        echo -e "${DIM}(press esc to cancel, TAB to select multiple, ENTER to confirm)${NC}"
        local selected_display
        selected_display=$(echo "$wt_options" | fzf --height=20 --reverse --multi --prompt="Select worktree(s) to update: ")

        if [[ -z "$selected_display" ]]; then
            print_warning "Cancelled."
            return 1
        fi

        # Check if Go Back was selected
        if echo "$selected_display" | grep -q "← Go Back"; then
            return 0
        fi

        # Parse selected items back to lts_dir:worktree format
        while IFS= read -r line; do
            # Skip empty lines and Go Back option
            [[ -z "$line" ]] && continue
            [[ "$line" == "← Go Back" ]] && continue

            # Format is: lts_dir/worktree_name
            local lts_part="${line%%/*}"
            local wt_part="${line#*/}"
            selected_worktrees+="${lts_part}:${wt_part}"$'\n'
        done <<< "$selected_display"
        selected_worktrees="${selected_worktrees%$'\n'}"

        local selected_count
        selected_count=$(echo "$selected_worktrees" | wc -l | tr -d ' ')
        print_success "Selected $selected_count worktree(s)"
    fi

    if [[ -z "$selected_worktrees" ]]; then
        print_warning "No worktrees selected"
        return 0
    fi

    # Get unique LTS directories from selected worktrees
    local unique_lts_dirs
    unique_lts_dirs=$(echo "$selected_worktrees" | cut -d':' -f1 | sort -u)

    # Process each LTS directory
    while IFS= read -r lts_dir; do
        [[ -z "$lts_dir" ]] && continue

        print_subheader "Updating: $lts_dir"

        # Get worktrees for this LTS directory
        local worktrees_to_update=""
        while IFS= read -r entry; do
            if [[ -n "$entry" ]]; then
                local entry_lts="${entry%%:*}"
                local entry_wt="${entry#*:}"
                if [[ "$entry_lts" == "$lts_dir" ]]; then
                    if [[ -z "$worktrees_to_update" ]]; then
                        worktrees_to_update="$entry_wt"
                    else
                        worktrees_to_update+=$'\n'"$entry_wt"
                    fi
                fi
            fi
        done <<< "$selected_worktrees"

        # Get the main repo - special handling for erp-lts
        # Detect LTS directory type via metadata
        local lts_type
        lts_type=$(get_lts_type "$lts_dir")
        local repo_name="${lts_dir%-lts}"
        local is_erp_lts=false
        local is_monorepo_lts_flag=false
        local monorepo_repos=()

        if [[ "$lts_type" == "erp" ]]; then
            is_erp_lts=true
            print_info "ERP LTS directory detected"

            local erp_repos=()
            while IFS= read -r r; do
                [[ -n "$r" ]] && erp_repos+=("$r")
            done <<< "$(get_lts_repos "$lts_dir")"

            # Check for ongoing operations
            local skip_lts=false
            for r in "${erp_repos[@]}"; do
                if [[ -d "$SCRIPT_DIR/$r" ]] && check_ongoing_operations "$SCRIPT_DIR/$r"; then
                    skip_lts=true
                    break
                fi
            done
            if [[ "$skip_lts" == true ]]; then
                continue
            fi

            # Prune, fetch, and update each ERP repo
            for r in "${erp_repos[@]}"; do
                if [[ -d "$SCRIPT_DIR/$r" ]]; then
                    prune_worktrees "$SCRIPT_DIR/$r"
                    print_step "Fetching and updating $r..."
                    cd "$SCRIPT_DIR/$r"
                    git fetch origin 2>/dev/null || true
                    local repo_main=$(get_main_branch "$SCRIPT_DIR/$r")
                    local current=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
                    if [[ "$current" == "$repo_main" ]]; then
                        git pull origin "$repo_main" 2>/dev/null || true
                    fi
                    cd "$SCRIPT_DIR"
                fi
            done
        elif [[ "$lts_type" == "monorepo" ]]; then
            is_monorepo_lts_flag=true
            while IFS= read -r mr; do
                [[ -n "$mr" ]] && monorepo_repos+=("$mr")
            done <<< "$(get_monorepo_repos "$lts_dir")"
            print_info "Monorepo LTS directory detected - repos: ${monorepo_repos[*]}"

            # Check for ongoing operations, prune, fetch, and update each repo
            local skip_lts=false
            for mr in "${monorepo_repos[@]}"; do
                if [[ -d "$SCRIPT_DIR/$mr" ]]; then
                    if check_ongoing_operations "$SCRIPT_DIR/$mr"; then
                        skip_lts=true
                        break
                    fi
                fi
            done
            if [[ "$skip_lts" == true ]]; then
                continue
            fi

            for mr in "${monorepo_repos[@]}"; do
                if [[ -d "$SCRIPT_DIR/$mr" ]]; then
                    prune_worktrees "$SCRIPT_DIR/$mr"
                    print_step "Fetching and updating $mr..."
                    cd "$SCRIPT_DIR/$mr"
                    git fetch origin 2>/dev/null || true
                    local mr_main
                    mr_main=$(get_main_branch "$SCRIPT_DIR/$mr")
                    local current
                    current=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
                    if [[ "$current" == "$mr_main" ]]; then
                        git pull origin "$mr_main" 2>/dev/null || true
                    fi
                    cd "$SCRIPT_DIR"
                fi
            done
        else
            local repo_path="$SCRIPT_DIR/$repo_name"

            if [[ ! -d "$repo_path" ]]; then
                print_error "Main repository $repo_name not found"
                continue
            fi

            # Check for ongoing operations
            if check_ongoing_operations "$repo_path"; then
                continue
            fi

            # Prune orphaned worktree entries
            prune_worktrees "$repo_path"

            # Update main branch (for non-ERP repos)
            print_step "Fetching and updating main branch..."
            local main_branch
            main_branch=$(get_main_branch "$repo_path")

            # Check if main branch was found
            if [[ -z "$main_branch" ]]; then
                print_error "Could not determine main branch for $repo_name"
                print_info "Skipping update for $lts_dir"
                continue
            fi

            cd "$repo_path"
            git fetch origin 2>/dev/null || true

            # Try to update main if we're on it
            local current_branch
            current_branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")

            if [[ "$current_branch" == "$main_branch" ]]; then
                git pull origin "$main_branch" 2>/dev/null || true
            fi

            cd "$SCRIPT_DIR"
        fi

        # Rebase each worktree
        while IFS= read -r wt; do
            [[ -z "$wt" ]] && continue

            echo ""
            print_step "Rebasing: $wt"

            local wt_path="$SCRIPT_DIR/$lts_dir/$wt"
            local wt_base
            wt_base=$(basename "$wt")

            if [[ ! -d "$wt_path" ]]; then
                print_warning "Worktree path not found: $wt_path"
                continue
            fi

            # For ERP/monorepo worktrees, determine correct repo and main branch
            local wt_main_branch=""
            if [[ "$is_erp_lts" == true ]]; then
                if [[ "$wt_base" == gorocky-erp-* ]]; then
                    wt_main_branch=$(get_main_branch "$SCRIPT_DIR/gorocky-erp" "true")
                elif [[ "$wt_base" == erp-ui-* ]]; then
                    wt_main_branch=$(get_main_branch "$SCRIPT_DIR/erp-ui" "true")
                else
                    print_warning "Cannot determine repo for worktree: $wt, skipping"
                    continue
                fi
            elif [[ "$is_monorepo_lts_flag" == true ]]; then
                local matched_repo
                matched_repo=$(get_repo_for_worktree "$wt_base" "${monorepo_repos[@]}")
                if [[ -n "$matched_repo" ]]; then
                    wt_main_branch=$(get_main_branch "$SCRIPT_DIR/$matched_repo" "true")
                else
                    print_warning "Cannot determine repo for worktree: $wt, skipping"
                    continue
                fi
            else
                wt_main_branch="$main_branch"
            fi

            # Safety check for empty main branch
            if [[ -z "$wt_main_branch" ]]; then
                print_error "Could not determine main branch for rebasing $wt"
                print_warning "Skipping this worktree"
                continue
            fi

            # Check for ongoing operations in this worktree
            if check_ongoing_operations "$wt_path"; then
                print_warning "Worktree has an ongoing git operation"
                if confirm "Would you like to abort it and continue?"; then
                    cd "$wt_path"
                    git rebase --abort 2>/dev/null || true
                    git merge --abort 2>/dev/null || true
                    git cherry-pick --abort 2>/dev/null || true
                    git revert --abort 2>/dev/null || true
                    cd "$SCRIPT_DIR"
                    print_info "Attempted to abort ongoing operation"
                else
                    print_warning "Skipping $wt"
                    continue
                fi
            fi

            cd "$wt_path"

            # Check for uncommitted changes
            local stashed=false
            if ! git diff --quiet HEAD 2>/dev/null || ! git diff --cached --quiet 2>/dev/null; then
                print_warning "Uncommitted changes detected"
                local stash_output
                stash_output=$(git stash push -m "LTS auto-stash before rebase $(date '+%Y-%m-%d %H:%M:%S')" 2>&1)
                if [[ $? -eq 0 ]] && ! echo "$stash_output" | grep -q "No local changes"; then
                    stashed=true
                    print_info "Changes stashed"
                else
                    print_warning "Stash may have failed, proceeding anyway"
                fi
            fi

            # Check for detached HEAD
            local wt_branch
            wt_branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")

            if [[ "$wt_branch" == "HEAD" ]]; then
                print_warning "Worktree is in detached HEAD state, skipping"
                cd "$SCRIPT_DIR"
                continue
            fi

            # Fetch and rebase
            git fetch origin 2>/dev/null || true

            local rebase_success=false
            if git rebase "origin/$wt_main_branch" 2>/dev/null; then
                print_success "Rebased $wt onto origin/$wt_main_branch"
                rebase_success=true
            else
                print_error "Rebase conflict in $wt"
                echo ""
                echo -e "${YELLOW}Conflicting files:${NC}"
                git diff --name-only --diff-filter=U 2>/dev/null || true
                echo ""

                if confirm "Abort rebase and skip this worktree?"; then
                    git rebase --abort 2>/dev/null || true
                    print_info "Rebase aborted"
                    rebase_success=true  # Aborted cleanly, safe to pop stash
                else
                    print_info "Please resolve conflicts manually in: $wt_path"
                    print_info "Then run: git rebase --continue"
                    if [[ "$stashed" == true ]]; then
                        print_warning "Your changes are stashed. After resolving conflicts, run 'git stash pop'"
                    fi
                fi
            fi

            # Pop stash only if rebase succeeded or was cleanly aborted
            # Don't pop during unresolved conflict - would make things worse
            if [[ "$stashed" == true && "$rebase_success" == true ]]; then
                if git stash pop 2>/dev/null; then
                    print_info "Restored stashed changes"
                else
                    print_warning "Could not restore stashed changes (may have conflicts)"
                    print_info "Your changes are still in stash. Run 'git stash pop' manually."
                fi
            fi

            cd "$SCRIPT_DIR"
        done <<< "$worktrees_to_update"
    done <<< "$unique_lts_dirs"

    print_subheader "Update Complete"
    print_success "Worktree update finished!"
}

# ============================================================================
#  MODE 4: CREATE ERP WORKTREE/S (SPECIAL)
# ============================================================================

mode_create_erp_worktrees() {
    print_header "Create ERP Worktree/s (Special)"

    # Check if both repos exist
    local erp_backend="$SCRIPT_DIR/gorocky-erp"
    local erp_frontend="$SCRIPT_DIR/erp-ui"

    if [[ ! -d "$erp_backend" ]]; then
        print_error "gorocky-erp repository not found"
        return 1
    fi

    if [[ ! -d "$erp_frontend" ]]; then
        print_error "erp-ui repository not found"
        return 1
    fi

    print_success "Found gorocky-erp and erp-ui repositories"

    # Select number of worktrees
    print_subheader "Step 1: How Many Worktrees?"
    echo -e "${DIM}Hanggang tatlo lang baka mabaliw ka hehe${NC}"
    echo -e "${DIM}(Each worktree will be created for BOTH repos)${NC}"
    echo -e "${DIM}(press esc to cancel)${NC}"
    echo ""

    local count_options="← Go Back
1
2
3"
    local worktree_count
    worktree_count=$(echo "$count_options" | fzf --height=7 --reverse --prompt="Number of worktrees: ")

    if [[ -z "$worktree_count" ]]; then
        print_warning "Cancelled."
        return 1
    fi

    if [[ "$worktree_count" == "← Go Back" ]]; then
        return 0
    fi

    print_success "Creating $worktree_count worktree pair(s)"

    # Get branch names
    print_subheader "Step 2: Enter Branch Names"
    echo -e "${DIM}You'll be prompted for each branch name one at a time.${NC}"
    echo -e "${DIM}The same branch will be created in BOTH gorocky-erp and erp-ui repos.${NC}"
    echo ""
    echo -e "${DIM}Format:    <type>/<description>  (e.g., fix/order-bug)${NC}"
    echo -e "${DIM}Examples:  fix/order-bug   feature/new-report   hotfix/api-timeout${NC}"
    echo ""
    echo -e "${DIM}The part after '/' becomes the folder suffix:${NC}"
    echo -e "${DIM}  fix/order-bug  →  gorocky-erp-order-bug + erp-ui-order-bug${NC}"
    echo ""
    echo -e "${DIM}(leave empty to cancel)${NC}"
    echo ""
    show_existing_branches "$SCRIPT_DIR/gorocky-erp"
    show_existing_branches "$SCRIPT_DIR/erp-ui"
    local branch_names=()

    for ((i=1; i<=worktree_count; i++)); do
        local branch_name
        branch_name=$(get_input "Branch name for worktree pair $i (or empty to cancel)")

        # Allow empty input to cancel
        if [[ -z "$branch_name" || "$branch_name" =~ ^[[:space:]]*$ ]]; then
            print_warning "Cancelled."
            return 0
        fi

        if ! validate_branch_name "$branch_name"; then
            return 1
        fi

        branch_names+=("$branch_name")
    done

    # Check for duplicate suffixes in user input
    if check_duplicate_suffixes "${branch_names[@]}"; then
        print_error "Please use unique branch suffixes (the part after the last /)"
        return 1
    fi

    # Confirm
    print_subheader "Step 3: Confirmation"
    echo -e "Repositories: ${BOLD}gorocky-erp${NC} + ${BOLD}erp-ui${NC}"
    echo -e "Worktrees to create:"
    local lts_dir="erp-lts"
    local lts_path="$SCRIPT_DIR/$lts_dir"

    for branch in "${branch_names[@]}"; do
        local suffix=$(extract_suffix "$branch")
        local branch_subdir="erp-${suffix}"
        local branch_subdir_path="$lts_path/$branch_subdir"
        local backend_name=$(generate_unique_worktree_name "gorocky-erp" "$suffix" "$branch_subdir_path")
        local frontend_name=$(generate_unique_worktree_name "erp-ui" "$suffix" "$branch_subdir_path")
        echo -e "  - Branch: ${CYAN}$branch${NC} → ${DIM}$lts_dir/$branch_subdir/${NC}"
        if [[ "$backend_name" != "gorocky-erp-${suffix}" ]]; then
            echo -e "    Backend: ${YELLOW}${backend_name}${NC} (renamed)"
        else
            echo -e "    Backend: ${CYAN}${backend_name}${NC}"
        fi
        if [[ "$frontend_name" != "erp-ui-${suffix}" ]]; then
            echo -e "    Frontend: ${YELLOW}${frontend_name}${NC} (renamed)"
        else
            echo -e "    Frontend: ${CYAN}${frontend_name}${NC}"
        fi
    done
    echo ""

    if ! confirm "Proceed with creation?"; then
        print_warning "Cancelled."
        return 1
    fi

    # Execute
    print_subheader "Step 4: Creating Worktrees"

    # Check for ongoing operations in both repos
    if check_ongoing_operations "$erp_backend"; then
        return 1
    fi
    if check_ongoing_operations "$erp_frontend"; then
        return 1
    fi

    # Prune orphaned worktree entries in both repos
    prune_worktrees "$erp_backend"
    prune_worktrees "$erp_frontend"

    # Ensure clean main for both repos
    print_step "Preparing gorocky-erp..."
    if ! ensure_clean_main "$erp_backend"; then
        print_error "Failed to prepare gorocky-erp"
        return 1
    fi

    print_step "Preparing erp-ui..."
    if ! ensure_clean_main "$erp_frontend"; then
        print_error "Failed to prepare erp-ui"
        return 1
    fi

    # After ensure_clean_main succeeds, repos are on their base branches
    # Get them directly rather than calling get_main_branch again (which might not find custom branches)
    cd "$erp_backend"
    local backend_main
    backend_main=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
    # Fetch all remote refs so we can detect remote-only branches
    print_step "Fetching remote branches for gorocky-erp..."
    git fetch origin 2>/dev/null || true
    cd "$erp_frontend"
    local frontend_main
    frontend_main=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
    print_step "Fetching remote branches for erp-ui..."
    git fetch origin 2>/dev/null || true
    cd "$SCRIPT_DIR"

    # Safety check for main branches
    if [[ -z "$backend_main" ]]; then
        print_error "Could not determine base branch for gorocky-erp"
        return 1
    fi
    if [[ -z "$frontend_main" ]]; then
        print_error "Could not determine base branch for erp-ui"
        return 1
    fi

    # Create shared erp-lts directory with metadata
    if [[ ! -d "$lts_path" ]]; then
        mkdir -p "$lts_path"
        echo "erp" > "$lts_path/.lts-type"
        printf '%s\n' "gorocky-erp" "erp-ui" > "$lts_path/.lts-repos"
        print_success "Created $lts_dir directory"
    fi

    # Track created subdirectories for summary
    local created_subdirs=()

    # Create worktrees for each branch
    for branch in "${branch_names[@]}"; do
        local suffix=$(extract_suffix "$branch")
        local branch_subdir="erp-${suffix}"
        local branch_subdir_path="$lts_path/$branch_subdir"

        # Create per-branch subdirectory
        mkdir -p "$branch_subdir_path"
        created_subdirs+=("$branch_subdir")

        echo ""
        print_step "Creating worktree pair for branch: $branch"

        # Backend worktree - use unique name generation
        local backend_wt_name=$(generate_unique_worktree_name "gorocky-erp" "$suffix" "$branch_subdir_path")
        local backend_wt_path="$branch_subdir_path/$backend_wt_name"
        local backend_existed=false

        # Inform if name was auto-adjusted
        if [[ "$backend_wt_name" != "gorocky-erp-${suffix}" ]]; then
            print_info "Backend using unique name: $backend_wt_name"
        fi

        # Check if this specific folder already exists (shouldn't happen with unique generation, but just in case)
        if [[ -d "$backend_wt_path" ]]; then
            print_warning "Backend worktree already exists: $backend_wt_name"
            backend_existed=true
        else
            cd "$erp_backend"

            if git show-ref --verify --quiet "refs/heads/$branch" 2>/dev/null; then
                print_warning "Branch $branch already exists locally in gorocky-erp"

                # Check if already checked out
                local checked_out_at
                checked_out_at=$(git worktree list 2>/dev/null | grep -E "\[$branch\]" | awk '{print $1}' || echo "")

                if [[ -n "$checked_out_at" ]]; then
                    print_error "Branch $branch is already checked out at: $checked_out_at"
                    print_info "Skipping backend worktree for this branch"
                    cd "$SCRIPT_DIR"
                    backend_existed=true
                else
                    local add_output
                    add_output=$(git worktree add "$backend_wt_path" "$branch" 2>&1)
                    if [[ $? -ne 0 ]]; then
                        print_error "Failed to add backend worktree: $add_output"
                        cd "$SCRIPT_DIR"
                        continue
                    fi
                    cd "$SCRIPT_DIR"
                    print_success "Created backend worktree: $backend_wt_name"
                fi
            elif git show-ref --verify --quiet "refs/remotes/origin/$branch" 2>/dev/null; then
                print_warning "Branch $branch exists on remote in gorocky-erp"
                local add_output
                add_output=$(git worktree add --track -b "$branch" "$backend_wt_path" "origin/$branch" 2>&1)
                if [[ $? -ne 0 ]]; then
                    print_error "Failed to add backend worktree: $add_output"
                    cd "$SCRIPT_DIR"
                    continue
                fi
                cd "$SCRIPT_DIR"
                print_success "Created backend worktree: $backend_wt_name"
            else
                local add_output
                add_output=$(git worktree add -b "$branch" "$backend_wt_path" "$backend_main" 2>&1)
                if [[ $? -ne 0 ]]; then
                    print_error "Failed to create backend worktree: $add_output"
                    cd "$SCRIPT_DIR"
                    continue
                fi
                cd "$SCRIPT_DIR"
                print_success "Created backend worktree: $backend_wt_name"
            fi
        fi

        # Frontend worktree - use unique name generation
        local frontend_wt_name=$(generate_unique_worktree_name "erp-ui" "$suffix" "$branch_subdir_path")
        local frontend_wt_path="$branch_subdir_path/$frontend_wt_name"
        local frontend_existed=false

        # Inform if name was auto-adjusted
        if [[ "$frontend_wt_name" != "erp-ui-${suffix}" ]]; then
            print_info "Frontend using unique name: $frontend_wt_name"
        fi

        # Check if this specific folder already exists
        if [[ -d "$frontend_wt_path" ]]; then
            print_warning "Frontend worktree already exists: $frontend_wt_name"
            frontend_existed=true
        else
            cd "$erp_frontend"

            if git show-ref --verify --quiet "refs/heads/$branch" 2>/dev/null; then
                print_warning "Branch $branch already exists locally in erp-ui"

                # Check if already checked out
                local checked_out_at
                checked_out_at=$(git worktree list 2>/dev/null | grep -E "\[$branch\]" | awk '{print $1}' || echo "")

                if [[ -n "$checked_out_at" ]]; then
                    print_error "Branch $branch is already checked out at: $checked_out_at"
                    print_info "Skipping frontend worktree for this branch"
                    cd "$SCRIPT_DIR"
                    frontend_existed=true
                else
                    local add_output
                    add_output=$(git worktree add "$frontend_wt_path" "$branch" 2>&1)
                    if [[ $? -ne 0 ]]; then
                        print_error "Failed to add frontend worktree: $add_output"
                        cd "$SCRIPT_DIR"
                        continue
                    fi
                    cd "$SCRIPT_DIR"
                    print_success "Created frontend worktree: $frontend_wt_name"
                fi
            elif git show-ref --verify --quiet "refs/remotes/origin/$branch" 2>/dev/null; then
                print_warning "Branch $branch exists on remote in erp-ui"
                local add_output
                add_output=$(git worktree add --track -b "$branch" "$frontend_wt_path" "origin/$branch" 2>&1)
                if [[ $? -ne 0 ]]; then
                    print_error "Failed to add frontend worktree: $add_output"
                    cd "$SCRIPT_DIR"
                    continue
                fi
                cd "$SCRIPT_DIR"
                print_success "Created frontend worktree: $frontend_wt_name"
            else
                local add_output
                add_output=$(git worktree add -b "$branch" "$frontend_wt_path" "$frontend_main" 2>&1)
                if [[ $? -ne 0 ]]; then
                    print_error "Failed to create frontend worktree: $add_output"
                    cd "$SCRIPT_DIR"
                    continue
                fi
                cd "$SCRIPT_DIR"
                print_success "Created frontend worktree: $frontend_wt_name"
            fi
        fi

        # If both already existed, skip the rest
        if [[ "$backend_existed" == true && "$frontend_existed" == true ]]; then
            print_info "Worktree pair already exists for branch: $branch"
            continue
        fi

        # Check if we have a partial state (one exists, one doesn't)
        local backend_ready=false
        local frontend_ready=false

        # Only process backend if the directory exists
        if [[ -d "$backend_wt_path" ]]; then
            backend_ready=true
            if [[ "$backend_existed" != true ]]; then
                # Newly created - copy .env and install
                copy_env_files "$erp_backend" "$backend_wt_path"
                run_package_install "$backend_wt_path"
            fi
        else
            print_warning "Backend worktree not created: $backend_wt_name"
        fi

        # Only process frontend if the directory exists
        if [[ -d "$frontend_wt_path" ]]; then
            frontend_ready=true
            if [[ "$frontend_existed" != true ]]; then
                # Newly created - copy .env and install
                copy_env_files "$erp_frontend" "$frontend_wt_path"
                run_package_install "$frontend_wt_path"
            fi
        else
            print_warning "Frontend worktree not created: $frontend_wt_name"
        fi

        # Only generate workspace if BOTH worktrees are ready
        if [[ "$backend_ready" == true && "$frontend_ready" == true ]]; then
            generate_erp_workspace "$lts_dir/$branch_subdir" "$suffix" "$backend_wt_name" "$frontend_wt_name"
        else
            print_warning "Skipping workspace generation - incomplete worktree pair for $branch"
            print_info "You may need to manually clean up partial worktrees"
        fi
    done

    # Collect all workspaces from created subdirectories
    local all_erp_workspaces=()
    for subdir in "${created_subdirs[@]}"; do
        for ws_file in "$lts_path/$subdir"/erp-*.code-workspace; do
            if [[ -f "$ws_file" ]]; then
                all_erp_workspaces+=("$lts_dir/$subdir/$(basename "$ws_file")")
            fi
        done
    done

    # Summary
    print_subheader "Summary"
    for subdir in "${created_subdirs[@]}"; do
        echo -e "  ${BOLD}$lts_dir/$subdir/${NC}"
        for wt_dir in "$lts_path/$subdir"/*/; do
            if [[ -d "$wt_dir" ]] && [[ -f "$wt_dir/.git" ]]; then
                echo -e "    ${CYAN}$(basename "$wt_dir")${NC}"
            fi
        done
    done
    echo ""

    # Offer to open workspace in IDE
    if [[ ${#all_erp_workspaces[@]} -gt 0 ]]; then
        local workspace_to_open=""

        if [[ ${#all_erp_workspaces[@]} -eq 1 ]]; then
            workspace_to_open="$SCRIPT_DIR/${all_erp_workspaces[0]}"
            if [[ -f "$workspace_to_open" ]]; then
                if confirm "Open workspace in $IDE_COMMAND?"; then
                    "$IDE_COMMAND" "$workspace_to_open" &
                    print_success "Opening $IDE_COMMAND..."
                fi
            fi
        else
            local open_options="Open all workspaces
Select which to open
Don't open, I'll open them"
            echo -e "${DIM}(press esc to skip)${NC}"
            local open_choice
            open_choice=$(echo "$open_options" | fzf --height=6 --reverse --prompt="Open workspace: ")

            if [[ -z "$open_choice" ]] || [[ "$open_choice" == "Don't open, I'll open them" ]]; then
                print_info "You can open workspaces later"
            elif [[ "$open_choice" == "Open all workspaces" ]]; then
                for ws in "${all_erp_workspaces[@]}"; do
                    workspace_to_open="$SCRIPT_DIR/$ws"
                    if [[ -f "$workspace_to_open" ]]; then
                        "$IDE_COMMAND" "$workspace_to_open" &
                        print_success "Opening: $ws"
                    fi
                done
            elif [[ "$open_choice" == "Select which to open" ]]; then
                echo -e "${DIM}(press esc to skip, TAB to select multiple)${NC}"
                local selected_ws
                selected_ws=$(printf '%s\n' "${all_erp_workspaces[@]}" | fzf --height=10 --reverse --multi --prompt="Select workspace(s): ")

                if [[ -n "$selected_ws" ]]; then
                    while IFS= read -r ws; do
                        if [[ -n "$ws" ]]; then
                            workspace_to_open="$SCRIPT_DIR/$ws"
                            if [[ -f "$workspace_to_open" ]]; then
                                "$IDE_COMMAND" "$workspace_to_open" &
                                print_success "Opening: $ws"
                            fi
                        fi
                    done <<< "$selected_ws"
                fi
            fi
        fi
    fi
}

# ============================================================================
#  MODE 5: CREATE 'MONOREPO-LIKE' WORKTREE/S
# ============================================================================

mode_create_monorepo_worktrees() {
    print_header "Create 'Monorepo-like' Worktree/s"

    # Get list of repositories
    local repos
    repos=$(get_git_repos)

    if [[ -z "$repos" ]]; then
        print_error "No git repositories found in $SCRIPT_DIR"
        return 1
    fi

    # Step 1: Select repositories (multi-select)
    print_subheader "Step 1: Select Repositories"
    echo -e "${DIM}Select one or more repos to create worktrees across.${NC}"
    echo -e "${DIM}(press esc to cancel, TAB to select multiple, ENTER to confirm)${NC}"

    local repo_options="← Go Back
$repos"
    local selected_repos_raw
    selected_repos_raw=$(echo "$repo_options" | fzf --height=15 --reverse --multi --prompt="Select repository(s): ")

    if [[ -z "$selected_repos_raw" ]]; then
        print_warning "Cancelled."
        return 1
    fi

    # Check if Go Back was selected
    if echo "$selected_repos_raw" | grep -q "← Go Back"; then
        return 0
    fi

    # Parse selected repos into array
    local selected_repos=()
    while IFS= read -r repo; do
        [[ -z "$repo" ]] && continue
        [[ "$repo" == "← Go Back" ]] && continue
        selected_repos+=("$repo")
    done <<< "$selected_repos_raw"

    if [[ ${#selected_repos[@]} -eq 0 ]]; then
        print_warning "No repositories selected"
        return 0
    fi

    print_success "Selected ${#selected_repos[@]} repository(s): ${selected_repos[*]}"

    # Step 1b: Single-repo short-circuit
    if [[ ${#selected_repos[@]} -eq 1 ]]; then
        local single_repo="${selected_repos[0]}"
        local single_repo_path="$SCRIPT_DIR/$single_repo"
        local single_lts_dir="${single_repo}-lts"
        local single_lts_path="$SCRIPT_DIR/$single_lts_dir"

        print_info "Single repo selected — using standard ${single_lts_dir} naming"

        # Branch name
        print_subheader "Step 2: Enter Branch Name"
        echo -e "${DIM}Format:    <type>/<description>  (e.g., fix/order-bug)${NC}"
        echo -e "${DIM}Examples:  fix/order-bug   feature/new-report   hotfix/api-timeout${NC}"
        echo ""
        echo -e "${DIM}(leave empty to cancel)${NC}"
        echo ""
        show_existing_branches "$single_repo_path"

        local branch_name
        branch_name=$(get_input "Branch name (or empty to cancel)")

        if [[ -z "$branch_name" || "$branch_name" =~ ^[[:space:]]*$ ]]; then
            print_warning "Cancelled."
            return 0
        fi

        if ! validate_branch_name "$branch_name"; then
            return 1
        fi

        local suffix
        suffix=$(extract_suffix "$branch_name")
        local wt_name
        wt_name=$(generate_unique_worktree_name "$single_repo" "$suffix" "$single_lts_path")

        # Confirmation
        print_subheader "Step 3: Confirmation"
        echo -e "Repository: ${BOLD}$single_repo${NC}"
        echo -e "Branch:     ${CYAN}$branch_name${NC}"
        echo -e "Worktree:   ${CYAN}$wt_name${NC}"
        echo -e "LTS dir:    ${CYAN}$single_lts_dir${NC}"
        echo ""

        if ! confirm "Proceed with creation?"; then
            print_warning "Cancelled."
            return 1
        fi

        # Execute
        print_subheader "Step 4: Creating Worktree"

        if [[ ! -d "$single_lts_path" ]]; then
            mkdir -p "$single_lts_path"
            print_success "Created $single_lts_dir directory"
        fi

        if check_ongoing_operations "$single_repo_path"; then
            return 1
        fi

        prune_worktrees "$single_repo_path"

        if ! ensure_clean_main "$single_repo_path"; then
            print_error "Failed to prepare $single_repo"
            return 1
        fi

        cd "$single_repo_path"
        local repo_main
        repo_main=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
        # Fetch all remote refs so we can detect remote-only branches
        print_step "Fetching remote branches for $single_repo..."
        git fetch origin 2>/dev/null || true
        cd "$SCRIPT_DIR"

        if [[ -z "$repo_main" ]]; then
            print_error "Could not determine base branch for $single_repo"
            return 1
        fi

        local wt_path="$single_lts_path/$wt_name"

        cd "$single_repo_path"
        if git show-ref --verify --quiet "refs/heads/$branch_name" 2>/dev/null; then
            local checked_out_at
            checked_out_at=$(git worktree list 2>/dev/null | grep -E "\[$branch_name\]" | awk '{print $1}' || echo "")
            if [[ -n "$checked_out_at" ]]; then
                print_error "Branch $branch_name is already checked out at: $checked_out_at"
                cd "$SCRIPT_DIR"
                return 1
            fi
            local add_output
            add_output=$(git worktree add "$wt_path" "$branch_name" 2>&1)
            if [[ $? -ne 0 ]]; then
                print_error "Failed to add worktree: $add_output"
                cd "$SCRIPT_DIR"
                return 1
            fi
        elif git show-ref --verify --quiet "refs/remotes/origin/$branch_name" 2>/dev/null; then
            local add_output
            add_output=$(git worktree add --track -b "$branch_name" "$wt_path" "origin/$branch_name" 2>&1)
            if [[ $? -ne 0 ]]; then
                print_error "Failed to add worktree: $add_output"
                cd "$SCRIPT_DIR"
                return 1
            fi
        else
            local add_output
            add_output=$(git worktree add -b "$branch_name" "$wt_path" "$repo_main" 2>&1)
            if [[ $? -ne 0 ]]; then
                print_error "Failed to create worktree: $add_output"
                cd "$SCRIPT_DIR"
                return 1
            fi
        fi
        cd "$SCRIPT_DIR"
        print_success "Created worktree: $wt_name"

        copy_env_files "$single_repo_path" "$wt_path"
        run_package_install "$wt_path"

        # Generate individual workspace
        generate_individual_workspace "$single_lts_dir" "$wt_name"

        # Regenerate combined workspace if >1 worktrees exist
        local all_wts=()
        local wt_list
        wt_list=$(get_worktrees_in_lts "$single_lts_dir")
        while IFS= read -r w; do
            [[ -n "$w" ]] && all_wts+=("$w")
        done <<< "$wt_list"

        if [[ ${#all_wts[@]} -gt 1 ]]; then
            generate_combined_workspace "$single_lts_dir" "$single_repo" "${all_wts[@]}"
        fi

        # Offer IDE open
        print_subheader "Summary"
        print_success "Worktree created in $single_lts_dir/"
        echo ""

        local workspace_to_open="$single_lts_path/${wt_name}.code-workspace"
        if [[ -f "$workspace_to_open" ]]; then
            if confirm "Open workspace in $IDE_COMMAND?"; then
                "$IDE_COMMAND" "$workspace_to_open" &
                print_success "Opening $IDE_COMMAND..."
            fi
        fi

        return 0
    fi

    # Multi-repo path (2+ repos)

    # Step 2: Branch name
    print_subheader "Step 2: Enter Branch Name"
    echo -e "${DIM}The same branch will be created across all selected repos.${NC}"
    echo ""
    echo -e "${DIM}Format:    <type>/<description>  (e.g., fix/order-bug)${NC}"
    echo -e "${DIM}Examples:  fix/order-bug   feature/new-report   hotfix/api-timeout${NC}"
    echo ""
    echo -e "${DIM}The part after '/' becomes the folder suffix:${NC}"
    for repo in "${selected_repos[@]}"; do
        echo -e "${DIM}  fix/order-bug  →  ${repo}-order-bug${NC}"
    done
    echo ""
    echo -e "${DIM}(leave empty to cancel)${NC}"
    echo ""

    # Show existing branches for each selected repo
    for repo in "${selected_repos[@]}"; do
        show_existing_branches "$SCRIPT_DIR/$repo"
    done

    local branch_name
    branch_name=$(get_input "Branch name (or empty to cancel)")

    if [[ -z "$branch_name" || "$branch_name" =~ ^[[:space:]]*$ ]]; then
        print_warning "Cancelled."
        return 0
    fi

    if ! validate_branch_name "$branch_name"; then
        return 1
    fi

    # Step 3: Compute LTS dir name and branch subdirectory
    local suffix
    suffix=$(extract_suffix "$branch_name")
    local lts_dir
    lts_dir=$(generate_monorepo_lts_name "${selected_repos[@]}")
    local lts_path="$SCRIPT_DIR/$lts_dir"
    local lts_prefix="${lts_dir%-lts}"
    local branch_subdir="${lts_prefix}-${suffix}"
    local branch_subdir_path="$lts_path/$branch_subdir"

    # Step 4: Confirmation
    print_subheader "Step 3: Confirmation"
    echo -e "Repositories:"
    for repo in "${selected_repos[@]}"; do
        echo -e "  - ${BOLD}$repo${NC}"
    done
    echo -e "Branch: ${CYAN}$branch_name${NC}"
    echo -e "Directory: ${CYAN}$lts_dir/$branch_subdir/${NC}"
    echo -e "Planned worktrees:"
    for repo in "${selected_repos[@]}"; do
        local planned_wt
        planned_wt=$(generate_unique_worktree_name "$repo" "$suffix" "$branch_subdir_path")
        echo -e "  - ${CYAN}${planned_wt}${NC}"
    done
    echo ""

    if ! confirm "Proceed with creation?"; then
        print_warning "Cancelled."
        return 1
    fi

    # Step 5: Execute creation
    print_subheader "Step 4: Creating Worktrees"

    # Create LTS directory with metadata
    if [[ ! -d "$lts_path" ]]; then
        mkdir -p "$lts_path"
        echo "monorepo" > "$lts_path/.lts-type"
        print_success "Created $lts_dir directory"
    fi

    # Create branch subdirectory
    mkdir -p "$branch_subdir_path"

    # Sort repos alphabetically for .lts-repos
    local sorted_repos=()
    while IFS= read -r r; do
        [[ -n "$r" ]] && sorted_repos+=("$r")
    done <<< "$(printf '%s\n' "${selected_repos[@]}" | sort)"

    # Prepare each repo and create worktrees
    local repo_wt_pairs=()
    local succeeded_count=0

    for repo in "${selected_repos[@]}"; do
        local repo_path="$SCRIPT_DIR/$repo"

        echo ""
        print_step "Processing: $repo"

        # Check for ongoing operations
        if check_ongoing_operations "$repo_path"; then
            print_warning "Skipping $repo due to ongoing operation"
            continue
        fi

        # Prune and prepare
        prune_worktrees "$repo_path"

        if ! ensure_clean_main "$repo_path"; then
            print_error "Failed to prepare $repo"
            continue
        fi

        cd "$repo_path"
        local repo_main
        repo_main=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
        # Fetch all remote refs so we can detect remote-only branches
        print_step "Fetching remote branches for $repo..."
        git fetch origin 2>/dev/null || true
        cd "$SCRIPT_DIR"

        if [[ -z "$repo_main" ]]; then
            print_error "Could not determine base branch for $repo"
            continue
        fi

        # Generate worktree name and path (inside branch subdirectory)
        local wt_name
        wt_name=$(generate_unique_worktree_name "$repo" "$suffix" "$branch_subdir_path")
        local wt_path="$branch_subdir_path/$wt_name"

        if [[ "$wt_name" != "${repo}-${suffix}" ]]; then
            print_info "Using unique name: $wt_name"
        fi

        # Check if already exists
        if [[ -d "$wt_path" ]]; then
            print_warning "Worktree already exists: $wt_name"
            repo_wt_pairs+=("${repo}:${wt_name}")
            ((succeeded_count++))
            continue
        fi

        # Create worktree
        cd "$repo_path"
        local add_output
        if git show-ref --verify --quiet "refs/heads/$branch_name" 2>/dev/null; then
            local checked_out_at
            checked_out_at=$(git worktree list 2>/dev/null | grep -E "\[$branch_name\]" | awk '{print $1}' || echo "")
            if [[ -n "$checked_out_at" ]]; then
                print_error "Branch $branch_name is already checked out at: $checked_out_at"
                print_info "Skipping $repo"
                cd "$SCRIPT_DIR"
                continue
            fi
            add_output=$(git worktree add "$wt_path" "$branch_name" 2>&1)
            if [[ $? -ne 0 ]]; then
                print_error "Failed to add worktree for $repo: $add_output"
                cd "$SCRIPT_DIR"
                continue
            fi
        elif git show-ref --verify --quiet "refs/remotes/origin/$branch_name" 2>/dev/null; then
            add_output=$(git worktree add --track -b "$branch_name" "$wt_path" "origin/$branch_name" 2>&1)
            if [[ $? -ne 0 ]]; then
                print_error "Failed to add worktree for $repo: $add_output"
                cd "$SCRIPT_DIR"
                continue
            fi
        else
            add_output=$(git worktree add -b "$branch_name" "$wt_path" "$repo_main" 2>&1)
            if [[ $? -ne 0 ]]; then
                print_error "Failed to create worktree for $repo: $add_output"
                cd "$SCRIPT_DIR"
                continue
            fi
        fi
        cd "$SCRIPT_DIR"
        print_success "Created worktree: $wt_name"

        # Post-creation setup
        copy_env_files "$repo_path" "$wt_path"
        run_package_install "$wt_path"

        repo_wt_pairs+=("${repo}:${wt_name}")
        ((succeeded_count++))
    done

    # Step 6: Write .lts-repos (only if at least one worktree succeeded)
    if [[ $succeeded_count -eq 0 ]]; then
        print_error "No worktrees were created"
        # Clean up empty LTS directory if we just created it
        if [[ -d "$lts_path" ]] && [[ -z "$(get_worktrees_in_lts "$lts_dir")" ]]; then
            rm -rf "$lts_path"
        fi
        return 1
    fi

    # Merge with existing .lts-repos if present (union of repos)
    if [[ -f "$lts_path/.lts-repos" ]]; then
        local merged_repos
        merged_repos=$({ cat "$lts_path/.lts-repos"; printf '%s\n' "${sorted_repos[@]}"; } | LC_ALL=C sort -u)
        printf '%s\n' "$merged_repos" > "$lts_path/.lts-repos"
        print_success "Updated .lts-repos metadata file"
    else
        printf '%s\n' "${sorted_repos[@]}" > "$lts_path/.lts-repos"
        print_success "Created .lts-repos metadata file"
    fi

    # Step 7: Generate workspace + IDE open
    print_subheader "Summary"

    if [[ $succeeded_count -ge 2 ]]; then
        generate_monorepo_workspace "$lts_dir/$branch_subdir" "$suffix" "${repo_wt_pairs[@]}"
    elif [[ $succeeded_count -eq 1 ]]; then
        local only_pair="${repo_wt_pairs[0]}"
        local only_wt="${only_pair#*:}"
        generate_individual_workspace "$lts_dir/$branch_subdir" "$only_wt"
        print_warning "Only 1 repo succeeded — created individual workspace instead of monorepo workspace"
    fi

    # Display summary
    print_success "Monorepo worktrees created in $lts_dir/$branch_subdir/"
    echo ""

    # Show worktrees in subdirectory
    echo -e "  ${BOLD}$lts_dir/$branch_subdir/${NC}"
    for wt_dir in "$branch_subdir_path"/*/; do
        if [[ -d "$wt_dir" ]] && [[ -f "$wt_dir/.git" ]]; then
            echo -e "    ${CYAN}$(basename "$wt_dir")${NC}"
        fi
    done
    echo ""

    # Collect workspace files from subdirectory
    local all_workspaces=()
    for ws_file in "$branch_subdir_path"/*.code-workspace; do
        if [[ -f "$ws_file" ]]; then
            all_workspaces+=("$(basename "$ws_file")")
        fi
    done

    if [[ ${#all_workspaces[@]} -gt 0 ]]; then
        echo -e "Workspaces:"
        for ws in "${all_workspaces[@]}"; do
            echo -e "  ${CYAN}$lts_dir/$branch_subdir/$ws${NC}"
        done
        echo ""
    fi

    # Offer to open workspace in IDE
    if [[ ${#all_workspaces[@]} -gt 0 ]]; then
        local workspace_to_open=""

        if [[ ${#all_workspaces[@]} -eq 1 ]]; then
            workspace_to_open="$branch_subdir_path/${all_workspaces[0]}"
            if [[ -f "$workspace_to_open" ]]; then
                if confirm "Open workspace in $IDE_COMMAND?"; then
                    "$IDE_COMMAND" "$workspace_to_open" &
                    print_success "Opening $IDE_COMMAND..."
                fi
            fi
        else
            local open_options="Open all workspaces
Select which to open
Don't open, I'll open them"
            echo -e "${DIM}(press esc to skip)${NC}"
            local open_choice
            open_choice=$(echo "$open_options" | fzf --height=6 --reverse --prompt="Open workspace: ")

            if [[ -z "$open_choice" ]] || [[ "$open_choice" == "Don't open, I'll open them" ]]; then
                print_info "You can open workspaces from: $lts_dir/$branch_subdir/"
            elif [[ "$open_choice" == "Open all workspaces" ]]; then
                for ws in "${all_workspaces[@]}"; do
                    workspace_to_open="$branch_subdir_path/$ws"
                    if [[ -f "$workspace_to_open" ]]; then
                        "$IDE_COMMAND" "$workspace_to_open" &
                        print_success "Opening: $ws"
                    fi
                done
            elif [[ "$open_choice" == "Select which to open" ]]; then
                echo -e "${DIM}(press esc to skip, TAB to select multiple)${NC}"
                local selected_ws
                selected_ws=$(printf '%s\n' "${all_workspaces[@]}" | fzf --height=10 --reverse --multi --prompt="Select workspace(s): ")

                if [[ -n "$selected_ws" ]]; then
                    while IFS= read -r ws; do
                        if [[ -n "$ws" ]]; then
                            workspace_to_open="$branch_subdir_path/$ws"
                            if [[ -f "$workspace_to_open" ]]; then
                                "$IDE_COMMAND" "$workspace_to_open" &
                                print_success "Opening: $ws"
                            fi
                        fi
                    done <<< "$selected_ws"
                fi
            fi
        fi
    fi
}

# ============================================================================
#  MAIN MENU
# ============================================================================

# Check if ERP repos exist (both gorocky-erp and erp-ui as main repos, not worktrees)
check_erp_repos_exist() {
    [[ -d "$SCRIPT_DIR/gorocky-erp/.git" ]] && [[ -d "$SCRIPT_DIR/erp-ui/.git" ]]
}

# Get worktree status for display
get_worktree_status() {
    local wt_path="$1"

    if [[ ! -d "$wt_path" ]]; then
        echo "missing"
        return
    fi

    cd "$wt_path" 2>/dev/null || { echo "missing"; return; }

    # Check for uncommitted changes
    local status
    status=$(git status --porcelain 2>/dev/null)

    local local_status=""
    if [[ -n "$status" ]]; then
        local changed_count
        changed_count=$(echo "$status" | wc -l | tr -d ' ')
        local_status="${changed_count} changed"
    fi

    # Check ahead/behind relative to upstream or matching remote branch
    local ahead behind sync_status="" remote_ref=""
    local current_branch
    current_branch=$(git branch --show-current 2>/dev/null)

    # Try upstream first, then fall back to origin/<branch>
    if git rev-parse --verify "@{upstream}" &>/dev/null; then
        remote_ref="@{upstream}"
    elif git rev-parse --verify "origin/${current_branch}" &>/dev/null; then
        remote_ref="origin/${current_branch}"
    fi

    if [[ -n "$remote_ref" ]]; then
        ahead=$(git rev-list --count "${remote_ref}..HEAD" 2>/dev/null || echo "0")
        behind=$(git rev-list --count "HEAD..${remote_ref}" 2>/dev/null || echo "0")

        if [[ "$ahead" -gt 0 && "$behind" -gt 0 ]]; then
            sync_status="diverged"
        elif [[ "$ahead" -gt 0 ]]; then
            sync_status="${ahead} to push"
        elif [[ "$behind" -gt 0 ]]; then
            sync_status="${behind} to pull"
        else
            sync_status="synced"
        fi
    else
        sync_status="no remote"
    fi

    # Detect main branch for merge/new detection
    # git commands work from worktrees since refs are shared
    local main_branch=""
    local _candidates=("$BASIS_BRANCH")
    [[ "$BASIS_BRANCH" != "main" ]] && _candidates+=("main")
    [[ "$BASIS_BRANCH" != "master" ]] && _candidates+=("master")
    for _c in "${_candidates[@]}"; do
        if git show-ref --verify --quiet "refs/heads/$_c" 2>/dev/null; then
            main_branch="$_c"
            break
        fi
    done

    # Check if branch is merged or new (only if we found a main branch and we're not on it)
    local is_merged=false
    local is_new=false

    if [[ -n "$main_branch" ]] && [[ "$current_branch" != "$main_branch" ]]; then
        # Count unique commits on this branch not in main
        local unique_commits
        unique_commits=$(git rev-list --count "${main_branch}..HEAD" 2>/dev/null || echo "0")

        if [[ "$unique_commits" -eq 0 ]]; then
            # No unique commits - either merged (regular merge) or brand new
            if [[ "$sync_status" == "no remote" ]]; then
                is_new=true
            else
                is_merged=true
            fi
        else
            # Has unique commits - check if they've been squash/rebase merged
            # git cherry shows commits not yet applied to upstream
            # "+" means not applied, "-" means equivalent exists
            # If no "+" lines, all commits have been merged (squash/rebase)
            local unmerged_count
            unmerged_count=$(git cherry "$main_branch" HEAD 2>/dev/null | grep -c "^+" || true)
            if [[ "$unmerged_count" -eq 0 ]]; then
                is_merged=true
            fi
        fi
    fi

    # Build final status string
    if $is_new; then
        if [[ -n "$local_status" ]]; then
            echo "${local_status} | new"
        else
            echo "new"
        fi
    elif $is_merged; then
        if [[ -n "$local_status" ]]; then
            echo "${local_status} | merged"
        else
            echo "merged · cleanable"
        fi
    elif [[ -n "$local_status" ]]; then
        echo "${local_status} | ${sync_status}"
    elif [[ "$sync_status" == "synced" ]]; then
        echo "clean"
    else
        echo "${sync_status}"
    fi

    cd "$SCRIPT_DIR"
}

# Pull latest from all repos to refresh overview status
mode_refresh_overview() {
    print_header "Refreshing Overview"

    local repos
    repos=$(get_git_repos)

    if [[ -z "$repos" ]]; then
        print_warning "No repositories found"
        return
    fi

    while IFS= read -r repo; do
        [[ -z "$repo" ]] && continue
        local repo_path="$SCRIPT_DIR/$repo"

        print_step "Fetching latest in $repo..."
        cd "$repo_path"

        local fetch_output
        fetch_output=$(git fetch origin 2>&1)
        local fetch_exit=$?

        if [[ $fetch_exit -ne 0 ]]; then
            if echo "$fetch_output" | grep -qi "Could not read from remote"; then
                print_warning "$repo: cannot reach remote (network issue or SSH not configured)"
            else
                print_warning "$repo: fetch failed - $fetch_output"
            fi
        else
            # Update local basis/main/master to match remote
            local main_branch=""
            local _candidates=("$BASIS_BRANCH")
            [[ "$BASIS_BRANCH" != "main" ]] && _candidates+=("main")
            [[ "$BASIS_BRANCH" != "master" ]] && _candidates+=("master")
            for _c in "${_candidates[@]}"; do
                if git show-ref --verify --quiet "refs/heads/$_c" 2>/dev/null; then
                    main_branch="$_c"
                    break
                fi
            done

            if [[ -n "$main_branch" ]]; then
                local current_branch
                current_branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null)

                if [[ "$current_branch" == "$main_branch" ]]; then
                    # On main - safe to pull
                    git pull origin "$main_branch" --ff-only 2>/dev/null || true
                else
                    # Not on main - update main ref without checkout
                    git fetch origin "$main_branch:$main_branch" 2>/dev/null || true
                fi
            fi

            print_success "$repo: up to date"
        fi

        cd "$SCRIPT_DIR"
    done <<< "$repos"

    echo ""
    print_success "Overview refreshed"
}

# Show repository overview with LTS status and worktrees
show_repo_overview() {
    local repos
    repos=$(get_git_repos)

    if [[ -z "$repos" ]]; then
        return
    fi

    local inner_width=$INNER_WIDTH
    local header_text="  Repository Overview"
    local header_pad=$((inner_width - ${#header_text}))

    echo ""
    echo -e "${GREEN}╔$(repeat_char '═' $inner_width)╗${NC}"
    printf "${GREEN}║${NC}${BOLD}%s${NC}%*s${GREEN}║${NC}\n" "$header_text" "$header_pad" ""
    echo -e "${GREEN}╠$(repeat_char '═' $inner_width)╣${NC}"

    while IFS= read -r repo; do
        [[ -z "$repo" ]] && continue

        # Determine the LTS directory for this repo
        local lts_dir="${repo}-lts"
        local lts_path="$SCRIPT_DIR/$lts_dir"

        # Collect worktrees with their full paths (name:path pairs)
        local wt_entries=()

        # Check repo-specific LTS dir (e.g., gorocky-erp-lts)
        if [[ -d "$lts_path" ]]; then
            local wt_list
            wt_list=$(get_worktrees_in_lts "$lts_dir")
            while IFS= read -r wt; do
                [[ -n "$wt" ]] && wt_entries+=("${wt}:${lts_path}/${wt}")
            done <<< "$wt_list"
        fi

        # Check all multi-repo LTS dirs (ERP + monorepo) that reference this repo
        local multi_lts_dirs
        multi_lts_dirs=$(get_monorepo_lts_dirs_for_repo "$repo")
        while IFS= read -r multi_lts; do
            [[ -z "$multi_lts" ]] && continue
            local wt_list
            wt_list=$(get_worktrees_in_lts "$multi_lts" | grep -E "(^|/)${repo}-" || true)
            while IFS= read -r wt; do
                [[ -n "$wt" ]] && wt_entries+=("${wt}:$SCRIPT_DIR/$multi_lts/${wt}")
            done <<< "$wt_list"
        done <<< "$multi_lts_dirs"

        # Check if any LTS directory exists for this repo
        local has_lts=false
        [[ -d "$lts_path" ]] && has_lts=true
        if [[ -n "$multi_lts_dirs" ]]; then
            has_lts=true
        fi

        if [[ "$has_lts" == true ]]; then
            if [[ ${#wt_entries[@]} -eq 0 ]]; then
                # LTS exists but no worktrees for this repo
                # Format: "  repo (x LTS)" -> 2 + repo_len + 8 = repo_len + 10
                local content_len=$((2 + ${#repo} + 8))
                local padding=$((inner_width - content_len))
                printf "${GREEN}║${NC}  %s ${DIM}(${NC}${RED}x${NC}${DIM} LTS)${NC}%*s${GREEN}║${NC}\n" "$repo" "$padding" ""
            else
                # LTS exists with worktrees
                # Format: "  repo (/ LTS)" -> 2 + repo_len + 8 = repo_len + 10
                local content_len=$((2 + ${#repo} + 8))
                local padding=$((inner_width - content_len))
                printf "${GREEN}║${NC}  %s ${DIM}(${NC}${GREEN}/${NC}${DIM} LTS)${NC}%*s${GREEN}║${NC}\n" "$repo" "$padding" ""

                # Show worktrees
                local wt_count=${#wt_entries[@]}
                local i=0

                for entry in "${wt_entries[@]}"; do
                    local wt="${entry%%:*}"
                    local wt_full_path="${entry#*:}"
                    i=$((i + 1))
                    local wt_status
                    wt_status=$(get_worktree_status "$wt_full_path")

                    # Get display name (remove repo prefix, handle nested paths)
                    local wt_basename
                    wt_basename=$(basename "$wt")
                    local display_name="${wt_basename#${repo}-}"

                    # Tree character
                    local tree_char="├"
                    [[ $i -eq $wt_count ]] && tree_char="└"

                    # Status color (check most specific patterns first)
                    # Order matters: "changed" before "new"/"cleanable" so
                    # "2 changed | new" gets CYAN (changes are priority signal)
                    local status_color="$GREEN"
                    case "$wt_status" in
                        *missing*) status_color="$RED" ;;
                        *diverged*) status_color="$RED" ;;
                        *changed*) status_color="$CYAN" ;;
                        *cleanable*) status_color="$GREEN" ;;
                        *"to push"*|*"to pull"*) status_color="$YELLOW" ;;
                        *"no remote"*) status_color="$DIM" ;;
                        *new*) status_color="$BLUE" ;;
                    esac

                    # Format: "    X name (status)" -> 4 + 2 + name_len + 2 + status_len + 1
                    local content_len=$((4 + 2 + ${#display_name} + 2 + ${#wt_status} + 1))
                    local wt_padding=$((inner_width - content_len))

                    printf "${GREEN}║${NC}    ${DIM}%s${NC} ${CYAN}%s${NC} ${DIM}(${NC}%b%s${NC}${DIM})${NC}%*s${GREEN}║${NC}\n" "$tree_char" "$display_name" "$status_color" "$wt_status" "$wt_padding" ""
                done
            fi
        else
            # No LTS
            # Format: "  repo (x LTS)" -> 2 + repo_len + 8 = repo_len + 10
            local content_len=$((2 + ${#repo} + 8))
            local padding=$((inner_width - content_len))
            printf "${GREEN}║${NC}  %s ${DIM}(${NC}${RED}x${NC}${DIM} LTS)${NC}%*s${GREEN}║${NC}\n" "$repo" "$padding" ""
        fi
    done <<< "$repos"

    echo -e "${GREEN}╚$(repeat_char '═' $inner_width)╝${NC}"
}

mode_configure_lts() {
    while true; do
        print_header "Configure LTS"

        local options="IDE Command (current: $IDE_COMMAND)
Package Manager (current: $PACKAGE_MANAGER)
Basis Branch (current: $BASIS_BRANCH)
Reset Tools Check (brew/git/fzf)
Reset Package Manager Check
Reset All Prerequisites
← Back"

        echo -e "${DIM}(press esc to go back)${NC}"
        local choice
        choice=$(echo "$options" | fzf --height=10 --reverse --prompt="Select setting: ")

        if [[ -z "$choice" ]] || [[ "$choice" == "← Back" ]]; then
            return
        fi

        case "$choice" in
            "IDE Command"*)
                local ide_options="cursor
code
windsurf
zed"
                local selected_ide
                selected_ide=$(echo "$ide_options" | fzf --height=7 --reverse --prompt="Select IDE (current: $IDE_COMMAND): ")
                if [[ -n "$selected_ide" ]]; then
                    IDE_COMMAND="$selected_ide"
                    save_config
                    print_success "IDE command set to: $IDE_COMMAND"
                fi
                ;;
            "Package Manager"*)
                local pm_options="pnpm
npm
yarn
bun"
                local selected_pm
                selected_pm=$(echo "$pm_options" | fzf --height=7 --reverse --prompt="Select package manager (current: $PACKAGE_MANAGER): ")
                if [[ -n "$selected_pm" ]]; then
                    PACKAGE_MANAGER="$selected_pm"
                    save_config
                    print_success "Package manager set to: $PACKAGE_MANAGER"
                fi
                ;;
            "Basis Branch"*)
                local branch_input
                branch_input=$(get_input "Basis branch (current: $BASIS_BRANCH)")
                if [[ -n "$branch_input" ]]; then
                    BASIS_BRANCH="$branch_input"
                    save_config
                    print_success "Basis branch set to: $BASIS_BRANCH"
                fi
                ;;
            "Reset Tools Check"*)
                TOOLS_CHECKED="false"
                save_config
                print_success "Tools check reset. Full tools check will run on next launch."
                ;;
            "Reset Package Manager Check"*)
                PACKAGE_MANAGER_CHECKED="false"
                save_config
                print_success "Package manager check reset. Full check will run on next launch."
                ;;
            "Reset All Prerequisites"*)
                TOOLS_CHECKED="false"
                PACKAGE_MANAGER_CHECKED="false"
                save_config
                print_success "All prerequisites reset. Full check will run on next launch."
                ;;
        esac
    done
}

show_main_menu() {
    local show_erp="$1"
    local iw=$INNER_WIDTH

    echo ""
    printf "${CYAN}╔$(repeat_char '═' $iw)╗${NC}\n"
    printf "${CYAN}║${NC}%${iw}s${CYAN}║${NC}\n" ""
    printf "${CYAN}║${NC}     ${GREEN}🌳${NC} ${BOLD}${WHITE}Led's Tree Script (LTS)${NC}%$((iw - 31))s${CYAN}║${NC}\n" ""
    printf "${CYAN}║${NC}        ${DIM}Git Worktree Management Tool${NC}%$((iw - 36))s${CYAN}║${NC}\n" ""
    printf "${CYAN}║${NC}%${iw}s${CYAN}║${NC}\n" ""
    printf "${CYAN}╠$(repeat_char '─' $iw)╣${NC}\n"
    printf "${CYAN}║${NC}  ${GREEN}Create${NC}    ${DIM}- Spawn new worktrees from main branch${NC}%$((iw - 50))s${CYAN}║${NC}\n" ""
    if [[ "$show_erp" == "true" ]]; then
        printf "${CYAN}║${NC}  ${GREEN}ERP${NC}       ${DIM}- Create paired backend + frontend worktrees${NC}%$((iw - 56))s${CYAN}║${NC}\n" ""
    fi
    printf "${CYAN}║${NC}  ${GREEN}Monorepo${NC}  ${DIM}- Create worktrees across multiple repos${NC}%$((iw - 52))s${CYAN}║${NC}\n" ""
    printf "${CYAN}║${NC}  ${YELLOW}Update${NC}    ${DIM}- Rebase worktrees with latest main${NC}%$((iw - 47))s${CYAN}║${NC}\n" ""
    printf "${CYAN}║${NC}  ${RED}Cleanup${NC}   ${DIM}- Remove worktrees and delete branches${NC}%$((iw - 50))s${CYAN}║${NC}\n" ""
    printf "${CYAN}║${NC}  ${MAGENTA}Cleanables${NC} ${DIM}- Cleanup merged/cleanable worktrees${NC}%$((iw - 49))s${CYAN}║${NC}\n" ""
    printf "${CYAN}║${NC}  ${CYAN}Refresh${NC}   ${DIM}- Pull all repos and refresh overview${NC}%$((iw - 49))s${CYAN}║${NC}\n" ""
    printf "${CYAN}║${NC}  ${CYAN}Configure${NC} ${DIM}- Change IDE, package manager, settings${NC}%$((iw - 51))s${CYAN}║${NC}\n" ""
    printf "${CYAN}║${NC}%${iw}s${CYAN}║${NC}\n" ""
    printf "${CYAN}╚$(repeat_char '═' $iw)╝${NC}\n"
    echo ""
}

# ============================================================================
#  MAIN SCRIPT
# ============================================================================

main() {
    # Load configuration
    if [[ ! -f "$LTS_CONF" ]]; then
        create_default_config
    fi
    load_config

    # Resize terminal and clear
    printf "\e[8;${HEIGHT};${WIDTH}t"
    clear

    # Welcome
    echo ""
    echo -e "${GREEN}$(repeat_char '━' $WIDTH)${NC}"
    echo -e "${GREEN}  Welcome to Led's Tree Script! 🌳${NC}"
    echo -e "${GREEN}$(repeat_char '━' $WIDTH)${NC}"

    # Check prerequisites (per-flag fast/full for tools & PM, SSH + repos-on-main always run)
    check_prerequisites

    # Auto-refresh if stale (>24h since last refresh)
    check_auto_refresh

    # Check if at least one git repository exists
    local repos
    repos=$(get_git_repos)
    if [[ -z "$repos" ]]; then
        echo ""
        print_error "No git repositories found in $SCRIPT_DIR"
        print_info "Please clone at least one repository to this directory first."
        echo ""
        exit 1
    fi

    # Check if ERP repos exist (for conditional menu display)
    local erp_available=false
    if check_erp_repos_exist; then
        erp_available=true
    fi

    # Main loop
    while true; do
        show_main_menu "$erp_available"
        show_repo_overview

        # Build menu options dynamically (colored)
        # Order: creation, update, cleanup, refresh, configure, exit
        local menu_options="${GREEN}1. Create Worktree/s${NC}"

        if [[ "$erp_available" == "true" ]]; then
            menu_options+="
${GREEN}2. Create ERP Worktree/s (Special)${NC}
${GREEN}3. Create 'Monorepo-like' Worktree/s${NC}
${YELLOW}4. Update Worktree/s (Rebase from Main)${NC}
${RED}5. Cleanup Worktree/s${NC}
${MAGENTA}6. Cleanup Merged Cleanables${NC}
${CYAN}7. Refresh Overview${NC}
${CYAN}8. Configure LTS${NC}
${DIM}9. Exit${NC}"
        else
            menu_options+="
${GREEN}2. Create 'Monorepo-like' Worktree/s${NC}
${YELLOW}3. Update Worktree/s (Rebase from Main)${NC}
${RED}4. Cleanup Worktree/s${NC}
${MAGENTA}5. Cleanup Merged Cleanables${NC}
${CYAN}6. Refresh Overview${NC}
${CYAN}7. Configure LTS${NC}
${DIM}8. Exit${NC}"
        fi

        echo -e "${DIM}(press esc to cancel)${NC}"
        local choice
        local fzf_height=12
        [[ "$erp_available" == "true" ]] && fzf_height=13
        choice=$(echo -e "$menu_options" | fzf --ansi --height=$fzf_height --reverse --prompt="Select an option: ")

        if [[ -z "$choice" ]]; then
            print_warning "No selection made"
            continue
        fi

        # Strip ANSI codes for matching
        local clean_choice
        clean_choice=$(echo "$choice" | sed 's/\x1b\[[0-9;]*m//g')

        case "$clean_choice" in
            "1. Create Worktree/s")
                mode_create_worktrees
                ;;
            *". Create ERP Worktree/s (Special)")
                mode_create_erp_worktrees
                ;;
            *". Create 'Monorepo-like' Worktree/s")
                mode_create_monorepo_worktrees
                ;;
            *". Update Worktree/s (Rebase from Main)")
                mode_update_worktrees
                ;;
            *". Cleanup Worktree/s")
                mode_cleanup_worktrees
                ;;
            *". Cleanup Merged Cleanables")
                mode_cleanup_merged_cleanables
                ;;
            *". Refresh Overview")
                mode_refresh_overview
                LAST_REFRESH=$(date +%s)
                save_config
                clear
                continue
                ;;
            *". Configure LTS")
                mode_configure_lts
                clear
                continue
                ;;
            *". Exit")
                echo ""
                echo -e "${GREEN}$(repeat_char '━' $WIDTH)${NC}"
                echo -e "${GREEN}  Thanks for using Led Salazar's Tree Script! 🌳${NC}"
                echo -e "${GREEN}  Happy coding!${NC}"
                echo -e "${GREEN}$(repeat_char '━' $WIDTH)${NC}"
                echo ""
                exit 0
                ;;
            *)
                print_warning "Invalid option"
                continue
                ;;
        esac

        # Ask if user wants to continue
        echo ""
        echo -e "${DIM}(press esc to cancel)${NC}"
        local continue_options="Yes, let's do another one
No, I would like to exit"

        local continue_choice
        continue_choice=$(echo "$continue_options" | fzf --height=5 --reverse --prompt="Would you like to choose a mode again? ")

        if [[ -z "$continue_choice" || "$continue_choice" == "No, I would like to exit" ]]; then
            echo ""
            echo -e "${GREEN}$(repeat_char '━' $WIDTH)${NC}"
            echo -e "${GREEN}  Thanks for using Led Salazar's Tree Script! 🌳${NC}"
            echo -e "${GREEN}  Happy coding!${NC}"
            echo -e "${GREEN}$(repeat_char '━' $WIDTH)${NC}"
            echo ""
            exit 0
        fi

        clear
    done
}

# Run main
main "$@"
