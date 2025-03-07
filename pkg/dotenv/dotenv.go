package dotenv

import (
	"bytes"
	"errors"
	"fmt"
	_ "github.com/KoNekoD/rootpath/pkg/rootpath"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

const (
	charComment       = '#'
	prefixSingleQuote = '\''
	prefixDoubleQuote = '"'

	exportPrefix = "export"
)

var (
	EnvKey     = "APP_ENV"
	DefaultEnv = "dev"

	escapeRegex        = regexp.MustCompile(`\\.`)
	expandVarRegex     = regexp.MustCompile(`(\\)?(\$)(\()?\{?([A-Z0-9_]+)?\}?`)
	unescapeCharsRegex = regexp.MustCompile(`\\([^$])`)
)

// LoadEnv loads env files by path, in order of precedence
func LoadEnv(path ...string) error {
	if len(path) != 1 {
		path = []string{".env"}
	}
	p := path[0]

	var originalVarNames []string
	for _, v := range os.Environ() {
		originalVarNames = append(originalVarNames, strings.Split(v, "=")[0])
	}

	filesFn := []func() string{
		func() string { return fmt.Sprintf("%s", p) },
		func() string { return fmt.Sprintf("%s.local", p) },
		func() string { return fmt.Sprintf("%s.%s", p, appEnv()) },
		func() string { return fmt.Sprintf("%s.%s.local", p, appEnv()) },
	}

	for _, f := range filesFn {
		file := f()

		individualEnvMap, individualErr := readFile(file)
		if individualErr != nil {
			return individualErr
		}
		for k, v := range individualEnvMap {
			if !slices.Contains(originalVarNames, k) {
				_ = os.Setenv(k, v)
			}
		}
	}

	return nil
}

func appEnv() string {
	env := os.Getenv(EnvKey)
	if env == "" {
		env, _ = DefaultEnv, os.Setenv(EnvKey, DefaultEnv)
	}

	return env
}

func readFile(filename string) (map[string]string, error) {
	file, err := os.Open(filename)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	} else if errors.Is(err, os.ErrNotExist) {
		return make(map[string]string), nil
	}
	defer func() { _ = file.Close() }()

	var buf bytes.Buffer
	_, err = io.Copy(&buf, file)
	if err != nil {
		return nil, err
	}

	out := make(map[string]string)

	src := buf.Bytes()

	src = bytes.Replace(src, []byte("\r\n"), []byte("\n"), -1)
	cutset := src
	for {
		if cutset = getStatementStart(cutset); cutset == nil {
			break
		}

		key, left, err := locateKeyName(cutset)
		if err != nil {
			return out, err
		}

		value, left, err := extractVarValue(left, out)
		if err != nil {
			return out, err
		}

		out[key], cutset = value, left
	}

	return out, err
}

func extractVarValue(src []byte, vars map[string]string) (value string, rest []byte, err error) {
	quote, hasPrefix := hasQuotePrefix(src)
	if !hasPrefix {
		// unquoted value - read until end of line
		endOfLine := bytes.IndexFunc(src, isLineEnd)

		// Hit EOF without a trailing newline
		if endOfLine == -1 {
			endOfLine = len(src)

			if endOfLine == 0 {
				return "", nil, nil
			}
		}

		// Convert line to rune away to do accurate countback of runes
		line := []rune(string(src[0:endOfLine]))

		// Assume end of line is end of var
		endOfVar := len(line)
		if endOfVar == 0 {
			return "", src[endOfLine:], nil
		}

		// Work backwards to check if the line ends in whitespace then
		// a comment (ie asdasd # some comment)
		for i := endOfVar - 1; i >= 0; i-- {
			if line[i] == charComment && i > 0 {
				if isSpace(line[i-1]) {
					endOfVar = i
					break
				}
			}
		}

		trimmed := strings.TrimFunc(string(line[0:endOfVar]), isSpace)

		return expandVariables(trimmed, vars), src[endOfLine:], nil
	}

	// lookup quoted string terminator
	for i := 1; i < len(src); i++ {
		if char := src[i]; char != quote {
			continue
		}

		// skip escaped quote symbol (\" or \', depends on quote)
		if prevChar := src[i-1]; prevChar == '\\' {
			continue
		}

		// trim quotes
		trimFunc := isCharFunc(rune(quote))
		value = string(bytes.TrimLeftFunc(bytes.TrimRightFunc(src[0:i], trimFunc), trimFunc))
		if quote == prefixDoubleQuote {
			// unescape newlines for double quote (this is compat feature)
			// and expand environment variables
			value = expandVariables(expandEscapes(value), vars)
		}

		return value, src[i+1:], nil
	}

	// return formatted error if quoted string is not terminated
	valEndIndex := bytes.IndexFunc(src, isCharFunc('\n'))
	if valEndIndex == -1 {
		valEndIndex = len(src)
	}

	return "", nil, fmt.Errorf("unterminated quoted value %s", src[:valEndIndex])
}

func expandEscapes(str string) string {
	out := escapeRegex.ReplaceAllStringFunc(str, func(match string) string {
		c := strings.TrimPrefix(match, `\`)
		switch c {
		case "n":
			return "\n"
		case "r":
			return "\r"
		default:
			return match
		}
	})
	return unescapeCharsRegex.ReplaceAllString(out, "$1")
}

func expandVariables(v string, m map[string]string) string {
	return expandVarRegex.ReplaceAllStringFunc(v, func(s string) string {
		submatch := expandVarRegex.FindStringSubmatch(s)

		if submatch == nil {
			return s
		}
		if submatch[1] == "\\" || submatch[2] == "(" {
			return submatch[0][1:]
		} else if submatch[4] != "" {
			return m[submatch[4]]
		}
		return s
	})
}

func getStatementStart(src []byte) []byte {
	pos := indexOfNonSpaceChar(src)
	if pos == -1 {
		return nil
	}

	src = src[pos:]
	if src[0] != charComment {
		return src
	}

	// skip comment section
	pos = bytes.IndexFunc(src, isCharFunc('\n'))
	if pos == -1 {
		return nil
	}

	return getStatementStart(src[pos:])
}

func locateKeyName(src []byte) (key string, cutset []byte, err error) {
	// trim "export" and space at beginning
	src = bytes.TrimLeftFunc(src, isSpace)
	if bytes.HasPrefix(src, []byte(exportPrefix)) {
		trimmed := bytes.TrimPrefix(src, []byte(exportPrefix))
		if bytes.IndexFunc(trimmed, isSpace) == 0 {
			src = bytes.TrimLeftFunc(trimmed, isSpace)
		}
	}

	// locate key name end and validate it in single loop
	offset := 0
loop:
	for i, char := range src {
		rchar := rune(char)
		if isSpace(rchar) {
			continue
		}

		switch char {
		case '=', ':':
			// library also supports yaml-style value declaration
			key = string(src[0:i])
			offset = i + 1
			break loop
		case '_':
		default:
			// variable name should match [A-Za-z0-9_.]
			if unicode.IsLetter(rchar) || unicode.IsNumber(rchar) || rchar == '.' {
				continue
			}

			return "", nil, fmt.Errorf(
				`unexpected character %q in variable name near %q`,
				string(char), string(src))
		}
	}

	if len(src) == 0 {
		return "", nil, errors.New("zero length string")
	}

	// trim whitespace
	key = strings.TrimRightFunc(key, unicode.IsSpace)
	cutset = bytes.TrimLeftFunc(src[offset:], isSpace)
	return key, cutset, nil
}

func indexOfNonSpaceChar(src []byte) int {
	return bytes.IndexFunc(src, func(r rune) bool { return !unicode.IsSpace(r) })
}

func isSpace(r rune) bool {
	return slices.Contains([]rune{'\t', '\v', '\f', '\r', ' ', 0x85, 0xA0}, r)
}

func isCharFunc(char rune) func(rune) bool {
	return func(v rune) bool { return v == char }
}

func isLineEnd(r rune) bool {
	return slices.Contains([]rune{'\n', '\r'}, r)
}

func hasQuotePrefix(src []byte) (prefix byte, isQuoted bool) {
	if len(src) > 0 && slices.Contains([]byte{prefixDoubleQuote, prefixSingleQuote}, src[0]) {
		return src[0], true
	}
	return 0, false
}
