package types

import (
	"encoding/json"
	"fmt"
)

// ServerCapabilities represents the capabilities provided by the language server.
type ServerCapabilities struct {
	PositionEncoding                 PositionEncodingKind             `json:"positionEncoding,omitempty"`
	TextDocumentSync                 TextDocumentSync                 `json:"textDocumentSync,omitempty"`
	NotebookDocumentSync             *NotebookDocumentSyncOptions     `json:"notebookDocumentSync,omitempty"`
	CompletionProvider               *CompletionOptions               `json:"completionProvider,omitempty"`
	HoverProvider                    HoverProvider                    `json:"hoverProvider,omitempty"`
	SignatureHelpProvider            *SignatureHelpOptions            `json:"signatureHelpProvider,omitempty"`
	DeclarationProvider              DeclarationProvider              `json:"declarationProvider,omitempty"`
	DefinitionProvider               DefinitionProvider               `json:"definitionProvider,omitempty"`
	TypeDefinitionProvider           TypeDefinitionProvider           `json:"typeDefinitionProvider,omitempty"`
	ImplementationProvider           ImplementationProvider           `json:"implementationProvider,omitempty"`
	ReferencesProvider               ReferenceProvider                `json:"referencesProvider,omitempty"`
	DocumentHighlightProvider        DocumentHighlightProvider        `json:"documentHighlightProvider,omitempty"`
	DocumentSymbolProvider           DocumentSymbolProvider           `json:"documentSymbolProvider,omitempty"`
	CodeActionProvider               CodeActionProvider               `json:"codeActionProvider,omitempty"`
	CodeLensProvider                 *CodeLensOptions                 `json:"codeLensProvider,omitempty"`
	DocumentLinkProvider             *DocumentLinkOptions             `json:"documentLinkProvider,omitempty"`
	ColorProvider                    DocumentColorProvider            `json:"colorProvider,omitempty"`
	DocumentFormattingProvider       DocumentFormattingProvider       `json:"documentFormattingProvider,omitempty"`
	DocumentRangeFormattingProvider  DocumentRangeFormattingProvider  `json:"documentRangeFormattingProvider,omitempty"`
	DocumentOnTypeFormattingProvider *DocumentOnTypeFormattingOptions `json:"documentOnTypeFormattingProvider,omitempty"`
	RenameProvider                   RenameProvider                   `json:"renameProvider,omitempty"`
	FoldingRangeProvider             FoldingRangeProvider             `json:"foldingRangeProvider,omitempty"`
	ExecuteCommandProvider           *ExecuteCommandOptions           `json:"executeCommandProvider,omitempty"`
	SelectionRangeProvider           SelectionRangeProvider           `json:"selectionRangeProvider,omitempty"`
	LinkedEditingRangeProvider       LinkedEditingRangeProvider       `json:"linkedEditingRangeProvider,omitempty"`
	CallHierarchyProvider            CallHierarchyProvider            `json:"callHierarchyProvider,omitempty"`
	SemanticTokensProvider           SemanticTokensProvider           `json:"semanticTokensProvider,omitempty"`
	MonikerProvider                  MonikerProvider                  `json:"monikerProvider,omitempty"`
	TypeHierarchyProvider            TypeHierarchyProvider            `json:"typeHierarchyProvider,omitempty"`
	InlineValueProvider              InlineValueProvider              `json:"inlineValueProvider,omitempty"`
	InlayHintProvider                InlayHintProvider                `json:"inlayHintProvider,omitempty"`
	DiagnosticProvider               DiagnosticProvider               `json:"diagnosticProvider,omitempty"`
	WorkspaceSymbolProvider          WorkspaceSymbolProvider          `json:"workspaceSymbolProvider,omitempty"`
	Workspace                        *ServerCapabilitiesWorkspace     `json:"workspace,omitempty"`
	Experimental                     interface{}                      `json:"experimental,omitempty"`
}

// NewServerCapabilities creates a new ServerCapabilities with default values.
func NewServerCapabilities() *ServerCapabilities {
	return &ServerCapabilities{
		PositionEncoding: PositionEncodingKindUTF16, // Default to UTF-16
		TextDocumentSync: TextDocumentSync{Value: TextDocumentSyncKindNone},
		Workspace:        &ServerCapabilitiesWorkspace{},
	}
}

// TextDocumentSyncOptions represents the options for text document synchronization.
type TextDocumentSyncOptions struct {
	OpenClose         *bool                 `json:"openClose,omitempty"`
	Change            *TextDocumentSyncKind `json:"change,omitempty"`
	WillSave          *bool                 `json:"willSave,omitempty"`
	WillSaveWaitUntil *bool                 `json:"willSaveWaitUntil,omitempty"`
	Save              *SaveOptions          `json:"save,omitempty"`
}

// SaveOptions represents options for saving documents.
type SaveOptions struct {
	// IncludeText is a flag that indicates whether the client should
	// include the content of the document when saving.
	IncludeText bool `json:"includeText,omitempty"`
}

// NewSaveOptions creates a new SaveOptions with default values.
func NewSaveOptions() *SaveOptions {
	return &SaveOptions{
		IncludeText: false,
	}
}

// TextDocumentSyncKind represents the synchronization kind for text documents.
type TextDocumentSyncKind int

const (
	TextDocumentSyncKindNone        TextDocumentSyncKind = 0
	TextDocumentSyncKindFull        TextDocumentSyncKind = 1
	TextDocumentSyncKindIncremental TextDocumentSyncKind = 2
)

// TextDocumentSync is a struct that can represent either TextDocumentSyncOptions or TextDocumentSyncKind.
type TextDocumentSync struct {
	Value interface{}
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (tds *TextDocumentSync) UnmarshalJSON(data []byte) error {
	// First, try to unmarshal as TextDocumentSyncKind
	var kind TextDocumentSyncKind
	if err := json.Unmarshal(data, &kind); err == nil {
		tds.Value = kind
		return nil
	}

	// If that fails, try to unmarshal as TextDocumentSyncOptions
	var options TextDocumentSyncOptions
	if err := json.Unmarshal(data, &options); err == nil {
		tds.Value = options
		return nil
	}

	// If both fail, return an error
	return fmt.Errorf("failed to unmarshal TextDocumentSync: %s", string(data))
}

// MarshalJSON implements the json.Marshaler interface.
func (tds TextDocumentSync) MarshalJSON() ([]byte, error) {
	return json.Marshal(tds.Value)
}

// IsKind returns true if the TextDocumentSync represents a TextDocumentSyncKind.
func (tds TextDocumentSync) IsKind() bool {
	_, ok := tds.Value.(TextDocumentSyncKind)
	return ok
}

// IsOptions returns true if the TextDocumentSync represents TextDocumentSyncOptions.
func (tds TextDocumentSync) IsOptions() bool {
	_, ok := tds.Value.(TextDocumentSyncOptions)
	return ok
}

// Kind returns the TextDocumentSyncKind if it represents a kind, or TextDocumentSyncKindNone otherwise.
func (tds TextDocumentSync) Kind() TextDocumentSyncKind {
	if kind, ok := tds.Value.(TextDocumentSyncKind); ok {
		return kind
	}
	return TextDocumentSyncKindNone
}

// Options returns the TextDocumentSyncOptions if it represents options, or nil otherwise.
func (tds TextDocumentSync) Options() *TextDocumentSyncOptions {
	if options, ok := tds.Value.(TextDocumentSyncOptions); ok {
		return &options
	}
	return nil
}

// HoverOptions represents options for hover support.
type HoverOptions struct {
	WorkDoneProgressOptions
}

// HoverProvider is a struct that can represent either a bool or HoverOptions.
type HoverProvider struct {
	Value interface{}
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (hp *HoverProvider) UnmarshalJSON(data []byte) error {
	// First, try to unmarshal as bool
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		hp.Value = b
		return nil
	}

	// If that fails, try to unmarshal as HoverOptions
	var options HoverOptions
	if err := json.Unmarshal(data, &options); err == nil {
		hp.Value = options
		return nil
	}

	// If both fail, return an error
	return fmt.Errorf("failed to unmarshal HoverProvider: %s", string(data))
}

// MarshalJSON implements the json.Marshaler interface.
func (hp HoverProvider) MarshalJSON() ([]byte, error) {
	return json.Marshal(hp.Value)
}

// IsBool returns true if the HoverProvider represents a bool.
func (hp HoverProvider) IsBool() bool {
	_, ok := hp.Value.(bool)
	return ok
}

// IsOptions returns true if the HoverProvider represents HoverOptions.
func (hp HoverProvider) IsOptions() bool {
	_, ok := hp.Value.(HoverOptions)
	return ok
}

// Bool returns the bool value if it represents a bool, or false otherwise.
func (hp HoverProvider) Bool() bool {
	if b, ok := hp.Value.(bool); ok {
		return b
	}
	return false
}

// Options returns the HoverOptions if it represents options, or nil otherwise.
func (hp HoverProvider) Options() *HoverOptions {
	if options, ok := hp.Value.(HoverOptions); ok {
		return &options
	}
	return nil
}

// NotebookDocumentSyncOptions represents options for notebook document synchronization.
type NotebookDocumentSyncOptions struct {
	NotebookSelector []NotebookDocumentSyncOptionsNotebookSelector `json:"notebookSelector"`
	Save             bool                                          `json:"save,omitempty"`
}

// NotebookDocumentSyncOptionsNotebookSelector represents a notebook selector in NotebookDocumentSyncOptions.
type NotebookDocumentSyncOptionsNotebookSelector struct {
	Notebook string                                                     `json:"notebook,omitempty"`
	Cells    []NotebookDocumentSyncOptionsNotebookSelectorCellsSelector `json:"cells,omitempty"`
}

// NotebookDocumentSyncOptionsNotebookSelectorCellsSelector represents a cells selector in NotebookDocumentSyncOptionsNotebookSelector.
type NotebookDocumentSyncOptionsNotebookSelectorCellsSelector struct {
	Language string `json:"language"`
}

// NotebookDocumentSyncRegistrationOptions represents registration options for notebook document synchronization.
type NotebookDocumentSyncRegistrationOptions struct {
	NotebookDocumentSyncOptions
	StaticRegistrationOptions
}

// NotebookDocumentSync is a struct that can represent either NotebookDocumentSyncOptions or NotebookDocumentSyncRegistrationOptions.
type NotebookDocumentSync struct {
	Value interface{}
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (nds *NotebookDocumentSync) UnmarshalJSON(data []byte) error {
	// First, try to unmarshal as NotebookDocumentSyncOptions
	var options NotebookDocumentSyncOptions
	if err := json.Unmarshal(data, &options); err == nil {
		nds.Value = options
		return nil
	}

	// If that fails, try to unmarshal as NotebookDocumentSyncRegistrationOptions
	var regOptions NotebookDocumentSyncRegistrationOptions
	if err := json.Unmarshal(data, &regOptions); err == nil {
		nds.Value = regOptions
		return nil
	}

	// If both fail, return an error
	return fmt.Errorf("failed to unmarshal NotebookDocumentSync: %s", string(data))
}

// MarshalJSON implements the json.Marshaler interface.
func (nds NotebookDocumentSync) MarshalJSON() ([]byte, error) {
	return json.Marshal(nds.Value)
}

// IsOptions returns true if the NotebookDocumentSync represents NotebookDocumentSyncOptions.
func (nds NotebookDocumentSync) IsOptions() bool {
	_, ok := nds.Value.(NotebookDocumentSyncOptions)
	return ok
}

// IsRegistrationOptions returns true if the NotebookDocumentSync represents NotebookDocumentSyncRegistrationOptions.
func (nds NotebookDocumentSync) IsRegistrationOptions() bool {
	_, ok := nds.Value.(NotebookDocumentSyncRegistrationOptions)
	return ok
}

// Options returns the NotebookDocumentSyncOptions if it represents options, or nil otherwise.
func (nds NotebookDocumentSync) Options() *NotebookDocumentSyncOptions {
	if options, ok := nds.Value.(NotebookDocumentSyncOptions); ok {
		return &options
	}
	return nil
}

// RegistrationOptions returns the NotebookDocumentSyncRegistrationOptions if it represents registration options, or nil otherwise.
func (nds NotebookDocumentSync) RegistrationOptions() *NotebookDocumentSyncRegistrationOptions {
	if regOptions, ok := nds.Value.(NotebookDocumentSyncRegistrationOptions); ok {
		return &regOptions
	}
	return nil
}

// TextDocumentRegistrationOptions describes options to be used when
// registering for text document change events.
type TextDocumentRegistrationOptions struct {
	// DocumentSelector is an optional document selector to identify the scope
	// of the registration. If no selector is provided the registration applies
	// to all documents. For backwards compatibility, a null value is treated
	// like an empty object.
	DocumentSelector DocumentSelector `json:"documentSelector"`
}

// StaticRegistrationOptions describes options to be used when registering
// for static registration.
type StaticRegistrationOptions struct {
	// ID is the id used to register the request. The id can be used to deregister
	// the request again. See also Registration#id.
	ID string `json:"id,omitempty"`
}

// NewTextDocumentRegistrationOptions creates a new TextDocumentRegistrationOptions
// with the given DocumentSelector.
func NewTextDocumentRegistrationOptions(selector DocumentSelector) TextDocumentRegistrationOptions {
	return TextDocumentRegistrationOptions{
		DocumentSelector: selector,
	}
}

// NewStaticRegistrationOptions creates a new StaticRegistrationOptions
// with the given ID.
func NewStaticRegistrationOptions(id string) StaticRegistrationOptions {
	return StaticRegistrationOptions{
		ID: id,
	}
}

// DeclarationProvider represents a declaration provider that can be either a boolean,
// DeclarationOptions, or DeclarationRegistrationOptions.
type DeclarationProvider struct {
	Value interface{}
}

// DeclarationOptions represents the options for declaration support.
type DeclarationOptions struct {
	WorkDoneProgressOptions
}

// DeclarationRegistrationOptions represents the registration options for declaration support.
type DeclarationRegistrationOptions struct {
	DeclarationOptions
	TextDocumentRegistrationOptions
	StaticRegistrationOptions
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (dp *DeclarationProvider) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as boolean
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		dp.Value = b
		return nil
	}

	// Try to unmarshal as DeclarationOptions
	var options DeclarationOptions
	if err := json.Unmarshal(data, &options); err == nil {
		dp.Value = options
		return nil
	}

	// Try to unmarshal as DeclarationRegistrationOptions
	var regOptions DeclarationRegistrationOptions
	if err := json.Unmarshal(data, &regOptions); err == nil {
		dp.Value = regOptions
		return nil
	}

	// If all attempts fail, return an error
	return fmt.Errorf("failed to unmarshal DeclarationProvider: %s", string(data))
}

// MarshalJSON implements the json.Marshaler interface.
func (dp DeclarationProvider) MarshalJSON() ([]byte, error) {
	return json.Marshal(dp.Value)
}

// IsBool returns true if the DeclarationProvider represents a bool.
func (dp DeclarationProvider) IsBool() bool {
	_, ok := dp.Value.(bool)
	return ok
}

// IsOptions returns true if the DeclarationProvider represents DeclarationOptions.
func (dp DeclarationProvider) IsOptions() bool {
	_, ok := dp.Value.(DeclarationOptions)
	return ok
}

// IsRegistrationOptions returns true if the DeclarationProvider represents DeclarationRegistrationOptions.
func (dp DeclarationProvider) IsRegistrationOptions() bool {
	_, ok := dp.Value.(DeclarationRegistrationOptions)
	return ok
}

// Bool returns the bool value if it represents a bool, or false otherwise.
func (dp DeclarationProvider) Bool() bool {
	if b, ok := dp.Value.(bool); ok {
		return b
	}
	return false
}

// Options returns the DeclarationOptions if it represents options, or nil otherwise.
func (dp DeclarationProvider) Options() *DeclarationOptions {
	if options, ok := dp.Value.(DeclarationOptions); ok {
		return &options
	}
	return nil
}

// RegistrationOptions returns the DeclarationRegistrationOptions if it represents registration options, or nil otherwise.
func (dp DeclarationProvider) RegistrationOptions() *DeclarationRegistrationOptions {
	if regOptions, ok := dp.Value.(DeclarationRegistrationOptions); ok {
		return &regOptions
	}
	return nil
}

// ServerCapabilitiesWorkspace represents workspace-specific server capabilities.
type ServerCapabilitiesWorkspace struct {
	WorkspaceFolders *WorkspaceFoldersServerCapabilities `json:"workspaceFolders,omitempty"`
	FileOperations   *FileOperationOptions               `json:"fileOperations,omitempty"`
}

// WorkspaceFoldersServerCapabilities represents the server's capabilities for workspace folders.
type WorkspaceFoldersServerCapabilities struct {
	Supported           bool   `json:"supported,omitempty"`
	ChangeNotifications string `json:"changeNotifications,omitempty"`
}

// FileOperationOptions represents the server's capabilities for file operations.
type FileOperationOptions struct {
	DidCreate  *FileOperationRegistrationOptions `json:"didCreate,omitempty"`
	WillCreate *FileOperationRegistrationOptions `json:"willCreate,omitempty"`
	DidRename  *FileOperationRegistrationOptions `json:"didRename,omitempty"`
	WillRename *FileOperationRegistrationOptions `json:"willRename,omitempty"`
	DidDelete  *FileOperationRegistrationOptions `json:"didDelete,omitempty"`
	WillDelete *FileOperationRegistrationOptions `json:"willDelete,omitempty"`
}

// FileOperationRegistrationOptions represents options for file operation registrations.
type FileOperationRegistrationOptions struct {
	Filters []FileOperationFilter `json:"filters"`
}

// FileOperationFilter represents a filter for file operations.
type FileOperationFilter struct {
	Scheme  string               `json:"scheme,omitempty"`
	Pattern FileOperationPattern `json:"pattern"`
}

// FileOperationPattern represents a pattern for file operations.
type FileOperationPattern struct {
	Glob    string                       `json:"glob"`
	Matches FileOperationPatternKind     `json:"matches,omitempty"`
	Options *FileOperationPatternOptions `json:"options,omitempty"`
}

// FileOperationPatternKind represents the kind of a file operation pattern.
type FileOperationPatternKind string

const (
	FileOperationPatternKindFile   FileOperationPatternKind = "file"
	FileOperationPatternKindFolder FileOperationPatternKind = "folder"
)

// FileOperationPatternOptions represents options for file operation patterns.
type FileOperationPatternOptions struct {
	IgnoreCase bool `json:"ignoreCase,omitempty"`
}

// CompletionOptions represents options for completion support.
type CompletionOptions struct {
	WorkDoneProgressOptions
	TriggerCharacters   []string `json:"triggerCharacters,omitempty"`
	AllCommitCharacters []string `json:"allCommitCharacters,omitempty"`
	ResolveProvider     bool     `json:"resolveProvider,omitempty"`
	CompletionItem      *struct {
		LabelDetailsSupport bool `json:"labelDetailsSupport,omitempty"`
	} `json:"completionItem,omitempty"`
}

// SignatureHelpOptions represents options for signature help support.
type SignatureHelpOptions struct {
	WorkDoneProgressOptions
	TriggerCharacters   []string `json:"triggerCharacters,omitempty"`
	RetriggerCharacters []string `json:"retriggerCharacters,omitempty"`
}

// CodeLensOptions represents options for code lens support.
type CodeLensOptions struct {
	WorkDoneProgressOptions
	ResolveProvider bool `json:"resolveProvider,omitempty"`
}

// DocumentLinkOptions represents options for document link support.
type DocumentLinkOptions struct {
	WorkDoneProgressOptions
	ResolveProvider bool `json:"resolveProvider,omitempty"`
}

// DocumentOnTypeFormattingOptions represents options for document formatting on typing.
type DocumentOnTypeFormattingOptions struct {
	FirstTriggerCharacter string   `json:"firstTriggerCharacter"`
	MoreTriggerCharacter  []string `json:"moreTriggerCharacter,omitempty"`
}

// ExecuteCommandOptions represents options for execute command support.
type ExecuteCommandOptions struct {
	WorkDoneProgressOptions
	Commands []string `json:"commands"`
}

// WorkDoneProgressOptions represents options for work done progress.
type WorkDoneProgressOptions struct {
	WorkDoneProgress bool `json:"workDoneProgress,omitempty"`
}

// DefinitionProvider represents a definition provider that can be either a boolean or DefinitionOptions.
type DefinitionProvider struct {
	Value interface{}
}

// DefinitionOptions represents options for definition support.
type DefinitionOptions struct {
	WorkDoneProgressOptions
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (dp *DefinitionProvider) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as boolean
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		dp.Value = b
		return nil
	}

	// Try to unmarshal as DefinitionOptions
	var options DefinitionOptions
	if err := json.Unmarshal(data, &options); err == nil {
		dp.Value = options
		return nil
	}

	// If both attempts fail, return an error
	return fmt.Errorf("failed to unmarshal DefinitionProvider: %s", string(data))
}

// MarshalJSON implements the json.Marshaler interface.
func (dp DefinitionProvider) MarshalJSON() ([]byte, error) {
	return json.Marshal(dp.Value)
}

// IsBool returns true if the DefinitionProvider represents a bool.
func (dp DefinitionProvider) IsBool() bool {
	_, ok := dp.Value.(bool)
	return ok
}

// IsOptions returns true if the DefinitionProvider represents DefinitionOptions.
func (dp DefinitionProvider) IsOptions() bool {
	_, ok := dp.Value.(DefinitionOptions)
	return ok
}

// Bool returns the bool value if it represents a bool, or false otherwise.
func (dp DefinitionProvider) Bool() bool {
	if b, ok := dp.Value.(bool); ok {
		return b
	}
	return false
}

// Options returns the DefinitionOptions if it represents options, or nil otherwise.
func (dp DefinitionProvider) Options() *DefinitionOptions {
	if options, ok := dp.Value.(DefinitionOptions); ok {
		return &options
	}
	return nil
}

// TypeDefinitionProvider represents a type definition provider that can be either a boolean,
// TypeDefinitionOptions, or TypeDefinitionRegistrationOptions.
type TypeDefinitionProvider struct {
	Value interface{}
}

// TypeDefinitionOptions represents options for type definition support.
type TypeDefinitionOptions struct {
	WorkDoneProgressOptions
}

// TypeDefinitionRegistrationOptions represents registration options for type definition support.
type TypeDefinitionRegistrationOptions struct {
	TypeDefinitionOptions
	TextDocumentRegistrationOptions
	StaticRegistrationOptions
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (tdp *TypeDefinitionProvider) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as boolean
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		tdp.Value = b
		return nil
	}

	// Try to unmarshal as TypeDefinitionOptions
	var options TypeDefinitionOptions
	if err := json.Unmarshal(data, &options); err == nil {
		tdp.Value = options
		return nil
	}

	// Try to unmarshal as TypeDefinitionRegistrationOptions
	var regOptions TypeDefinitionRegistrationOptions
	if err := json.Unmarshal(data, &regOptions); err == nil {
		tdp.Value = regOptions
		return nil
	}

	// If all attempts fail, return an error
	return fmt.Errorf("failed to unmarshal TypeDefinitionProvider: %s", string(data))
}

// MarshalJSON implements the json.Marshaler interface.
func (tdp TypeDefinitionProvider) MarshalJSON() ([]byte, error) {
	return json.Marshal(tdp.Value)
}

// IsBool returns true if the TypeDefinitionProvider represents a bool.
func (tdp TypeDefinitionProvider) IsBool() bool {
	_, ok := tdp.Value.(bool)
	return ok
}

// IsOptions returns true if the TypeDefinitionProvider represents TypeDefinitionOptions.
func (tdp TypeDefinitionProvider) IsOptions() bool {
	_, ok := tdp.Value.(TypeDefinitionOptions)
	return ok
}

// IsRegistrationOptions returns true if the TypeDefinitionProvider represents TypeDefinitionRegistrationOptions.
func (tdp TypeDefinitionProvider) IsRegistrationOptions() bool {
	_, ok := tdp.Value.(TypeDefinitionRegistrationOptions)
	return ok
}

// Bool returns the bool value if it represents a bool, or false otherwise.
func (tdp TypeDefinitionProvider) Bool() bool {
	if b, ok := tdp.Value.(bool); ok {
		return b
	}
	return false
}

// Options returns the TypeDefinitionOptions if it represents options, or nil otherwise.
func (tdp TypeDefinitionProvider) Options() *TypeDefinitionOptions {
	if options, ok := tdp.Value.(TypeDefinitionOptions); ok {
		return &options
	}
	return nil
}

// RegistrationOptions returns the TypeDefinitionRegistrationOptions if it represents registration options, or nil otherwise.
func (tdp TypeDefinitionProvider) RegistrationOptions() *TypeDefinitionRegistrationOptions {
	if regOptions, ok := tdp.Value.(TypeDefinitionRegistrationOptions); ok {
		return &regOptions
	}
	return nil
}

// ImplementationProvider represents an implementation provider that can be either a boolean,
// ImplementationOptions, or ImplementationRegistrationOptions.
type ImplementationProvider struct {
	Value interface{}
}

// ImplementationOptions represents options for implementation support.
type ImplementationOptions struct {
	WorkDoneProgressOptions
}

// ImplementationRegistrationOptions represents registration options for implementation support.
type ImplementationRegistrationOptions struct {
	ImplementationOptions
	TextDocumentRegistrationOptions
	StaticRegistrationOptions
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (ip *ImplementationProvider) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as boolean
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		ip.Value = b
		return nil
	}

	// Try to unmarshal as ImplementationOptions
	var options ImplementationOptions
	if err := json.Unmarshal(data, &options); err == nil {
		ip.Value = options
		return nil
	}

	// Try to unmarshal as ImplementationRegistrationOptions
	var regOptions ImplementationRegistrationOptions
	if err := json.Unmarshal(data, &regOptions); err == nil {
		ip.Value = regOptions
		return nil
	}

	// If all attempts fail, return an error
	return fmt.Errorf("failed to unmarshal ImplementationProvider: %s", string(data))
}

// MarshalJSON implements the json.Marshaler interface.
func (ip ImplementationProvider) MarshalJSON() ([]byte, error) {
	return json.Marshal(ip.Value)
}

// IsBool returns true if the ImplementationProvider represents a bool.
func (ip ImplementationProvider) IsBool() bool {
	_, ok := ip.Value.(bool)
	return ok
}

// IsOptions returns true if the ImplementationProvider represents ImplementationOptions.
func (ip ImplementationProvider) IsOptions() bool {
	_, ok := ip.Value.(ImplementationOptions)
	return ok
}

// IsRegistrationOptions returns true if the ImplementationProvider represents ImplementationRegistrationOptions.
func (ip ImplementationProvider) IsRegistrationOptions() bool {
	_, ok := ip.Value.(ImplementationRegistrationOptions)
	return ok
}

// Bool returns the bool value if it represents a bool, or false otherwise.
func (ip ImplementationProvider) Bool() bool {
	if b, ok := ip.Value.(bool); ok {
		return b
	}
	return false
}

// Options returns the ImplementationOptions if it represents options, or nil otherwise.
func (ip ImplementationProvider) Options() *ImplementationOptions {
	if options, ok := ip.Value.(ImplementationOptions); ok {
		return &options
	}
	return nil
}

// RegistrationOptions returns the ImplementationRegistrationOptions if it represents registration options, or nil otherwise.
func (ip ImplementationProvider) RegistrationOptions() *ImplementationRegistrationOptions {
	if regOptions, ok := ip.Value.(ImplementationRegistrationOptions); ok {
		return &regOptions
	}
	return nil
}

// ReferenceProvider represents a reference provider that can be either a boolean or ReferenceOptions.
type ReferenceProvider struct {
	Value interface{}
}

// ReferenceOptions represents options for reference support.
type ReferenceOptions struct {
	WorkDoneProgressOptions
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (rp *ReferenceProvider) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as boolean
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		rp.Value = b
		return nil
	}

	// Try to unmarshal as ReferenceOptions
	var options ReferenceOptions
	if err := json.Unmarshal(data, &options); err == nil {
		rp.Value = options
		return nil
	}

	// If both attempts fail, return an error
	return fmt.Errorf("failed to unmarshal ReferenceProvider: %s", string(data))
}

// MarshalJSON implements the json.Marshaler interface.
func (rp ReferenceProvider) MarshalJSON() ([]byte, error) {
	return json.Marshal(rp.Value)
}

// IsBool returns true if the ReferenceProvider represents a bool.
func (rp ReferenceProvider) IsBool() bool {
	_, ok := rp.Value.(bool)
	return ok
}

// IsOptions returns true if the ReferenceProvider represents ReferenceOptions.
func (rp ReferenceProvider) IsOptions() bool {
	_, ok := rp.Value.(ReferenceOptions)
	return ok
}

// Bool returns the bool value if it represents a bool, or false otherwise.
func (rp ReferenceProvider) Bool() bool {
	if b, ok := rp.Value.(bool); ok {
		return b
	}
	return false
}

// Options returns the ReferenceOptions if it represents options, or nil otherwise.
func (rp ReferenceProvider) Options() *ReferenceOptions {
	if options, ok := rp.Value.(ReferenceOptions); ok {
		return &options
	}
	return nil
}

// DocumentHighlightProvider represents a document highlight provider that can be either a boolean or DocumentHighlightOptions.
type DocumentHighlightProvider struct {
	Value interface{}
}

// DocumentHighlightOptions represents options for document highlight support.
type DocumentHighlightOptions struct {
	WorkDoneProgressOptions
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (dhp *DocumentHighlightProvider) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as boolean
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		dhp.Value = b
		return nil
	}

	// Try to unmarshal as DocumentHighlightOptions
	var options DocumentHighlightOptions
	if err := json.Unmarshal(data, &options); err == nil {
		dhp.Value = options
		return nil
	}

	// If both attempts fail, return an error
	return fmt.Errorf("failed to unmarshal DocumentHighlightProvider: %s", string(data))
}

// MarshalJSON implements the json.Marshaler interface.
func (dhp DocumentHighlightProvider) MarshalJSON() ([]byte, error) {
	return json.Marshal(dhp.Value)
}

// IsBool returns true if the DocumentHighlightProvider represents a bool.
func (dhp DocumentHighlightProvider) IsBool() bool {
	_, ok := dhp.Value.(bool)
	return ok
}

// IsOptions returns true if the DocumentHighlightProvider represents DocumentHighlightOptions.
func (dhp DocumentHighlightProvider) IsOptions() bool {
	_, ok := dhp.Value.(DocumentHighlightOptions)
	return ok
}

// Bool returns the bool value if it represents a bool, or false otherwise.
func (dhp DocumentHighlightProvider) Bool() bool {
	if b, ok := dhp.Value.(bool); ok {
		return b
	}
	return false
}

// Options returns the DocumentHighlightOptions if it represents options, or nil otherwise.
func (dhp DocumentHighlightProvider) Options() *DocumentHighlightOptions {
	if options, ok := dhp.Value.(DocumentHighlightOptions); ok {
		return &options
	}
	return nil
}

// DocumentSymbolProvider represents a document symbol provider that can be either a boolean or DocumentSymbolOptions.
type DocumentSymbolProvider struct {
	Value interface{}
}

// DocumentSymbolOptions represents options for document symbol support.
type DocumentSymbolOptions struct {
	WorkDoneProgressOptions
	Label string `json:"label,omitempty"`
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (dsp *DocumentSymbolProvider) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as boolean
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		dsp.Value = b
		return nil
	}

	// Try to unmarshal as DocumentSymbolOptions
	var options DocumentSymbolOptions
	if err := json.Unmarshal(data, &options); err == nil {
		dsp.Value = options
		return nil
	}

	// If both attempts fail, return an error
	return fmt.Errorf("failed to unmarshal DocumentSymbolProvider: %s", string(data))
}

// MarshalJSON implements the json.Marshaler interface.
func (dsp DocumentSymbolProvider) MarshalJSON() ([]byte, error) {
	return json.Marshal(dsp.Value)
}

// IsBool returns true if the DocumentSymbolProvider represents a bool.
func (dsp DocumentSymbolProvider) IsBool() bool {
	_, ok := dsp.Value.(bool)
	return ok
}

// IsOptions returns true if the DocumentSymbolProvider represents DocumentSymbolOptions.
func (dsp DocumentSymbolProvider) IsOptions() bool {
	_, ok := dsp.Value.(DocumentSymbolOptions)
	return ok
}

// Bool returns the bool value if it represents a bool, or false otherwise.
func (dsp DocumentSymbolProvider) Bool() bool {
	if b, ok := dsp.Value.(bool); ok {
		return b
	}
	return false
}

// Options returns the DocumentSymbolOptions if it represents options, or nil otherwise.
func (dsp DocumentSymbolProvider) Options() *DocumentSymbolOptions {
	if options, ok := dsp.Value.(DocumentSymbolOptions); ok {
		return &options
	}
	return nil
}

// CodeActionProvider represents a code action provider that can be either a boolean or CodeActionOptions.
type CodeActionProvider struct {
	Value interface{}
}

// CodeActionOptions represents options for code action support.
type CodeActionOptions struct {
	WorkDoneProgressOptions
	CodeActionKinds []CodeActionKind `json:"codeActionKinds,omitempty"`
	ResolveProvider bool             `json:"resolveProvider,omitempty"`
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (cap *CodeActionProvider) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as boolean
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		cap.Value = b
		return nil
	}

	// Try to unmarshal as CodeActionOptions
	var options CodeActionOptions
	if err := json.Unmarshal(data, &options); err == nil {
		cap.Value = options
		return nil
	}

	// If both attempts fail, return an error
	return fmt.Errorf("failed to unmarshal CodeActionProvider: %s", string(data))
}

// MarshalJSON implements the json.Marshaler interface.
func (cap CodeActionProvider) MarshalJSON() ([]byte, error) {
	return json.Marshal(cap.Value)
}

// IsBool returns true if the CodeActionProvider represents a bool.
func (cap CodeActionProvider) IsBool() bool {
	_, ok := cap.Value.(bool)
	return ok
}

// IsOptions returns true if the CodeActionProvider represents CodeActionOptions.
func (cap CodeActionProvider) IsOptions() bool {
	_, ok := cap.Value.(CodeActionOptions)
	return ok
}

// Bool returns the bool value if it represents a bool, or false otherwise.
func (cap CodeActionProvider) Bool() bool {
	if b, ok := cap.Value.(bool); ok {
		return b
	}
	return false
}

// Options returns the CodeActionOptions if it represents options, or nil otherwise.
func (cap CodeActionProvider) Options() *CodeActionOptions {
	if options, ok := cap.Value.(CodeActionOptions); ok {
		return &options
	}
	return nil
}

// DocumentColorProvider represents a document color provider that can be either a boolean,
// DocumentColorOptions, or DocumentColorRegistrationOptions.
type DocumentColorProvider struct {
	Value interface{}
}

// DocumentColorOptions represents options for document color support.
type DocumentColorOptions struct {
	WorkDoneProgressOptions
}

// DocumentColorRegistrationOptions represents registration options for document color support.
type DocumentColorRegistrationOptions struct {
	DocumentColorOptions
	TextDocumentRegistrationOptions
	StaticRegistrationOptions
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (dcp *DocumentColorProvider) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as boolean
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		dcp.Value = b
		return nil
	}

	// Try to unmarshal as DocumentColorOptions
	var options DocumentColorOptions
	if err := json.Unmarshal(data, &options); err == nil {
		dcp.Value = options
		return nil
	}

	// Try to unmarshal as DocumentColorRegistrationOptions
	var regOptions DocumentColorRegistrationOptions
	if err := json.Unmarshal(data, &regOptions); err == nil {
		dcp.Value = regOptions
		return nil
	}

	// If all attempts fail, return an error
	return fmt.Errorf("failed to unmarshal DocumentColorProvider: %s", string(data))
}

// MarshalJSON implements the json.Marshaler interface.
func (dcp DocumentColorProvider) MarshalJSON() ([]byte, error) {
	return json.Marshal(dcp.Value)
}

// DocumentFormattingProvider represents a document formatting provider that can be either a boolean or DocumentFormattingOptions.
type DocumentFormattingProvider struct {
	Value interface{}
}

// DocumentFormattingOptions represents options for document formatting support.
type DocumentFormattingOptions struct {
	WorkDoneProgressOptions
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (dfp *DocumentFormattingProvider) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as boolean
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		dfp.Value = b
		return nil
	}

	// Try to unmarshal as DocumentFormattingOptions
	var options DocumentFormattingOptions
	if err := json.Unmarshal(data, &options); err == nil {
		dfp.Value = options
		return nil
	}

	// If both attempts fail, return an error
	return fmt.Errorf("failed to unmarshal DocumentFormattingProvider: %s", string(data))
}

// MarshalJSON implements the json.Marshaler interface.
func (dfp DocumentFormattingProvider) MarshalJSON() ([]byte, error) {
	return json.Marshal(dfp.Value)
}

// DocumentRangeFormattingProvider represents a document range formatting provider that can be either a boolean or DocumentRangeFormattingOptions.
type DocumentRangeFormattingProvider struct {
	Value interface{}
}

// DocumentRangeFormattingOptions represents options for document range formatting support.
type DocumentRangeFormattingOptions struct {
	WorkDoneProgressOptions
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (drfp *DocumentRangeFormattingProvider) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as boolean
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		drfp.Value = b
		return nil
	}

	// Try to unmarshal as DocumentRangeFormattingOptions
	var options DocumentRangeFormattingOptions
	if err := json.Unmarshal(data, &options); err == nil {
		drfp.Value = options
		return nil
	}

	// If both attempts fail, return an error
	return fmt.Errorf("failed to unmarshal DocumentRangeFormattingProvider: %s", string(data))
}

// MarshalJSON implements the json.Marshaler interface.
func (drfp DocumentRangeFormattingProvider) MarshalJSON() ([]byte, error) {
	return json.Marshal(drfp.Value)
}

// RenameProvider represents a rename provider that can be either a boolean or RenameOptions.
type RenameProvider struct {
	Value interface{}
}

// RenameOptions represents options for rename support.
type RenameOptions struct {
	WorkDoneProgressOptions
	PrepareProvider bool `json:"prepareProvider,omitempty"`
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (rp *RenameProvider) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as boolean
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		rp.Value = b
		return nil
	}

	// Try to unmarshal as RenameOptions
	var options RenameOptions
	if err := json.Unmarshal(data, &options); err == nil {
		rp.Value = options
		return nil
	}

	// If both attempts fail, return an error
	return fmt.Errorf("failed to unmarshal RenameProvider: %s", string(data))
}

// MarshalJSON implements the json.Marshaler interface.
func (rp RenameProvider) MarshalJSON() ([]byte, error) {
	return json.Marshal(rp.Value)
}

// FoldingRangeProvider represents a folding range provider that can be either a boolean,
// FoldingRangeOptions, or FoldingRangeRegistrationOptions.
type FoldingRangeProvider struct {
	Value interface{}
}

// FoldingRangeOptions represents options for folding range support.
type FoldingRangeOptions struct {
	WorkDoneProgressOptions
}

// FoldingRangeRegistrationOptions represents registration options for folding range support.
type FoldingRangeRegistrationOptions struct {
	FoldingRangeOptions
	TextDocumentRegistrationOptions
	StaticRegistrationOptions
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (frp *FoldingRangeProvider) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as boolean
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		frp.Value = b
		return nil
	}

	// Try to unmarshal as FoldingRangeOptions
	var options FoldingRangeOptions
	if err := json.Unmarshal(data, &options); err == nil {
		frp.Value = options
		return nil
	}

	// Try to unmarshal as FoldingRangeRegistrationOptions
	var regOptions FoldingRangeRegistrationOptions
	if err := json.Unmarshal(data, &regOptions); err == nil {
		frp.Value = regOptions
		return nil
	}

	// If all attempts fail, return an error
	return fmt.Errorf("failed to unmarshal FoldingRangeProvider: %s", string(data))
}

// MarshalJSON implements the json.Marshaler interface.
func (frp FoldingRangeProvider) MarshalJSON() ([]byte, error) {
	return json.Marshal(frp.Value)
}

// SelectionRangeProvider represents a selection range provider that can be either a boolean,
// SelectionRangeOptions, or SelectionRangeRegistrationOptions.
type SelectionRangeProvider struct {
	Value interface{}
}

// SelectionRangeOptions represents options for selection range support.
type SelectionRangeOptions struct {
	WorkDoneProgressOptions
}

// SelectionRangeRegistrationOptions represents registration options for selection range support.
type SelectionRangeRegistrationOptions struct {
	SelectionRangeOptions
	TextDocumentRegistrationOptions
	StaticRegistrationOptions
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (srp *SelectionRangeProvider) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as boolean
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		srp.Value = b
		return nil
	}

	// Try to unmarshal as SelectionRangeOptions
	var options SelectionRangeOptions
	if err := json.Unmarshal(data, &options); err == nil {
		srp.Value = options
		return nil
	}

	// Try to unmarshal as SelectionRangeRegistrationOptions
	var regOptions SelectionRangeRegistrationOptions
	if err := json.Unmarshal(data, &regOptions); err == nil {
		srp.Value = regOptions
		return nil
	}

	// If all attempts fail, return an error
	return fmt.Errorf("failed to unmarshal SelectionRangeProvider: %s", string(data))
}

// MarshalJSON implements the json.Marshaler interface.
func (srp SelectionRangeProvider) MarshalJSON() ([]byte, error) {
	return json.Marshal(srp.Value)
}

// LinkedEditingRangeProvider represents a linked editing range provider that can be either a boolean,
// LinkedEditingRangeOptions, or LinkedEditingRangeRegistrationOptions.
type LinkedEditingRangeProvider struct {
	Value interface{}
}

// LinkedEditingRangeOptions represents options for linked editing range support.
type LinkedEditingRangeOptions struct {
	WorkDoneProgressOptions
}

// LinkedEditingRangeRegistrationOptions represents registration options for linked editing range support.
type LinkedEditingRangeRegistrationOptions struct {
	LinkedEditingRangeOptions
	TextDocumentRegistrationOptions
	StaticRegistrationOptions
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (lerp *LinkedEditingRangeProvider) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as boolean
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		lerp.Value = b
		return nil
	}

	// Try to unmarshal as LinkedEditingRangeOptions
	var options LinkedEditingRangeOptions
	if err := json.Unmarshal(data, &options); err == nil {
		lerp.Value = options
		return nil
	}

	// Try to unmarshal as LinkedEditingRangeRegistrationOptions
	var regOptions LinkedEditingRangeRegistrationOptions
	if err := json.Unmarshal(data, &regOptions); err == nil {
		lerp.Value = regOptions
		return nil
	}

	// If all attempts fail, return an error
	return fmt.Errorf("failed to unmarshal LinkedEditingRangeProvider: %s", string(data))
}

// MarshalJSON implements the json.Marshaler interface.
func (lerp LinkedEditingRangeProvider) MarshalJSON() ([]byte, error) {
	return json.Marshal(lerp.Value)
}

// CallHierarchyProvider represents a call hierarchy provider that can be either a boolean,
// CallHierarchyOptions, or CallHierarchyRegistrationOptions.
type CallHierarchyProvider struct {
	Value interface{}
}

// CallHierarchyOptions represents options for call hierarchy support.
type CallHierarchyOptions struct {
	WorkDoneProgressOptions
}

// CallHierarchyRegistrationOptions represents registration options for call hierarchy support.
type CallHierarchyRegistrationOptions struct {
	CallHierarchyOptions
	TextDocumentRegistrationOptions
	StaticRegistrationOptions
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (chp *CallHierarchyProvider) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as boolean
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		chp.Value = b
		return nil
	}

	// Try to unmarshal as CallHierarchyOptions
	var options CallHierarchyOptions
	if err := json.Unmarshal(data, &options); err == nil {
		chp.Value = options
		return nil
	}

	// Try to unmarshal as CallHierarchyRegistrationOptions
	var regOptions CallHierarchyRegistrationOptions
	if err := json.Unmarshal(data, &regOptions); err == nil {
		chp.Value = regOptions
		return nil
	}

	// If all attempts fail, return an error
	return fmt.Errorf("failed to unmarshal CallHierarchyProvider: %s", string(data))
}

// MarshalJSON implements the json.Marshaler interface.
func (chp CallHierarchyProvider) MarshalJSON() ([]byte, error) {
	return json.Marshal(chp.Value)
}

// SemanticTokensProvider represents a semantic tokens provider that can be either
// SemanticTokensOptions or SemanticTokensRegistrationOptions.
type SemanticTokensProvider struct {
	Value interface{}
}

// SemanticTokensOptions represents options for semantic tokens support.
type SemanticTokensOptions struct {
	WorkDoneProgressOptions
	Legend SemanticTokensLegend `json:"legend"`
	Range  interface{}          `json:"range,omitempty"`
	Full   interface{}          `json:"full,omitempty"`
}

// SemanticTokensRegistrationOptions represents registration options for semantic tokens support.
type SemanticTokensRegistrationOptions struct {
	SemanticTokensOptions
	TextDocumentRegistrationOptions
	StaticRegistrationOptions
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (stp *SemanticTokensProvider) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as SemanticTokensOptions
	var options SemanticTokensOptions
	if err := json.Unmarshal(data, &options); err == nil {
		stp.Value = options
		return nil
	}

	// Try to unmarshal as SemanticTokensRegistrationOptions
	var regOptions SemanticTokensRegistrationOptions
	if err := json.Unmarshal(data, &regOptions); err == nil {
		stp.Value = regOptions
		return nil
	}

	// If all attempts fail, return an error
	return fmt.Errorf("failed to unmarshal SemanticTokensProvider: %s", string(data))
}

// MarshalJSON implements the json.Marshaler interface.
func (stp SemanticTokensProvider) MarshalJSON() ([]byte, error) {
	return json.Marshal(stp.Value)
}

// MonikerProvider represents a moniker provider that can be either a boolean,
// MonikerOptions, or MonikerRegistrationOptions.
type MonikerProvider struct {
	Value interface{}
}

// MonikerOptions represents options for moniker support.
type MonikerOptions struct {
	WorkDoneProgressOptions
}

// MonikerRegistrationOptions represents registration options for moniker support.
type MonikerRegistrationOptions struct {
	MonikerOptions
	TextDocumentRegistrationOptions
	StaticRegistrationOptions
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (mp *MonikerProvider) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as boolean
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		mp.Value = b
		return nil
	}

	// Try to unmarshal as MonikerOptions
	var options MonikerOptions
	if err := json.Unmarshal(data, &options); err == nil {
		mp.Value = options
		return nil
	}

	// Try to unmarshal as MonikerRegistrationOptions
	var regOptions MonikerRegistrationOptions
	if err := json.Unmarshal(data, &regOptions); err == nil {
		mp.Value = regOptions
		return nil
	}

	// If all attempts fail, return an error
	return fmt.Errorf("failed to unmarshal MonikerProvider: %s", string(data))
}

// MarshalJSON implements the json.Marshaler interface.
func (mp MonikerProvider) MarshalJSON() ([]byte, error) {
	return json.Marshal(mp.Value)
}

// TypeHierarchyProvider represents a type hierarchy provider that can be either a boolean,
// TypeHierarchyOptions, or TypeHierarchyRegistrationOptions.
type TypeHierarchyProvider struct {
	Value interface{}
}

// TypeHierarchyOptions represents options for type hierarchy support.
type TypeHierarchyOptions struct {
	WorkDoneProgressOptions
}

// TypeHierarchyRegistrationOptions represents registration options for type hierarchy support.
type TypeHierarchyRegistrationOptions struct {
	TypeHierarchyOptions
	TextDocumentRegistrationOptions
	StaticRegistrationOptions
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (thp *TypeHierarchyProvider) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as boolean
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		thp.Value = b
		return nil
	}

	// Try to unmarshal as TypeHierarchyOptions
	var options TypeHierarchyOptions
	if err := json.Unmarshal(data, &options); err == nil {
		thp.Value = options
		return nil
	}

	// Try to unmarshal as TypeHierarchyRegistrationOptions
	var regOptions TypeHierarchyRegistrationOptions
	if err := json.Unmarshal(data, &regOptions); err == nil {
		thp.Value = regOptions
		return nil
	}

	// If all attempts fail, return an error
	return fmt.Errorf("failed to unmarshal TypeHierarchyProvider: %s", string(data))
}

// MarshalJSON implements the json.Marshaler interface.
func (thp TypeHierarchyProvider) MarshalJSON() ([]byte, error) {
	return json.Marshal(thp.Value)
}

// InlineValueProvider represents an inline value provider that can be either a boolean,
// InlineValueOptions, or InlineValueRegistrationOptions.
type InlineValueProvider struct {
	Value interface{}
}

// InlineValueOptions represents options for inline value support.
type InlineValueOptions struct {
	WorkDoneProgressOptions
}

// InlineValueRegistrationOptions represents registration options for inline value support.
type InlineValueRegistrationOptions struct {
	InlineValueOptions
	TextDocumentRegistrationOptions
	StaticRegistrationOptions
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (ivp *InlineValueProvider) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as boolean
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		ivp.Value = b
		return nil
	}

	// Try to unmarshal as InlineValueOptions
	var options InlineValueOptions
	if err := json.Unmarshal(data, &options); err == nil {
		ivp.Value = options
		return nil
	}

	// Try to unmarshal as InlineValueRegistrationOptions
	var regOptions InlineValueRegistrationOptions
	if err := json.Unmarshal(data, &regOptions); err == nil {
		ivp.Value = regOptions
		return nil
	}

	// If all attempts fail, return an error
	return fmt.Errorf("failed to unmarshal InlineValueProvider: %s", string(data))
}

// MarshalJSON implements the json.Marshaler interface.
func (ivp InlineValueProvider) MarshalJSON() ([]byte, error) {
	return json.Marshal(ivp.Value)
}

// InlayHintProvider represents an inlay hint provider that can be either a boolean,
// InlayHintOptions, or InlayHintRegistrationOptions.
type InlayHintProvider struct {
	Value interface{}
}

// InlayHintOptions represents options for inlay hint support.
type InlayHintOptions struct {
	WorkDoneProgressOptions
	ResolveProvider bool `json:"resolveProvider,omitempty"`
}

// InlayHintRegistrationOptions represents registration options for inlay hint support.
type InlayHintRegistrationOptions struct {
	InlayHintOptions
	TextDocumentRegistrationOptions
	StaticRegistrationOptions
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (ihp *InlayHintProvider) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as boolean
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		ihp.Value = b
		return nil
	}

	// Try to unmarshal as InlayHintOptions
	var options InlayHintOptions
	if err := json.Unmarshal(data, &options); err == nil {
		ihp.Value = options
		return nil
	}

	// Try to unmarshal as InlayHintRegistrationOptions
	var regOptions InlayHintRegistrationOptions
	if err := json.Unmarshal(data, &regOptions); err == nil {
		ihp.Value = regOptions
		return nil
	}

	// If all attempts fail, return an error
	return fmt.Errorf("failed to unmarshal InlayHintProvider: %s", string(data))
}

// MarshalJSON implements the json.Marshaler interface.
func (ihp InlayHintProvider) MarshalJSON() ([]byte, error) {
	return json.Marshal(ihp.Value)
}

// DiagnosticProvider represents a diagnostic provider that can be either
// DiagnosticOptions or DiagnosticRegistrationOptions.
type DiagnosticProvider struct {
	Value interface{}
}

// DiagnosticOptions represents options for diagnostic support.
type DiagnosticOptions struct {
	WorkDoneProgressOptions
	Identifier            string `json:"identifier,omitempty"`
	InterFileDependencies bool   `json:"interFileDependencies"`
	WorkspaceDiagnostics  bool   `json:"workspaceDiagnostics"`
}

// DiagnosticRegistrationOptions represents registration options for diagnostic support.
type DiagnosticRegistrationOptions struct {
	DiagnosticOptions
	TextDocumentRegistrationOptions
	StaticRegistrationOptions
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (dp *DiagnosticProvider) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as DiagnosticOptions
	var options DiagnosticOptions
	if err := json.Unmarshal(data, &options); err == nil {
		dp.Value = options
		return nil
	}

	// Try to unmarshal as DiagnosticRegistrationOptions
	var regOptions DiagnosticRegistrationOptions
	if err := json.Unmarshal(data, &regOptions); err == nil {
		dp.Value = regOptions
		return nil
	}

	// If all attempts fail, return an error
	return fmt.Errorf("failed to unmarshal DiagnosticProvider: %s", string(data))
}

// MarshalJSON implements the json.Marshaler interface.
func (dp DiagnosticProvider) MarshalJSON() ([]byte, error) {
	return json.Marshal(dp.Value)
}

// WorkspaceSymbolProvider represents a workspace symbol provider that can be either a boolean
// or WorkspaceSymbolOptions.
type WorkspaceSymbolProvider struct {
	Value interface{}
}

// WorkspaceSymbolOptions represents options for workspace symbol support.
type WorkspaceSymbolOptions struct {
	WorkDoneProgressOptions
	ResolveProvider bool `json:"resolveProvider,omitempty"`
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (wsp *WorkspaceSymbolProvider) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as boolean
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		wsp.Value = b
		return nil
	}

	// Try to unmarshal as WorkspaceSymbolOptions
	var options WorkspaceSymbolOptions
	if err := json.Unmarshal(data, &options); err == nil {
		wsp.Value = options
		return nil
	}

	// If both attempts fail, return an error
	return fmt.Errorf("failed to unmarshal WorkspaceSymbolProvider: %s", string(data))
}

// MarshalJSON implements the json.Marshaler interface.
func (wsp WorkspaceSymbolProvider) MarshalJSON() ([]byte, error) {
	return json.Marshal(wsp.Value)
}

// SemanticTokensLegend represents the legend for semantic tokens.
type SemanticTokensLegend struct {
	// The token types supported by the server.
	TokenTypes []string `json:"tokenTypes"`

	// The token modifiers supported by the server.
	TokenModifiers []string `json:"tokenModifiers"`
}

// NewSemanticTokensLegend creates a new SemanticTokensLegend with the given token types and modifiers.
func NewSemanticTokensLegend(tokenTypes, tokenModifiers []string) SemanticTokensLegend {
	return SemanticTokensLegend{
		TokenTypes:     tokenTypes,
		TokenModifiers: tokenModifiers,
	}
}

// AddTokenType adds a new token type to the legend if it doesn't already exist.
func (stl *SemanticTokensLegend) AddTokenType(tokenType string) {
	for _, tt := range stl.TokenTypes {
		if tt == tokenType {
			return // Token type already exists
		}
	}
	stl.TokenTypes = append(stl.TokenTypes, tokenType)
}

// AddTokenModifier adds a new token modifier to the legend if it doesn't already exist.
func (stl *SemanticTokensLegend) AddTokenModifier(tokenModifier string) {
	for _, tm := range stl.TokenModifiers {
		if tm == tokenModifier {
			return // Token modifier already exists
		}
	}
	stl.TokenModifiers = append(stl.TokenModifiers, tokenModifier)
}

// GetTokenTypeIndex returns the index of a given token type in the legend.
// Returns -1 if the token type is not found.
func (stl *SemanticTokensLegend) GetTokenTypeIndex(tokenType string) int {
	for i, tt := range stl.TokenTypes {
		if tt == tokenType {
			return i
		}
	}
	return -1
}

// GetTokenModifierIndex returns the index of a given token modifier in the legend.
// Returns -1 if the token modifier is not found.
func (stl *SemanticTokensLegend) GetTokenModifierIndex(tokenModifier string) int {
	for i, tm := range stl.TokenModifiers {
		if tm == tokenModifier {
			return i
		}
	}
	return -1
}
