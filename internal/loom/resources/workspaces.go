package resources

// Workspace links a reusable working folder to its execution machine. It is
// separate from a logical project and from a harness's native permissions.
type Workspace struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Target  string `json:"target"`
	Path    string `json:"path"`
	Default bool   `json:"default"`
	Managed bool   `json:"managed,omitempty"`
}

type Workspaces struct {
	Items    []Workspace       `json:"items"`
	Defaults map[string]string `json:"defaults"`
}
