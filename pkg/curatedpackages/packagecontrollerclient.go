package curatedpackages

import (
	"context"
	_ "embed"
	"errors"
	"fmt"

	packagesv1 "github.com/aws/eks-anywhere-packages/api/v1alpha1"
	"github.com/aws/eks-anywhere/pkg/constants"
	"github.com/aws/eks-anywhere/pkg/logger"
	"github.com/aws/eks-anywhere/pkg/templater"
)

//go:embed config/awssecret.yaml
var awsSecretYaml string

const (
	eksaDefaultRegion = "us-west-2"
	cronJobName       = "cronjob/cron-ecr-renew"
	jobName           = "eksa-auth-refresher"
)

type PackageControllerClientOpt func(client *PackageControllerClient)

type PackageControllerClient struct {
	kubeConfig          string
	uri                 string
	chartName           string
	chartVersion        string
	chartInstaller      ChartInstaller
	clusterName         string
	kubectl             KubectlRunner
	eksaAccessKeyId     string
	eksaSecretAccessKey string
	eksaRegion          string
}

type ChartInstaller interface {
	InstallChart(ctx context.Context, chart, ociURI, version, kubeconfigFilePath string, values []string) error
}

func NewPackageControllerClient(chartInstaller ChartInstaller, kubectl KubectlRunner, clusterName, kubeConfig, uri, chartName, chartVersion string, options ...PackageControllerClientOpt) *PackageControllerClient {
	pcc := &PackageControllerClient{
		kubeConfig:     kubeConfig,
		clusterName:    clusterName,
		uri:            uri,
		chartName:      chartName,
		chartVersion:   chartVersion,
		chartInstaller: chartInstaller,
		kubectl:        kubectl,
	}

	for _, o := range options {
		o(pcc)
	}
	return pcc
}

func (pc *PackageControllerClient) InstallController(ctx context.Context) error {
	ociUri := fmt.Sprintf("%s%s", "oci://", pc.uri)
	registry := GetRegistry(pc.uri)
	sourceRegistry := fmt.Sprintf("sourceRegistry=%s", registry)
	clusterName := fmt.Sprintf("clusterName=%s", pc.clusterName)
	values := []string{sourceRegistry, clusterName}
	err := pc.chartInstaller.InstallChart(ctx, pc.chartName, ociUri, pc.chartVersion, pc.kubeConfig, values)
	if err != nil {
		return err
	}

	if err = pc.ApplySecret(ctx); err != nil {
		logger.Info("Warning: No AWS key/license provided. Please be aware this will prevent the package controller from installing curated packages.")
	}

	if err = pc.CreateCronJob(ctx); err != nil {
		logger.Info("Warning: not able to trigger cron job, please be aware this will prevent the package controller from installing curated packages.")
	}
	return nil
}

func (pc *PackageControllerClient) ValidateControllerDoesNotExist(ctx context.Context) error {
	found, _ := pc.kubectl.GetResource(ctx, "packageBundleController", packagesv1.PackageBundleControllerName, pc.kubeConfig, constants.EksaPackagesName)
	if found {
		return errors.New("curated Packages controller exists in the current cluster")
	}
	return nil
}

func (pc *PackageControllerClient) ApplySecret(ctx context.Context) error {
	templateValues := map[string]string{
		"eksaAccessKeyId":     pc.eksaAccessKeyId,
		"eksaSecretAccessKey": pc.eksaSecretAccessKey,
		"eksaRegion":          pc.eksaRegion,
	}

	result, err := templater.Execute(awsSecretYaml, templateValues)
	if err != nil {
		return fmt.Errorf("replacing template values %v", err)
	}

	params := []string{"create", "-f", "-", "--kubeconfig", pc.kubeConfig}
	stdOut, err := pc.kubectl.ExecuteFromYaml(ctx, result, params...)
	if err != nil {
		return fmt.Errorf("creating secret %v", err)
	}

	fmt.Print(&stdOut)
	return nil
}

func (pc *PackageControllerClient) CreateCronJob(ctx context.Context) error {
	params := []string{"create", "job", jobName, "--from=" + cronJobName, "--kubeconfig", pc.kubeConfig, "--namespace", constants.EksaPackagesName}
	stdOut, err := pc.kubectl.ExecuteCommand(ctx, params...)
	if err != nil {
		return fmt.Errorf("executing cron job %v", err)
	}
	fmt.Print(&stdOut)
	return nil
}

func WithEksaAccessKeyId(eksaAccessKeyId string) func(client *PackageControllerClient) {
	return func(config *PackageControllerClient) {
		config.eksaAccessKeyId = eksaAccessKeyId
	}
}

func WithEksaSecretAccessKey(eksaSecretAccessKey string) func(client *PackageControllerClient) {
	return func(config *PackageControllerClient) {
		config.eksaSecretAccessKey = eksaSecretAccessKey
	}
}

func WithEksaRegion(eksaRegion string) func(client *PackageControllerClient) {
	return func(config *PackageControllerClient) {
		if eksaRegion != "" {
			config.eksaRegion = eksaRegion
		} else {
			config.eksaRegion = eksaDefaultRegion
		}
	}
}
