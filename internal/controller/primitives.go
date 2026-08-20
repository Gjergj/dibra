package controller

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gjergjiramku/dibra/internal/config"
	"github.com/gjergjiramku/dibra/internal/template"
	"github.com/gjergjiramku/dibra/internal/vars"
)

func isControllerPrimitive(task config.Task) bool {
	return task.Debug != nil ||
		task.Fail != nil ||
		task.Assert != nil ||
		task.SetFact != nil ||
		task.IncludeVars != nil ||
		task.Pause != nil ||
		(task.Meta != nil && task.Meta.Action == "noop")
}

func executeControllerPrimitive(
	task config.Task,
	context map[string]interface{},
	runtimeVars map[string]interface{},
	baseDir string,
	verbose bool,
	sleep func(time.Duration),
) (map[string]interface{}, bool) {
	switch {
	case task.Debug != nil:
		result := executeDebug(task.Debug, context, verbose)
		printControllerResult(result, verbose, debugDisplayValue(task.Debug, result))
		return result, true
	case task.Fail != nil:
		result := executeFail(task.Fail, context)
		printControllerResult(result, verbose, "")
		return result, true
	case task.Assert != nil:
		result := executeAssert(task.Assert, context)
		displayResult := result
		if task.Assert.Quiet && !boolResultField(result, "failed") {
			displayResult = clonePrimitiveResult(result)
			displayResult["msg"] = ""
		}
		printControllerResult(displayResult, verbose, "")
		return result, true
	case task.SetFact != nil:
		result := executeSetFact(task.SetFact, context, runtimeVars)
		printControllerResult(result, verbose, "")
		return result, true
	case task.IncludeVars != nil:
		result := executeIncludeVars(task.IncludeVars, context, runtimeVars, taskSourceDir(task, baseDir))
		printControllerResult(result, verbose, "")
		return result, true
	case task.Pause != nil:
		result := executePause(task.Pause, context, sleep)
		printControllerResult(result, verbose, "")
		return result, true
	case task.Meta != nil && task.Meta.Action == "noop":
		result := primitiveSuccess("noop")
		printControllerResult(result, verbose, "")
		return result, true
	default:
		return nil, false
	}
}

func executeDebug(params *config.DebugParams, context map[string]interface{}, verbose bool) map[string]interface{} {
	if params.MsgSet && strings.TrimSpace(params.Var) != "" {
		return primitiveFailure("debug: parameters are mutually exclusive: msg|var")
	}
	availableVerbosity := 0
	if verbose {
		availableVerbosity = 1
	}
	if params.Verbosity > availableVerbosity {
		return map[string]interface{}{
			"changed":        false,
			"failed":         false,
			"skipped":        true,
			"skipped_reason": "Verbosity threshold not met.",
			"msg":            "Verbosity threshold not met.",
		}
	}

	result := primitiveSuccess("")
	if expression := strings.TrimSpace(params.Var); expression != "" {
		value, err := template.EvaluateExpression(expression, context)
		if err != nil {
			return primitiveFailure(fmt.Sprintf("debug: error while resolving var expression %q: %v", params.Var, err))
		}
		result[params.Var] = value
		return result
	}

	message := interface{}("Hello world!")
	if params.MsgSet {
		rendered, err := template.RenderValue(params.Msg, context)
		if err != nil {
			return primitiveFailure(fmt.Sprintf("debug: failed to render msg: %v", err))
		}
		message = rendered
	}
	result["msg"] = message
	return result
}

func executeFail(params *config.FailParams, context map[string]interface{}) map[string]interface{} {
	message := interface{}("Failed as requested from task")
	if params.MsgSet {
		rendered, err := template.RenderValue(params.Msg, context)
		if err != nil {
			return primitiveFailure(fmt.Sprintf("fail: failed to render msg: %v", err))
		}
		message = rendered
	}
	return map[string]interface{}{
		"changed": false,
		"failed":  true,
		"msg":     message,
	}
}

func executeAssert(params *config.AssertParams, context map[string]interface{}) map[string]interface{} {
	if len(params.That) == 0 {
		return primitiveFailure("assert: missing required arguments: that")
	}

	for _, assertion := range params.That {
		evaluatedAssertion := assertion
		if assertionString, ok := assertion.(string); ok {
			rendered, err := template.RenderValue(assertionString, context)
			if err != nil {
				return primitiveFailure(fmt.Sprintf("assert: failed to render assertion %q: %v", assertionString, err))
			}
			evaluatedAssertion = rendered
		}
		passed, err := template.EvaluateWhen([]interface{}{evaluatedAssertion}, context)
		if err != nil {
			return primitiveFailure(fmt.Sprintf("assert: failed to evaluate assertion %q: %v", fmt.Sprint(assertion), err))
		}
		if !passed {
			message, err := renderPrimitiveMessage(assertFailureMessage(params), "Assertion failed", context)
			if err != nil {
				return primitiveFailure(fmt.Sprintf("assert: failed to render fail_msg: %v", err))
			}
			return map[string]interface{}{
				"changed":      false,
				"failed":       true,
				"evaluated_to": false,
				"assertion":    assertion,
				"msg":          message,
			}
		}
	}

	message, err := renderPrimitiveMessage(params.SuccessMsg, "All assertions passed", context)
	if err != nil {
		return primitiveFailure(fmt.Sprintf("assert: failed to render success_msg: %v", err))
	}
	return primitiveSuccess(message)
}

func executeSetFact(params *config.SetFactParams, context, runtimeVars map[string]interface{}) map[string]interface{} {
	if len(params.Facts) == 0 {
		return primitiveFailure("set_fact: no key/value pairs provided, at least one is required for this action to succeed")
	}

	rawKeys := make([]string, 0, len(params.Facts))
	for key := range params.Facts {
		rawKeys = append(rawKeys, key)
	}
	sort.Strings(rawKeys)

	facts := make(map[string]interface{}, len(params.Facts))
	for _, rawKey := range rawKeys {
		key, err := template.RenderString(rawKey, context)
		if err != nil {
			return primitiveFailure(fmt.Sprintf("set_fact: failed to render variable name %q: %v", rawKey, err))
		}
		if err := validateVarName(key); err != nil {
			return primitiveFailure(fmt.Sprintf("set_fact: %v", err))
		}
		value, err := template.RenderValue(params.Facts[rawKey], context)
		if err != nil {
			return primitiveFailure(fmt.Sprintf("set_fact: failed to render %q: %v", key, err))
		}
		facts[key] = value
	}

	for key, value := range facts {
		runtimeVars[key] = value
	}
	if params.Cacheable {
		cached, _ := runtimeVars["ansible_facts"].(map[string]interface{})
		runtimeVars["ansible_facts"] = vars.MergeMaps(cached, facts, vars.MergeReplace)
	}

	return map[string]interface{}{
		"changed":                  false,
		"failed":                   false,
		"msg":                      "",
		"ansible_facts":            facts,
		"_ansible_facts_cacheable": params.Cacheable,
	}
}

func executePause(params *config.PauseParams, context map[string]interface{}, sleep func(time.Duration)) map[string]interface{} {
	if sleep == nil {
		sleep = time.Sleep
	}
	if params.Minutes != nil && params.Seconds != nil {
		return primitiveFailure("pause: minutes and seconds are mutually exclusive")
	}

	echo := true
	if params.Echo != nil {
		renderedEcho, err := template.RenderValue(params.Echo, context)
		if err != nil {
			return primitiveFailure(fmt.Sprintf("pause: failed to render echo: %v", err))
		}
		parsedEcho, convertErr := primitiveBool(renderedEcho)
		if convertErr != nil {
			return primitiveFailure(fmt.Sprintf("pause: echo: %v", convertErr))
		}
		echo = parsedEcho
	}

	prompt, err := template.RenderString(params.Prompt, context)
	if err != nil {
		return primitiveFailure(fmt.Sprintf("pause: failed to render prompt: %v", err))
	}

	durationUnit := "minutes"
	var seconds *int
	switch {
	case params.Minutes != nil:
		rendered, renderErr := template.RenderValue(params.Minutes, context)
		if renderErr != nil {
			return primitiveFailure(fmt.Sprintf("pause: failed to render minutes: %v", renderErr))
		}
		minutes, convertErr := primitiveInt(rendered)
		if convertErr != nil {
			return primitiveFailure(fmt.Sprintf("pause: minutes: %v", convertErr))
		}
		value := minutes * 60
		seconds = &value
	case params.Seconds != nil:
		rendered, renderErr := template.RenderValue(params.Seconds, context)
		if renderErr != nil {
			return primitiveFailure(fmt.Sprintf("pause: failed to render seconds: %v", renderErr))
		}
		value, convertErr := primitiveInt(rendered)
		if convertErr != nil {
			return primitiveFailure(fmt.Sprintf("pause: seconds: %v", convertErr))
		}
		seconds = &value
		durationUnit = "seconds"
	}

	start := time.Now()
	result := map[string]interface{}{
		"changed":    false,
		"failed":     false,
		"rc":         0,
		"stderr":     "",
		"start":      formatPauseTime(start),
		"echo":       echo,
		"user_input": "",
	}
	if prompt != "" {
		result["prompt"] = prompt
	}

	if seconds == nil {
		stop := time.Now()
		result["stop"] = formatPauseTime(stop)
		result["delta"] = int(stop.Sub(start).Seconds())
		result["stdout"] = "Paused for 0.0 minutes"
		result["msg"] = "stdin is not interactive; continuing without waiting"
		return result
	}

	if *seconds < 1 {
		*seconds = 1
	}
	sleep(time.Duration(*seconds) * time.Second)
	stop := time.Now()
	elapsed := stop.Sub(start)
	displayDuration := elapsed.Seconds()
	if durationUnit == "minutes" {
		displayDuration /= 60
	}
	result["stop"] = formatPauseTime(stop)
	result["delta"] = int(elapsed.Seconds())
	result["stdout"] = fmt.Sprintf("Paused for %.2f %s", displayDuration, durationUnit)
	result["msg"] = ""
	return result
}

func renderPrimitiveMessage(value interface{}, fallback string, context map[string]interface{}) (interface{}, error) {
	if value == nil {
		return fallback, nil
	}
	return template.RenderValue(value, context)
}

func assertFailureMessage(params *config.AssertParams) interface{} {
	if params.FailMsg != nil {
		return params.FailMsg
	}
	return params.Msg
}

func primitiveSuccess(message interface{}) map[string]interface{} {
	return map[string]interface{}{
		"changed": false,
		"failed":  false,
		"msg":     message,
	}
}

func primitiveFailure(message interface{}) map[string]interface{} {
	return map[string]interface{}{
		"changed": false,
		"failed":  true,
		"msg":     message,
	}
}

func primitiveInt(value interface{}) (int, error) {
	switch typed := value.(type) {
	case int:
		return typed, nil
	case int32:
		return int(typed), nil
	case int64:
		return int(typed), nil
	case uint:
		return int(typed), nil
	case uint32:
		return int(typed), nil
	case uint64:
		return int(typed), nil
	case float32:
		return int(typed), nil
	case float64:
		return int(typed), nil
	case string:
		number, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0, fmt.Errorf("must be a number, got %q", typed)
		}
		return int(number), nil
	default:
		return 0, fmt.Errorf("must be a number, got %T", value)
	}
}

func primitiveBool(value interface{}) (bool, error) {
	switch typed := value.(type) {
	case bool:
		return typed, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "yes", "on", "1":
			return true, nil
		case "false", "no", "off", "0":
			return false, nil
		}
	}
	return false, fmt.Errorf("must be a boolean, got %v", value)
}

func taskSourceDir(task config.Task, baseDir string) string {
	if task.SourceDir != "" {
		return task.SourceDir
	}
	return baseDir
}

func debugDisplayValue(params *config.DebugParams, result map[string]interface{}) string {
	if params == nil || params.Var == "" {
		return ""
	}
	value, ok := result[params.Var]
	if !ok {
		return ""
	}
	data, err := json.MarshalIndent(value, "      ", "  ")
	if err != nil {
		return fmt.Sprintf("%s: %v", params.Var, value)
	}
	return fmt.Sprintf("%s: %s", params.Var, data)
}

func printControllerResult(result map[string]interface{}, verbose bool, detail string) {
	message := ""
	if rawMessage, ok := result["msg"]; ok && rawMessage != nil {
		if text, ok := rawMessage.(string); ok {
			message = text
		} else if data, err := json.Marshal(rawMessage); err == nil {
			message = string(data)
		} else {
			message = fmt.Sprint(rawMessage)
		}
	}
	response := GenericResponse{
		Changed: boolResultField(result, "changed"),
		Failed:  boolResultField(result, "failed"),
		Skipped: boolResultField(result, "skipped"),
		Msg:     message,
	}
	if stdout, ok := result["stdout"].(string); ok {
		response.Stdout = stdout
	}
	if stderr, ok := result["stderr"].(string); ok {
		response.Stderr = stderr
	}
	printResponse(response, verbose)
	if detail != "" {
		printf("    %s\n", detail)
	}
}

func boolResultField(result map[string]interface{}, key string) bool {
	value, _ := result[key].(bool)
	return value
}

func clonePrimitiveResult(result map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(result))
	for key, value := range result {
		cloned[key] = value
	}
	return cloned
}

func formatPauseTime(value time.Time) string {
	return value.Format("2006-01-02 15:04:05.000000")
}
