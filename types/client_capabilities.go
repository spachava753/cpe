package types

// PositionEncodingKind represents how positions are encoded.
type PositionEncodingKind string

const (
	// PositionEncodingKindUTF8 indicates that character offsets count UTF-8 code units (e.g., bytes).
	PositionEncodingKindUTF8 PositionEncodingKind = "utf-8"

	// PositionEncodingKindUTF16 indicates that character offsets count UTF-16 code units.
	// This is the default and must always be supported by servers.
	PositionEncodingKindUTF16 PositionEncodingKind = "utf-16"

	// PositionEncodingKindUTF32 indicates that character offsets count UTF-32 code units.
	PositionEncodingKindUTF32 PositionEncodingKind = "utf-32"
)

// IsValid returns true if the PositionEncodingKind is a valid value.
func (pek PositionEncodingKind) IsValid() bool {
	switch pek {
	case PositionEncodingKindUTF8, PositionEncodingKindUTF16, PositionEncodingKindUTF32:
		return true
	default:
		return false
	}
}

// String returns the string representation of the PositionEncodingKind.
func (pek PositionEncodingKind) String() string {
	return string(pek)
}

// WorkspaceClientCapabilities represents workspace-specific client capabilities
type WorkspaceClientCapabilities struct {
	ApplyEdit              bool                                    `json:"applyEdit,omitempty"`
	WorkspaceEdit          *WorkspaceEditClientCapabilities        `json:"workspaceEdit,omitempty"`
	DidChangeConfiguration *DidChangeConfigurationCapabilities     `json:"didChangeConfiguration,omitempty"`
	DidChangeWatchedFiles  *DidChangeWatchedFilesCapabilities      `json:"didChangeWatchedFiles,omitempty"`
	Symbol                 *WorkspaceSymbolClientCapabilities      `json:"symbol,omitempty"`
	ExecuteCommand         *ExecuteCommandClientCapabilities       `json:"executeCommand,omitempty"`
	WorkspaceFolders       bool                                    `json:"workspaceFolders,omitempty"`
	Configuration          bool                                    `json:"configuration,omitempty"`
	SemanticTokens         *SemanticTokensWorkspaceCapabilities    `json:"semanticTokens,omitempty"`
	CodeLens               *CodeLensWorkspaceClientCapabilities    `json:"codeLens,omitempty"`
	FileOperations         *FileOperationClientCapabilities        `json:"fileOperations,omitempty"`
	InlineValue            *InlineValueWorkspaceClientCapabilities `json:"inlineValue,omitempty"`
	InlayHint              *InlayHintWorkspaceClientCapabilities   `json:"inlayHint,omitempty"`
	Diagnostics            *DiagnosticWorkspaceClientCapabilities  `json:"diagnostics,omitempty"`
}

// NewWorkspaceClientCapabilities creates a new WorkspaceClientCapabilities with default values.
func NewWorkspaceClientCapabilities() *WorkspaceClientCapabilities {
	return &WorkspaceClientCapabilities{
		ApplyEdit: false,
		WorkspaceEdit: &WorkspaceEditClientCapabilities{
			DocumentChanges:       false,
			ResourceOperations:    nil,
			FailureHandling:       "",
			NormalizesLineEndings: false,
			ChangeAnnotationSupport: &ChangeAnnotationSupport{
				GroupsOnLabel: false,
			},
		},
		DidChangeConfiguration: &DidChangeConfigurationCapabilities{
			DynamicRegistration: false,
		},
		DidChangeWatchedFiles: &DidChangeWatchedFilesCapabilities{
			DynamicRegistration:    false,
			RelativePatternSupport: false,
		},
		Symbol: &WorkspaceSymbolClientCapabilities{
			DynamicRegistration: false,
			SymbolKind:          nil,
			TagSupport:          nil,
			ResolveSupport:      nil,
		},
		ExecuteCommand: &ExecuteCommandClientCapabilities{
			DynamicRegistration: false,
		},
		WorkspaceFolders: false,
		Configuration:    false,
		SemanticTokens: &SemanticTokensWorkspaceCapabilities{
			RefreshSupport: false,
		},
		CodeLens: &CodeLensWorkspaceClientCapabilities{
			RefreshSupport: false,
		},
		FileOperations: &FileOperationClientCapabilities{
			DynamicRegistration: false,
			DidCreate:           false,
			WillCreate:          false,
			DidRename:           false,
			WillRename:          false,
			DidDelete:           false,
			WillDelete:          false,
		},
		InlineValue: &InlineValueWorkspaceClientCapabilities{
			RefreshSupport: false,
		},
		InlayHint: &InlayHintWorkspaceClientCapabilities{
			RefreshSupport: false,
		},
		Diagnostics: &DiagnosticWorkspaceClientCapabilities{
			RefreshSupport: false,
		},
	}
}

type WorkspaceEditClientCapabilities struct {
	DocumentChanges         bool                     `json:"documentChanges,omitempty"`
	ResourceOperations      []string                 `json:"resourceOperations,omitempty"`
	FailureHandling         string                   `json:"failureHandling,omitempty"`
	NormalizesLineEndings   bool                     `json:"normalizesLineEndings,omitempty"`
	ChangeAnnotationSupport *ChangeAnnotationSupport `json:"changeAnnotationSupport,omitempty"`
}

type ChangeAnnotationSupport struct {
	GroupsOnLabel bool `json:"groupsOnLabel,omitempty"`
}

type DidChangeConfigurationCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

type DidChangeWatchedFilesCapabilities struct {
	DynamicRegistration    bool `json:"dynamicRegistration,omitempty"`
	RelativePatternSupport bool `json:"relativePatternSupport,omitempty"`
}

type WorkspaceSymbolClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
	SymbolKind          *struct {
		ValueSet []SymbolKind `json:"valueSet,omitempty"`
	} `json:"symbolKind,omitempty"`
	TagSupport *struct {
		ValueSet []SymbolTag `json:"valueSet,omitempty"`
	} `json:"tagSupport,omitempty"`
	ResolveSupport *struct {
		Properties []string `json:"properties,omitempty"`
	} `json:"resolveSupport,omitempty"`
}

type ExecuteCommandClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

type SemanticTokensWorkspaceCapabilities struct {
	RefreshSupport bool `json:"refreshSupport,omitempty"`
}

type CodeLensWorkspaceClientCapabilities struct {
	RefreshSupport bool `json:"refreshSupport,omitempty"`
}

type FileOperationClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
	DidCreate           bool `json:"didCreate,omitempty"`
	WillCreate          bool `json:"willCreate,omitempty"`
	DidRename           bool `json:"didRename,omitempty"`
	WillRename          bool `json:"willRename,omitempty"`
	DidDelete           bool `json:"didDelete,omitempty"`
	WillDelete          bool `json:"willDelete,omitempty"`
}

type InlineValueWorkspaceClientCapabilities struct {
	RefreshSupport bool `json:"refreshSupport,omitempty"`
}

type InlayHintWorkspaceClientCapabilities struct {
	RefreshSupport bool `json:"refreshSupport,omitempty"`
}

type DiagnosticWorkspaceClientCapabilities struct {
	RefreshSupport bool `json:"refreshSupport,omitempty"`
}

// TextDocumentClientCapabilities defines capabilities the editor / tool provides on text documents.
type TextDocumentClientCapabilities struct {
	Synchronization    *TextDocumentSyncClientCapabilities         `json:"synchronization,omitempty"`
	Completion         *CompletionClientCapabilities               `json:"completion,omitempty"`
	Hover              *HoverClientCapabilities                    `json:"hover,omitempty"`
	SignatureHelp      *SignatureHelpClientCapabilities            `json:"signatureHelp,omitempty"`
	Declaration        *DeclarationClientCapabilities              `json:"declaration,omitempty"`
	Definition         *DefinitionClientCapabilities               `json:"definition,omitempty"`
	TypeDefinition     *TypeDefinitionClientCapabilities           `json:"typeDefinition,omitempty"`
	Implementation     *ImplementationClientCapabilities           `json:"implementation,omitempty"`
	References         *ReferenceClientCapabilities                `json:"references,omitempty"`
	DocumentHighlight  *DocumentHighlightClientCapabilities        `json:"documentHighlight,omitempty"`
	DocumentSymbol     *DocumentSymbolClientCapabilities           `json:"documentSymbol,omitempty"`
	CodeAction         *CodeActionClientCapabilities               `json:"codeAction,omitempty"`
	CodeLens           *CodeLensClientCapabilities                 `json:"codeLens,omitempty"`
	DocumentLink       *DocumentLinkClientCapabilities             `json:"documentLink,omitempty"`
	ColorProvider      *DocumentColorClientCapabilities            `json:"colorProvider,omitempty"`
	Formatting         *DocumentFormattingClientCapabilities       `json:"formatting,omitempty"`
	RangeFormatting    *DocumentRangeFormattingClientCapabilities  `json:"rangeFormatting,omitempty"`
	OnTypeFormatting   *DocumentOnTypeFormattingClientCapabilities `json:"onTypeFormatting,omitempty"`
	Rename             *RenameClientCapabilities                   `json:"rename,omitempty"`
	PublishDiagnostics *PublishDiagnosticsClientCapabilities       `json:"publishDiagnostics,omitempty"`
	FoldingRange       *FoldingRangeClientCapabilities             `json:"foldingRange,omitempty"`
	SelectionRange     *SelectionRangeClientCapabilities           `json:"selectionRange,omitempty"`
	LinkedEditingRange *LinkedEditingRangeClientCapabilities       `json:"linkedEditingRange,omitempty"`
	CallHierarchy      *CallHierarchyClientCapabilities            `json:"callHierarchy,omitempty"`
	SemanticTokens     *SemanticTokensClientCapabilities           `json:"semanticTokens,omitempty"`
	Moniker            *MonikerClientCapabilities                  `json:"moniker,omitempty"`
	TypeHierarchy      *TypeHierarchyClientCapabilities            `json:"typeHierarchy,omitempty"`
	InlineValue        *InlineValueClientCapabilities              `json:"inlineValue,omitempty"`
	InlayHint          *InlayHintClientCapabilities                `json:"inlayHint,omitempty"`
	Diagnostic         *DiagnosticClientCapabilities               `json:"diagnostic,omitempty"`
}

// NewTextDocumentClientCapabilities creates a new TextDocumentClientCapabilities with default values.
func NewTextDocumentClientCapabilities() *TextDocumentClientCapabilities {
	return &TextDocumentClientCapabilities{}
}

type TextDocumentSyncClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
	WillSave            bool `json:"willSave,omitempty"`
	WillSaveWaitUntil   bool `json:"willSaveWaitUntil,omitempty"`
	DidSave             bool `json:"didSave,omitempty"`
}

type CompletionClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
	CompletionItem      *struct {
		SnippetSupport          bool     `json:"snippetSupport,omitempty"`
		CommitCharactersSupport bool     `json:"commitCharactersSupport,omitempty"`
		DocumentationFormat     []string `json:"documentationFormat,omitempty"`
		DeprecatedSupport       bool     `json:"deprecatedSupport,omitempty"`
		PreselectSupport        bool     `json:"preselectSupport,omitempty"`
		TagSupport              *struct {
			ValueSet []CompletionItemTag `json:"valueSet"`
		} `json:"tagSupport,omitempty"`
		InsertReplaceSupport bool `json:"insertReplaceSupport,omitempty"`
		ResolveSupport       *struct {
			Properties []string `json:"properties"`
		} `json:"resolveSupport,omitempty"`
		InsertTextModeSupport *struct {
			ValueSet []InsertTextMode `json:"valueSet"`
		} `json:"insertTextModeSupport,omitempty"`
		LabelDetailsSupport bool `json:"labelDetailsSupport,omitempty"`
	} `json:"completionItem,omitempty"`
	CompletionItemKind *struct {
		ValueSet []CompletionItemKind `json:"valueSet,omitempty"`
	} `json:"completionItemKind,omitempty"`
	ContextSupport bool           `json:"contextSupport,omitempty"`
	InsertTextMode InsertTextMode `json:"insertTextMode,omitempty"`
	CompletionList *struct {
		ItemDefaults []string `json:"itemDefaults,omitempty"`
	} `json:"completionList,omitempty"`
}

type HoverClientCapabilities struct {
	DynamicRegistration bool         `json:"dynamicRegistration,omitempty"`
	ContentFormat       []MarkupKind `json:"contentFormat,omitempty"`
}

type SignatureHelpClientCapabilities struct {
	DynamicRegistration  bool `json:"dynamicRegistration,omitempty"`
	SignatureInformation *struct {
		DocumentationFormat  []MarkupKind `json:"documentationFormat,omitempty"`
		ParameterInformation *struct {
			LabelOffsetSupport bool `json:"labelOffsetSupport,omitempty"`
		} `json:"parameterInformation,omitempty"`
		ActiveParameterSupport bool `json:"activeParameterSupport,omitempty"`
	} `json:"signatureInformation,omitempty"`
	ContextSupport bool `json:"contextSupport,omitempty"`
}

type DeclarationClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
	LinkSupport         bool `json:"linkSupport,omitempty"`
}

type DefinitionClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
	LinkSupport         bool `json:"linkSupport,omitempty"`
}

type TypeDefinitionClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
	LinkSupport         bool `json:"linkSupport,omitempty"`
}

type ImplementationClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
	LinkSupport         bool `json:"linkSupport,omitempty"`
}

type ReferenceClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

type DocumentHighlightClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

type DocumentSymbolClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
	SymbolKind          *struct {
		ValueSet []SymbolKind `json:"valueSet,omitempty"`
	} `json:"symbolKind,omitempty"`
	HierarchicalDocumentSymbolSupport bool `json:"hierarchicalDocumentSymbolSupport,omitempty"`
	TagSupport                        *struct {
		ValueSet []SymbolTag `json:"valueSet"`
	} `json:"tagSupport,omitempty"`
	LabelSupport bool `json:"labelSupport,omitempty"`
}

type CodeActionClientCapabilities struct {
	DynamicRegistration      bool `json:"dynamicRegistration,omitempty"`
	CodeActionLiteralSupport *struct {
		CodeActionKind struct {
			ValueSet []CodeActionKind `json:"valueSet"`
		} `json:"codeActionKind"`
	} `json:"codeActionLiteralSupport,omitempty"`
	IsPreferredSupport bool `json:"isPreferredSupport,omitempty"`
	DisabledSupport    bool `json:"disabledSupport,omitempty"`
	DataSupport        bool `json:"dataSupport,omitempty"`
	ResolveSupport     *struct {
		Properties []string `json:"properties"`
	} `json:"resolveSupport,omitempty"`
	HonorsChangeAnnotations bool `json:"honorsChangeAnnotations,omitempty"`
}

type CodeLensClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

type DocumentLinkClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
	TooltipSupport      bool `json:"tooltipSupport,omitempty"`
}

type DocumentColorClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

type DocumentFormattingClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

type DocumentRangeFormattingClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

type DocumentOnTypeFormattingClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

type RenameClientCapabilities struct {
	DynamicRegistration           bool                          `json:"dynamicRegistration,omitempty"`
	PrepareSupport                bool                          `json:"prepareSupport,omitempty"`
	PrepareSupportDefaultBehavior PrepareSupportDefaultBehavior `json:"prepareSupportDefaultBehavior,omitempty"`
	HonorsChangeAnnotations       bool                          `json:"honorsChangeAnnotations,omitempty"`
}

type PublishDiagnosticsClientCapabilities struct {
	RelatedInformation bool `json:"relatedInformation,omitempty"`
	TagSupport         *struct {
		ValueSet []DiagnosticTag `json:"valueSet"`
	} `json:"tagSupport,omitempty"`
	VersionSupport         bool `json:"versionSupport,omitempty"`
	CodeDescriptionSupport bool `json:"codeDescriptionSupport,omitempty"`
	DataSupport            bool `json:"dataSupport,omitempty"`
}

type FoldingRangeClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
	RangeLimit          uint `json:"rangeLimit,omitempty"`
	LineFoldingOnly     bool `json:"lineFoldingOnly,omitempty"`
	FoldingRangeKind    *struct {
		ValueSet []FoldingRangeKind `json:"valueSet,omitempty"`
	} `json:"foldingRangeKind,omitempty"`
	FoldingRange *struct {
		CollapsedText bool `json:"collapsedText,omitempty"`
	} `json:"foldingRange,omitempty"`
}

type SelectionRangeClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

type LinkedEditingRangeClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

type CallHierarchyClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

type SemanticTokensClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
	Requests            struct {
		Range *struct{}   `json:"range,omitempty"`
		Full  interface{} `json:"full,omitempty"`
	} `json:"requests"`
	TokenTypes              []string      `json:"tokenTypes"`
	TokenModifiers          []string      `json:"tokenModifiers"`
	Formats                 []TokenFormat `json:"formats"`
	OverlappingTokenSupport bool          `json:"overlappingTokenSupport,omitempty"`
	MultilineTokenSupport   bool          `json:"multilineTokenSupport,omitempty"`
	ServerCancelSupport     bool          `json:"serverCancelSupport,omitempty"`
	AugmentsSyntaxTokens    bool          `json:"augmentsSyntaxTokens,omitempty"`
}

type MonikerClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

type TypeHierarchyClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

type InlineValueClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

type InlayHintClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
	ResolveSupport      *struct {
		Properties []string `json:"properties"`
	} `json:"resolveSupport,omitempty"`
}

type DiagnosticClientCapabilities struct {
	DynamicRegistration    bool `json:"dynamicRegistration,omitempty"`
	RelatedDocumentSupport bool `json:"relatedDocumentSupport,omitempty"`
}

type PrepareSupportDefaultBehavior int

const (
	PrepareSupportDefaultBehaviorIdentifier PrepareSupportDefaultBehavior = 1
)

type InsertTextMode int

const (
	InsertTextModeAsIs              InsertTextMode = 1
	InsertTextModeAdjustIndentation InsertTextMode = 2
)

type CompletionItemTag int

const (
	CompletionItemTagDeprecated CompletionItemTag = 1
)

// CompletionItemKind represents the kind of a completion item.
type CompletionItemKind int

const (
	CompletionItemKindText          CompletionItemKind = 1
	CompletionItemKindMethod        CompletionItemKind = 2
	CompletionItemKindFunction      CompletionItemKind = 3
	CompletionItemKindConstructor   CompletionItemKind = 4
	CompletionItemKindField         CompletionItemKind = 5
	CompletionItemKindVariable      CompletionItemKind = 6
	CompletionItemKindClass         CompletionItemKind = 7
	CompletionItemKindInterface     CompletionItemKind = 8
	CompletionItemKindModule        CompletionItemKind = 9
	CompletionItemKindProperty      CompletionItemKind = 10
	CompletionItemKindUnit          CompletionItemKind = 11
	CompletionItemKindValue         CompletionItemKind = 12
	CompletionItemKindEnum          CompletionItemKind = 13
	CompletionItemKindKeyword       CompletionItemKind = 14
	CompletionItemKindSnippet       CompletionItemKind = 15
	CompletionItemKindColor         CompletionItemKind = 16
	CompletionItemKindFile          CompletionItemKind = 17
	CompletionItemKindReference     CompletionItemKind = 18
	CompletionItemKindFolder        CompletionItemKind = 19
	CompletionItemKindEnumMember    CompletionItemKind = 20
	CompletionItemKindConstant      CompletionItemKind = 21
	CompletionItemKindStruct        CompletionItemKind = 22
	CompletionItemKindEvent         CompletionItemKind = 23
	CompletionItemKindOperator      CompletionItemKind = 24
	CompletionItemKindTypeParameter CompletionItemKind = 25
)

// String returns the string representation of the CompletionItemKind.
func (cik CompletionItemKind) String() string {
	switch cik {
	case CompletionItemKindText:
		return "Text"
	case CompletionItemKindMethod:
		return "Method"
	case CompletionItemKindFunction:
		return "Function"
	case CompletionItemKindConstructor:
		return "Constructor"
	case CompletionItemKindField:
		return "Field"
	case CompletionItemKindVariable:
		return "Variable"
	case CompletionItemKindClass:
		return "Class"
	case CompletionItemKindInterface:
		return "Interface"
	case CompletionItemKindModule:
		return "Module"
	case CompletionItemKindProperty:
		return "Property"
	case CompletionItemKindUnit:
		return "Unit"
	case CompletionItemKindValue:
		return "Value"
	case CompletionItemKindEnum:
		return "Enum"
	case CompletionItemKindKeyword:
		return "Keyword"
	case CompletionItemKindSnippet:
		return "Snippet"
	case CompletionItemKindColor:
		return "Color"
	case CompletionItemKindFile:
		return "File"
	case CompletionItemKindReference:
		return "Reference"
	case CompletionItemKindFolder:
		return "Folder"
	case CompletionItemKindEnumMember:
		return "EnumMember"
	case CompletionItemKindConstant:
		return "Constant"
	case CompletionItemKindStruct:
		return "Struct"
	case CompletionItemKindEvent:
		return "Event"
	case CompletionItemKindOperator:
		return "Operator"
	case CompletionItemKindTypeParameter:
		return "TypeParameter"
	default:
		return "Unknown"
	}
}

// CodeActionKind represents the kind of a code action.
type CodeActionKind string

const (
	CodeActionKindQuickFix              CodeActionKind = "quickfix"
	CodeActionKindRefactor              CodeActionKind = "refactor"
	CodeActionKindRefactorExtract       CodeActionKind = "refactor.extract"
	CodeActionKindRefactorInline        CodeActionKind = "refactor.inline"
	CodeActionKindRefactorRewrite       CodeActionKind = "refactor.rewrite"
	CodeActionKindSource                CodeActionKind = "source"
	CodeActionKindSourceOrganizeImports CodeActionKind = "source.organizeImports"
	CodeActionKindSourceFixAll          CodeActionKind = "source.fixAll"
)

// IsValid returns true if the CodeActionKind is a valid value.
func (cak CodeActionKind) IsValid() bool {
	switch cak {
	case CodeActionKindQuickFix, CodeActionKindRefactor, CodeActionKindRefactorExtract,
		CodeActionKindRefactorInline, CodeActionKindRefactorRewrite, CodeActionKindSource,
		CodeActionKindSourceOrganizeImports, CodeActionKindSourceFixAll:
		return true
	default:
		return false
	}
}

// String returns the string representation of the CodeActionKind.
func (cak CodeActionKind) String() string {
	return string(cak)
}

type DiagnosticTag int

const (
	DiagnosticTagUnnecessary DiagnosticTag = 1
	DiagnosticTagDeprecated  DiagnosticTag = 2
)

type FoldingRangeKind string

const (
	FoldingRangeKindComment FoldingRangeKind = "comment"
	FoldingRangeKindImports FoldingRangeKind = "imports"
	FoldingRangeKindRegion  FoldingRangeKind = "region"
)

type TokenFormat string

const (
	TokenFormatRelative TokenFormat = "relative"
)

// WindowClientCapabilities represents the client capabilities specific to window features.
type WindowClientCapabilities struct {
	// Whether the client supports server-initiated progress using the
	// `window/workDoneProgress/create` request.
	WorkDoneProgress bool `json:"workDoneProgress,omitempty"`

	// Capabilities specific to the showMessage request.
	ShowMessage *ShowMessageRequestClientCapabilities `json:"showMessage,omitempty"`

	// Capabilities specific to the showDocument request.
	ShowDocument *ShowDocumentClientCapabilities `json:"showDocument,omitempty"`
}

// ShowMessageRequestClientCapabilities represents the client capabilities for the showMessage request.
type ShowMessageRequestClientCapabilities struct {
	// Capabilities specific to the MessageActionItem type.
	MessageActionItem *MessageActionItemCapabilities `json:"messageActionItem,omitempty"`
}

// MessageActionItemCapabilities represents the capabilities specific to MessageActionItem.
type MessageActionItemCapabilities struct {
	// Whether the client supports additional attributes which
	// are preserved and sent back to the server in the
	// request's response.
	AdditionalPropertiesSupport bool `json:"additionalPropertiesSupport,omitempty"`
}

// ShowDocumentClientCapabilities represents the client capabilities for the showDocument request.
type ShowDocumentClientCapabilities struct {
	// The client has support for the showDocument request.
	Support bool `json:"support"`
}

// NewWindowClientCapabilities creates a new WindowClientCapabilities with default values.
func NewWindowClientCapabilities() *WindowClientCapabilities {
	return &WindowClientCapabilities{
		WorkDoneProgress: false,
		ShowMessage:      nil,
		ShowDocument:     nil,
	}
}

// NewShowMessageRequestClientCapabilities creates a new ShowMessageRequestClientCapabilities with default values.
func NewShowMessageRequestClientCapabilities() *ShowMessageRequestClientCapabilities {
	return &ShowMessageRequestClientCapabilities{
		MessageActionItem: nil,
	}
}

// NewMessageActionItemCapabilities creates a new MessageActionItemCapabilities with default values.
func NewMessageActionItemCapabilities() *MessageActionItemCapabilities {
	return &MessageActionItemCapabilities{
		AdditionalPropertiesSupport: false,
	}
}

// NewShowDocumentClientCapabilities creates a new ShowDocumentClientCapabilities with default values.
func NewShowDocumentClientCapabilities() *ShowDocumentClientCapabilities {
	return &ShowDocumentClientCapabilities{
		Support: false,
	}
}

// GeneralClientCapabilities represents general client capabilities.
type GeneralClientCapabilities struct {
	// The client supports applying batch edits to the workspace.
	StaleRequestSupport *StaleRequestSupportClientCapabilities `json:"staleRequestSupport,omitempty"`

	// Client capabilities specific to regular expressions.
	RegularExpressions *RegularExpressionsClientCapabilities `json:"regularExpressions,omitempty"`

	// Client capabilities specific to the client's markdown parser.
	Markdown *MarkdownClientCapabilities `json:"markdown,omitempty"`

	// The position encodings supported by the client.
	PositionEncodings []PositionEncodingKind `json:"positionEncodings,omitempty"`
}

// StaleRequestSupportClientCapabilities represents the client's capabilities for handling stale requests.
type StaleRequestSupportClientCapabilities struct {
	// The client will actively cancel the request.
	Cancel bool `json:"cancel"`

	// The list of requests for which the client will retry the request if it
	// receives a response with error code ContentModified.
	RetryOnContentModified []string `json:"retryOnContentModified"`
}

// RegularExpressionsClientCapabilities represents the client's capabilities for regular expressions.
type RegularExpressionsClientCapabilities struct {
	// The engine's name.
	Engine string `json:"engine"`

	// The engine's version.
	Version string `json:"version,omitempty"`
}

// MarkdownClientCapabilities represents the client's capabilities for markdown support.
type MarkdownClientCapabilities struct {
	// The name of the parser.
	Parser string `json:"parser"`

	// The version of the parser.
	Version string `json:"version,omitempty"`

	// A list of HTML tags that the client allows / supports in Markdown.
	AllowedTags []string `json:"allowedTags,omitempty"`
}

// NewGeneralClientCapabilities creates a new GeneralClientCapabilities with default values.
func NewGeneralClientCapabilities() *GeneralClientCapabilities {
	return &GeneralClientCapabilities{
		StaleRequestSupport: nil,
		RegularExpressions:  nil,
		Markdown:            nil,
		PositionEncodings:   []PositionEncodingKind{PositionEncodingKindUTF16},
	}
}

// NewStaleRequestSupportClientCapabilities creates a new StaleRequestSupportClientCapabilities with default values.
func NewStaleRequestSupportClientCapabilities() *StaleRequestSupportClientCapabilities {
	return &StaleRequestSupportClientCapabilities{
		Cancel:                 false,
		RetryOnContentModified: []string{},
	}
}

// NewRegularExpressionsClientCapabilities creates a new RegularExpressionsClientCapabilities with default values.
func NewRegularExpressionsClientCapabilities() *RegularExpressionsClientCapabilities {
	return &RegularExpressionsClientCapabilities{
		Engine:  "",
		Version: "",
	}
}

// NewMarkdownClientCapabilities creates a new MarkdownClientCapabilities with default values.
func NewMarkdownClientCapabilities() *MarkdownClientCapabilities {
	return &MarkdownClientCapabilities{
		Parser:      "",
		Version:     "",
		AllowedTags: []string{},
	}
}

// ClientCapabilities represents the capabilities provided by the client.
type ClientCapabilities struct {
	// Workspace specific client capabilities.
	Workspace *WorkspaceClientCapabilities `json:"workspace,omitempty"`

	// Text document specific client capabilities.
	TextDocument *TextDocumentClientCapabilities `json:"textDocument,omitempty"`

	// NotebookDocument specific client capabilities.
	NotebookDocument *NotebookDocumentClientCapabilities `json:"notebookDocument,omitempty"`

	// Window specific client capabilities.
	Window *WindowClientCapabilities `json:"window,omitempty"`

	// General client capabilities.
	General *GeneralClientCapabilities `json:"general,omitempty"`

	// Experimental client capabilities.
	Experimental interface{} `json:"experimental,omitempty"`
}

// NotebookDocumentClientCapabilities defines capabilities the client has for notebook documents.
type NotebookDocumentClientCapabilities struct {
	// Capabilities specific to notebook document synchronization
	Synchronization *NotebookDocumentSyncClientCapabilities `json:"synchronization,omitempty"`
}

// NotebookDocumentSyncClientCapabilities defines client capabilities for notebook document synchronization.
type NotebookDocumentSyncClientCapabilities struct {
	// Whether the client supports dynamic registration.
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`

	// The client supports sending execution summary data per cell.
	ExecutionSummarySupport bool `json:"executionSummarySupport,omitempty"`
}

// NewClientCapabilities creates a new ClientCapabilities with default values.
func NewClientCapabilities() *ClientCapabilities {
	return &ClientCapabilities{
		Workspace:        NewWorkspaceClientCapabilities(),
		TextDocument:     NewTextDocumentClientCapabilities(),
		NotebookDocument: NewNotebookDocumentClientCapabilities(),
		Window:           NewWindowClientCapabilities(),
		General:          NewGeneralClientCapabilities(),
		Experimental:     nil,
	}
}

// NewNotebookDocumentClientCapabilities creates a new NotebookDocumentClientCapabilities with default values.
func NewNotebookDocumentClientCapabilities() *NotebookDocumentClientCapabilities {
	return &NotebookDocumentClientCapabilities{
		Synchronization: NewNotebookDocumentSyncClientCapabilities(),
	}
}

// NewNotebookDocumentSyncClientCapabilities creates a new NotebookDocumentSyncClientCapabilities with default values.
func NewNotebookDocumentSyncClientCapabilities() *NotebookDocumentSyncClientCapabilities {
	return &NotebookDocumentSyncClientCapabilities{
		DynamicRegistration:     false,
		ExecutionSummarySupport: false,
	}
}
