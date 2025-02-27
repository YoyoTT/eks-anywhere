package dependencies

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/aws/eks-anywhere/pkg/api/v1alpha1"
	"github.com/aws/eks-anywhere/pkg/awsiamauth"
	"github.com/aws/eks-anywhere/pkg/bootstrapper"
	"github.com/aws/eks-anywhere/pkg/clients/kubernetes"
	"github.com/aws/eks-anywhere/pkg/cluster"
	"github.com/aws/eks-anywhere/pkg/clusterapi"
	"github.com/aws/eks-anywhere/pkg/clustermanager"
	"github.com/aws/eks-anywhere/pkg/config"
	"github.com/aws/eks-anywhere/pkg/crypto"
	"github.com/aws/eks-anywhere/pkg/curatedpackages"
	"github.com/aws/eks-anywhere/pkg/diagnostics"
	"github.com/aws/eks-anywhere/pkg/eksd"
	"github.com/aws/eks-anywhere/pkg/executables"
	"github.com/aws/eks-anywhere/pkg/files"
	"github.com/aws/eks-anywhere/pkg/filewriter"
	gitfactory "github.com/aws/eks-anywhere/pkg/git/factory"
	"github.com/aws/eks-anywhere/pkg/gitops/flux"
	"github.com/aws/eks-anywhere/pkg/govmomi"
	"github.com/aws/eks-anywhere/pkg/kubeconfig"
	"github.com/aws/eks-anywhere/pkg/logger"
	"github.com/aws/eks-anywhere/pkg/manifests"
	"github.com/aws/eks-anywhere/pkg/networking/cilium"
	"github.com/aws/eks-anywhere/pkg/networking/kindnetd"
	"github.com/aws/eks-anywhere/pkg/networkutils"
	"github.com/aws/eks-anywhere/pkg/providers"
	"github.com/aws/eks-anywhere/pkg/providers/cloudstack"
	"github.com/aws/eks-anywhere/pkg/providers/cloudstack/decoder"
	"github.com/aws/eks-anywhere/pkg/providers/docker"
	"github.com/aws/eks-anywhere/pkg/providers/snow"
	"github.com/aws/eks-anywhere/pkg/providers/tinkerbell"
	"github.com/aws/eks-anywhere/pkg/providers/vsphere"
	"github.com/aws/eks-anywhere/pkg/types"
	"github.com/aws/eks-anywhere/pkg/utils/urls"
	"github.com/aws/eks-anywhere/pkg/version"
	"github.com/aws/eks-anywhere/pkg/workflows/interfaces"
)

type Dependencies struct {
	Provider                  providers.Provider
	ClusterAwsCli             *executables.Clusterawsadm
	DockerClient              *executables.Docker
	Kubectl                   *executables.Kubectl
	Govc                      *executables.Govc
	Cmk                       *executables.Cmk
	SnowAwsClientRegistry     *snow.AwsClientRegistry
	SnowConfigManager         *snow.ConfigManager
	Writer                    filewriter.FileWriter
	Kind                      *executables.Kind
	Clusterctl                *executables.Clusterctl
	Flux                      *executables.Flux
	Troubleshoot              *executables.Troubleshoot
	Helm                      *executables.Helm
	UnAuthKubeClient          *kubernetes.UnAuthClient
	Networking                clustermanager.Networking
	CiliumTemplater           *cilium.Templater
	AwsIamAuth                clustermanager.AwsIamAuth
	ClusterManager            *clustermanager.ClusterManager
	Bootstrapper              *bootstrapper.Bootstrapper
	GitOpsFlux                *flux.Flux
	Git                       *gitfactory.GitTools
	EksdInstaller             *eksd.Installer
	EksdUpgrader              *eksd.Upgrader
	AnalyzerFactory           diagnostics.AnalyzerFactory
	CollectorFactory          diagnostics.CollectorFactory
	DignosticCollectorFactory diagnostics.DiagnosticBundleFactory
	CAPIManager               *clusterapi.Manager
	ResourceSetManager        *clusterapi.ResourceSetManager
	FileReader                *files.Reader
	ManifestReader            *manifests.Reader
	closers                   []types.Closer
	CliConfig                 *config.CliConfig
	PackageInstaller          interfaces.PackageInstaller
	BundleRegistry            curatedpackages.BundleRegistry
	PackageControllerClient   curatedpackages.PackageController
	PackageClient             curatedpackages.PackageHandler
	VSphereValidator          *vsphere.Validator
	VSphereDefaulter          *vsphere.Defaulter
	SnowValidator             *snow.AwsClientValidator
}

func (d *Dependencies) Close(ctx context.Context) error {
	// Reverse the loop so we close like LIFO
	for i := len(d.closers) - 1; i >= 0; i-- {
		if err := d.closers[i].Close(ctx); err != nil {
			return err
		}
	}

	return nil
}

func ForSpec(ctx context.Context, clusterSpec *cluster.Spec) *Factory {
	eksaToolsImage := clusterSpec.VersionsBundle.Eksa.CliTools
	return NewFactory().
		UseExecutableImage(eksaToolsImage.VersionedImage()).
		WithRegistryMirror(clusterSpec.Cluster.RegistryMirror()).
		UseProxyConfiguration(clusterSpec.Cluster.ProxyConfiguration()).
		WithWriterFolder(clusterSpec.Cluster.Name).
		WithDiagnosticCollectorImage(clusterSpec.VersionsBundle.Eksa.DiagnosticCollector.VersionedImage())
}

type Factory struct {
	executablesConfig        *executablesConfig
	registryMirror           string
	proxyConfiguration       map[string]string
	writerFolder             string
	diagnosticCollectorImage string
	buildSteps               []buildStep
	dependencies             Dependencies
}

type executablesConfig struct {
	builder            *executables.ExecutablesBuilder
	image              string
	useDockerContainer bool
	dockerClient       executables.DockerClient
	mountDirs          []string
}

type buildStep func(ctx context.Context) error

func NewFactory() *Factory {
	return &Factory{
		writerFolder: "./",
		executablesConfig: &executablesConfig{
			useDockerContainer: executables.ExecutablesInDocker(),
		},
		buildSteps: make([]buildStep, 0),
	}
}

func (f *Factory) Build(ctx context.Context) (*Dependencies, error) {
	for _, step := range f.buildSteps {
		if err := step(ctx); err != nil {
			return nil, err
		}
	}

	// clean up stack
	f.buildSteps = make([]buildStep, 0)

	// Make copy of dependencies since its attributes are public
	d := f.dependencies

	return &d, nil
}

func (f *Factory) WithWriterFolder(folder string) *Factory {
	f.writerFolder = folder
	return f
}

func (f *Factory) WithRegistryMirror(mirror string) *Factory {
	f.registryMirror = mirror
	return f
}

func (f *Factory) UseProxyConfiguration(proxyConfig map[string]string) *Factory {
	f.proxyConfiguration = proxyConfig
	return f
}

func (f *Factory) GetProxyConfiguration() map[string]string {
	return f.proxyConfiguration
}

func (f *Factory) WithProxyConfiguration() *Factory {
	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.proxyConfiguration == nil {
			proxyConfig := config.GetProxyConfigFromEnv()
			f.UseProxyConfiguration(proxyConfig)
		}
		return nil
	},
	)

	return f
}

func (f *Factory) UseExecutableImage(image string) *Factory {
	f.executablesConfig.image = image
	return f
}

// WithExecutableImage sets the right cli tools image for the executable builder, reading
// from the Bundle and using the first VersionsBundle
// This is just the default for when there is not an specific kubernetes version available
// For commands that receive a cluster config file or a kubernetes version directly as input,
// use UseExecutableImage to specify the image directly
func (f *Factory) WithExecutableImage() *Factory {
	f.WithManifestReader()

	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.executablesConfig.image != "" {
			return nil
		}

		bundles, err := f.dependencies.ManifestReader.ReadBundlesForVersion(version.Get().GitVersion)
		if err != nil {
			return fmt.Errorf("retrieving executable tools image from bundle in dependency factory: %v", err)
		}

		f.executablesConfig.image = bundles.DefaultEksAToolsImage().VersionedImage()
		return nil
	})

	return f
}

func (f *Factory) WithExecutableMountDirs(mountDirs ...string) *Factory {
	f.executablesConfig.mountDirs = mountDirs
	return f
}

func (f *Factory) WithLocalExecutables() *Factory {
	f.executablesConfig.useDockerContainer = false
	return f
}

// UseExecutablesDockerClient forces a specific DockerClient to build
// Executables as opposed to follow the normal building flow
// This is only for testing
func (f *Factory) UseExecutablesDockerClient(client executables.DockerClient) *Factory {
	f.executablesConfig.dockerClient = client
	return f
}

func (f *Factory) WithExecutableBuilder() *Factory {
	if f.executablesConfig.useDockerContainer {
		f.WithExecutableImage().WithDocker()
	}

	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.executablesConfig.builder != nil {
			return nil
		}

		if f.executablesConfig.useDockerContainer {
			image := urls.ReplaceHost(f.executablesConfig.image, f.registryMirror)
			b, err := executables.NewInDockerExecutablesBuilder(
				f.executablesConfig.dockerClient,
				image,
				f.executablesConfig.mountDirs...,
			)
			if err != nil {
				return err
			}

			f.executablesConfig.builder = b
		} else {
			f.executablesConfig.builder = executables.NewLocalExecutablesBuilder()
		}

		closer, err := f.executablesConfig.builder.Init(ctx)
		if err != nil {
			return err
		}
		f.dependencies.closers = append(f.dependencies.closers, closer)

		return nil
	})

	return f
}

func (f *Factory) WithProvider(clusterConfigFile string, clusterConfig *v1alpha1.Cluster, skipIpCheck bool, hardwareCSVPath string, force bool, tinkerbellBootstrapIp string) *Factory {
	switch clusterConfig.Spec.DatacenterRef.Kind {
	case v1alpha1.VSphereDatacenterKind:
		f.WithKubectl().WithGovc().WithWriter().WithCAPIClusterResourceSetManager()
	case v1alpha1.CloudStackDatacenterKind:
		f.WithKubectl().WithCmk().WithWriter()
	case v1alpha1.DockerDatacenterKind:
		f.WithDocker().WithKubectl()
	case v1alpha1.TinkerbellDatacenterKind:
		if clusterConfig.Spec.RegistryMirrorConfiguration != nil {
			f.WithDocker().WithKubectl().WithWriter().WithHelm(executables.WithInsecure())
		} else {
			f.WithDocker().WithKubectl().WithWriter().WithHelm()
		}
	case v1alpha1.SnowDatacenterKind:
		f.WithUnAuthKubeClient().WithSnowConfigManager()
	}

	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.Provider != nil {
			return nil
		}

		switch clusterConfig.Spec.DatacenterRef.Kind {
		case v1alpha1.VSphereDatacenterKind:
			datacenterConfig, err := v1alpha1.GetVSphereDatacenterConfig(clusterConfigFile)
			if err != nil {
				return fmt.Errorf("unable to get datacenter config from file %s: %v", clusterConfigFile, err)
			}

			machineConfigs, err := v1alpha1.GetVSphereMachineConfigs(clusterConfigFile)
			if err != nil {
				return fmt.Errorf("unable to get machine config from file %s: %v", clusterConfigFile, err)
			}

			f.dependencies.Provider = vsphere.NewProvider(
				datacenterConfig,
				machineConfigs,
				clusterConfig,
				f.dependencies.Govc,
				f.dependencies.Kubectl,
				f.dependencies.Writer,
				time.Now,
				skipIpCheck,
				f.dependencies.ResourceSetManager,
			)

		case v1alpha1.CloudStackDatacenterKind:
			datacenterConfig, err := v1alpha1.GetCloudStackDatacenterConfig(clusterConfigFile)
			if err != nil {
				return fmt.Errorf("unable to get datacenter config from file %s: %v", clusterConfigFile, err)
			}

			machineConfigs, err := v1alpha1.GetCloudStackMachineConfigs(clusterConfigFile)
			if err != nil {
				return fmt.Errorf("unable to get machine config from file %s: %v", clusterConfigFile, err)
			}

			f.dependencies.Provider = cloudstack.NewProvider(
				datacenterConfig,
				machineConfigs,
				clusterConfig,
				f.dependencies.Kubectl,
				f.dependencies.Cmk,
				f.dependencies.Writer,
				time.Now,
				skipIpCheck,
			)

		case v1alpha1.SnowDatacenterKind:
			f.dependencies.Provider = snow.NewProvider(
				f.dependencies.UnAuthKubeClient,
				f.dependencies.SnowConfigManager,
				skipIpCheck,
			)

		case v1alpha1.TinkerbellDatacenterKind:
			datacenterConfig, err := v1alpha1.GetTinkerbellDatacenterConfig(clusterConfigFile)
			if err != nil {
				return fmt.Errorf("unable to get datacenter config from file %s: %v", clusterConfigFile, err)
			}

			machineConfigs, err := v1alpha1.GetTinkerbellMachineConfigs(clusterConfigFile)
			if err != nil {
				return fmt.Errorf("unable to get machine config from file %s: %v", clusterConfigFile, err)
			}

			tinkerbellIp := tinkerbellBootstrapIp
			if tinkerbellIp == "" {
				logger.V(4).Info("Inferring local Tinkerbell Bootstrap IP from environment")
				localIp, err := networkutils.GetLocalIP()
				if err != nil {
					return err
				}
				tinkerbellIp = localIp.String()
			}
			logger.V(4).Info("Tinkerbell IP", "tinkerbell-ip", tinkerbellIp)

			provider, err := tinkerbell.NewProvider(
				datacenterConfig,
				machineConfigs,
				clusterConfig,
				hardwareCSVPath,
				f.dependencies.Writer,
				f.dependencies.DockerClient,
				f.dependencies.Helm,
				f.dependencies.Kubectl,
				tinkerbellIp,
				time.Now,
				force,
				skipIpCheck,
			)
			if err != nil {
				return err
			}

			f.dependencies.Provider = provider

		case v1alpha1.DockerDatacenterKind:
			datacenterConfig, err := v1alpha1.GetDockerDatacenterConfig(clusterConfigFile)
			if err != nil {
				return fmt.Errorf("unable to get datacenter config from file %s: %v", clusterConfigFile, err)
			}

			f.dependencies.Provider = docker.NewProvider(
				datacenterConfig,
				f.dependencies.DockerClient,
				f.dependencies.Kubectl,
				time.Now,
			)
		default:
			return fmt.Errorf("no provider support for datacenter kind: %s", clusterConfig.Spec.DatacenterRef.Kind)
		}

		return nil
	})

	return f
}

func (f *Factory) WithDocker() *Factory {
	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.DockerClient != nil {
			return nil
		}

		f.dependencies.DockerClient = executables.BuildDockerExecutable()
		if f.executablesConfig.dockerClient == nil {
			f.executablesConfig.dockerClient = f.dependencies.DockerClient
		}

		return nil
	})

	return f
}

func (f *Factory) WithKubectl() *Factory {
	f.WithExecutableBuilder()

	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.Kubectl != nil {
			return nil
		}

		f.dependencies.Kubectl = f.executablesConfig.builder.BuildKubectlExecutable()
		return nil
	})

	return f
}

func (f *Factory) WithGovc() *Factory {
	f.WithExecutableBuilder().WithWriter()

	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.Govc != nil {
			return nil
		}

		f.dependencies.Govc = f.executablesConfig.builder.BuildGovcExecutable(f.dependencies.Writer)
		f.dependencies.closers = append(f.dependencies.closers, f.dependencies.Govc)

		return nil
	})

	return f
}

func (f *Factory) WithCmk() *Factory {
	f.WithExecutableBuilder().WithWriter()

	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.Cmk != nil {
			return nil
		}

		execConfig, err := decoder.ParseCloudStackSecret()
		if err != nil {
			return fmt.Errorf("building cmk executable: %v", err)
		}

		f.dependencies.Cmk = f.executablesConfig.builder.BuildCmkExecutable(f.dependencies.Writer, execConfig.Profiles)
		f.dependencies.closers = append(f.dependencies.closers, f.dependencies.Cmk)

		return nil
	})

	return f
}

func (f *Factory) WithSnowConfigManager() *Factory {
	f.WithAwsSnow().WithWriter()

	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.SnowConfigManager != nil {
			return nil
		}

		validator := snow.NewValidator(f.dependencies.SnowAwsClientRegistry)
		defaulters := snow.NewDefaulters(f.dependencies.SnowAwsClientRegistry, f.dependencies.Writer)

		f.dependencies.SnowConfigManager = snow.NewConfigManager(defaulters, validator)

		return nil
	})

	return f
}

func (f *Factory) WithAwsSnow() *Factory {
	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.SnowAwsClientRegistry != nil {
			return nil
		}

		clientRegistry := snow.NewAwsClientRegistry()
		err := clientRegistry.Build(ctx)
		if err != nil {
			return err
		}
		f.dependencies.SnowAwsClientRegistry = clientRegistry

		return nil
	})

	return f
}

func (f *Factory) WithWriter() *Factory {
	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.Writer != nil {
			return nil
		}

		var err error
		f.dependencies.Writer, err = filewriter.NewWriter(f.writerFolder)
		if err != nil {
			return err
		}

		return nil
	})

	return f
}

func (f *Factory) WithKind() *Factory {
	f.WithExecutableBuilder().WithWriter()

	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.Kind != nil {
			return nil
		}

		f.dependencies.Kind = f.executablesConfig.builder.BuildKindExecutable(f.dependencies.Writer)
		return nil
	})

	return f
}

func (f *Factory) WithClusterctl() *Factory {
	f.WithExecutableBuilder().WithWriter()

	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.Clusterctl != nil {
			return nil
		}

		f.dependencies.Clusterctl = f.executablesConfig.builder.BuildClusterCtlExecutable(f.dependencies.Writer)
		return nil
	})

	return f
}

func (f *Factory) WithFlux() *Factory {
	f.WithExecutableBuilder()

	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.Flux != nil {
			return nil
		}

		f.dependencies.Flux = f.executablesConfig.builder.BuildFluxExecutable()
		return nil
	})

	return f
}

func (f *Factory) WithTroubleshoot() *Factory {
	f.WithExecutableBuilder()

	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.Troubleshoot != nil {
			return nil
		}

		f.dependencies.Troubleshoot = f.executablesConfig.builder.BuildTroubleshootExecutable()
		return nil
	})

	return f
}

func (f *Factory) WithHelm(opts ...executables.HelmOpt) *Factory {
	f.WithExecutableBuilder().WithProxyConfiguration()

	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.registryMirror != "" {
			opts = append(opts, executables.WithRegistryMirror(f.registryMirror))
		}

		if f.proxyConfiguration != nil {
			opts = append(opts, executables.WithEnv(f.proxyConfiguration))
		}

		f.dependencies.Helm = f.executablesConfig.builder.BuildHelmExecutable(opts...)
		return nil
	})

	return f
}

func (f *Factory) WithNetworking(clusterConfig *v1alpha1.Cluster) *Factory {
	var networkingBuilder func() clustermanager.Networking
	if clusterConfig.Spec.ClusterNetwork.CNIConfig.Kindnetd != nil {
		f.WithKubectl()
		networkingBuilder = func() clustermanager.Networking {
			return kindnetd.NewKindnetd(f.dependencies.Kubectl)
		}
	} else {
		f.WithKubectl().WithHelm(executables.WithInsecure())
		networkingBuilder = func() clustermanager.Networking {
			return cilium.NewCilium(f.dependencies.Kubectl, f.dependencies.Helm)
		}
	}

	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.Networking != nil {
			return nil
		}
		f.dependencies.Networking = networkingBuilder()

		return nil
	})

	return f
}

func (f *Factory) WithCiliumTemplater() *Factory {
	f.WithHelm()

	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.CiliumTemplater != nil {
			return nil
		}
		f.dependencies.CiliumTemplater = cilium.NewTemplater(f.dependencies.Helm)

		return nil
	})

	return f
}

func (f *Factory) WithAwsIamAuth() *Factory {
	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.AwsIamAuth != nil {
			return nil
		}
		certgen := crypto.NewCertificateGenerator()
		clusterId := uuid.New()
		f.dependencies.AwsIamAuth = awsiamauth.NewAwsIamAuth(certgen, clusterId)
		return nil
	})

	return f
}

type bootstrapperClient struct {
	*executables.Kind
	*executables.Kubectl
}

func (f *Factory) WithBootstrapper() *Factory {
	f.WithKind().WithKubectl()

	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.Bootstrapper != nil {
			return nil
		}

		f.dependencies.Bootstrapper = bootstrapper.New(&bootstrapperClient{f.dependencies.Kind, f.dependencies.Kubectl})
		return nil
	})

	return f
}

type clusterManagerClient struct {
	*executables.Clusterctl
	*executables.Kubectl
}

func (f *Factory) WithClusterManager(clusterConfig *v1alpha1.Cluster, opts ...clustermanager.ClusterManagerOpt) *Factory {
	f.WithClusterctl().WithKubectl().WithNetworking(clusterConfig).WithWriter().WithDiagnosticBundleFactory().WithAwsIamAuth()

	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.ClusterManager != nil {
			return nil
		}

		f.dependencies.ClusterManager = clustermanager.New(
			&clusterManagerClient{
				f.dependencies.Clusterctl,
				f.dependencies.Kubectl,
			},
			f.dependencies.Networking,
			f.dependencies.Writer,
			f.dependencies.DignosticCollectorFactory,
			f.dependencies.AwsIamAuth,
			opts...,
		)
		return nil
	})

	return f
}

func (f *Factory) WithCliConfig(cliConfig *config.CliConfig) *Factory {
	f.dependencies.CliConfig = cliConfig
	return f
}

type eksdInstallerClient struct {
	*executables.Kubectl
}

func (f *Factory) WithEksdInstaller() *Factory {
	f.WithKubectl().WithFileReader()

	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.EksdInstaller != nil {
			return nil
		}

		f.dependencies.EksdInstaller = eksd.NewEksdInstaller(
			&eksdInstallerClient{
				f.dependencies.Kubectl,
			},
			f.dependencies.FileReader,
		)
		return nil
	})

	return f
}

func (f *Factory) WithEksdUpgrader() *Factory {
	f.WithKubectl().WithFileReader()

	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.EksdUpgrader != nil {
			return nil
		}

		f.dependencies.EksdUpgrader = eksd.NewUpgrader(
			&eksdInstallerClient{
				f.dependencies.Kubectl,
			},
			f.dependencies.FileReader,
		)
		return nil
	})

	return f
}

func (f *Factory) WithGit(clusterConfig *v1alpha1.Cluster, fluxConfig *v1alpha1.FluxConfig) *Factory {
	f.WithWriter()
	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.Git != nil {
			return nil
		}

		if fluxConfig == nil {
			return nil
		}

		tools, err := gitfactory.Build(ctx, clusterConfig, fluxConfig, f.dependencies.Writer)
		if err != nil {
			return fmt.Errorf("creating Git provider: %v", err)
		}

		if fluxConfig.Spec.Git != nil {
			err = tools.Client.ValidateRemoteExists(ctx)
			if err != nil {
				return err
			}
		}

		if tools.Provider != nil {
			err = tools.Provider.Validate(ctx)
			if err != nil {
				return fmt.Errorf("validating provider: %v", err)
			}
		}

		f.dependencies.Git = tools
		return nil
	})
	return f
}

func (f *Factory) WithGitOpsFlux(clusterConfig *v1alpha1.Cluster, fluxConfig *v1alpha1.FluxConfig, cliConfig *config.CliConfig) *Factory {
	f.WithWriter().WithFlux().WithKubectl().WithGit(clusterConfig, fluxConfig)

	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.GitOpsFlux != nil {
			return nil
		}

		f.dependencies.GitOpsFlux = flux.NewFlux(f.dependencies.Flux, f.dependencies.Kubectl, f.dependencies.Git, cliConfig)

		return nil
	})

	return f
}

func (f *Factory) WithPackageInstaller(spec *cluster.Spec, packagesLocation string) *Factory {
	f.WithKubectl().WithPackageControllerClient(spec).WithPackageClient()
	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.PackageInstaller != nil {
			return nil
		}

		f.dependencies.PackageInstaller = curatedpackages.NewInstaller(
			f.dependencies.Kubectl,
			f.dependencies.PackageClient,
			f.dependencies.PackageControllerClient,
			spec,
			packagesLocation,
		)
		return nil
	})
	return f
}

func (f *Factory) WithPackageControllerClient(spec *cluster.Spec) *Factory {
	f.WithHelm(executables.WithInsecure()).WithKubectl()

	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.PackageControllerClient != nil {
			return nil
		}
		kubeConfig := kubeconfig.FromClusterName(spec.Cluster.Name)

		chart := spec.VersionsBundle.PackageController.HelmChart
		imageUrl := urls.ReplaceHost(chart.Image(), spec.Cluster.RegistryMirror())
		eksaAccessKeyId, eksaSecretKey, eksaRegion := os.Getenv(config.EksaAccessKeyIdEnv), os.Getenv(config.EksaSecretAcessKeyEnv), os.Getenv(config.EksaRegionEnv)
		f.dependencies.PackageControllerClient = curatedpackages.NewPackageControllerClient(
			f.dependencies.Helm,
			f.dependencies.Kubectl,
			spec.Cluster.Name,
			kubeConfig,
			imageUrl,
			chart.Name,
			chart.Tag(),
			curatedpackages.WithEksaAccessKeyId(eksaAccessKeyId),
			curatedpackages.WithEksaSecretAccessKey(eksaSecretKey),
			curatedpackages.WithEksaRegion(eksaRegion),
		)
		return nil
	})

	return f
}

func (f *Factory) WithPackageClient() *Factory {
	f.WithKubectl()
	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.PackageClient != nil {
			return nil
		}

		f.dependencies.PackageClient = curatedpackages.NewPackageClient(
			f.dependencies.Kubectl,
		)
		return nil
	})
	return f
}

func (f *Factory) WithCuratedPackagesRegistry(registryName, kubeVersion string, version version.Info) *Factory {
	if registryName != "" {
		f.WithHelm(executables.WithInsecure())
	} else {
		f.WithManifestReader()
	}

	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.BundleRegistry != nil {
			return nil
		}

		if registryName != "" {
			f.dependencies.BundleRegistry = curatedpackages.NewCustomRegistry(
				f.dependencies.Helm,
				registryName,
			)
		} else {
			f.dependencies.BundleRegistry = curatedpackages.NewDefaultRegistry(
				f.dependencies.ManifestReader,
				kubeVersion,
				version,
			)
		}
		return nil
	})
	return f
}

func (f *Factory) WithDiagnosticBundleFactory() *Factory {
	f.WithWriter().WithTroubleshoot().WithCollectorFactory().WithAnalyzerFactory().WithKubectl()
	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.DignosticCollectorFactory != nil {
			return nil
		}

		opts := diagnostics.EksaDiagnosticBundleFactoryOpts{
			AnalyzerFactory:  f.dependencies.AnalyzerFactory,
			Client:           f.dependencies.Troubleshoot,
			CollectorFactory: f.dependencies.CollectorFactory,
			Kubectl:          f.dependencies.Kubectl,
			Writer:           f.dependencies.Writer,
		}

		f.dependencies.DignosticCollectorFactory = diagnostics.NewFactory(opts)
		return nil
	})

	return f
}

func (f *Factory) WithAnalyzerFactory() *Factory {
	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.AnalyzerFactory != nil {
			return nil
		}

		f.dependencies.AnalyzerFactory = diagnostics.NewAnalyzerFactory()
		return nil
	})

	return f
}

func (f *Factory) WithDiagnosticCollectorImage(diagnosticCollectorImage string) *Factory {
	f.diagnosticCollectorImage = diagnosticCollectorImage
	return f
}

func (f *Factory) WithCollectorFactory() *Factory {
	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.CollectorFactory != nil {
			return nil
		}

		if f.diagnosticCollectorImage == "" {
			f.dependencies.CollectorFactory = diagnostics.NewDefaultCollectorFactory()
		} else {
			f.dependencies.CollectorFactory = diagnostics.NewCollectorFactory(f.diagnosticCollectorImage)
		}
		return nil
	})

	return f
}

func (f *Factory) WithCAPIManager() *Factory {
	f.WithClusterctl()
	f.WithKubectl()

	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.CAPIManager != nil {
			return nil
		}

		f.dependencies.CAPIManager = clusterapi.NewManager(f.dependencies.Clusterctl, f.dependencies.Kubectl)
		return nil
	})

	return f
}

func (f *Factory) WithCAPIClusterResourceSetManager() *Factory {
	f.WithKubectl()

	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.ResourceSetManager != nil {
			return nil
		}

		f.dependencies.ResourceSetManager = clusterapi.NewResourceSetManager(f.dependencies.Kubectl)
		return nil
	})

	return f
}

func (f *Factory) WithFileReader() *Factory {
	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.FileReader != nil {
			return nil
		}

		f.dependencies.FileReader = files.NewReader(files.WithUserAgent(
			fmt.Sprintf("eks-a-cli/%s", version.Get().GitVersion)),
		)
		return nil
	})

	return f
}

func (f *Factory) WithManifestReader() *Factory {
	f.WithFileReader()

	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.ManifestReader != nil {
			return nil
		}

		f.dependencies.ManifestReader = manifests.NewReader(f.dependencies.FileReader)
		return nil
	})

	return f
}

func (f *Factory) WithUnAuthKubeClient() *Factory {
	f.WithKubectl()

	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.UnAuthKubeClient != nil {
			return nil
		}

		f.dependencies.UnAuthKubeClient = kubernetes.NewUnAuthClient(f.dependencies.Kubectl)
		if err := f.dependencies.UnAuthKubeClient.Init(); err != nil {
			return fmt.Errorf("building unauth kube client: %v", err)
		}

		return nil
	})

	return f
}

func (f *Factory) WithVSphereValidator() *Factory {
	f.WithGovc()

	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.VSphereValidator != nil {
			return nil
		}
		vcb := govmomi.NewVMOMIClientBuilder()
		v := vsphere.NewValidator(
			f.dependencies.Govc,
			&networkutils.DefaultNetClient{},
			vcb,
		)
		f.dependencies.VSphereValidator = v

		return nil
	})

	return f
}

func (f *Factory) WithVSphereDefaulter() *Factory {
	f.WithGovc()

	f.buildSteps = append(f.buildSteps, func(ctx context.Context) error {
		if f.dependencies.VSphereDefaulter != nil {
			return nil
		}

		f.dependencies.VSphereDefaulter = vsphere.NewDefaulter(f.dependencies.Govc)

		return nil
	})

	return f
}
