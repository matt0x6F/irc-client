package extension

// Kind distinguishes the runtime backing an extension.
type Kind string

const (
	KindScript Kind = "script"
	KindPlugin Kind = "plugin"
)

// Status is an extension's lifecycle state.
type Status string

const (
	StatusLoaded   Status = "loaded"
	StatusDisabled Status = "disabled"
	StatusError    Status = "error"
	StatusRunaway  Status = "runaway"
)

// ID uniquely identifies an extension within its kind (e.g. a script directory name).
type ID string

// Extension is the runtime-agnostic identity and status of a loaded extension.
type Extension struct {
	ID      ID
	Name    string
	Kind    Kind
	Enabled bool
	Status  Status
	Perms   []string
	Err     string // last load/dispatch error; "" if none
}
