package docker_config

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
)

func Execute(req Request) Response {
	cli, err := docker.GetClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("create client", "", err).Error()}
	}
	defer cli.Close()

	ctx, cancel := docker.GetContext(req.CommonArgs)
	defer cancel()

	// List configs to find existing one
	configs, err := cli.ConfigList(ctx, client.ConfigListOptions{})
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("list configs", "", err).Error()}
	}

	var existingConfig *swarm.Config
	for _, c := range configs.Items {
		if c.Spec.Name == req.Name {
			configCopy := c
			existingConfig = &configCopy
			break
		}
	}

	state := req.State
	if state == "" {
		state = "present"
	}

	if state == "absent" {
		if existingConfig != nil {
			_, err := cli.ConfigRemove(ctx, existingConfig.ID, client.ConfigRemoveOptions{})
			if err != nil {
				return Response{Failed: true, Msg: docker.WrapError("remove config", req.Name, err).Error()}
			}
			return Response{Changed: true, Msg: "config removed"}
		}
		return Response{Changed: false, Msg: "config already absent"}
	}

	// State present - decode data
	var data []byte
	if req.Data != "" {
		if req.DataIsB64 {
			decoded, err := base64.StdEncoding.DecodeString(req.Data)
			if err != nil {
				return Response{Failed: true, Msg: fmt.Sprintf("failed to decode base64 data: %v", err)}
			}
			data = decoded
		} else {
			data = []byte(req.Data)
		}
	}

	// Calculate hash for idempotency
	dataHash := computeHash(data)

	// Merge labels with hash label
	labels := make(map[string]string)
	for k, v := range req.Labels {
		labels[k] = v
	}
	labels[DataHashLabel] = dataHash

	if existingConfig == nil {
		if req.Data == "" {
			return Response{Failed: true, Msg: "data is required when creating a config"}
		}

		spec := swarm.ConfigSpec{
			Annotations: swarm.Annotations{
				Name:   req.Name,
				Labels: labels,
			},
			Data: data,
		}

		resp, err := cli.ConfigCreate(ctx, client.ConfigCreateOptions{Spec: spec})
		if err != nil {
			return Response{Failed: true, Msg: docker.WrapError("create config", req.Name, err).Error()}
		}
		return Response{Changed: true, Msg: "config created", ConfigID: resp.ID, DataHash: dataHash}
	}

	// Config exists - check if update needed
	// Docker configs are immutable, but we can check hash to see if data would change

	existingHash := existingConfig.Spec.Labels[DataHashLabel]
	labelsMatch := compareLabelsIgnoringHash(existingConfig.Spec.Labels, labels)

	// If force is set, always recreate
	if req.Force {
		return recreateConfig(ctx, cli, existingConfig, req.Name, labels, data, dataHash)
	}

	// If data hash matches and labels match, no change needed
	if existingHash == dataHash && labelsMatch {
		return Response{Changed: false, Msg: "config already present (data and labels unchanged)", ConfigID: existingConfig.ID, DataHash: dataHash}
	}

	// If only labels differ (not data), we can update in place
	if existingHash == dataHash && !labelsMatch {
		spec := existingConfig.Spec
		spec.Labels = labels

		_, err = cli.ConfigUpdate(ctx, existingConfig.ID, client.ConfigUpdateOptions{Version: existingConfig.Version, Spec: spec})
		if err != nil {
			return Response{Failed: true, Msg: docker.WrapError("update config labels", req.Name, err).Error()}
		}
		return Response{Changed: true, Msg: "config labels updated", ConfigID: existingConfig.ID, DataHash: dataHash}
	}

	// Data differs - must recreate
	return recreateConfig(ctx, cli, existingConfig, req.Name, labels, data, dataHash)
}

// recreateConfig removes and recreates a config (required for data changes since configs are immutable)
func recreateConfig(ctx context.Context, cli *client.Client, existingConfig *swarm.Config, name string, labels map[string]string, data []byte, dataHash string) Response {
	_, err := cli.ConfigRemove(ctx, existingConfig.ID, client.ConfigRemoveOptions{})
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("remove config for recreation", name, err).Error()}
	}

	spec := swarm.ConfigSpec{
		Annotations: swarm.Annotations{
			Name:   name,
			Labels: labels,
		},
		Data: data,
	}
	resp, err := cli.ConfigCreate(ctx, client.ConfigCreateOptions{Spec: spec})
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("recreate config", name, err).Error()}
	}
	return Response{Changed: true, Msg: "config recreated (data changed)", ConfigID: resp.ID, DataHash: dataHash}
}

// computeHash returns SHA256 hash of data
func computeHash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// compareLabelsIgnoringHash compares labels ignoring the data hash label
func compareLabelsIgnoringHash(existing, desired map[string]string) bool {
	// Copy maps without hash label
	e := make(map[string]string)
	d := make(map[string]string)
	for k, v := range existing {
		if k != DataHashLabel {
			e[k] = v
		}
	}
	for k, v := range desired {
		if k != DataHashLabel {
			d[k] = v
		}
	}
	return docker.CompareMaps(e, d)
}
