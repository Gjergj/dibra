package docker_swarm_service

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/swarm"
)

func anyOrNil[T any](values []T) any {
	if values == nil {
		return nil
	}
	return values
}

func mapOrNil[K comparable, V any](values map[K]V) any {
	if values == nil {
		return nil
	}
	return values
}

func nilIfFalse(value bool) any {
	if !value {
		return nil
	}
	return true
}

func nilIfZero(value uint64) any {
	if value == 0 {
		return nil
	}
	return value
}

func stringPtr(value string) *string { return &value }

func boolPtr(value bool) *bool { return &value }

func nilIfEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func durationPtr(value time.Duration) *int64 {
	nanos := value.Nanoseconds()
	return &nanos
}

func uint64Ptr(value uint64) *uint64 { return &value }

func float32Ptr(value float32) *float32 { return &value }

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func derefBool(value *bool) bool {
	return value != nil && *value
}

func mapsEqual(left, right map[string]string) bool {
	if len(left) == 0 && len(right) == 0 {
		return true
	}
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func uintEqual(left, right *uint64) bool {
	if left == nil && right == nil {
		return true
	}
	if left == nil || right == nil {
		return false
	}
	return *left == *right
}

func intEqual(left, right *int64) bool {
	if left == nil && right == nil {
		return true
	}
	if left == nil || right == nil {
		return false
	}
	return *left == *right
}

func floatEqual(left, right *float64) bool {
	if left == nil && right == nil {
		return true
	}
	if left == nil || right == nil {
		return false
	}
	return *left == *right
}

func float32Equal(left, right *float32) bool {
	if left == nil && right == nil {
		return true
	}
	if left == nil || right == nil {
		return false
	}
	return *left == *right
}

func toAnySlice[T any](values []T) []any {
	if values == nil {
		return nil
	}
	result := make([]any, len(values))
	for i, value := range values {
		result[i] = value
	}
	return result
}

func jsonEqual(left, right any) bool {
	leftRaw, _ := json.Marshal(left)
	rightRaw, _ := json.Marshal(right)
	return string(leftRaw) == string(rightRaw)
}

func healthcheckConfig(values map[string]any) *container.HealthConfig {
	config := &container.HealthConfig{}
	if test, err := stringList(values["test"]); err == nil {
		config.Test = test
	}
	if interval, err := nanosecondsFromRaw("interval", values["interval"]); err == nil && interval != nil {
		config.Interval = time.Duration(*interval)
	}
	if timeout, err := nanosecondsFromRaw("timeout", values["timeout"]); err == nil && timeout != nil {
		config.Timeout = time.Duration(*timeout)
	}
	if startPeriod, err := nanosecondsFromRaw("start_period", values["start_period"]); err == nil && startPeriod != nil {
		config.StartPeriod = time.Duration(*startPeriod)
	}
	switch retries := values["retries"].(type) {
	case int:
		config.Retries = retries
	case int64:
		config.Retries = int(retries)
	case float64:
		config.Retries = int(retries)
	}
	return config
}

func formatHosts(hosts map[string]string) []string {
	names := make([]string, 0, len(hosts))
	for hostname := range hosts {
		names = append(names, hostname)
	}
	sort.Strings(names)
	result := make([]string, 0, len(names))
	for _, hostname := range names {
		result = append(result, hosts[hostname]+" "+hostname)
	}
	return result
}

func parseAddr(value string) (netip.Addr, error) {
	return netip.ParseAddr(value)
}

func buildMounts(values []map[string]any) []mount.Mount {
	result := make([]mount.Mount, 0, len(values))
	for _, item := range values {
		entry := mount.Mount{
			Type:     mount.Type(fmt.Sprint(item["type"])),
			Source:   fmt.Sprint(item["source"]),
			Target:   fmt.Sprint(item["target"]),
			ReadOnly: derefBool(boolFromAny(item["readonly"])),
		}
		if propagation, ok := item["propagation"].(*string); ok && propagation != nil {
			entry.BindOptions = &mount.BindOptions{Propagation: mount.Propagation(*propagation)}
		} else if text, ok := item["propagation"].(string); ok && text != "" {
			entry.BindOptions = &mount.BindOptions{Propagation: mount.Propagation(text)}
		}
		if item["type"] == "volume" || item["labels"] != nil || item["no_copy"] != nil || item["driver_config"] != nil {
			options := &mount.VolumeOptions{}
			if labels, ok := item["labels"].(map[string]string); ok {
				options.Labels = labels
			}
			if noCopy := boolFromAny(item["no_copy"]); noCopy != nil {
				options.NoCopy = *noCopy
			}
			if driver, ok := item["driver_config"].(map[string]any); ok {
				options.DriverConfig = &mount.Driver{
					Name: fmt.Sprint(driver["name"]),
				}
				if opts, ok := driver["options"].(map[string]string); ok {
					options.DriverConfig.Options = opts
				}
			}
			if options.Labels != nil || options.NoCopy || options.DriverConfig != nil {
				entry.VolumeOptions = options
			}
		}
		if item["type"] == "tmpfs" || item["tmpfs_size"] != nil || item["tmpfs_mode"] != nil {
			options := &mount.TmpfsOptions{}
			if size, ok := item["tmpfs_size"].(int64); ok {
				options.SizeBytes = size
			}
			if mode := fileModeFromAny(item["tmpfs_mode"]); mode != 0 {
				options.Mode = mode
			}
			entry.TmpfsOptions = options
		}
		result = append(result, entry)
	}
	return result
}

func boolFromAny(value any) *bool {
	switch typed := value.(type) {
	case nil:
		return nil
	case bool:
		return &typed
	case *bool:
		return typed
	default:
		return nil
	}
}

func fileModeFromAny(value any) os.FileMode {
	switch typed := value.(type) {
	case nil:
		return 0
	case os.FileMode:
		return typed
	case uint32:
		return os.FileMode(typed)
	case int:
		return os.FileMode(typed)
	case int64:
		return os.FileMode(typed)
	case float64:
		return os.FileMode(typed)
	case string:
		parsed, err := strconv.ParseUint(typed, 8, 32)
		if err != nil {
			parsed, _ = strconv.ParseUint(typed, 10, 32)
		}
		return os.FileMode(parsed)
	default:
		return 0
	}
}

func buildConfigs(values []map[string]any) []*swarm.ConfigReference {
	result := make([]*swarm.ConfigReference, 0, len(values))
	for _, item := range values {
		result = append(result, &swarm.ConfigReference{
			ConfigID:   fmt.Sprint(item["config_id"]),
			ConfigName: fmt.Sprint(item["config_name"]),
			File:       fileTarget(item),
		})
	}
	return result
}

func buildSecrets(values []map[string]any) []*swarm.SecretReference {
	result := make([]*swarm.SecretReference, 0, len(values))
	for _, item := range values {
		target := &swarm.SecretReferenceFileTarget{}
		if filename, ok := item["filename"]; ok && filename != nil {
			target.Name = fmt.Sprint(filename)
		}
		if uid, ok := item["uid"]; ok && uid != nil {
			target.UID = fmt.Sprint(uid)
		}
		if gid, ok := item["gid"]; ok && gid != nil {
			target.GID = fmt.Sprint(gid)
		}
		if mode, ok := item["mode"]; ok && mode != nil {
			target.Mode = fileModeFromAny(mode)
		}
		result = append(result, &swarm.SecretReference{
			SecretID:   fmt.Sprint(item["secret_id"]),
			SecretName: fmt.Sprint(item["secret_name"]),
			File:       target,
		})
	}
	return result
}

func fileTarget(item map[string]any) *swarm.ConfigReferenceFileTarget {
	target := &swarm.ConfigReferenceFileTarget{}
	if filename, ok := item["filename"]; ok && filename != nil {
		target.Name = fmt.Sprint(filename)
	}
	if uid, ok := item["uid"]; ok && uid != nil {
		target.UID = fmt.Sprint(uid)
	}
	if gid, ok := item["gid"]; ok && gid != nil {
		target.GID = fmt.Sprint(gid)
	}
	if mode, ok := item["mode"]; ok && mode != nil {
		target.Mode = fileModeFromAny(mode)
	}
	return target
}

func buildPreferences(values []map[string]any) []swarm.PlacementPreference {
	result := make([]swarm.PlacementPreference, 0, len(values))
	for _, item := range values {
		if spread, ok := item["spread"]; ok && spread != nil {
			result = append(result, swarm.PlacementPreference{
				Spread: &swarm.SpreadOver{SpreadDescriptor: fmt.Sprint(spread)},
			})
		}
	}
	return result
}

func buildNetworks(values []map[string]any) []swarm.NetworkAttachmentConfig {
	result := make([]swarm.NetworkAttachmentConfig, 0, len(values))
	for _, item := range values {
		entry := swarm.NetworkAttachmentConfig{Target: fmt.Sprint(item["id"])}
		switch aliases := item["aliases"].(type) {
		case []string:
			entry.Aliases = aliases
		case []any:
			for _, alias := range aliases {
				entry.Aliases = append(entry.Aliases, fmt.Sprint(alias))
			}
		}
		switch options := item["options"].(type) {
		case map[string]string:
			entry.DriverOpts = options
		case map[string]any:
			entry.DriverOpts = stringifyOptions(options)
		}
		result = append(result, entry)
	}
	return result
}

func updateConfigFromMap(values map[string]any) *swarm.UpdateConfig {
	if values == nil {
		return nil
	}
	config := &swarm.UpdateConfig{}
	hasValue := false
	if value, ok := values["parallelism"]; ok && value != nil {
		config.Parallelism = toUint64(value)
		hasValue = true
	}
	if value, ok := values["delay"]; ok && value != nil {
		if nanos, err := nanosecondsFromRaw("delay", value); err == nil && nanos != nil {
			config.Delay = time.Duration(*nanos)
			hasValue = true
		}
	}
	if value, ok := values["failure_action"]; ok && value != nil && fmt.Sprint(value) != "" {
		config.FailureAction = swarm.FailureAction(fmt.Sprint(value))
		hasValue = true
	}
	if value, ok := values["monitor"]; ok && value != nil {
		if nanos, err := nanosecondsFromRaw("monitor", value); err == nil && nanos != nil {
			config.Monitor = time.Duration(*nanos)
			hasValue = true
		}
	}
	if value, ok := values["max_failure_ratio"]; ok && value != nil {
		switch typed := value.(type) {
		case float32:
			config.MaxFailureRatio = typed
		case float64:
			config.MaxFailureRatio = float32(typed)
		}
		hasValue = true
	}
	if value, ok := values["order"]; ok && value != nil && fmt.Sprint(value) != "" {
		config.Order = swarm.UpdateOrder(fmt.Sprint(value))
		hasValue = true
	}
	if !hasValue {
		return nil
	}
	return config
}

func buildPorts(values []map[string]any) []swarm.PortConfig {
	result := make([]swarm.PortConfig, 0, len(values))
	for _, item := range values {
		entry := swarm.PortConfig{
			Protocol:      network.IPProtocol(defaultString(fmt.Sprint(item["protocol"]), "tcp")),
			TargetPort:    uint32(toUint64(item["target_port"])),
			PublishedPort: uint32(toUint64(item["published_port"])),
		}
		if mode := fmt.Sprint(item["mode"]); mode != "" && mode != "<nil>" {
			entry.PublishMode = swarm.PortConfigPublishMode(mode)
		}
		result = append(result, entry)
	}
	return result
}

func defaultString(value, fallback string) string {
	if value == "" || value == "<nil>" {
		return fallback
	}
	return value
}

func failedResponse(message string) Response {
	return Response{Failed: true, Msg: message}
}

func resolveBool(value *bool) bool {
	return value != nil && *value
}

func pointerValue[T any](value *T) any {
	if value == nil {
		return nil
	}
	return *value
}
