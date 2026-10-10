package harness

import (
	_ "embed"
	"encoding/json"
	"slices"
	"sort"
)

// Record is the single identity and launcher record shared by builtin ACP,
// inspection/lifecycle, SSH recipes and Node recipes. Nil consumer metadata
// means unsupported, not a default command. Custom ACP remains user-defined.
type Record struct {
	ID       string       `json:"id"`
	Name     string       `json:"name"`
	Logo     string       `json:"logo"`
	Docs     string       `json:"docs"`
	Launcher *Launcher    `json:"launcher"`
	Inspect  *InspectSpec `json:"inspect"`
	Remote   *RemoteSpec  `json:"remote"`
}

type Launcher struct {
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args"`
	Detect  []string `json:"detect"`
	Self    bool     `json:"self,omitempty"` // Loom's existing Antigravity ACP bridge
}

type RemoteSpec struct {
	Order int `json:"order"`
}

type RemoteRecipe struct {
	ID, Name, Logo string
	Needs, Launch  []string
}

type LatestSpec struct {
	NPM    string `json:"npm,omitempty"`
	GitHub string `json:"github,omitempty"`
}

type InspectCmd struct {
	Cmd       []string `json:"cmd"`
	File      string   `json:"file"`
	Format    string   `json:"format"`
	Connected string   `json:"connected"`
	Method    string   `json:"method"`
	Account   string   `json:"account"`
}

type InspectSpec struct {
	NativeRoots   []string            `json:"native_roots"`
	RepairPaths   []string            `json:"repair_paths"`
	NativeUpdate  []string            `json:"native_update"`
	BrewNames     []string            `json:"brew_names"`
	Install       map[string][]string `json:"install"`
	Latest        *LatestSpec         `json:"latest"`
	Requires      []string            `json:"requires"`
	RequiresOS    map[string][]string `json:"requires_os"`
	UpdateInstall bool                `json:"update_install"`
	Unverified    bool                `json:"unverified"`
	Source        string              `json:"source"`
	Binary        string              `json:"binary"`
	Version       []string            `json:"version"`
	Update        []string            `json:"update"`
	Auth          *InspectCmd         `json:"auth"`
	MCP           *InspectCmd         `json:"mcp"`
	Plugins       *InspectCmd         `json:"plugins"`
	Skills        []string            `json:"skills"`
	Env           []string            `json:"env"`
}

//go:embed catalog.json
var catalogJSON []byte

// Catalog returns an independent snapshot. Consumers may amend their local
// inspection caches without changing launchers or future snapshots.
func Catalog() []Record {
	var records []Record
	if err := json.Unmarshal(catalogJSON, &records); err != nil {
		panic("invalid harness catalog: " + err.Error())
	}
	return records
}

func Lookup(id string) (Record, bool) {
	for _, r := range Catalog() {
		if r.ID == id {
			return r, true
		}
	}
	return Record{}, false
}

func Inspections() map[string]InspectSpec {
	out := map[string]InspectSpec{}
	for _, r := range Catalog() {
		if r.Inspect != nil {
			out[r.ID] = *r.Inspect
		}
	}
	return out
}

// RemoteRecipes derives argv and command prerequisites from the same launcher
// used locally. Self bridges and entries without explicit remote support are
// never made executable. Shell quoting and Node validation stay in Loom.
func RemoteRecipes() []RemoteRecipe {
	records := Catalog()
	records = slices.DeleteFunc(records, func(r Record) bool { return r.Remote == nil })
	sort.Slice(records, func(i, j int) bool { return records[i].Remote.Order < records[j].Remote.Order })
	out := []RemoteRecipe{}
	for _, r := range records {
		if r.Launcher == nil || r.Launcher.Self || r.Launcher.Command == "" {
			panic("invalid remote harness launcher: " + r.ID)
		}
		l := r.Launcher
		needs := append([]string{}, l.Detect...)
		if !slices.Contains(needs, l.Command) {
			needs = append(needs, l.Command)
		}
		out = append(out, RemoteRecipe{r.ID, r.Name, r.Logo, needs, append([]string{l.Command}, l.Args...)})
	}
	return out
}
