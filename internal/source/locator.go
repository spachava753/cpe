package source

// GitLocator identifies a Git source accepted by GitClient.
// Construct one with ParseGitLocator; its zero value is not a source.
type GitLocator struct {
	url string
}

// ParseGitLocator parses a Git URL or GitHub owner/repository shorthand.
func ParseGitLocator(locator string) (GitLocator, error) {
	resolved, err := gitLocator(locator)
	if err != nil {
		return GitLocator{}, err
	}
	return GitLocator{
		url: resolved,
	}, nil
}

// String returns the source's Git URL, or an empty string for the zero value.
func (locator GitLocator) String() string {
	return locator.url
}
