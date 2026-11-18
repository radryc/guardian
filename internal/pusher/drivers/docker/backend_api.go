package dockerdriver

import "context"

type BackendAPI interface {
	EnsureNetwork(ctx context.Context, network Network) error
	GetNetwork(ctx context.Context, name string) (Network, bool, error)
	DeleteNetwork(ctx context.Context, name string) error
	ConnectNetwork(ctx context.Context, networkName, containerName string, aliases []string) error

	UpsertVolume(ctx context.Context, volume Volume) error
	GetVolume(ctx context.Context, name string) (Volume, bool, error)
	DeleteVolume(ctx context.Context, name string) error

	UpsertConfig(ctx context.Context, config Config) error
	GetConfig(ctx context.Context, name string) (Config, bool, error)
	DeleteConfig(ctx context.Context, name string) error

	UpsertContainer(ctx context.Context, container Container) error
	GetContainer(ctx context.Context, name string) (Container, bool, error)
	DeleteContainer(ctx context.Context, name string) error
	ListContainersByAsset(ctx context.Context, partition, intent, asset string) ([]Container, error)
}
