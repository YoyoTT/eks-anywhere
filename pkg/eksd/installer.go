package eksd

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/eks-anywhere/pkg/cluster"
	"github.com/aws/eks-anywhere/pkg/constants"
	"github.com/aws/eks-anywhere/pkg/logger"
	"github.com/aws/eks-anywhere/pkg/retrier"
	"github.com/aws/eks-anywhere/pkg/types"
)

const (
	maxRetries    = 5
	backOffPeriod = 5 * time.Second
)

type EksdInstallerClient interface {
	ApplyKubeSpecFromBytesWithNamespace(ctx context.Context, cluster *types.Cluster, data []byte, namespace string) error
}

type Reader interface {
	ReadFile(url string) ([]byte, error)
}

type Installer struct {
	client  EksdInstallerClient
	retrier *retrier.Retrier
	reader  Reader
}

func NewEksdInstaller(client EksdInstallerClient, reader Reader) *Installer {
	return &Installer{
		client:  client,
		retrier: retrier.NewWithMaxRetries(maxRetries, backOffPeriod),
		reader:  reader,
	}
}

func (i *Installer) InstallEksdCRDs(ctx context.Context, clusterSpec *cluster.Spec, cluster *types.Cluster) error {
	var eksdCRDs []byte
	if err := i.retrier.Retry(
		func() error {
			var readerErr error
			eksdCRDs, readerErr = i.reader.ReadFile(clusterSpec.VersionsBundle.EksD.Components)
			return readerErr
		},
	); err != nil {
		return fmt.Errorf("loading manifest for eksd components: %v", err)
	}

	if err := i.retrier.Retry(
		func() error {
			return i.client.ApplyKubeSpecFromBytesWithNamespace(ctx, cluster, eksdCRDs, constants.EksaSystemNamespace)
		},
	); err != nil {
		return fmt.Errorf("applying eksd release crd: %v", err)
	}

	return nil
}

// SetRetrier allows to modify the internal retrier
// For unit testing purposes only. It is not thread safe
func (i *Installer) SetRetrier(retrier *retrier.Retrier) {
	i.retrier = retrier
}

func (i *Installer) InstallEksdManifest(ctx context.Context, clusterSpec *cluster.Spec, cluster *types.Cluster) error {
	var eksdReleaseManifest []byte
	if err := i.retrier.Retry(
		func() error {
			var readerErr error
			eksdReleaseManifest, readerErr = i.reader.ReadFile(clusterSpec.VersionsBundle.EksD.EksDReleaseUrl)
			return readerErr
		},
	); err != nil {
		return fmt.Errorf("loading manifest for eksd components: %v", err)
	}

	logger.V(4).Info("Applying eksd manifest to cluster")
	if err := i.retrier.Retry(
		func() error {
			return i.client.ApplyKubeSpecFromBytesWithNamespace(ctx, cluster, eksdReleaseManifest, constants.EksaSystemNamespace)
		},
	); err != nil {
		return fmt.Errorf("applying eksd release manifest: %v", err)
	}

	return nil
}
