// Package manifests deals with creating manifests for all manifests to be installed for the cluster
package manifests

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/ghodss/yaml"
	"github.com/pkg/errors"
	"github.com/vincent-petithory/dataurl"

	"github.com/openshift/installer/pkg/asset"
	"github.com/openshift/installer/pkg/asset/installconfig"
	"github.com/openshift/installer/pkg/asset/templates/content/bootkube"
	"github.com/openshift/installer/pkg/asset/tls"
	"github.com/openshift/installer/pkg/types"
)

const (
	manifestDir = "manifests"
)

var (
	kubeSysConfigPath = filepath.Join(manifestDir, "cluster-config.yaml")

	_ asset.WritableAsset = (*Manifests)(nil)

	customTmplFuncs = template.FuncMap{
		"indent": indent,
		"add": func(i, j int) int {
			return i + j
		},
	}
)

// Manifests generates the dependent operator config.yaml files
type Manifests struct {
	KubeSysConfig *configurationObject
	FileList      []*asset.File
}

type genericData map[string]string

// Name returns a human friendly name for the operator
func (m *Manifests) Name() string {
	return "Common Manifests"
}

// Dependencies returns all of the dependencies directly needed by a
// Manifests asset.
func (m *Manifests) Dependencies() []asset.Asset {
	return []asset.Asset{
		&installconfig.ClusterID{},
		&installconfig.InstallConfig{},
		&Ingress{},
		&DNS{},
		&Infrastructure{},
		&Networking{},
		&Proxy{},
		&Scheduler{},
		&ImageContentSourcePolicy{},
		&tls.RootCA{},
		&tls.EtcdSignerCertKey{},
		&tls.EtcdCABundle{},
		&tls.EtcdSignerClientCertKey{},
		&tls.EtcdMetricCABundle{},
		&tls.EtcdMetricSignerCertKey{},
		&tls.EtcdMetricSignerClientCertKey{},
		&tls.MCSCertKey{},

		&bootkube.CVOOverrides{},
		&bootkube.EtcdCAConfigMap{},
		&bootkube.EtcdClientSecret{},
		&bootkube.EtcdMetricClientSecret{},
		&bootkube.EtcdMetricServingCAConfigMap{},
		&bootkube.EtcdMetricSignerSecret{},
		&bootkube.EtcdNamespace{},
		&bootkube.EtcdService{},
		&bootkube.EtcdSignerSecret{},
		&bootkube.KubeCloudConfig{},
		&bootkube.EtcdServingCAConfigMap{},
		&bootkube.KubeSystemConfigmapRootCA{},
		&bootkube.MachineConfigServerTLSSecret{},
		&bootkube.OpenshiftConfigSecretPullSecret{},
		&bootkube.OpenshiftMachineConfigOperator{},
		&bootkube.KubevirtInfraNamespace{},
		&bootkube.AROWorkerRegistries{},
		&bootkube.AROIngressService{},
		&bootkube.ARODNSConfig{},
		&bootkube.AROImageRegistry{},
		&bootkube.AROImageRegistryConfig{},
	}
}

// Generate generates the respective operator config.yml files
func (m *Manifests) Generate(dependencies asset.Parents) error {
	ingress := &Ingress{}
	dns := &DNS{}
	network := &Networking{}
	infra := &Infrastructure{}
	installConfig := &installconfig.InstallConfig{}
	proxy := &Proxy{}
	scheduler := &Scheduler{}
	imageContentSourcePolicy := &ImageContentSourcePolicy{}
	dependencies.Get(installConfig, ingress, dns, network, infra, proxy, scheduler, imageContentSourcePolicy)

	redactedConfig, err := redactedInstallConfig(*installConfig.Config)
	if err != nil {
		return errors.Wrap(err, "failed to redact install-config")
	}
	// mao go to kube-system config map
	m.KubeSysConfig = configMap("kube-system", "cluster-config-v1", genericData{
		"install-config": string(redactedConfig),
	})
	kubeSysConfigData, err := yaml.Marshal(m.KubeSysConfig)
	if err != nil {
		return errors.Wrap(err, "failed to create kube-system/cluster-config-v1 configmap")
	}

	m.FileList = []*asset.File{
		{
			Filename: kubeSysConfigPath,
			Data:     kubeSysConfigData,
		},
	}
	m.FileList = append(m.FileList, m.generateBootKubeManifests(dependencies)...)

	m.FileList = append(m.FileList, ingress.Files()...)
	m.FileList = append(m.FileList, dns.Files()...)
	m.FileList = append(m.FileList, network.Files()...)
	m.FileList = append(m.FileList, infra.Files()...)
	m.FileList = append(m.FileList, proxy.Files()...)
	m.FileList = append(m.FileList, scheduler.Files()...)
	m.FileList = append(m.FileList, imageContentSourcePolicy.Files()...)

	asset.SortFiles(m.FileList)

	return nil
}

// Files returns the files generated by the asset.
func (m *Manifests) Files() []*asset.File {
	return m.FileList
}

func (m *Manifests) generateBootKubeManifests(dependencies asset.Parents) []*asset.File {
	clusterID := &installconfig.ClusterID{}
	installConfig := &installconfig.InstallConfig{}
	mcsCertKey := &tls.MCSCertKey{}
	etcdMetricCABundle := &tls.EtcdMetricCABundle{}
	etcdMetricSignerClientCertKey := &tls.EtcdMetricSignerClientCertKey{}
	etcdMetricSignerCertKey := &tls.EtcdMetricSignerCertKey{}
	rootCA := &tls.RootCA{}
	etcdSignerCertKey := &tls.EtcdSignerCertKey{}
	etcdCABundle := &tls.EtcdCABundle{}
	etcdSignerClientCertKey := &tls.EtcdSignerClientCertKey{}
	aroDNSConfig := &bootkube.ARODNSConfig{}
	aroImageRegistryConfig := &bootkube.AROImageRegistryConfig{}
	dependencies.Get(
		clusterID,
		installConfig,
		etcdSignerCertKey,
		etcdCABundle,
		etcdSignerClientCertKey,
		etcdMetricCABundle,
		etcdMetricSignerClientCertKey,
		etcdMetricSignerCertKey,
		mcsCertKey,
		rootCA,
		aroDNSConfig,
		aroImageRegistryConfig,
	)

	templateData := &bootkubeTemplateData{
		CVOClusterID:                  clusterID.UUID,
		EtcdCaBundle:                  string(etcdCABundle.Cert()),
		EtcdMetricCaCert:              string(etcdMetricCABundle.Cert()),
		EtcdMetricSignerCert:          base64.StdEncoding.EncodeToString(etcdMetricSignerCertKey.Cert()),
		EtcdMetricSignerClientCert:    base64.StdEncoding.EncodeToString(etcdMetricSignerClientCertKey.Cert()),
		EtcdMetricSignerClientKey:     base64.StdEncoding.EncodeToString(etcdMetricSignerClientCertKey.Key()),
		EtcdMetricSignerKey:           base64.StdEncoding.EncodeToString(etcdMetricSignerCertKey.Key()),
		EtcdSignerCert:                base64.StdEncoding.EncodeToString(etcdSignerCertKey.Cert()),
		EtcdSignerClientCert:          base64.StdEncoding.EncodeToString(etcdSignerClientCertKey.Cert()),
		EtcdSignerClientKey:           base64.StdEncoding.EncodeToString(etcdSignerClientCertKey.Key()),
		EtcdSignerKey:                 base64.StdEncoding.EncodeToString(etcdSignerCertKey.Key()),
		McsTLSCert:                    base64.StdEncoding.EncodeToString(mcsCertKey.Cert()),
		McsTLSKey:                     base64.StdEncoding.EncodeToString(mcsCertKey.Key()),
		PullSecretBase64:              base64.StdEncoding.EncodeToString([]byte(installConfig.Config.PullSecret)),
		RootCaCert:                    string(rootCA.Cert()),
		AROWorkerRegistries:           aroWorkerRegistries(installConfig.Config.ImageContentSources),
		AROIngressIP:                  aroDNSConfig.IngressIP,
		AROIngressInternal:            installConfig.Config.Publish == types.InternalPublishingStrategy,
		AROImageRegistryHTTPSecret:    aroImageRegistryConfig.HTTPSecret,
		AROImageRegistryAccountName:   aroImageRegistryConfig.AccountName,
		AROImageRegistryContainerName: aroImageRegistryConfig.ContainerName,
		AROCloudName:                  installConfig.Azure.CloudName.Name(),
	}

	files := []*asset.File{}
	for _, a := range []asset.WritableAsset{
		&bootkube.CVOOverrides{},
		&bootkube.EtcdCAConfigMap{},
		&bootkube.EtcdClientSecret{},
		&bootkube.EtcdMetricClientSecret{},
		&bootkube.EtcdMetricSignerSecret{},
		&bootkube.EtcdMetricServingCAConfigMap{},
		&bootkube.EtcdNamespace{},
		&bootkube.EtcdService{},
		&bootkube.EtcdServingCAConfigMap{},
		&bootkube.EtcdSignerSecret{},
		&bootkube.KubeCloudConfig{},
		&bootkube.KubeSystemConfigmapRootCA{},
		&bootkube.MachineConfigServerTLSSecret{},
		&bootkube.OpenshiftConfigSecretPullSecret{},
		&bootkube.OpenshiftMachineConfigOperator{},
		&bootkube.KubevirtInfraNamespace{},
		&bootkube.AROWorkerRegistries{},
		&bootkube.AROIngressService{},
		&bootkube.AROImageRegistry{},
	} {
		dependencies.Get(a)
		for _, f := range a.Files() {
			files = append(files, &asset.File{
				Filename: filepath.Join(manifestDir, strings.TrimSuffix(filepath.Base(f.Filename), ".template")),
				Data:     applyTemplateData(f.Data, templateData),
			})
		}
	}
	return files
}

func applyTemplateData(data []byte, templateData interface{}) []byte {
	template := template.Must(template.New("template").Funcs(customTmplFuncs).Parse(string(data)))
	buf := &bytes.Buffer{}
	if err := template.Execute(buf, templateData); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// Load returns the manifests asset from disk.
func (m *Manifests) Load(f asset.FileFetcher) (bool, error) {
	yamlFileList, err := f.FetchByPattern(filepath.Join(manifestDir, "*.yaml"))
	if err != nil {
		return false, errors.Wrap(err, "failed to load *.yaml files")
	}
	ymlFileList, err := f.FetchByPattern(filepath.Join(manifestDir, "*.yml"))
	if err != nil {
		return false, errors.Wrap(err, "failed to load *.yml files")
	}
	jsonFileList, err := f.FetchByPattern(filepath.Join(manifestDir, "*.json"))
	if err != nil {
		return false, errors.Wrap(err, "failed to load *.json files")
	}
	fileList := append(yamlFileList, ymlFileList...)
	fileList = append(fileList, jsonFileList...)

	if len(fileList) == 0 {
		return false, nil
	}

	kubeSysConfig := &configurationObject{}
	var found bool
	for _, file := range fileList {
		if file.Filename == kubeSysConfigPath {
			if err := yaml.Unmarshal(file.Data, kubeSysConfig); err != nil {
				return false, errors.Wrapf(err, "failed to unmarshal %s", kubeSysConfigPath)
			}
			found = true
		}
	}

	if !found {
		return false, nil

	}

	m.FileList, m.KubeSysConfig = fileList, kubeSysConfig

	asset.SortFiles(m.FileList)

	return true, nil
}

func redactedInstallConfig(config types.InstallConfig) ([]byte, error) {
	config.PullSecret = ""
	if config.Platform.VSphere != nil {
		p := *config.Platform.VSphere
		p.Username = ""
		p.Password = ""
		config.Platform.VSphere = &p
	}
	return yaml.Marshal(config)
}

func indent(indention int, v string) string {
	newline := "\n" + strings.Repeat(" ", indention)
	return strings.Replace(v, "\n", newline, -1)
}

func aroWorkerRegistries(icss []types.ImageContentSource) string {
	b := &bytes.Buffer{}

	fmt.Fprintf(b, "unqualified-search-registries = [\"registry.access.redhat.com\", \"docker.io\"]\n")

	for _, ics := range icss {
		fmt.Fprintf(b, "\n[[registry]]\n  prefix = \"\"\n  location = \"%s\"\n  mirror-by-digest-only = true\n", ics.Source)

		for _, mirror := range ics.Mirrors {
			fmt.Fprintf(b, "\n  [[registry.mirror]]\n    location = \"%s\"\n", mirror)
		}
	}

	du := dataurl.New(b.Bytes(), "text/plain")
	du.Encoding = dataurl.EncodingASCII

	return du.String()
}
