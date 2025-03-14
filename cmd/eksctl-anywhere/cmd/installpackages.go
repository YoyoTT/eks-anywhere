package cmd

import (
	"context"
	"fmt"
	"log"

	"github.com/spf13/cobra"

	"github.com/aws/eks-anywhere/pkg/curatedpackages"
	"github.com/aws/eks-anywhere/pkg/kubeconfig"
	"github.com/aws/eks-anywhere/pkg/validations"
)

type installPackageOptions struct {
	source        curatedpackages.BundleSource
	kubeVersion   string
	packageName   string
	registry      string
	customConfigs []string
	// kubeConfig is an optional kubeconfig file to use when querying an
	// existing cluster
	kubeConfig string
}

var ipo = &installPackageOptions{}

func init() {
	installCmd.AddCommand(installPackageCommand)

	installPackageCommand.Flags().Var(&ipo.source, "source",
		"Location to find curated packages: (cluster, registry)")
	installPackageCommand.Flags().StringVar(&ipo.kubeVersion, "kube-version", "",
		"Kubernetes Version of the cluster to be used. Format <major>.<minor>")
	installPackageCommand.Flags().StringVarP(&ipo.packageName, "package-name", "n",
		"", "Custom name of the curated package to install")
	installPackageCommand.Flags().StringVar(&ipo.registry, "registry",
		"", "Used to specify an alternative registry for discovery")
	installPackageCommand.Flags().StringArrayVar(&ipo.customConfigs, "set",
		[]string{}, "Provide custom configurations for curated packages. Format key:value")
	installPackageCommand.Flags().StringVar(&ipo.kubeConfig, "kubeconfig", "",
		"Path to an optional kubeconfig file to use.")

	if err := installPackageCommand.MarkFlagRequired("source"); err != nil {
		log.Fatalf("marking source flag as required: %s", err)
	}
	if err := installPackageCommand.MarkFlagRequired("package-name"); err != nil {
		log.Fatalf("marking package-name flag as required: %s", err)
	}
}

var installPackageCommand = &cobra.Command{
	Use:          "package [package] [flags]",
	Aliases:      []string{"package"},
	Short:        "Install package",
	Long:         "This command is used to Install a curated package. Use list to discover curated packages",
	PreRunE:      preRunPackages,
	SilenceUsage: true,
	RunE:         runInstallPackages,
	Args:         cobra.ExactArgs(1),
}

func runInstallPackages(cmd *cobra.Command, args []string) error {
	if err := curatedpackages.ValidateKubeVersion(ipo.kubeVersion, ipo.source); err != nil {
		return err
	}

	return installPackages(cmd.Context(), args)
}

func installPackages(ctx context.Context, args []string) error {
	kubeConfig := ipo.kubeConfig
	if kubeConfig == "" {
		kubeConfig = kubeconfig.FromEnvironment()
	} else if !validations.FileExistsAndIsNotEmpty(kubeConfig) {
		return fmt.Errorf("kubeconfig file %q is empty or does not exist", kubeConfig)
	}
	deps, err := NewDependenciesForPackages(ctx, WithRegistryName(ipo.registry), WithKubeVersion(ipo.kubeVersion), WithMountPaths(kubeConfig))
	if err != nil {
		return fmt.Errorf("unable to initialize executables: %v", err)
	}

	bm := curatedpackages.CreateBundleManager()

	b := curatedpackages.NewBundleReader(
		kubeConfig,
		ipo.source,
		deps.Kubectl,
		bm,
		deps.BundleRegistry,
	)

	bundle, err := b.GetLatestBundle(ctx, ipo.kubeVersion)
	if err != nil {
		return err
	}

	packages := curatedpackages.NewPackageClient(
		deps.Kubectl,
		curatedpackages.WithBundle(bundle),
		curatedpackages.WithCustomConfigs(ipo.customConfigs),
	)

	p, err := packages.GetPackageFromBundle(args[0])
	if err != nil {
		return err
	}

	curatedpackages.PrintLicense()
	err = packages.InstallPackage(ctx, p, ipo.packageName, kubeConfig)
	if err != nil {
		return err
	}
	return nil
}
