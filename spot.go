package godoc

// ----------------------------------------------------------------------------
// SpotInfo

// A SpotInfo value describes a particular identifier spot in a given file;
// It encodes three values: the SpotKind (declaration or use), a line or
// snippet index "lori", and whether it's a line or index.
//
// The following encoding is used:
//
//   bits    32   4    1       0
//   value    [lori|kind|isIndex]
//
type SpotInfo uint32

// SpotKind describes whether an identifier is declared (and what kind of
// declaration) or used.
type SpotKind uint32

const (
	PackageClause SpotKind = iota
	ImportDecl
	ConstDecl
	TypeDecl
	VarDecl
	FuncDecl
	MethodDecl
	Use
	nKinds
)

var (
	// These must match the SpotKind values above.
	name = []string{
		"Packages",
		"Imports",
		"Constants",
		"Types",
		"Variables",
		"Functions",
		"Methods",
		"Uses",
		"Unknown",
	}
)

func (x SpotKind) Name() string { return name[x] }

func init() {
	// sanity check: if nKinds is too large, the SpotInfo
	// accessor functions may need to be updated
	if nKinds > 8 {
		panic("internal error: nKinds > 8")
	}
}

func (x SpotInfo) Kind() SpotKind { return SpotKind(x >> 1 & 7) }
func (x SpotInfo) Lori() int      { return int(x >> 4) }
func (x SpotInfo) IsIndex() bool  { return x&1 != 0 }
