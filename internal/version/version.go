package version

const (
	Version = "3.1.1"
	Name    = "LTS"
)

// Source identifies the build channel. Official release binaries are stamped
// via -ldflags "-X lts-revamp/internal/version.Source=release" (see
// .goreleaser.yaml); anything built by hand from a clone stays "dev".
var Source = "dev"

// IsDev reports whether this binary was built outside the release pipeline.
func IsDev() bool {
	return Source != "release"
}

// Display returns the version with a dev marker when applicable, e.g. "3.1.1 (dev)".
func Display() string {
	if IsDev() {
		return Version + " (dev)"
	}
	return Version
}

// Full returns e.g. "LTS v3.1.1 (dev)".
func Full() string {
	return Name + " v" + Display()
}
