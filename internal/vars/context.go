package vars

func BuildContext(hostName string, groupNames []string, resolved ResolvedVars, hostvars map[string]map[string]interface{}, groups map[string][]string) map[string]interface{} {
	context := map[string]interface{}{}
	for key, value := range resolved.Values {
		context[key] = value
	}
	context["inventory_hostname"] = hostName
	groupNamesAny := make([]interface{}, len(groupNames))
	for i, g := range groupNames {
		groupNamesAny[i] = g
	}
	context["group_names"] = groupNamesAny
	context["vars"] = resolved.Namespaces

	hostvarsAny := map[string]interface{}{}
	for name, hv := range hostvars {
		hostvarsAny[name] = hv
	}
	context["hostvars"] = hostvarsAny

	contextGroups := map[string]interface{}{}
	for name, members := range groups {
		membersAny := make([]interface{}, len(members))
		for i, m := range members {
			membersAny[i] = m
		}
		contextGroups[name] = membersAny
	}
	context["groups"] = contextGroups

	return context
}

func NormalizeContext(ctx map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(ctx))
	for k, v := range ctx {
		result[k] = normalizeValue(v)
	}
	return result
}

func normalizeValue(v interface{}) interface{} {
	switch typed := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(typed))
		for k, val := range typed {
			out[k] = normalizeValue(val)
		}
		return out
	case map[string]map[string]interface{}:
		out := make(map[string]interface{}, len(typed))
		for k, val := range typed {
			out[k] = normalizeValue(val)
		}
		return out
	case map[string][]string:
		out := make(map[string]interface{}, len(typed))
		for k, val := range typed {
			out[k] = normalizeValue(val)
		}
		return out
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(typed))
		for k, val := range typed {
			key, _ := k.(string)
			out[key] = normalizeValue(val)
		}
		return out
	case []string:
		out := make([]interface{}, len(typed))
		for i, val := range typed {
			out[i] = val
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(typed))
		for i, val := range typed {
			out[i] = normalizeValue(val)
		}
		return out
	default:
		return v
	}
}
