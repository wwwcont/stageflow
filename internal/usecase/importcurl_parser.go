package usecase

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"stageflow/internal/domain"
)

type CurlParseError struct {
	Message string
}

func (e *CurlParseError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("curl parse error: %s", e.Message)
}

type UnsupportedCurlFlagError struct {
	Flag string
}

func (e *UnsupportedCurlFlagError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("unsupported curl flag %q", e.Flag)
}

type DefaultCurlCommandParser struct{}

func NewDefaultCurlCommandParser() *DefaultCurlCommandParser {
	return &DefaultCurlCommandParser{}
}

func (p *DefaultCurlCommandParser) Parse(command string) (domain.RequestSpec, error) {
	tokens, err := tokenizeCurlCommand(command)
	if err != nil {
		return domain.RequestSpec{}, err
	}
	if len(tokens) == 0 {
		return domain.RequestSpec{}, &CurlParseError{Message: "empty command"}
	}
	if tokens[0] != "curl" {
		return domain.RequestSpec{}, &CurlParseError{Message: "command must start with curl"}
	}

	var (
		method    string
		urlValue  string
		headers   = make(map[string]string)
		bodyParts []string
		hasBody   bool
	)

	for idx := 1; idx < len(tokens); idx++ {
		token := tokens[idx]
		if flag, value, ok := splitInlineCurlFlag(token); ok {
			switch flag {
			case "-X", "--request":
				method = strings.ToUpper(strings.TrimSpace(value))
			case "-H", "--header":
				name, headerValue, err := parseHeader(value)
				if err != nil {
					return domain.RequestSpec{}, err
				}
				headers[name] = headerValue
			case "-d", "--data", "--data-raw":
				bodyParts = append(bodyParts, value)
				hasBody = true
			case "--url":
				if urlValue != "" {
					return domain.RequestSpec{}, &CurlParseError{Message: "multiple URLs provided"}
				}
				urlValue = value
			}
			continue
		}

		switch token {
		case "-X", "--request":
			value, next, err := requireValue(tokens, idx, token)
			if err != nil {
				return domain.RequestSpec{}, err
			}
			method = strings.ToUpper(strings.TrimSpace(value))
			idx = next
		case "-H", "--header":
			value, next, err := requireValue(tokens, idx, token)
			if err != nil {
				return domain.RequestSpec{}, err
			}
			name, headerValue, err := parseHeader(value)
			if err != nil {
				return domain.RequestSpec{}, err
			}
			headers[name] = headerValue
			idx = next
		case "-d", "--data", "--data-raw":
			value, next, err := requireValue(tokens, idx, token)
			if err != nil {
				return domain.RequestSpec{}, err
			}
			bodyParts = append(bodyParts, value)
			hasBody = true
			idx = next
		case "--url":
			value, next, err := requireValue(tokens, idx, token)
			if err != nil {
				return domain.RequestSpec{}, err
			}
			if urlValue != "" {
				return domain.RequestSpec{}, &CurlParseError{Message: "multiple URLs provided"}
			}
			urlValue = value
			idx = next
		default:
			if strings.HasPrefix(token, "-") {
				return domain.RequestSpec{}, &UnsupportedCurlFlagError{Flag: token}
			}
			if urlValue != "" {
				return domain.RequestSpec{}, &CurlParseError{Message: fmt.Sprintf("unexpected token %q after URL", token)}
			}
			urlValue = token
		}
	}

	if urlValue == "" {
		return domain.RequestSpec{}, &CurlParseError{Message: "URL is required"}
	}
	parsedURL, err := url.Parse(urlValue)
	if err != nil {
		return domain.RequestSpec{}, &CurlParseError{Message: fmt.Sprintf("invalid URL: %v", err)}
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return domain.RequestSpec{}, &CurlParseError{Message: "URL must use http or https"}
	}
	if parsedURL.Host == "" {
		return domain.RequestSpec{}, &CurlParseError{Message: "URL must contain a host"}
	}
	if parsedURL.Fragment != "" {
		return domain.RequestSpec{}, &CurlParseError{Message: "URL fragments are not supported"}
	}

	query, err := toSingleValueQuery(parsedURL.Query())
	if err != nil {
		return domain.RequestSpec{}, err
	}
	parsedURL.RawQuery = ""
	parsedURL.Fragment = ""
	if parsedURL.Path == "" {
		parsedURL.Path = "/"
	}

	if method == "" {
		if hasBody {
			method = "POST"
		} else {
			method = "GET"
		}
	}

	requestSpec := domain.RequestSpec{
		Method:       method,
		URLTemplate:  parsedURL.String(),
		Headers:      normalizeStringMap(headers),
		Query:        query,
		BodyTemplate: strings.Join(bodyParts, "&"),
	}
	if err := requestSpec.Validate(); err != nil {
		return domain.RequestSpec{}, err
	}
	return requestSpec, nil
}

func requireValue(tokens []string, idx int, flag string) (string, int, error) {
	if idx+1 >= len(tokens) {
		return "", idx, &CurlParseError{Message: fmt.Sprintf("flag %s requires a value", flag)}
	}
	return tokens[idx+1], idx + 1, nil
}

func splitInlineCurlFlag(token string) (flag string, value string, ok bool) {
	switch {
	case strings.HasPrefix(token, "--request="):
		return "--request", strings.TrimPrefix(token, "--request="), true
	case strings.HasPrefix(token, "--header="):
		return "--header", strings.TrimPrefix(token, "--header="), true
	case strings.HasPrefix(token, "--data="):
		return "--data", strings.TrimPrefix(token, "--data="), true
	case strings.HasPrefix(token, "--data-raw="):
		return "--data-raw", strings.TrimPrefix(token, "--data-raw="), true
	case strings.HasPrefix(token, "--url="):
		return "--url", strings.TrimPrefix(token, "--url="), true
	case len(token) > 2 && strings.HasPrefix(token, "-X"):
		return "-X", token[2:], true
	case len(token) > 2 && strings.HasPrefix(token, "-H"):
		return "-H", token[2:], true
	case len(token) > 2 && strings.HasPrefix(token, "-d"):
		return "-d", token[2:], true
	default:
		return "", "", false
	}
}

func parseHeader(raw string) (string, string, error) {
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 {
		return "", "", &CurlParseError{Message: fmt.Sprintf("invalid header %q", raw)}
	}
	name := strings.TrimSpace(parts[0])
	if name == "" {
		return "", "", &CurlParseError{Message: fmt.Sprintf("invalid header %q", raw)}
	}
	return name, strings.TrimSpace(parts[1]), nil
}

func toSingleValueQuery(values url.Values) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	query := make(map[string]string, len(values))
	for key, items := range values {
		if len(items) > 1 {
			return nil, &CurlParseError{Message: fmt.Sprintf("query parameter %q has multiple values and is not supported", key)}
		}
		query[key] = items[0]
	}
	return normalizeStringMap(query), nil
}

func normalizeStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make(map[string]string, len(input))
	for _, key := range keys {
		result[key] = input[key]
	}
	return result
}

func tokenizeCurlCommand(input string) ([]string, error) {
	var (
		tokens   []string
		current  strings.Builder
		inSingle bool
		inDouble bool
		escaped  bool
	)

	runes := []rune(input)

	flush := func() {
		if current.Len() == 0 {
			return
		}
		tokens = append(tokens, current.String())
		current.Reset()
	}

	for idx := 0; idx < len(runes); idx++ {
		r := runes[idx]
		switch {
		case escaped:
			escaped = false
			if r == '\n' {
				continue
			}
			if r == '\r' {
				if idx+1 < len(runes) && runes[idx+1] == '\n' {
					idx++
				}
				continue
			}
			current.WriteRune(r)
		case r == '\\' && !inSingle:
			escaped = true
		case r == '\'' && !inDouble:
			inSingle = !inSingle
		case r == '"' && !inSingle:
			inDouble = !inDouble
		case (r == ' ' || r == '\t' || r == '\n' || r == '\r') && !inSingle && !inDouble:
			flush()
		default:
			current.WriteRune(r)
		}
	}

	if escaped {
		return nil, &CurlParseError{Message: "unfinished escape sequence"}
	}
	if inSingle || inDouble {
		return nil, &CurlParseError{Message: "unclosed quote in curl command"}
	}
	flush()
	return tokens, nil
}
