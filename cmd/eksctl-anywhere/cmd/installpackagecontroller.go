package cmd

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/spf13/cobra"

	"github.com/aws/eks-anywhere/pkg/config"
	"github.com/aws/eks-anywhere/pkg/curatedpackages"
	"github.com/aws/eks-anywhere/pkg/kubeconfig"
	"github.com/aws/eks-anywhere/pkg/utils/urls"
	"github.com/aws/eks-anywhere/pkg/validations"
	"github.com/aws/eks-anywhere/pkg/version"
)

type installControllerOptions struct {
	fileName string
}

var ico = &installControllerOptions{}

func init() {
	installCmd.AddCommand(installPackageControllerCommand)
	installPackageControllerCommand.Flags().StringVarP(&ico.fileName, "filename", "f", "", "Filename that contains EKS-A cluster configuration")
	if err := installPackageControllerCommand.MarkFlagRequired("filename"); err != nil {
		log.Fatalf("Error marking flag as required: %v", err)
	}
}

var installPackageControllerCommand = &cobra.Command{
	Use:          "packagecontroller",
	Aliases:      []string{"pc"},
	Short:        "Install packagecontroller on the cluster",
	Long:         "This command is used to Install the packagecontroller on to an existing cluster",
	PreRunE:      preRunPackages,
	SilenceUsage: true,
	RunE:         runInstallPackageController,
}

func runInstallPackageController(cmd *cobra.Command, args []string) error {
	clusterConfigFileExist := validations.FileExists(ico.fileName)
	if !clusterConfigFileExist {
		return fmt.Errorf("the cluster config file %s does not exist", ico.fileName)
	}
	return installPackageController(cmd.Context())
}

func installPackageController(ctx context.Context) error {
	kubeConfig := kubeconfig.FromEnvironment()

	clusterSpec, err := readAndValidateClusterSpec(ico.fileName, version.Get())
	if err != nil {
		return fmt.Errorf("the cluster config file provided is invalid: %v", err)
	}

	deps, err := NewDependenciesForPackages(ctx, WithMountPaths(kubeConfig))
	if err != nil {
		return fmt.Errorf("unable to initialize executables: %v", err)
	}

	versionBundle, err := curatedpackages.GetVersionBundle(deps.ManifestReader, version.Get().GitVersion, clusterSpec.Cluster)
	if err != nil {
		return err
	}
	registryEndpoint := ""
	if clusterSpec.Cluster.Spec.RegistryMirrorConfiguration != nil {
		registryEndpoint = clusterSpec.Cluster.Spec.RegistryMirrorConfiguration.Endpoint
	}
	helmChart := versionBundle.PackageController.HelmChart
	imageUrl := urls.ReplaceHost(helmChart.Image(), registryEndpoint)
	eksaAccessKeyId, eksaSecretKey, eksaRegion := os.Getenv(config.EksaAccessKeyIdEnv), os.Getenv(config.EksaSecretAcessKeyEnv), os.Getenv(config.EksaRegionEnv)
	ctrlClient := curatedpackages.NewPackageControllerClient(
		deps.Helm,
		deps.Kubectl,
		clusterSpec.Cluster.Name,
		kubeConfig,
		imageUrl,
		helmChart.Name,
		helmChart.Tag(),
		curatedpackages.WithEksaRegion(eksaRegion),
		curatedpackages.WithEksaSecretAccessKey(eksaSecretKey),
		curatedpackages.WithEksaAccessKeyId(eksaAccessKeyId),
	)

	if err = curatedpackages.VerifyCertManagerExists(ctx, deps.Kubectl, kubeConfig); err != nil {
		return err
	}

	if err = ctrlClient.ValidateControllerDoesNotExist(ctx); err != nil {
		return err
	}

	curatedpackages.PrintLicense()
	err = ctrlClient.InstallController(ctx)
	if err != nil {
		return err
	}

	return nil
}
