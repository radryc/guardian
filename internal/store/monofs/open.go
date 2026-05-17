package monofs

import (
	"context"
	"fmt"
	"time"

	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

type OpenConfig struct {
	RouterAddr           string
	Token                string
	PrincipalID          string
	Role                 string
	BaseURL              string
	ClientIDPrefix       string
	Version              string
	MountPoint           string
	UseExternalAddresses bool
	Writable             bool
}

func Open(ctx context.Context, cfg OpenConfig) (guardianapi.Store, *GRPCClient, error) {
	if cfg.ClientIDPrefix == "" {
		cfg.ClientIDPrefix = "guardian"
	}
	client, err := NewGRPCClient(ctx, ClientConfig{
		RouterAddr:           cfg.RouterAddr,
		ClientID:             fmt.Sprintf("%s-%d", cfg.ClientIDPrefix, time.Now().UnixNano()),
		Token:                cfg.Token,
		PrincipalID:          cfg.PrincipalID,
		Role:                 cfg.Role,
		BaseURL:              cfg.BaseURL,
		Version:              cfg.Version,
		MountPoint:           cfg.MountPoint,
		UseExternalAddresses: cfg.UseExternalAddresses,
		Writable:             cfg.Writable,
	})
	if err != nil {
		return nil, nil, err
	}
	return New(client, cfg.Token), client, nil
}
