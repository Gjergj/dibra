package docker_secret

import (
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

	ctx, cancel := docker.GetContext(req.CommonArgs)
	defer cancel()

	// 1. Check if Secret exists
	secrets, err := cli.SecretList(ctx, types.SecretListOptions{})
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to list secrets: %v", err)}
	}

	var existingSecret *swarm.Secret
	for _, s := range secrets {
		if s.Spec.Name == req.Name {
			secretCopy := s // copy
			existingSecret = &secretCopy
			break
		}
	}

	state := req.State
	if state == "" {
		state = "present"
	}

	if state == "absent" {
		if existingSecret != nil {
			err := cli.SecretRemove(ctx, existingSecret.ID)
			if err != nil {
				return Response{Failed: true, Msg: fmt.Sprintf("failed to remove secret: %v", err)}
			}
			return Response{Changed: true, Msg: "secret removed"}
		}
		return Response{Changed: false, Msg: "secret already absent"}
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

	if existingSecret == nil {
		if req.Data == "" {
			return Response{Failed: true, Msg: "data is required when creating a secret"}
		}

		spec := swarm.SecretSpec{
			Annotations: swarm.Annotations{
				Name:   req.Name,
				Labels: req.Labels,
			},
			Data: data,
		}

		resp, err := cli.SecretCreate(ctx, spec)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to create secret: %v", err)}
		}
		return Response{Changed: true, Msg: "secret created", SecretID: resp.ID}
	}

	// Existing Secret: Secrets are immutable in Swarm (except labels)
	// If data is different, we must remove and recreate (if force=true) or error out.
	// We can't easily check secret data (it's not returned in inspect).
	// So usually we only update labels.
	// If user wants to rotate, they usually use a new name.
	// Ansible module allows `rolling_versions` or force?
	// If force is true, we remove and recreate.

	updateNeeded := false
	recreateNeeded := false

	// Compare labels
	if req.Labels != nil {
		// Just checking equality
		if len(existingSecret.Spec.Labels) != len(req.Labels) {
			updateNeeded = true
		} else {
			for k, v := range req.Labels {
				if existingSecret.Spec.Labels[k] != v {
					updateNeeded = true
					break
				}
			}
		}
	}

	if req.Force {
		// If force is true, we assume data *might* have changed and we recreate.
		// NOTE: This is destructive to running services if not handled carefully (services using it will fail to update if not rotated).
		// But basic module behavior is just recreate.
		recreateNeeded = true
	}

	if recreateNeeded {
		err := cli.SecretRemove(ctx, existingSecret.ID)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to remove secret for recreation: %v", err)}
		}

		spec := swarm.SecretSpec{
			Annotations: swarm.Annotations{
				Name:   req.Name,
				Labels: req.Labels,
			},
			Data: data,
		}
		resp, err := cli.SecretCreate(ctx, spec)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to re-create secret: %v", err)}
		}
		return Response{Changed: true, Msg: "secret recreated", SecretID: resp.ID}
	}

	if updateNeeded {
		// Update only labels (Data is immutable)
		// SecretUpdate takes version and spec. Spec must include data? No, Data is cleared.
		// Wait, SecretUpdate docs say "Inspect the secret... The SecretSpec... Data field is ignored."
		// So we can update labels.

		spec := existingSecret.Spec
		if req.Labels != nil {
			spec.Labels = req.Labels
		}

		err = cli.SecretUpdate(ctx, existingSecret.ID, existingSecret.Version, spec)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to update secret: %v", err)}
		}
		return Response{Changed: true, Msg: "secret updated"}
	}

	return Response{Changed: false, Msg: "secret already present", SecretID: existingSecret.ID}
}
