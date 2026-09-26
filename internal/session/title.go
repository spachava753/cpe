package session

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const titleEntry = "title"

// Title is nonempty, single-line session metadata, limited to 120 Unicode runes.
// Its zero value means the session has not been titled yet.
type Title struct{ text string }

func ParseTitle(text string) (Title, error) {
	text = strings.TrimSpace(text)
	if text == "" || !utf8.ValidString(text) || utf8.RuneCountInString(text) > 120 || strings.ContainsFunc(text, func(r rune) bool { return unicode.IsControl(r) || r == '\u2028' || r == '\u2029' }) {
		return Title{}, errors.New("session title must be 1–120 characters on one line, without control characters")
	}
	return Title{text}, nil
}

func (t Title) String() string { return t.text }

func decodeTitle(data []byte) (Title, error) {
	var value struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return Title{}, err
	}
	return ParseTitle(value.Title)
}

// Title returns the most recent title across all branches of this session.
func (s *Store) Title() Title {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, entry := range slices.Backward(s.entries) {
		if entry.Type == titleEntry {
			title, _ := decodeTitle(entry.Data) // validated at the journal boundary
			return title
		}
	}
	return Title{}
}

// SetTitle appends synced metadata without changing the file's identity.
func (s *Store) SetTitle(title Title) error {
	_, err := s.Append(titleEntry, struct {
		Title string `json:"title"`
	}{title.String()})
	return err
}
