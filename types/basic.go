package types

import (
	"net/url"
	"path/filepath"
	"strings"

	"github.com/gobwas/glob"
)

// Position in a text document expressed as zero-based line and character offset.
// A position is between two characters like an 'insert' cursor in an editor.
type Position struct {
	// Line position in a document (zero-based).
	Line uint32 `json:"line"`

	// Character offset on a line in a document (zero-based).
	Character uint32 `json:"character"`
}

// NewPosition creates a new Position
func NewPosition(line, character uint32) Position {
	return Position{
		Line:      line,
		Character: character,
	}
}

// Before returns true if this position is before the other position
func (p Position) Before(other Position) bool {
	if p.Line < other.Line {
		return true
	}
	if p.Line > other.Line {
		return false
	}
	return p.Character < other.Character
}

// After returns true if this position is after the other position
func (p Position) After(other Position) bool {
	if p.Line > other.Line {
		return true
	}
	if p.Line < other.Line {
		return false
	}
	return p.Character > other.Character
}

// Equal returns true if this position is equal to the other position
func (p Position) Equal(other Position) bool {
	return p.Line == other.Line && p.Character == other.Character
}

// Range in a text document expressed as (zero-based) start and end positions.
// A range is comparable to a selection in an editor. Therefore the end position is exclusive.
type Range struct {
	// The range's start position.
	Start Position `json:"start"`

	// The range's end position.
	End Position `json:"end"`
}

// NewRange creates a new Range
func NewRange(start, end Position) Range {
	return Range{
		Start: start,
		End:   end,
	}
}

// Contains returns true if the given position is contained in the range
func (r Range) Contains(p Position) bool {
	return (p.After(r.Start) || p.Equal(r.Start)) &&
		(p.Before(r.End) || p.Equal(r.End))
}

// Overlaps returns true if this range overlaps with the other range
func (r Range) Overlaps(other Range) bool {
	return r.Contains(other.Start) || r.Contains(other.End) ||
		other.Contains(r.Start) || other.Contains(r.End)
}

// IsEmpty returns true if the range is empty (start and end are the same)
func (r Range) IsEmpty() bool {
	return r.Start.Equal(r.End)
}

// IsSingleLine returns true if the range is on a single line
func (r Range) IsSingleLine() bool {
	return r.Start.Line == r.End.Line
}

// TextDocumentItem represents an open text document in the client.
// It contains all the information needed for the initial opening of a document.
type TextDocumentItem struct {
	// The text document's URI.
	URI string `json:"uri"`

	// The text document's language identifier.
	LanguageID string `json:"languageId"`

	// The version number of this document (it will increase after each
	// change, including undo/redo).
	Version int32 `json:"version"`

	// The content of the opened text document.
	Text string `json:"text"`
}

// NewTextDocumentItem creates a new TextDocumentItem
func NewTextDocumentItem(uri, languageID string, version int32, text string) TextDocumentItem {
	return TextDocumentItem{
		URI:        uri,
		LanguageID: languageID,
		Version:    version,
		Text:       text,
	}
}

// TextDocumentIdentifier is a lightweight representation of a TextDocumentItem,
// containing only the URI.
type TextDocumentIdentifier struct {
	// The text document's URI.
	URI string `json:"uri"`
}

// NewTextDocumentIdentifier creates a new TextDocumentIdentifier
func NewTextDocumentIdentifier(uri string) TextDocumentIdentifier {
	return TextDocumentIdentifier{
		URI: uri,
	}
}

// VersionedTextDocumentIdentifier is a TextDocumentIdentifier with a version number.
type VersionedTextDocumentIdentifier struct {
	// The text document's URI.
	URI string `json:"uri"`

	// The version number of this document.
	Version int32 `json:"version"`
}

// NewVersionedTextDocumentIdentifier creates a new VersionedTextDocumentIdentifier
func NewVersionedTextDocumentIdentifier(uri string, version int32) VersionedTextDocumentIdentifier {
	return VersionedTextDocumentIdentifier{
		URI:     uri,
		Version: version,
	}
}

// TextDocumentPositionParams is a parameter literal used in requests to pass a text document and a position inside that document.
type TextDocumentPositionParams struct {
	// The text document.
	TextDocument TextDocumentIdentifier `json:"textDocument"`

	// The position inside the text document.
	Position Position `json:"position"`
}

// NewTextDocumentPositionParams creates a new TextDocumentPositionParams
func NewTextDocumentPositionParams(uri string, line, character uint32) TextDocumentPositionParams {
	return TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: line, Character: character},
	}
}

// TextDocumentPositionParamsWithVersion is an extension of TextDocumentPositionParams that includes the document version.
// This is not a standard LSP type, but it can be useful in scenarios where the version is needed.
type TextDocumentPositionParamsWithVersion struct {
	// The text document.
	TextDocument VersionedTextDocumentIdentifier `json:"textDocument"`

	// The position inside the text document.
	Position Position `json:"position"`
}

// NewTextDocumentPositionParamsWithVersion creates a new TextDocumentPositionParamsWithVersion
func NewTextDocumentPositionParamsWithVersion(
	uri string, version int32, line, character uint32,
) TextDocumentPositionParamsWithVersion {
	return TextDocumentPositionParamsWithVersion{
		TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: version},
		Position:     Position{Line: line, Character: character},
	}
}

// DocumentFilter denotes a document through properties like language, scheme or pattern.
type DocumentFilter struct {
	// A language id, like `typescript`.
	Language string `json:"language,omitempty"`

	// A Uri scheme, like `file` or `untitled`.
	Scheme string `json:"scheme,omitempty"`

	// A glob pattern, like `*.{ts,js}`.
	Pattern string `json:"pattern,omitempty"`
}

// NewDocumentFilter creates a new DocumentFilter
func NewDocumentFilter(language, scheme, pattern string) DocumentFilter {
	return DocumentFilter{
		Language: language,
		Scheme:   scheme,
		Pattern:  pattern,
	}
}

// Matches checks if a given document URI and language match this filter
func (df DocumentFilter) Matches(uri, language string) bool {
	// If Language is specified, it must match
	if df.Language != "" && df.Language != language {
		return false
	}

	// If Scheme is specified, it must match the URI scheme
	if df.Scheme != "" {
		scheme, _ := splitSchemeURI(uri)
		if df.Scheme != scheme {
			return false
		}
	}

	// If Pattern is specified, it must match the URI
	if df.Pattern != "" && !matchGlob(df.Pattern, uri) {
		return false
	}

	return true
}

// DocumentSelector is an array of DocumentFilter
type DocumentSelector []DocumentFilter

// Matches checks if a given document URI and language match any of the filters in the selector
func (ds DocumentSelector) Matches(uri, language string) bool {
	for _, filter := range ds {
		if filter.Matches(uri, language) {
			return true
		}
	}
	return false
}

// Helper functions

// splitSchemeURI splits a URI into its scheme and path
func splitSchemeURI(uri string) (scheme, path string) {
	parsedURI, err := url.Parse(uri)
	if err != nil {
		return "", uri // If parsing fails, return empty scheme and the original URI as path
	}
	return parsedURI.Scheme, parsedURI.Path
}

// matchGlob checks if a string matches a glob pattern
func matchGlob(pattern, str string) bool {
	// Convert Windows-style paths to Unix-style for consistent matching
	pattern = filepath.ToSlash(pattern)
	str = filepath.ToSlash(str)

	// Remove the scheme from the URI if present
	if strings.Contains(str, "://") {
		parts := strings.SplitN(str, "://", 2)
		if len(parts) == 2 {
			str = parts[1]
		}
	}

	// Create a glob matcher
	g, err := glob.Compile(pattern)
	if err != nil {
		return false // If the pattern is invalid, assume no match
	}

	return g.Match(str)
}

// TextDocumentEdit represents a change to a text document.
type TextDocumentEdit struct {
	// The text document to change.
	TextDocument OptionalVersionedTextDocumentIdentifier `json:"textDocument"`

	// The edits to be applied.
	Edits []TextEdit `json:"edits"`
}

// NewTextDocumentEdit creates a new TextDocumentEdit
func NewTextDocumentEdit(textDocument OptionalVersionedTextDocumentIdentifier, edits []TextEdit) TextDocumentEdit {
	return TextDocumentEdit{
		TextDocument: textDocument,
		Edits:        edits,
	}
}

// TextEdit represents a textual edit applicable to a text document.
type TextEdit struct {
	// The range of the text document to be manipulated. To insert
	// text into a document create a range where start === end.
	Range Range `json:"range"`

	// The string to be inserted. For delete operations use an
	// empty string.
	NewText string `json:"newText"`
}

// NewTextEdit creates a new TextEdit
func NewTextEdit(r Range, newText string) TextEdit {
	return TextEdit{
		Range:   r,
		NewText: newText,
	}
}

// IsInsert returns true if this edit is an insertion (i.e., start === end)
func (te TextEdit) IsInsert() bool {
	return te.Range.Start == te.Range.End
}

// IsDelete returns true if this edit is a deletion (i.e., newText is empty)
func (te TextEdit) IsDelete() bool {
	return te.NewText == ""
}

// OptionalVersionedTextDocumentIdentifier represents a text document identifier that
// might optionally contain a version number.
type OptionalVersionedTextDocumentIdentifier struct {
	// The text document's URI.
	URI string `json:"uri"`

	// The version number of this document. If a versioned text document identifier
	// is sent from the server to the client and the file is not open in the editor
	// (the server has not received an open notification before) the server can send
	// `null` to indicate that the version is unknown and the content on disk is the
	// truth (as specified with document content ownership).
	Version *int32 `json:"version"`
}

// NewOptionalVersionedTextDocumentIdentifier creates a new OptionalVersionedTextDocumentIdentifier
func NewOptionalVersionedTextDocumentIdentifier(uri string, version *int32) OptionalVersionedTextDocumentIdentifier {
	return OptionalVersionedTextDocumentIdentifier{
		URI:     uri,
		Version: version,
	}
}

// Location represents a location inside a resource, such as a line inside a text file.
type Location struct {
	// The text document's URI.
	URI string `json:"uri"`

	// The range inside the text document.
	Range Range `json:"range"`
}

// NewLocation creates a new Location
func NewLocation(uri string, r Range) Location {
	return Location{
		URI:   uri,
		Range: r,
	}
}

// LocationLink represents a link between a source and a target location.
type LocationLink struct {
	// Span of the origin of this link.
	//
	// Used as the underlined span for mouse interaction. Defaults to the word range at
	// the mouse position.
	OriginSelectionRange *Range `json:"originSelectionRange,omitempty"`

	// The target resource identifier of this link.
	TargetURI string `json:"targetUri"`

	// The full target range of this link. If the target for example is a symbol then
	// target range is the range enclosing this symbol not including leading/trailing
	// whitespace but everything else like comments. This information is typically used
	// to highlight the range in the editor.
	TargetRange Range `json:"targetRange"`

	// The range that should be selected and revealed when this link is being followed,
	// e.g. the name of a function. Must be contained by the `targetRange`.
	TargetSelectionRange Range `json:"targetSelectionRange"`
}

// NewLocationLink creates a new LocationLink
func NewLocationLink(
	targetURI string, targetRange, targetSelectionRange Range, originSelectionRange *Range,
) LocationLink {
	return LocationLink{
		OriginSelectionRange: originSelectionRange,
		TargetURI:            targetURI,
		TargetRange:          targetRange,
		TargetSelectionRange: targetSelectionRange,
	}
}

// Command represents a reference to a command. Provides a title which
// will be used to represent a command in the UI and, optionally,
// an array of arguments which will be passed to the command handler
// function when invoked.
type Command struct {
	// Title of the command, like `save`.
	Title string `json:"title"`

	// The identifier of the actual command handler.
	Command string `json:"command"`

	// Arguments that the command handler should be
	// invoked with.
	Arguments []interface{} `json:"arguments,omitempty"`
}

// NewCommand creates a new Command
func NewCommand(title, command string, arguments ...interface{}) Command {
	return Command{
		Title:     title,
		Command:   command,
		Arguments: arguments,
	}
}

// MarkupKind represents the format of the markup content.
type MarkupKind string

const (
	// PlainText represents plain text content.
	PlainText MarkupKind = "plaintext"

	// Markdown represents Markdown content.
	Markdown MarkupKind = "markdown"
)

// MarkupContent represents a human-readable string that supports different formats.
type MarkupContent struct {
	// The type of markup content.
	Kind MarkupKind `json:"kind"`

	// The actual content.
	Value string `json:"value"`
}

// NewMarkupContent creates a new MarkupContent
func NewMarkupContent(kind MarkupKind, value string) MarkupContent {
	return MarkupContent{
		Kind:  kind,
		Value: value,
	}
}

// IsPlainText returns true if the content is plain text
func (m MarkupContent) IsPlainText() bool {
	return m.Kind == PlainText
}

// IsMarkdown returns true if the content is Markdown
func (m MarkupContent) IsMarkdown() bool {
	return m.Kind == Markdown
}

// CreateFileOptions represents options to create a file.
type CreateFileOptions struct {
	// Overwrite existing file. Overwrite wins over `ignoreIfExists`
	Overwrite *bool `json:"overwrite,omitempty"`

	// Ignore if exists.
	IgnoreIfExists *bool `json:"ignoreIfExists,omitempty"`
}

// CreateFile represents a create file operation.
type CreateFile struct {
	// A create
	Kind string `json:"kind"`

	// The resource to create.
	URI string `json:"uri"`

	// Additional options
	Options *CreateFileOptions `json:"options,omitempty"`

	// An optional annotation identifier describing the operation.
	AnnotationID *string `json:"annotationId,omitempty"`
}

// RenameFileOptions represents options to rename a file.
type RenameFileOptions struct {
	// Overwrite target if existing. Overwrite wins over `ignoreIfExists`
	Overwrite *bool `json:"overwrite,omitempty"`

	// Ignores if target exists.
	IgnoreIfExists *bool `json:"ignoreIfExists,omitempty"`
}

// RenameFile represents a rename file operation.
type RenameFile struct {
	// A rename
	Kind string `json:"kind"`

	// The old (existing) location.
	OldURI string `json:"oldUri"`

	// The new location.
	NewURI string `json:"newUri"`

	// Rename options.
	Options *RenameFileOptions `json:"options,omitempty"`

	// An optional annotation identifier describing the operation.
	AnnotationID *string `json:"annotationId,omitempty"`
}

// DeleteFileOptions represents options to delete a file.
type DeleteFileOptions struct {
	// Delete the content recursively if a folder is denoted.
	Recursive *bool `json:"recursive,omitempty"`

	// Ignore the operation if the file doesn't exist.
	IgnoreIfNotExists *bool `json:"ignoreIfNotExists,omitempty"`
}

// DeleteFile represents a delete file operation.
type DeleteFile struct {
	// A delete
	Kind string `json:"kind"`

	// The file to delete.
	URI string `json:"uri"`

	// Delete options.
	Options *DeleteFileOptions `json:"options,omitempty"`

	// An optional annotation identifier describing the operation.
	AnnotationID *string `json:"annotationId,omitempty"`
}

// NewCreateFile creates a new CreateFile operation
func NewCreateFile(uri string, options *CreateFileOptions, annotationID *string) CreateFile {
	return CreateFile{
		Kind:         "create",
		URI:          uri,
		Options:      options,
		AnnotationID: annotationID,
	}
}

// NewRenameFile creates a new RenameFile operation
func NewRenameFile(oldURI, newURI string, options *RenameFileOptions, annotationID *string) RenameFile {
	return RenameFile{
		Kind:         "rename",
		OldURI:       oldURI,
		NewURI:       newURI,
		Options:      options,
		AnnotationID: annotationID,
	}
}

// NewDeleteFile creates a new DeleteFile operation
func NewDeleteFile(uri string, options *DeleteFileOptions, annotationID *string) DeleteFile {
	return DeleteFile{
		Kind:         "delete",
		URI:          uri,
		Options:      options,
		AnnotationID: annotationID,
	}
}

// WorkspaceEdit represents changes to many resources managed in the workspace.
type WorkspaceEdit struct {
	// Holds changes to existing resources.
	Changes map[string][]TextEdit `json:"changes,omitempty"`

	// Depending on the client capability `workspace.workspaceEdit.resourceOperations`
	// document changes are either an array of `TextDocumentEdit`s to express
	// changes to n different text documents where each text document edit
	// addresses a specific version of a text document. Or it can contain
	// above `TextDocumentEdit`s mixed with create, rename and delete
	// file / folder operations.
	DocumentChanges []DocumentChange `json:"documentChanges,omitempty"`
}

// DocumentChange represents a change to a text document or a file/folder operation.
type DocumentChange struct {
	TextDocumentEdit *TextDocumentEdit `json:"textDocumentEdit,omitempty"`
	CreateFile       *CreateFile       `json:"createFile,omitempty"`
	RenameFile       *RenameFile       `json:"renameFile,omitempty"`
	DeleteFile       *DeleteFile       `json:"deleteFile,omitempty"`
}

// NewWorkspaceEdit creates a new WorkspaceEdit
func NewWorkspaceEdit() *WorkspaceEdit {
	return &WorkspaceEdit{
		Changes:         make(map[string][]TextEdit),
		DocumentChanges: []DocumentChange{},
	}
}

// AddTextDocumentEdit adds a TextDocumentEdit to the WorkspaceEdit
func (w *WorkspaceEdit) AddTextDocumentEdit(textDocumentEdit TextDocumentEdit) {
	w.DocumentChanges = append(
		w.DocumentChanges, DocumentChange{
			TextDocumentEdit: &textDocumentEdit,
		},
	)
}

// AddFileOperation adds a file operation (create, rename, delete) to the WorkspaceEdit
func (w *WorkspaceEdit) AddFileOperation(op interface{}) {
	switch o := op.(type) {
	case CreateFile:
		w.DocumentChanges = append(w.DocumentChanges, DocumentChange{CreateFile: &o})
	case RenameFile:
		w.DocumentChanges = append(w.DocumentChanges, DocumentChange{RenameFile: &o})
	case DeleteFile:
		w.DocumentChanges = append(w.DocumentChanges, DocumentChange{DeleteFile: &o})
	}
}

// AddTextEdit adds a TextEdit to the Changes map
func (w *WorkspaceEdit) AddTextEdit(uri string, edit TextEdit) {
	if w.Changes == nil {
		w.Changes = make(map[string][]TextEdit)
	}
	w.Changes[uri] = append(w.Changes[uri], edit)
}
