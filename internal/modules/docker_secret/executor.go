package docker_secret

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/gjergjiramku/goansible/internal/modules/docker"
)

func Execute(req Request) Response {
	cli, err := docker.GetClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("create client", "", err).Error()}
	}
	defer cli.Close()

	ctx, cancel := docker.GetContext(req.CommonArgs)
	defer cancel()

	// List secrets to find existing one
	secrets, err := cli.SecretList(ctx, types.SecretListOptions{})
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("list secrets", "", err).Error()}
	}

	var existingSecret *swarm.Secret
	for _, s := range secrets {
		if s.Spec.Name == req.Name {
			secretCopy := s
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
				return Response{Failed: true, Msg: docker.WrapError("remove secret", req.Name, err).Error()}
			}
			return Response{Changed: true, Msg: "secret removed"}
		}
		return Response{Changed: false, Msg: "secret already absent"}
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

	if existingSecret == nil {
		if req.Data == "" {
			return Response{Failed: true, Msg: "data is required when creating a secret"}
		}

		spec := swarm.SecretSpec{
			Annotations: swarm.Annotations{
				Name:   req.Name,
				Labels: labels,
			},
			Data: data,
		}

		resp, err := cli.SecretCreate(ctx, spec)
		if err != nil {
			return Response{Failed: true, Msg: docker.WrapError("create secret", req.Name, err).Error()}
		}
		return Response{Changed: true, Msg: "secret created", SecretID: resp.ID, DataHash: dataHash}
	}

	// Secret exists - check if update needed
	// Docker secrets are immutable, but we can check hash to see if data would change

	existingHash := existingSecret.Spec.Labels[DataHashLabel]
	labelsMatch := compareLabelsIgnoringHash(existingSecret.Spec.Labels, labels)

	// If force is set, always recreate
	if req.Force {
		return recreateSecret(ctx, cli, existingSecret, req.Name, labels, data, dataHash)
	}

	// If data hash matches and labels match, no change needed
	if existingHash == dataHash && labelsMatch {
		return Response{Changed: false, Msg: "secret already present (data and labels unchanged)", SecretID: existingSecret.ID, DataHash: dataHash}
	}

	// If only labels differ (not data), we can update in place
	if existingHash == dataHash && !labelsMatch {
		spec := existingSecret.Spec
		spec.Labels = labels

		err = cli.SecretUpdate(ctx, existingSecret.ID, existingSecret.Version, spec)
		if err != nil {
			return Response{Failed: true, Msg: docker.WrapError("update secret labels", req.Name, err).Error()}
		}
		return Response{Changed: true, Msg: "secret labels updated", SecretID: existingSecret.ID, DataHash: dataHash}
	}

	// Data differs - must recreate
	return recreateSecret(ctx, cli, existingSecret, req.Name, labels, data, dataHash)
}

// recreateSecret removes and recreates a secret (required for data changes since secrets are immutable)
func recreateSecret(ctx context.Context, cli *client.Client, existingSecret *swarm.Secret, name string, labels map[string]string, data []byte, dataHash string) Response {
	err := cli.SecretRemove(ctx, existingSecret.ID)
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("remove secret for recreation", name, err).Error()}
	}

	spec := swarm.SecretSpec{
		Annotations: swarm.Annotations{
			Name:   name,
			Labels: labels,
		},
		Data: data,
	}
	resp, err := cli.SecretCreate(ctx, spec)
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("recreate secret", name, err).Error()}
	}
	return Response{Changed: true, Msg: "secret recreated (data changed)", SecretID: resp.ID, DataHash: dataHash}
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
