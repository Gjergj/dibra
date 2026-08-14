package docker_swarm_service

import (
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/docker/go-units"
	"github.com/google/shlex"
)

var (
	hoursPattern        = regexp.MustCompile(`^(\d+)h`)
	minutesPattern      = regexp.MustCompile(`^(\d+)m`)
	secondsPattern      = regexp.MustCompile(`^(\d+)s`)
	millisecondsPattern = regexp.MustCompile(`^(\d+)ms`)
	microsecondsPattern = regexp.MustCompile(`^(\d+)us`)
)

func durationToNanoseconds(value string) (int64, error) {
	remaining := value
	hours, remaining := consumeDuration(remaining, hoursPattern)
	minutes := 0
	if match := minutesPattern.FindStringSubmatch(remaining); match != nil && !strings.HasPrefix(remaining[len(match[1]):], "ms") {
		minutes = atoiDefault(match[1])
		remaining = remaining[len(match[0]):]
	}
	seconds, remaining := consumeDuration(remaining, secondsPattern)
	milliseconds, remaining := consumeDuration(remaining, millisecondsPattern)
	microseconds, remaining := consumeDuration(remaining, microsecondsPattern)
	if remaining != "" || value == "" {
		return 0, fmt.Errorf("Invalid time duration - %s", value)
	}
	delta := time.Duration(hours)*time.Hour +
		time.Duration(minutes)*time.Minute +
		time.Duration(seconds)*time.Second +
		time.Duration(milliseconds)*time.Millisecond +
		time.Duration(microseconds)*time.Microsecond
	return int64(delta), nil
}

func consumeDuration(value string, pattern *regexp.Regexp) (int, string) {
	match := pattern.FindStringSubmatch(value)
	if match == nil {
		return 0, value
	}
	return atoiDefault(match[1]), value[len(match[0]):]
}

func atoiDefault(value string) int {
	parsed, _ := strconv.Atoi(value)
	return parsed
}

func nanosecondsFromRaw(name string, value any) (*int64, error) {
	if value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case int:
		result := int64(typed)
		return &result, nil
	case int64:
		result := typed
		return &result, nil
	case float64:
		result := int64(typed)
		return &result, nil
	case string:
		if typed == "" {
			return nil, nil
		}
		if parsed, err := strconv.ParseInt(typed, 10, 64); err == nil {
			return &parsed, nil
		}
		nanos, err := durationToNanoseconds(typed)
		if err != nil {
			return nil, err
		}
		return &nanos, nil
	case DurationValue:
		result := typed.Nanoseconds
		return &result, nil
	case *DurationValue:
		if typed == nil {
			return nil, nil
		}
		result := typed.Nanoseconds
		return &result, nil
	default:
		return nil, fmt.Errorf("Invalid type for %s %v (%T). Only string or int allowed.", name, value, value)
	}
}

func parseHumanBytes(value any) (int64, error) {
	switch typed := value.(type) {
	case nil:
		return 0, nil
	case int:
		return int64(typed), nil
	case int64:
		return typed, nil
	case float64:
		return int64(typed), nil
	case SizeValue:
		return typed.Bytes, nil
	case *SizeValue:
		if typed == nil {
			return 0, nil
		}
		return typed.Bytes, nil
	case string:
		parsed, err := units.RAMInBytes(strings.TrimSpace(typed))
		if err != nil {
			return 0, err
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("invalid size %v", value)
	}
}

func hasDictChanged(newDict, oldDict map[string]any) bool {
	if newDict == nil {
		return false
	}
	if len(newDict) == 0 && len(oldDict) > 0 {
		return true
	}
	if len(oldDict) == 0 && len(newDict) > 0 {
		return true
	}
	if oldDict == nil {
		return false
	}
	for option, value := range newDict {
		if value == nil {
			continue
		}
		oldValue := oldDict[option]
		if isFalsy(value) && isFalsy(oldValue) {
			continue
		}
		if !valuesEqual(value, oldValue) {
			return true
		}
	}
	return false
}

func valuesEqual(left, right any) bool {
	left, right = derefAny(left), derefAny(right)
	if reflect.DeepEqual(left, right) {
		return true
	}
	if isFalsy(left) && isFalsy(right) {
		return true
	}
	if leftNum, ok := asFloat(left); ok {
		if rightNum, ok := asFloat(right); ok {
			return leftNum == rightNum
		}
	}
	if isStringLike(left) && isStringLike(right) {
		return fmt.Sprint(left) == fmt.Sprint(right)
	}
	return false
}

func derefAny(value any) any {
	if value == nil {
		return nil
	}
	reflected := reflect.ValueOf(value)
	for reflected.Kind() == reflect.Ptr {
		if reflected.IsNil() {
			return nil
		}
		reflected = reflected.Elem()
		value = reflected.Interface()
	}
	return value
}

func asFloat(value any) (float64, bool) {
	if value == nil {
		return 0, false
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(reflected.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(reflected.Uint()), true
	case reflect.Float32, reflect.Float64:
		return reflected.Float(), true
	default:
		return 0, false
	}
}

func isStringLike(value any) bool {
	return value != nil && reflect.ValueOf(value).Kind() == reflect.String
}

func hasListChanged(newList, oldList []any, sortLists bool, sortKey string) (bool, error) {
	if newList == nil {
		return false, nil
	}
	if oldList == nil {
		oldList = []any{}
	}
	if len(newList) != len(oldList) {
		return true, nil
	}
	left, right := newList, oldList
	if sortLists {
		var err error
		left, err = sortAnyList(newList, sortKey)
		if err != nil {
			return false, err
		}
		right, err = sortAnyList(oldList, sortKey)
		if err != nil {
			return false, err
		}
	}
	for index := range left {
		if listItemsDiffer(left[index], right[index]) {
			return true, nil
		}
	}
	return false, nil
}

func listItemsDiffer(newItem, oldItem any) bool {
	if reflect.TypeOf(newItem) != reflect.TypeOf(oldItem) {
		newText, newOK := newItem.(string)
		oldText, oldOK := oldItem.(string)
		if newOK && oldOK {
			return newText != oldText
		}
		return true
	}
	newMap, newIsMap := asStringMap(newItem)
	oldMap, oldIsMap := asStringMap(oldItem)
	if newIsMap && oldIsMap {
		return hasDictChanged(newMap, oldMap)
	}
	return !reflect.DeepEqual(newItem, oldItem)
}

func sortAnyList(values []any, sortKey string) ([]any, error) {
	if len(values) == 0 {
		return values, nil
	}
	copied := append([]any{}, values...)
	if _, ok := asStringMap(copied[0]); ok {
		if sortKey == "" {
			return nil, fmt.Errorf("A sort key was not specified when sorting list")
		}
		sort.SliceStable(copied, func(i, j int) bool {
			left, _ := asStringMap(copied[i])
			right, _ := asStringMap(copied[j])
			return fmt.Sprint(left[sortKey]) < fmt.Sprint(right[sortKey])
		})
		return copied, nil
	}
	sort.SliceStable(copied, func(i, j int) bool {
		return lessAny(copied[i], copied[j])
	})
	return copied, nil
}

func lessAny(left, right any) bool {
	switch leftValue := left.(type) {
	case int:
		if rightValue, ok := right.(int); ok {
			return leftValue < rightValue
		}
	case int64:
		if rightValue, ok := right.(int64); ok {
			return leftValue < rightValue
		}
	case float64:
		if rightValue, ok := right.(float64); ok {
			return leftValue < rightValue
		}
	case string:
		if rightValue, ok := right.(string); ok {
			return leftValue < rightValue
		}
	}
	return fmt.Sprint(left) < fmt.Sprint(right)
}

func haveNetworksChanged(newNetworks, oldNetworks []map[string]any) bool {
	if newNetworks == nil {
		return false
	}
	if oldNetworks == nil {
		oldNetworks = []map[string]any{}
	}
	if len(newNetworks) != len(oldNetworks) {
		return true
	}
	left := copyNetworkList(newNetworks)
	right := copyNetworkList(oldNetworks)
	sort.SliceStable(left, func(i, j int) bool {
		return fmt.Sprint(left[i]["id"]) < fmt.Sprint(left[j]["id"])
	})
	sort.SliceStable(right, func(i, j int) bool {
		return fmt.Sprint(right[i]["id"]) < fmt.Sprint(right[j]["id"])
	})
	for index := range left {
		item := copyStringMap(left[index])
		other := copyStringMap(right[index])
		normalizeNetworkAliases(item)
		normalizeNetworkAliases(other)
		if hasDictChanged(item, other) {
			return true
		}
	}
	return false
}

func copyNetworkList(values []map[string]any) []map[string]any {
	result := make([]map[string]any, len(values))
	for i, item := range values {
		result[i] = copyStringMap(item)
	}
	return result
}

func normalizeNetworkAliases(item map[string]any) {
	switch aliases := item["aliases"].(type) {
	case []any:
		item["aliases"] = sortStringLike(aliases)
	case []string:
		sorted := append([]string{}, aliases...)
		sort.Strings(sorted)
		item["aliases"] = sorted
	}
}

func getDockerNetworks(networks any, networkIDs map[string]string) ([]map[string]any, error) {
	if networks == nil || isNilContainer(networks) {
		return nil, nil
	}
	items, err := asAnySlice(networks)
	if err != nil {
		return nil, fmt.Errorf("Only a list of strings or dictionaries are allowed to be passed as networks.")
	}
	parsed := make([]map[string]any, 0, len(items))
	for _, item := range items {
		spec, err := networkSpecMap(item)
		if err != nil {
			return nil, err
		}
		name, _ := spec["name"].(string)
		if name == "" {
			return nil, fmt.Errorf(`"name" is required when networks are passed as dictionaries.`)
		}
		delete(spec, "name")
		if aliases, found := spec["aliases"]; found {
			aliasList, err := stringList(aliases)
			if err != nil {
				if _, isList := aliases.([]any); !isList && reflect.ValueOf(aliases).Kind() != reflect.Slice {
					return nil, fmt.Errorf(`"aliases" network option is only allowed as a list`)
				}
				return nil, fmt.Errorf("Only strings are allowed as network aliases.")
			}
			spec["aliases"] = aliasList
		}
		if options, found := spec["options"]; found {
			optionMap, err := optionMap(options)
			if err != nil {
				return nil, err
			}
			spec["options"] = optionMap
		}
		allowed := map[string]bool{"aliases": true, "options": true}
		for key := range spec {
			if !allowed[key] {
				return nil, fmt.Errorf("%s are not valid keys for the networks option", key)
			}
		}
		id, found := networkIDs[name]
		if !found {
			return nil, fmt.Errorf("Could not find a network named: %s.", name)
		}
		spec["id"] = id
		parsed = append(parsed, spec)
	}
	if len(parsed) == 0 {
		return []map[string]any{}, nil
	}
	return parsed, nil
}

func networkSpecMap(item any) (map[string]any, error) {
	switch typed := item.(type) {
	case string:
		return map[string]any{"name": typed}, nil
	case map[string]any:
		return copyStringMap(typed), nil
	case NetworkSpec:
		spec := map[string]any{"name": typed.Name}
		if typed.Aliases != nil {
			spec["aliases"] = typed.Aliases
		}
		if typed.Options != nil {
			spec["options"] = typed.Options
		}
		return spec, nil
	default:
		return nil, fmt.Errorf("Only a list of strings or dictionaries are allowed to be passed as networks.")
	}
}

func optionMap(value any) (map[string]string, error) {
	switch typed := value.(type) {
	case map[string]string:
		return typed, nil
	case map[string]any:
		return stringifyOptions(typed), nil
	default:
		return nil, fmt.Errorf("Only dict is allowed as network options.")
	}
}

func getDockerEnvironment(env any, envFiles []string, readFile func(string) ([]byte, error)) ([]string, error) {
	envDict := map[string]string{}
	for _, path := range envFiles {
		parsed, err := parseEnvFile(path, readFile)
		if err != nil {
			return nil, err
		}
		for key, value := range parsed {
			envDict[key] = value
		}
	}
	if env != nil {
		if err := mergeEnv(envDict, env); err != nil {
			return nil, err
		}
	}
	if len(envDict) == 0 {
		if env != nil || envFiles != nil {
			return []string{}, nil
		}
		return nil, nil
	}
	result := make([]string, 0, len(envDict))
	for key, value := range envDict {
		result = append(result, key+"="+value)
	}
	sort.Strings(result)
	return result, nil
}

func mergeEnv(envDict map[string]string, env any) error {
	switch typed := env.(type) {
	case string:
		if typed == "" {
			return nil
		}
		for _, item := range strings.Split(typed, ",") {
			key, value, ok := strings.Cut(item, "=")
			if !ok {
				return fmt.Errorf("Invalid environment variable found in list, needs to be in format KEY=VALUE.")
			}
			envDict[key] = value
		}
		return nil
	case map[string]any:
		for key, value := range typed {
			text, ok := value.(string)
			if !ok {
				return fmt.Errorf("Non-string value found for env option. Ambiguous env options must be wrapped in quotes to avoid them being interpreted when directly specified in YAML, or explicitly converted to strings when the option is templated. Key: %s", key)
			}
			envDict[key] = text
		}
		return nil
	case map[string]string:
		for key, value := range typed {
			envDict[key] = value
		}
		return nil
	default:
		items, err := asAnySlice(env)
		if err != nil {
			return fmt.Errorf("Invalid type for env %v (%T). Only list or dict allowed.", env, env)
		}
		for _, item := range items {
			text, ok := item.(string)
			if !ok {
				return fmt.Errorf("Invalid environment variable found in list, needs to be in format KEY=VALUE.")
			}
			key, value, ok := strings.Cut(text, "=")
			if !ok {
				return fmt.Errorf("Invalid environment variable found in list, needs to be in format KEY=VALUE.")
			}
			envDict[key] = value
		}
		return nil
	}
}

func parseEnvFile(path string, readFile func(string) ([]byte, error)) (map[string]string, error) {
	content, err := readFile(path)
	if err != nil {
		return nil, err
	}
	environment := map[string]string{}
	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("Invalid line in environment file %s:\n%s", path, line)
		}
		environment[key] = value
	}
	return environment, nil
}

func parseCommand(value any) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	if text, ok := value.(string); ok {
		parts, err := shlex.Split(text)
		if err != nil {
			return nil, err
		}
		return parts, nil
	}
	items, err := asAnySlice(value)
	if err != nil {
		return nil, fmt.Errorf("Invalid type for command %v (%T). Only string or list allowed. Check quoting.", value, value)
	}
	result := make([]string, 0, len(items))
	var invalid []string
	for index, item := range items {
		text, ok := item.(string)
		if !ok {
			invalid = append(invalid, fmt.Sprintf("%v (%T) at index %d", item, item, index))
			continue
		}
		result = append(result, text)
	}
	if len(invalid) > 0 {
		return nil, fmt.Errorf("All items in a command list need to be strings. Check quoting. Invalid items: %s.", strings.Join(invalid, ", "))
	}
	return result, nil
}

func parseHealthcheck(healthcheck *Healthcheck) (map[string]any, bool, error) {
	if healthcheck == nil || healthcheck.Test == nil {
		return nil, false, nil
	}
	test, err := normalizeHealthcheckTest(healthcheck.Test)
	if err != nil {
		return nil, false, err
	}
	if len(test) == 1 && test[0] == "NONE" {
		return nil, true, nil
	}
	result := map[string]any{"test": test}
	if healthcheck.Interval != nil {
		result["interval"] = healthcheck.Interval.Nanoseconds
	}
	if healthcheck.Timeout != nil {
		result["timeout"] = healthcheck.Timeout.Nanoseconds
	}
	if healthcheck.StartPeriod != nil {
		result["start_period"] = healthcheck.StartPeriod.Nanoseconds
	}
	if healthcheck.Retries != nil {
		result["retries"] = *healthcheck.Retries
	}
	return result, false, nil
}

func normalizeHealthcheckTest(value any) ([]string, error) {
	if items, err := asAnySlice(value); err == nil {
		result := make([]string, 0, len(items))
		for _, item := range items {
			result = append(result, fmt.Sprint(item))
		}
		return result, nil
	}
	return []string{"CMD-SHELL", fmt.Sprint(value)}, nil
}

func asStringMap(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	case map[string]string:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[key] = item
		}
		return result, true
	default:
		return nil, false
	}
}

func copyStringMap(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func asAnySlice(value any) ([]any, error) {
	if value == nil {
		return nil, fmt.Errorf("not a list")
	}
	switch typed := value.(type) {
	case []any:
		return typed, nil
	case []string:
		result := make([]any, len(typed))
		for i, item := range typed {
			result[i] = item
		}
		return result, nil
	case NetworkList:
		result := make([]any, len(typed))
		for i, item := range typed {
			result[i] = item
		}
		return result, nil
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() != reflect.Slice {
		return nil, fmt.Errorf("not a list")
	}
	result := make([]any, reflected.Len())
	for i := 0; i < reflected.Len(); i++ {
		result[i] = reflected.Index(i).Interface()
	}
	return result, nil
}

func stringList(value any) ([]string, error) {
	items, err := asAnySlice(value)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("not a string list")
		}
		result = append(result, text)
	}
	return result, nil
}

func sortStringLike(values []any) []any {
	copied := append([]any{}, values...)
	sort.SliceStable(copied, func(i, j int) bool {
		return fmt.Sprint(copied[i]) < fmt.Sprint(copied[j])
	})
	return copied
}

func stringifyOptions(values map[string]any) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		switch typed := value.(type) {
		case bool:
			result[key] = strconv.FormatBool(typed)
		default:
			result[key] = fmt.Sprint(value)
		}
	}
	return result
}

func isNilContainer(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Slice, reflect.Map, reflect.Ptr, reflect.Interface:
		return reflected.IsNil()
	}
	return false
}

func isFalsy(value any) bool {
	if value == nil {
		return true
	}
	switch typed := value.(type) {
	case bool:
		return !typed
	case string:
		return typed == ""
	case []any:
		return len(typed) == 0
	case []string:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	case map[string]string:
		return len(typed) == 0
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Slice, reflect.Map:
		return reflected.Len() == 0
	case reflect.Ptr, reflect.Interface:
		return reflected.IsNil()
	}
	return false
}
