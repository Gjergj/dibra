package template

import (
	"fmt"
	"strings"

	"github.com/aisbergg/gonja/pkg/gonja"
	"github.com/aisbergg/gonja/pkg/gonja/exec"
)

// EvaluateWhen evaluates one or more when expressions against the provided context.
// All conditions must resolve to true for the task to run.
func EvaluateWhen(conditions []interface{}, context map[string]interface{}) (bool, error) {
	if len(conditions) == 0 {
		return true, nil
	}

	env := gonja.NewEnvironment(gonja.OptUndefined(exec.NewChainedStrictUndefinedValue))
	registerAnsibleFilters(env)

	for _, cond := range conditions {
		matched, err := evalWhenCondition(env, cond, context)
		if err != nil {
			return false, err
		}
		if !matched {
			return false, nil
		}
	}

	return true, nil
}

func evalWhenCondition(env *gonja.Environment, cond interface{}, context map[string]interface{}) (bool, error) {
	switch typed := cond.(type) {
	case bool:
		return typed, nil
	case string:
		return evalWhenExpression(env, typed, context)
	case int:
		return typed != 0, nil
	case int32:
		return typed != 0, nil
	case int64:
		return typed != 0, nil
	case uint:
		return typed != 0, nil
	case uint32:
		return typed != 0, nil
	case uint64:
		return typed != 0, nil
	case float32:
		return typed != 0, nil
	case float64:
		return typed != 0, nil
	default:
		return false, fmt.Errorf("unsupported when condition type %T", cond)
	}
}

func evalWhenExpression(env *gonja.Environment, expr string, context map[string]interface{}) (bool, error) {
	tpl, err := env.FromString("{% if " + expr + " %}true{% else %}false{% endif %}")
	if err != nil {
		return false, err
	}
	output, err := tpl.Execute(context)
	if err != nil {
		return false, err
	}
	rendered := strings.TrimSpace(output)
	switch rendered {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("unexpected when evaluation output %q", rendered)
	}
}
