package brain

import "github.com/google/jsonschema-go/jsonschema"

// The SDK's jsonschema tags are descriptions; constraints are explicit here.
func memoryToolSchema[T any]() *jsonschema.Schema {
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		panic(err)
	}
	var constrain func(*jsonschema.Schema)
	constrain = func(s *jsonschema.Schema) {
		for name, p := range s.Properties {
			switch name {
			case "id":
				p.Pattern = memoryID.String()
			case "class":
				p.Enum = memoryEnum(memoryClasses)
			case "classes":
				p.Items.Enum = memoryEnum(memoryClasses)
			case "scope":
				p.Pattern = `^(global|(project|machine|agent|task):[^\s\x00].*)$`
			case "scopes":
				p.Items.Pattern = `^(global|(project|machine|agent|task):[^\s\x00].*)$`
			case "status":
				p.Enum = memoryEnum([]string{"active", "superseded", "uncertain", "expired"})
			case "kind":
				p.Enum = memoryEnum([]string{"user", "agent", "discussion", "import", "distilled"})
			case "importance", "confidence":
				low, high := 0.0, 1.0
				p.Minimum, p.Maximum = &low, &high
			case "message_index", "limit":
				low := 0.0
				p.Minimum = &low
			case "text":
				low, high := 1, 8<<10
				p.MinLength, p.MaxLength = &low, &high
			}
			constrain(p)
		}
	}
	constrain(schema)
	switch any(*new(T)).(type) {
	case RememberRequest:
		schema.Properties["importance"].Default = []byte("0.5")
		schema.Properties["confidence"].Default = []byte("0.7")
		schema.Properties["status"].Default = []byte(`"active"`)
	case MemoryFilter:
		schema.Properties["status"].Enum = append(schema.Properties["status"].Enum, "all")
		schema.Properties["status"].Default = []byte(`"active"`)
	}
	return schema
}
func memoryEnum(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}
