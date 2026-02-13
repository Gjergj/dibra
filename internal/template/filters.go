package template

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/aisbergg/gonja/pkg/gonja"
	"github.com/aisbergg/gonja/pkg/gonja/errors"
	"github.com/aisbergg/gonja/pkg/gonja/exec"
	"gopkg.in/yaml.v3"
)

// registerAnsibleFilters adds Ansible-compatible filters to the gonja environment.
func registerAnsibleFilters(env *gonja.Environment) {
	filters := map[string]exec.FilterFunction{
		"regex_replace":        filterRegexReplace,
		"regex_search":         filterRegexSearch,
		"regex_findall":        filterRegexFindall,
		"split":                filterSplit,
		"basename":             filterBasename,
		"dirname":              filterDirname,
		"to_yaml":              filterToYAML,
		"to_nice_yaml":         filterToYAML,
		"from_yaml":            filterFromYAML,
		"from_json":            filterFromJSON,
		"to_nice_json":         filterToNiceJSON,
		"mandatory":            filterMandatory,
		"quote":                filterQuote,
		"ternary":              filterTernary,
		"combine":              filterCombine,
		"comment":              filterComment,
		"splitext":             filterSplitext,
		"hash":                 filterHash,
		"b64encode":            filterB64Encode,
		"b64decode":            filterB64Decode,
		"to_datetime":          filterToDatetime,
		"strftime":             filterStrftime,
		"type_debug":           filterTypeDebug,
		"dict2items":           filterDict2Items,
		"items2dict":           filterItems2Dict,
		"zip_longest":          filterZipLongest,
		"flatten":              filterFlatten,
		"product":              filterProduct,
		"regex_escape":         filterRegexEscape,
		"bool":                 filterAnsibleBool,
		"to_json":              filterToJSON,
		"from_yaml_all":        filterFromYAML,
		"map_attribute":        filterMapAttribute,
		"symmetric_difference": filterSymmetricDifference,
		"union":                filterUnion,
		"intersect":            filterIntersect,
		"difference":           filterDifference,
		"unique":               filterAnsibleUnique,
	}

	for name, fn := range filters {
		(*env.Filters)[name] = fn
	}
}

// regex_replace(pattern, replacement, count=0)
func filterRegexReplace(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	p := params.Expect(2, []*exec.Kwarg{{Name: "count", Default: 0}})
	if p.IsError() {
		errors.ThrowFilterArgumentError("regex_replace(pattern, replacement, count=0)", p.Error())
	}
	pattern := p.Args[0].String()
	replacement := p.Args[1].String()
	count := p.GetKwarg("count").Integer()

	re, err := regexp.Compile(pattern)
	if err != nil {
		errors.ThrowFilterArgumentError("regex_replace", "invalid regex pattern: %s", err.Error())
	}

	input := in.String()
	if count <= 0 {
		return e.ValueFactory.Value(re.ReplaceAllString(input, replacement))
	}

	result := input
	for i := 0; i < count; i++ {
		loc := re.FindStringIndex(result)
		if loc == nil {
			break
		}
		replaced := re.ReplaceAllString(result[loc[0]:loc[1]], replacement)
		result = result[:loc[0]] + replaced + result[loc[1]:]
	}
	return e.ValueFactory.Value(result)
}

// regex_search(pattern)
func filterRegexSearch(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	p := params.ExpectArgs(1)
	if p.IsError() {
		errors.ThrowFilterArgumentError("regex_search(pattern)", p.Error())
	}
	pattern := p.Args[0].String()
	re, err := regexp.Compile(pattern)
	if err != nil {
		errors.ThrowFilterArgumentError("regex_search", "invalid regex pattern: %s", err.Error())
	}

	match := re.FindString(in.String())
	if match == "" {
		return e.ValueFactory.Value("")
	}
	return e.ValueFactory.Value(match)
}

// regex_findall(pattern)
func filterRegexFindall(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	p := params.ExpectArgs(1)
	if p.IsError() {
		errors.ThrowFilterArgumentError("regex_findall(pattern)", p.Error())
	}
	pattern := p.Args[0].String()
	re, err := regexp.Compile(pattern)
	if err != nil {
		errors.ThrowFilterArgumentError("regex_findall", "invalid regex pattern: %s", err.Error())
	}

	matches := re.FindAllString(in.String(), -1)
	if matches == nil {
		return e.ValueFactory.Value([]string{})
	}
	result := make([]interface{}, len(matches))
	for i, m := range matches {
		result[i] = m
	}
	return e.ValueFactory.Value(result)
}

// split(sep)
func filterSplit(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	sep := " "
	if len(params.Args) > 0 {
		sep = params.Args[0].String()
	} else if params.HasKwarg("sep") {
		sep = params.GetKwarg("sep").String()
	}

	parts := strings.Split(in.String(), sep)
	result := make([]interface{}, len(parts))
	for i, s := range parts {
		result[i] = s
	}
	return e.ValueFactory.Value(result)
}

// basename
func filterBasename(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	if p := params.ExpectNothing(); p.IsError() {
		errors.ThrowFilterArgumentError("basename()", p.Error())
	}
	return e.ValueFactory.Value(filepath.Base(in.String()))
}

// dirname
func filterDirname(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	if p := params.ExpectNothing(); p.IsError() {
		errors.ThrowFilterArgumentError("dirname()", p.Error())
	}
	return e.ValueFactory.Value(filepath.Dir(in.String()))
}

// to_yaml / to_nice_yaml
func filterToYAML(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	if p := params.ExpectNothing(); p.IsError() {
		errors.ThrowFilterArgumentError("to_yaml()", p.Error())
	}
	data, err := yaml.Marshal(in.Interface())
	if err != nil {
		errors.ThrowFilterArgumentError("to_yaml", "failed to marshal to YAML: %s", err.Error())
	}
	return e.ValueFactory.Value(strings.TrimRight(string(data), "\n"))
}

// from_yaml
func filterFromYAML(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	if p := params.ExpectNothing(); p.IsError() {
		errors.ThrowFilterArgumentError("from_yaml()", p.Error())
	}
	var result interface{}
	if err := yaml.Unmarshal([]byte(in.String()), &result); err != nil {
		errors.ThrowFilterArgumentError("from_yaml", "failed to parse YAML: %s", err.Error())
	}
	return e.ValueFactory.Value(normalizeYAMLTypes(result))
}

// from_json
func filterFromJSON(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	if p := params.ExpectNothing(); p.IsError() {
		errors.ThrowFilterArgumentError("from_json()", p.Error())
	}
	var result interface{}
	if err := json.Unmarshal([]byte(in.String()), &result); err != nil {
		errors.ThrowFilterArgumentError("from_json", "failed to parse JSON: %s", err.Error())
	}
	return e.ValueFactory.Value(result)
}

// to_nice_json(indent=4)
func filterToNiceJSON(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	p := params.Expect(0, []*exec.Kwarg{{Name: "indent", Default: 4}})
	if p.IsError() {
		errors.ThrowFilterArgumentError("to_nice_json(indent=4)", p.Error())
	}
	indent := p.GetKwarg("indent").Integer()
	data, err := json.MarshalIndent(in.Interface(), "", strings.Repeat(" ", indent))
	if err != nil {
		errors.ThrowFilterArgumentError("to_nice_json", "failed to marshal to JSON: %s", err.Error())
	}
	return e.ValueFactory.Value(string(data))
}

// to_json (compact)
func filterToJSON(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	if p := params.ExpectNothing(); p.IsError() {
		errors.ThrowFilterArgumentError("to_json()", p.Error())
	}
	data, err := json.Marshal(in.Interface())
	if err != nil {
		errors.ThrowFilterArgumentError("to_json", "failed to marshal to JSON: %s", err.Error())
	}
	return e.ValueFactory.Value(string(data))
}

// mandatory(msg)
func filterMandatory(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	p := params.Expect(0, []*exec.Kwarg{{Name: "msg", Default: ""}})
	if p.IsError() {
		errors.ThrowFilterArgumentError("mandatory(msg='')", p.Error())
	}

	if in.IsNil() {
		msg := p.GetKwarg("msg").String()
		if msg == "" {
			msg = "mandatory variable is not defined"
		}
		errors.ThrowFilterArgumentError("mandatory", "%s", msg)
	}
	return in
}

// quote - shell-safe quoting
func filterQuote(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	if p := params.ExpectNothing(); p.IsError() {
		errors.ThrowFilterArgumentError("quote()", p.Error())
	}
	s := in.String()
	// Use single quotes with escaping for shell safety
	escaped := strings.ReplaceAll(s, "'", "'\"'\"'")
	return e.ValueFactory.Value("'" + escaped + "'")
}

// ternary(true_val, false_val, none_val=none_val)
func filterTernary(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	p := params.ExpectArgs(2)
	if p.IsError() {
		errors.ThrowFilterArgumentError("ternary(true_val, false_val)", p.Error())
	}

	if in.Bool() {
		return p.Args[0]
	}
	return p.Args[1]
}

// combine - merge dicts
func filterCombine(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	if len(params.Args) == 0 {
		errors.ThrowFilterArgumentError("combine(dict)", "at least one dictionary argument required")
	}

	result := make(map[string]interface{})
	// Copy input dict
	if in.IsDict() {
		for _, pair := range in.Items() {
			result[pair.Key.String()] = pair.Value.Interface()
		}
	}
	// Merge each argument dict
	for _, arg := range params.Args {
		if arg.IsDict() {
			for _, pair := range arg.Items() {
				result[pair.Key.String()] = pair.Value.Interface()
			}
		}
	}
	return e.ValueFactory.Value(result)
}

// comment(prefix='# ')
func filterComment(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	p := params.Expect(0, []*exec.Kwarg{{Name: "prefix", Default: "# "}})
	if p.IsError() {
		errors.ThrowFilterArgumentError("comment(prefix='# ')", p.Error())
	}
	prefix := p.GetKwarg("prefix").String()
	lines := strings.Split(in.String(), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return e.ValueFactory.Value(strings.Join(lines, "\n"))
}

// splitext - split filename and extension
func filterSplitext(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	if p := params.ExpectNothing(); p.IsError() {
		errors.ThrowFilterArgumentError("splitext()", p.Error())
	}
	s := in.String()
	ext := filepath.Ext(s)
	name := s[:len(s)-len(ext)]
	return e.ValueFactory.Value([]interface{}{name, ext})
}

// hash(algo)
func filterHash(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	p := params.ExpectArgs(1)
	if p.IsError() {
		errors.ThrowFilterArgumentError("hash(algo)", p.Error())
	}
	algo := p.Args[0].String()
	data := []byte(in.String())

	var hashStr string
	switch strings.ToLower(algo) {
	case "md5":
		h := md5.Sum(data)
		hashStr = hex.EncodeToString(h[:])
	case "sha1":
		h := sha1.Sum(data)
		hashStr = hex.EncodeToString(h[:])
	case "sha256":
		h := sha256.Sum256(data)
		hashStr = hex.EncodeToString(h[:])
	case "sha512":
		h := sha512.Sum512(data)
		hashStr = hex.EncodeToString(h[:])
	default:
		errors.ThrowFilterArgumentError("hash", "unsupported algorithm: %s", algo)
	}
	return e.ValueFactory.Value(hashStr)
}

// b64encode
func filterB64Encode(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	if p := params.ExpectNothing(); p.IsError() {
		errors.ThrowFilterArgumentError("b64encode()", p.Error())
	}
	return e.ValueFactory.Value(base64.StdEncoding.EncodeToString([]byte(in.String())))
}

// b64decode
func filterB64Decode(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	if p := params.ExpectNothing(); p.IsError() {
		errors.ThrowFilterArgumentError("b64decode()", p.Error())
	}
	decoded, err := base64.StdEncoding.DecodeString(in.String())
	if err != nil {
		errors.ThrowFilterArgumentError("b64decode", "failed to decode base64: %s", err.Error())
	}
	return e.ValueFactory.Value(string(decoded))
}

// to_datetime(format)
func filterToDatetime(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	p := params.Expect(0, []*exec.Kwarg{{Name: "format", Default: ""}})
	if p.IsError() {
		errors.ThrowFilterArgumentError("to_datetime(format='')", p.Error())
	}

	s := in.String()
	format := p.GetKwarg("format").String()

	var t time.Time
	var err error
	if format != "" {
		t, err = time.Parse(pythonToGoTimeFormat(format), s)
	} else {
		// Try common formats
		for _, f := range []string{
			time.RFC3339,
			"2006-01-02 15:04:05",
			"2006-01-02",
			time.RFC1123,
		} {
			t, err = time.Parse(f, s)
			if err == nil {
				break
			}
		}
	}
	if err != nil {
		errors.ThrowFilterArgumentError("to_datetime", "failed to parse datetime: %s", err.Error())
	}
	return e.ValueFactory.Value(t.Format(time.RFC3339))
}

// strftime(format)
func filterStrftime(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	p := params.ExpectArgs(1)
	if p.IsError() {
		errors.ThrowFilterArgumentError("strftime(format)", p.Error())
	}
	format := p.Args[0].String()
	goFmt := pythonToGoTimeFormat(format)

	s := in.String()
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		// Try other common formats
		for _, f := range []string{
			"2006-01-02 15:04:05",
			"2006-01-02",
		} {
			t, err = time.Parse(f, s)
			if err == nil {
				break
			}
		}
	}
	if err != nil {
		errors.ThrowFilterArgumentError("strftime", "failed to parse datetime: %s", err.Error())
	}
	return e.ValueFactory.Value(t.Format(goFmt))
}

// type_debug - returns Go type name
func filterTypeDebug(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	if p := params.ExpectNothing(); p.IsError() {
		errors.ThrowFilterArgumentError("type_debug()", p.Error())
	}
	v := in.Interface()
	if v == nil {
		return e.ValueFactory.Value("NoneType")
	}
	t := reflect.TypeOf(v)
	return e.ValueFactory.Value(t.String())
}

// dict2items - convert dict to list of {key, value} maps
func filterDict2Items(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	if p := params.ExpectNothing(); p.IsError() {
		errors.ThrowFilterArgumentError("dict2items()", p.Error())
	}
	if !in.IsDict() {
		errors.ThrowFilterArgumentError("dict2items", "input must be a dictionary")
	}

	var items []interface{}
	for _, pair := range in.Items() {
		items = append(items, map[string]interface{}{
			"key":   pair.Key.Interface(),
			"value": pair.Value.Interface(),
		})
	}
	return e.ValueFactory.Value(items)
}

// items2dict - convert list of {key, value} maps to dict
func filterItems2Dict(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	p := params.Expect(0, []*exec.Kwarg{
		{Name: "key_name", Default: "key"},
		{Name: "value_name", Default: "value"},
	})
	if p.IsError() {
		errors.ThrowFilterArgumentError("items2dict(key_name='key', value_name='value')", p.Error())
	}
	keyName := p.GetKwarg("key_name").String()
	valueName := p.GetKwarg("value_name").String()

	result := make(map[string]interface{})
	in.Iterate(func(idx, count int, key, value exec.Value) bool {
		// For list iteration, gonja puts the item in key and nil in value
		item := key
		if value != nil && !value.IsNil() {
			item = value
		}
		k := item.GetItem(keyName)
		v := item.GetItem(valueName)
		if k != nil && !k.IsNil() {
			result[k.String()] = v.Interface()
		}
		return true
	}, func() {})
	return e.ValueFactory.Value(result)
}

// zip_longest - zip lists, padding shorter with fillvalue
func filterZipLongest(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	if len(params.Args) < 1 {
		errors.ThrowFilterArgumentError("zip_longest(list, fillvalue=None)", "at least one list argument required")
	}

	lists := []exec.Value{in}
	lists = append(lists, params.Args...)

	maxLen := 0
	for _, l := range lists {
		if l.Len() > maxLen {
			maxLen = l.Len()
		}
	}

	var result []interface{}
	for i := 0; i < maxLen; i++ {
		var tuple []interface{}
		for _, l := range lists {
			if i < l.Len() {
				tuple = append(tuple, l.Index(i).Interface())
			} else {
				tuple = append(tuple, nil)
			}
		}
		result = append(result, tuple)
	}
	return e.ValueFactory.Value(result)
}

// flatten(depth=1)
func filterFlatten(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	p := params.Expect(0, []*exec.Kwarg{{Name: "levels", Default: 1}})
	if p.IsError() {
		errors.ThrowFilterArgumentError("flatten(levels=1)", p.Error())
	}
	depth := p.GetKwarg("levels").Integer()

	result := flattenValue(in.Interface(), depth)
	return e.ValueFactory.Value(result)
}

// product - cartesian product
func filterProduct(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	if len(params.Args) < 1 {
		errors.ThrowFilterArgumentError("product(list)", "at least one list argument required")
	}

	list1 := toSlice(in)
	list2 := toSlice(params.Args[0])

	var result []interface{}
	for _, a := range list1 {
		for _, b := range list2 {
			result = append(result, []interface{}{a, b})
		}
	}
	return e.ValueFactory.Value(result)
}

// regex_escape - escape regex special characters
func filterRegexEscape(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	if p := params.ExpectNothing(); p.IsError() {
		errors.ThrowFilterArgumentError("regex_escape()", p.Error())
	}
	return e.ValueFactory.Value(regexp.QuoteMeta(in.String()))
}

// Ansible bool filter (converts various truthy/falsy values)
func filterAnsibleBool(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	if p := params.ExpectNothing(); p.IsError() {
		errors.ThrowFilterArgumentError("bool()", p.Error())
	}
	if in.IsString() {
		s := strings.ToLower(strings.TrimSpace(in.String()))
		switch s {
		case "true", "yes", "1", "on":
			return e.ValueFactory.Value(true)
		case "false", "no", "0", "off", "":
			return e.ValueFactory.Value(false)
		}
	}
	return e.ValueFactory.Value(in.Bool())
}

// map(attribute=...) - Ansible-style attribute extraction
func filterMapAttribute(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	p := params.ExpectArgs(1)
	if p.IsError() {
		errors.ThrowFilterArgumentError("map_attribute(attribute)", p.Error())
	}
	attr := p.Args[0].String()
	var result []interface{}
	in.Iterate(func(idx, count int, key, value exec.Value) bool {
		item := key
		if value != nil && !value.IsNil() {
			item = value
		}
		result = append(result, item.GetItem(attr).Interface())
		return true
	}, func() {})
	return e.ValueFactory.Value(result)
}

// Set operations for lists
func filterSymmetricDifference(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	p := params.ExpectArgs(1)
	if p.IsError() {
		errors.ThrowFilterArgumentError("symmetric_difference(other)", p.Error())
	}
	set1 := toStringSet(in)
	set2 := toStringSet(params.Args[0])
	var result []interface{}
	for k := range set1 {
		if _, ok := set2[k]; !ok {
			result = append(result, k)
		}
	}
	for k := range set2 {
		if _, ok := set1[k]; !ok {
			result = append(result, k)
		}
	}
	return e.ValueFactory.Value(result)
}

func filterUnion(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	p := params.ExpectArgs(1)
	if p.IsError() {
		errors.ThrowFilterArgumentError("union(other)", p.Error())
	}
	seen := make(map[string]bool)
	var result []interface{}
	for _, v := range toSlice(in) {
		s := fmt.Sprint(v)
		if !seen[s] {
			seen[s] = true
			result = append(result, v)
		}
	}
	for _, v := range toSlice(params.Args[0]) {
		s := fmt.Sprint(v)
		if !seen[s] {
			seen[s] = true
			result = append(result, v)
		}
	}
	return e.ValueFactory.Value(result)
}

func filterIntersect(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	p := params.ExpectArgs(1)
	if p.IsError() {
		errors.ThrowFilterArgumentError("intersect(other)", p.Error())
	}
	set2 := toStringSet(params.Args[0])
	seen := make(map[string]bool)
	var result []interface{}
	for _, v := range toSlice(in) {
		s := fmt.Sprint(v)
		if _, ok := set2[s]; ok && !seen[s] {
			seen[s] = true
			result = append(result, v)
		}
	}
	return e.ValueFactory.Value(result)
}

func filterDifference(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	p := params.ExpectArgs(1)
	if p.IsError() {
		errors.ThrowFilterArgumentError("difference(other)", p.Error())
	}
	set2 := toStringSet(params.Args[0])
	var result []interface{}
	for _, v := range toSlice(in) {
		s := fmt.Sprint(v)
		if _, ok := set2[s]; !ok {
			result = append(result, v)
		}
	}
	return e.ValueFactory.Value(result)
}

// Ansible-compatible unique (simpler than gonja's)
func filterAnsibleUnique(e *exec.Evaluator, in exec.Value, params *exec.VarArgs) exec.Value {
	seen := make(map[string]bool)
	var result []interface{}
	in.Iterate(func(idx, count int, key, value exec.Value) bool {
		item := key
		if value != nil && !value.IsNil() {
			item = value
		}
		s := fmt.Sprint(item.Interface())
		if !seen[s] {
			seen[s] = true
			result = append(result, item.Interface())
		}
		return true
	}, func() {})
	return e.ValueFactory.Value(result)
}

// --- Helper functions ---

// normalizeYAMLTypes converts yaml.Unmarshal output types to standard Go types
// that gonja can work with (map[string]interface{} instead of map[interface{}]interface{}).
func normalizeYAMLTypes(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		for k, v2 := range val {
			val[k] = normalizeYAMLTypes(v2)
		}
		return val
	case map[interface{}]interface{}:
		result := make(map[string]interface{})
		for k, v2 := range val {
			result[fmt.Sprint(k)] = normalizeYAMLTypes(v2)
		}
		return result
	case []interface{}:
		for i, v2 := range val {
			val[i] = normalizeYAMLTypes(v2)
		}
		return val
	default:
		return v
	}
}

func flattenValue(v interface{}, depth int) []interface{} {
	var result []interface{}
	switch val := v.(type) {
	case []interface{}:
		for _, item := range val {
			if depth != 0 {
				result = append(result, flattenValue(item, depth-1)...)
			} else {
				result = append(result, item)
			}
		}
	default:
		result = append(result, v)
	}
	return result
}

func toSlice(v exec.Value) []interface{} {
	var result []interface{}
	v.Iterate(func(idx, count int, key, value exec.Value) bool {
		// For list iteration, gonja puts the item in key and nil in value
		item := key
		if value != nil && !value.IsNil() {
			item = value
		}
		result = append(result, item.Interface())
		return true
	}, func() {})
	return result
}

func toStringSet(v exec.Value) map[string]bool {
	set := make(map[string]bool)
	v.Iterate(func(idx, count int, key, value exec.Value) bool {
		item := key
		if value != nil && !value.IsNil() {
			item = value
		}
		set[fmt.Sprint(item.Interface())] = true
		return true
	}, func() {})
	return set
}

// pythonToGoTimeFormat converts Python strftime format codes to Go time format.
func pythonToGoTimeFormat(format string) string {
	replacer := strings.NewReplacer(
		"%Y", "2006",
		"%m", "01",
		"%d", "02",
		"%H", "15",
		"%M", "04",
		"%S", "05",
		"%f", "000000",
		"%z", "-0700",
		"%Z", "MST",
		"%a", "Mon",
		"%A", "Monday",
		"%b", "Jan",
		"%B", "January",
		"%p", "PM",
		"%I", "03",
		"%j", "002",
		"%%", "%",
	)
	return replacer.Replace(format)
}
