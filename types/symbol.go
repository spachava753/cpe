package types

// SymbolKind represents the kind of a symbol.
type SymbolKind int

const (
	SymbolKindFile          SymbolKind = 1
	SymbolKindModule        SymbolKind = 2
	SymbolKindNamespace     SymbolKind = 3
	SymbolKindPackage       SymbolKind = 4
	SymbolKindClass         SymbolKind = 5
	SymbolKindMethod        SymbolKind = 6
	SymbolKindProperty      SymbolKind = 7
	SymbolKindField         SymbolKind = 8
	SymbolKindConstructor   SymbolKind = 9
	SymbolKindEnum          SymbolKind = 10
	SymbolKindInterface     SymbolKind = 11
	SymbolKindFunction      SymbolKind = 12
	SymbolKindVariable      SymbolKind = 13
	SymbolKindConstant      SymbolKind = 14
	SymbolKindString        SymbolKind = 15
	SymbolKindNumber        SymbolKind = 16
	SymbolKindBoolean       SymbolKind = 17
	SymbolKindArray         SymbolKind = 18
	SymbolKindObject        SymbolKind = 19
	SymbolKindKey           SymbolKind = 20
	SymbolKindNull          SymbolKind = 21
	SymbolKindEnumMember    SymbolKind = 22
	SymbolKindStruct        SymbolKind = 23
	SymbolKindEvent         SymbolKind = 24
	SymbolKindOperator      SymbolKind = 25
	SymbolKindTypeParameter SymbolKind = 26
)

// String returns the string representation of the SymbolKind.
func (sk SymbolKind) String() string {
	switch sk {
	case SymbolKindFile:
		return "File"
	case SymbolKindModule:
		return "Module"
	case SymbolKindNamespace:
		return "Namespace"
	case SymbolKindPackage:
		return "Package"
	case SymbolKindClass:
		return "Class"
	case SymbolKindMethod:
		return "Method"
	case SymbolKindProperty:
		return "Property"
	case SymbolKindField:
		return "Field"
	case SymbolKindConstructor:
		return "Constructor"
	case SymbolKindEnum:
		return "Enum"
	case SymbolKindInterface:
		return "Interface"
	case SymbolKindFunction:
		return "Function"
	case SymbolKindVariable:
		return "Variable"
	case SymbolKindConstant:
		return "Constant"
	case SymbolKindString:
		return "String"
	case SymbolKindNumber:
		return "Number"
	case SymbolKindBoolean:
		return "Boolean"
	case SymbolKindArray:
		return "Array"
	case SymbolKindObject:
		return "Object"
	case SymbolKindKey:
		return "Key"
	case SymbolKindNull:
		return "Null"
	case SymbolKindEnumMember:
		return "EnumMember"
	case SymbolKindStruct:
		return "Struct"
	case SymbolKindEvent:
		return "Event"
	case SymbolKindOperator:
		return "Operator"
	case SymbolKindTypeParameter:
		return "TypeParameter"
	default:
		return "Unknown"
	}
}

// IsValid returns true if the SymbolKind is a valid value.
func (sk SymbolKind) IsValid() bool {
	return sk >= SymbolKindFile && sk <= SymbolKindTypeParameter
}

// SymbolTag represents extra information for a symbol.
type SymbolTag int

const (
	// SymbolTagDeprecated indicates the symbol is deprecated.
	SymbolTagDeprecated SymbolTag = 1
)

// String returns the string representation of the SymbolTag.
func (st SymbolTag) String() string {
	switch st {
	case SymbolTagDeprecated:
		return "Deprecated"
	default:
		return "Unknown"
	}
}

// IsValid returns true if the SymbolTag is a valid value.
func (st SymbolTag) IsValid() bool {
	return st == SymbolTagDeprecated
}
