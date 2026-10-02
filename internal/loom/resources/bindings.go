package resources

func SkillBound(b map[string]map[string]bool, skillID, target string) bool {
	if t, ok := b[skillID]; ok {
		if v, set := t[target]; set {
			return v
		}
	}
	return true
}

// JSONStore preserves Loom's existing read/failure and write semantics.
type JSONStore interface {
	GetJSON(string, any) bool
	PutJSON(string, any) error
}
type Bindings struct{ Store JSONStore }

const HarnessMCPState = "harness_mcp_bindings"
const SkillBindingsState = "skill_target_bindings"

func (b Bindings) HarnessMCPBinding(id string) *[]string {
	m := map[string][]string{}
	if !b.Store.GetJSON(HarnessMCPState, &m) {
		return nil
	}
	if names, ok := m[id]; ok {
		return &names
	}
	return nil
}

func (b Bindings) SkillBindings() map[string]map[string]bool {
	m := map[string]map[string]bool{}
	_ = b.Store.GetJSON(SkillBindingsState, &m)
	return m
}

func (b Bindings) SetHarnessMCPBinding(id string, names *[]string, validate func([]string) error) error {
	m := map[string][]string{}
	_ = b.Store.GetJSON(HarnessMCPState, &m)
	if names == nil {
		delete(m, id)
	} else {
		if err := validate(*names); err != nil {
			return err
		}
		m[id] = append([]string{}, *names...)
	}
	return b.Store.PutJSON(HarnessMCPState, m)
}
