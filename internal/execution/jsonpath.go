package execution

import (
	"fmt"
	"strconv"
	"strings"
)

func resolveJSONPath(root any, selector string) (any, bool, error) {
	selector = strings.TrimSpace(selector)
	if selector == "$" {
		return root, true, nil
	}
	if !strings.HasPrefix(selector, "$.") {
		return nil, false, fmt.Errorf("unsupported selector %q", selector)
	}
	segments := strings.Split(strings.TrimPrefix(selector, "$."), ".")
	current := root
	for _, segment := range segments {
		if segment == "" {
			return nil, false, fmt.Errorf("invalid selector %q", selector)
		}
		switch typed := current.(type) {
		case map[string]any:
			value, ok := typed[segment]
			if !ok {
				return nil, false, nil
			}
			current = value
		case []any:
			idx, err := strconv.Atoi(segment)
			if err != nil {
				return nil, false, fmt.Errorf("selector %q references non-numeric array segment %q", selector, segment)
			}
			if idx < 0 || idx >= len(typed) {
				return nil, false, nil
			}
			current = typed[idx]
		default:
			return nil, false, nil
		}
	}
	return current, true, nil
}
