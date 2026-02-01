package docker_config

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/swarm"
	"github.com/gjergjiramku/goansible/internal/modules/docker"
)

func Execute(req Request) Response {
	cli, err := docker.GetClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create docker client: %v", err)}
	}
	defer cli.Close()

	ctx := context.Background()

	// 1. Check if Config exists
	configs, err := cli.ConfigList(ctx, types.ConfigListOptions{})
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to list configs: %v", err)}
	}

	var existingConfig *swarm.Config
	for _, c := range configs {
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
			err := cli.ConfigRemove(ctx, existingConfig.ID)
			if err != nil {
				return Response{Failed: true, Msg: fmt.Sprintf("failed to remove config: %v", err)}
			}
			return Response{Changed: true, Msg: "config removed"}
		}
		return Response{Changed: false, Msg: "config already absent"}
	}

	// State present
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

	if existingConfig == nil {
		if req.Data == "" {
			return Response{Failed: true, Msg: "data is required when creating a config"}
		}

		spec := swarm.ConfigSpec{
			Annotations: swarm.Annotations{
				Name:   req.Name,
				Labels: req.Labels,
			},
			Data: data,
		}

		resp, err := cli.ConfigCreate(ctx, spec)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to create config: %v", err)}
		}
		return Response{Changed: true, Msg: "config created", ConfigID: resp.ID}
	}

	// Existing Config: Immutable data, mutable labels.
	updateNeeded := false
	recreateNeeded := false

	// Compare labels
	if req.Labels != nil {
		if len(existingConfig.Spec.Labels) != len(req.Labels) {
			updateNeeded = true
		} else {
			for k, v := range req.Labels {
				if existingConfig.Spec.Labels[k] != v {
					updateNeeded = true
					break
				}
			}
		}
	}

	if req.Force {
		recreateNeeded = true
	}

	if recreateNeeded {
		err := cli.ConfigRemove(ctx, existingConfig.ID)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to remove config for recreation: %v", err)}
		}

		spec := swarm.ConfigSpec{
			Annotations: swarm.Annotations{
				Name:   req.Name,
				Labels: req.Labels,
			},
			Data: data,
		}
		resp, err := cli.ConfigCreate(ctx, spec)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to re-create config: %v", err)}
		}
		return Response{Changed: true, Msg: "config recreated", ConfigID: resp.ID}
	}

	if updateNeeded {
		spec := existingConfig.Spec
		if req.Labels != nil {
			spec.Labels = req.Labels
		}

		err = cli.ConfigUpdate(ctx, existingConfig.ID, existingConfig.Version, spec)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to update config: %v", err)}
		}
		return Response{Changed: true, Msg: "config updated"}
	}

	return Response{Changed: false, Msg: "config already present", ConfigID: existingConfig.ID}
}
