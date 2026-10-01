package loom

import (
	"context"
	"errors"
)

// Application-owned catalog persistence and consent stay outside the CLI protocol.
type antigravityConnection struct {
	Models []string `json:"models"`
}

func agyConnection() (antigravityConnection, bool) {
	var c antigravityConnection
	ok := getStoreJSON(bkHarnessConnections, "antigravity", &c)
	return c, ok
}

func (antigravityAdapter) Connect(ctx context.Context, consent bool) (any, error) {
	if !consent {
		return nil, errors.New("confirmez l’utilisation du compte et des permissions natifs Antigravity")
	}
	models, err := discoverAgyModels(ctx)
	if err != nil {
		return nil, err
	}
	if err = runtimeVaultError(); err != nil {
		return nil, err
	}
	if putStoreJSON(bkHarnessConnections, "antigravity", antigravityConnection{Models: models}) != nil {
		return nil, runtimeActionError{status: 500, message: "enregistrement impossible"}
	}
	return models, nil
}

func (antigravityAdapter) Quota(ctx context.Context) (QuotaSnapshot, error) { return readAgyQuota(ctx) }
