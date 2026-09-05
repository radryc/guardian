package dockerdriver

type BackendAPI interface {
	EnsureNetwork(network Network) error
	GetNetwork(name string) (Network, bool, error)
	DeleteNetwork(name string) error
	ConnectNetwork(networkName, containerName string, aliases []string) error

	UpsertVolume(volume Volume) error
	GetVolume(name string) (Volume, bool, error)
	DeleteVolume(name string) error

	UpsertConfig(config Config) error
	GetConfig(name string) (Config, bool, error)
	DeleteConfig(name string) error

	UpsertContainer(container Container) error
	GetContainer(name string) (Container, bool, error)
	DeleteContainer(name string) error
	ListContainersByAsset(partition, intent, asset string) ([]Container, error)
}
