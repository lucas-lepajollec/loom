package loom

import (
	"fmt"
	"strings"
)

// project_id is an optional read scope for Brain HTTP/MCP calls. It uses the
// same selected sources and individual transcript references as the prompt.
func scopedBrainProject(id string, sources []string, paths map[string][]string) ([]string, map[string][]string, error) {
	if id == "" {
		return sources, paths, nil
	}
	p, ok := getProject(id)
	if !ok {
		return nil, nil, fmt.Errorf("project not found or locked")
	}
	if len(sources) == 0 {
		sources = effectiveProjectBrainSources(p)
	}
	if len(sources) == 0 {
		return nil, nil, fmt.Errorf("this project has no selected Brain sources")
	}
	allowedSources := effectiveProjectBrainSources(p)
	for _, source := range sources {
		if !hasName(allowedSources, source) {
			return nil, nil, fmt.Errorf("source not selected for this project")
		}
	}
	scoped := map[string][]string{}
	for source, prefixes := range paths {
		scoped[source] = append([]string{}, prefixes...)
	}
	allowed := projectReferenceScope(p)["conversations"]
	if requested, present := paths["conversations"]; present {
		// A caller's narrower selection remains narrower, including explicit [].
		intersection := []string{}
		for _, a := range allowed {
			for _, b := range requested {
				b = strings.TrimSuffix(b, "/")
				if a == b || strings.HasPrefix(a, b+"/") {
					intersection = append(intersection, a)
				} else if strings.HasPrefix(b, a+"/") {
					intersection = append(intersection, b)
				}
			}
		}
		allowed = intersection
	}
	scoped["conversations"] = allowed
	return sources, scoped, nil
}
