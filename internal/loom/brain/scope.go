package brain

import (
	"errors"
	"strings"
)

// PathPrefixes optionally narrows a selected source to individual documents or
// subtrees. An explicitly empty list denies that source; absent means unfiltered.
func allowsPath(scope map[string][]string, source, path string) bool {
	prefixes, constrained := scope[source]
	if !constrained {
		return true
	}
	for _, prefix := range prefixes {
		prefix = strings.TrimSuffix(prefix, "/")
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}
func validateScope(scope map[string][]string) error {
	if len(scope) > MaxSources {
		return errors.New("too many path scopes")
	}
	for _, prefixes := range scope {
		if len(prefixes) > 32 {
			return errors.New("maximum 32 path prefixes per source")
		}
		for _, prefix := range prefixes {
			if len(prefix) > 500 || !safeRelative(strings.TrimSuffix(prefix, "/")) {
				return errors.New("invalid path prefix")
			}
		}
	}
	return nil
}
