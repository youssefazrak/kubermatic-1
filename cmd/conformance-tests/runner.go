/*
Copyright 2020 The Kubermatic Kubernetes Platform contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"time"

	"github.com/go-openapi/runtime"
	"github.com/onsi/ginkgo/reporters"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	clusterv1alpha1 "github.com/kubermatic/machine-controller/pkg/apis/cluster/v1alpha1"
	kubermaticapiv1 "k8c.io/kubermatic/v2/pkg/api/v1"
	clusterclient "k8c.io/kubermatic/v2/pkg/cluster/client"
	kubermaticv1 "k8c.io/kubermatic/v2/pkg/crd/kubermatic/v1"
	kubermaticv1helper "k8c.io/kubermatic/v2/pkg/crd/kubermatic/v1/helper"
	"k8c.io/kubermatic/v2/pkg/provider"
	"k8c.io/kubermatic/v2/pkg/resources"
	"k8c.io/kubermatic/v2/pkg/test/e2e/api/utils"
	apiclient "k8c.io/kubermatic/v2/pkg/test/e2e/api/utils/apiclient/client"
	projectclient "k8c.io/kubermatic/v2/pkg/test/e2e/api/utils/apiclient/client/project"
	apimodels "k8c.io/kubermatic/v2/pkg/test/e2e/api/utils/apiclient/models"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilerror "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func podIsReady(p *corev1.Pod) bool {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

type testScenario interface {
	Name() string
	Cluster(secrets secrets) *apimodels.CreateClusterSpec
	NodeDeployments(num int, secrets secrets) ([]apimodels.NodeDeployment, error)
	OS() apimodels.OperatingSystemSpec
}

func newRunner(scenarios []testScenario, opts *Opts, log *zap.SugaredLogger) *testRunner {
	return &testRunner{
		ctx:                          context.Background(),
		scenarios:                    scenarios,
		controlPlaneReadyWaitTimeout: opts.controlPlaneReadyWaitTimeout,
		nodeReadyTimeout:             opts.nodeReadyTimeout,
		customTestTimeout:            opts.customTestTimeout,
		userClusterPollInterval:      opts.userClusterPollInterval,
		deleteClusterAfterTests:      opts.deleteClusterAfterTests,
		secrets:                      opts.secrets,
		namePrefix:                   opts.namePrefix,
		clusterClientProvider:        opts.clusterClientProvider,
		seed:                         opts.seed,
		seedRestConfig:               opts.seedRestConfig,
		nodeCount:                    opts.nodeCount,
		repoRoot:                     opts.repoRoot,
		reportsRoot:                  opts.reportsRoot,
		clusterParallelCount:         opts.clusterParallelCount,
		PublicKeys:                   opts.publicKeys,
		workerName:                   opts.workerName,
		homeDir:                      opts.homeDir,
		seedClusterClient:            opts.seedClusterClient,
		seedGeneratedClient:          opts.seedGeneratedClient,
		log:                          log,
		existingClusterLabel:         opts.existingClusterLabel,
		openshift:                    opts.openshift,
		openshiftPullSecret:          opts.openshiftPullSecret,
		printGinkoLogs:               opts.printGinkoLogs,
		printContainerLogs:           opts.printContainerLogs,
		onlyTestCreation:             opts.onlyTestCreation,
		pspEnabled:                   opts.pspEnabled,
		kubermaticProjectID:          opts.kubermaticProjectID,
		kubermaticClient:             opts.kubermaticClient,
		kubermaticAuthenticator:      opts.kubermaticAuthenticator,
	}
}

type testRunner struct {
	ctx                 context.Context
	scenarios           []testScenario
	secrets             secrets
	namePrefix          string
	repoRoot            string
	reportsRoot         string
	PublicKeys          [][]byte
	workerName          string
	homeDir             string
	log                 *zap.SugaredLogger
	openshift           bool
	openshiftPullSecret string
	printGinkoLogs      bool
	printContainerLogs  bool
	onlyTestCreation    bool
	pspEnabled          bool

	controlPlaneReadyWaitTimeout time.Duration
	nodeReadyTimeout             time.Duration
	customTestTimeout            time.Duration
	userClusterPollInterval      time.Duration
	deleteClusterAfterTests      bool
	nodeCount                    int
	clusterParallelCount         int

	seedClusterClient     ctrlruntimeclient.Client
	seedGeneratedClient   kubernetes.Interface
	clusterClientProvider *clusterclient.Provider
	seed                  *kubermaticv1.Seed
	seedRestConfig        *rest.Config

	// The label to use to select an existing cluster to test against instead of
	// creating a new one
	existingClusterLabel string

	kubermaticProjectID     string
	kubermaticClient        *apiclient.KubermaticAPI
	kubermaticAuthenticator runtime.ClientAuthInfoWriter
}

type testResult struct {
	report   *reporters.JUnitTestSuite
	err      error
	scenario testScenario
}

func (t *testResult) Passed() bool {
	if t.err != nil {
		return false
	}

	if t.report == nil {
		return false
	}

	if len(t.report.TestCases) == 0 {
		return false
	}

	if t.report.Errors > 0 || t.report.Failures > 0 {
		return false
	}

	return true
}

func (r *testRunner) worker(scenarios <-chan testScenario, results chan<- testResult) {
	for s := range scenarios {
		var report *reporters.JUnitTestSuite

		scenarioLog := r.log.With("scenario", s.Name())
		scenarioLog.Info("Starting to test scenario...")

		err := measureTime(scenarioRuntimeMetric.With(prometheus.Labels{"scenario": s.Name()}), scenarioLog, func() error {
			var err error
			report, err = r.executeScenario(scenarioLog, s)
			return err
		})
		if err != nil {
			scenarioLog.Warnw("Finished with error", zap.Error(err))
		} else {
			scenarioLog.Info("Finished")
		}

		results <- testResult{
			report:   report,
			scenario: s,
			err:      err,
		}
	}
}

func (r *testRunner) Run() error {
	scenariosCh := make(chan testScenario, len(r.scenarios))
	resultsCh := make(chan testResult, len(r.scenarios))

	r.log.Info("Test suite:")
	for _, scenario := range r.scenarios {
		r.log.Info(scenario.Name())
		scenariosCh <- scenario
	}
	r.log.Infof("Total: %d tests", len(r.scenarios))

	for i := 1; i <= r.clusterParallelCount; i++ {
		go r.worker(scenariosCh, resultsCh)
	}

	close(scenariosCh)

	var results []testResult
	for range r.scenarios {
		results = append(results, <-resultsCh)
		r.log.Infof("Finished %d/%d test cases", len(results), len(r.scenarios))
	}

	overallResultBuf := &bytes.Buffer{}
	hadFailure := false
	for _, result := range results {
		prefix := "PASS"
		if !result.Passed() {
			prefix = "FAIL"
			hadFailure = true
		}
		scenarioResultMsg := fmt.Sprintf("[%s] - %s", prefix, result.scenario.Name())
		if result.err != nil {
			scenarioResultMsg = fmt.Sprintf("%s : %v", scenarioResultMsg, result.err)
		}

		fmt.Fprintln(overallResultBuf, scenarioResultMsg)
		if result.report != nil {
			printDetailedReport(result.report)
		}
	}

	fmt.Println("========================== RESULT ===========================")
	fmt.Println(overallResultBuf.String())

	if hadFailure {
		return errors.New("some tests failed")
	}

	return nil
}

func (r *testRunner) executeScenario(log *zap.SugaredLogger, scenario testScenario) (*reporters.JUnitTestSuite, error) {
	var err error
	var cluster *kubermaticv1.Cluster

	report := &reporters.JUnitTestSuite{
		Name: scenario.Name(),
	}
	totalStart := time.Now()

	// We'll store the report there and all kinds of logs
	scenarioFolder := path.Join(r.reportsRoot, scenario.Name())
	if err := os.MkdirAll(scenarioFolder, os.ModePerm); err != nil {
		return nil, fmt.Errorf("failed to create the scenario folder '%s': %v", scenarioFolder, err)
	}

	// We need the closure to defer the evaluation of the time.Since(totalStart) call
	defer func() {
		log.Infof("Finished testing cluster after %s", time.Since(totalStart))
	}()

	// Always write junit to disk
	defer func() {
		report.Time = time.Since(totalStart).Seconds()
		b, err := xml.Marshal(report)
		if err != nil {
			log.Errorw("failed to marshal junit", zap.Error(err))
			return
		}
		if err := ioutil.WriteFile(path.Join(r.reportsRoot, fmt.Sprintf("junit.%s.xml", scenario.Name())), b, 0644); err != nil {
			log.Errorw("Failed to write junit", zap.Error(err))
		}
	}()

	ctx := context.Background()
	if r.existingClusterLabel == "" {
		if err := junitReporterWrapper(
			"[Kubermatic] Create cluster",
			report,
			func() error {
				cluster, err = r.createCluster(log, scenario)
				return err
			}); err != nil {
			return report, fmt.Errorf("failed to create cluster: %v", err)
		}
	} else {
		log.Info("Using existing cluster")
		selector, err := labels.Parse(r.existingClusterLabel)
		if err != nil {
			return nil, fmt.Errorf("failed to parse labelselector %q: %v", r.existingClusterLabel, err)
		}
		clusterList := &kubermaticv1.ClusterList{}
		listOptions := &ctrlruntimeclient.ListOptions{LabelSelector: selector}
		if err := r.seedClusterClient.List(ctx, clusterList, listOptions); err != nil {
			return nil, fmt.Errorf("failed to list clusters: %v", err)
		}
		if foundClusterNum := len(clusterList.Items); foundClusterNum != 1 {
			return nil, fmt.Errorf("expected to find exactly one existing cluster, but got %d", foundClusterNum)
		}
		cluster = &clusterList.Items[0]
	}
	clusterName := cluster.Name
	log = log.With("cluster", cluster.Name)

	if err := junitReporterWrapper(
		"[Kubermatic] Wait for successful reconciliation",
		report,
		timeMeasurementWrapper(
			kubermaticReconciliationDurationMetric.With(prometheus.Labels{"scenario": scenario.Name()}),
			log,
			func() error {
				return wait.Poll(5*time.Second, 5*time.Minute, func() (bool, error) {
					if err := r.seedClusterClient.Get(ctx, types.NamespacedName{Name: clusterName}, cluster); err != nil {
						log.Errorw("Failed to get cluster when waiting for successful reconciliation", zap.Error(err))
						return false, nil
					}

					// ignore Kubermatic version in this check, to allow running against a 3rd party setup
					missingConditions, success := kubermaticv1helper.ClusterReconciliationSuccessful(cluster, true)
					if len(missingConditions) > 0 {
						log.Infof("Waiting for the following conditions: %v", missingConditions)
					}
					return success, nil
				})
			},
		),
	); err != nil {
		return report, fmt.Errorf("failed to wait for successful reconciliation: %v", err)
	}

	if err := r.executeTests(log, cluster, report, scenario); err != nil {
		return report, err
	}


	return nil,nil
}

func (r *testRunner) executeTests(
	log *zap.SugaredLogger,
	cluster *kubermaticv1.Cluster,
	report *reporters.JUnitTestSuite,
	scenario testScenario,
) error {

	// We must store the name here because the cluster object may be nil on error
	clusterName := cluster.Name

	if r.printContainerLogs {
		// Print all controlplane logs to both make debugging easier and show issues
		// that didn't result in test failures.
		defer r.printAllControlPlaneLogs(log, clusterName)
	}

	var err error

	if err := junitReporterWrapper(
		"[Kubermatic] Wait for control plane",
		report,
		timeMeasurementWrapper(
			seedControlplaneDurationMetric.With(prometheus.Labels{"scenario": scenario.Name()}),
			log,
			func() error {
				cluster, err = r.waitForControlPlane(log, clusterName)
				return err
			},
		),
	); err != nil {
		return fmt.Errorf("failed waiting for control plane to become ready: %v", err)
	}

	if err := junitReporterWrapper(
		"[Kubermatic] Add LB and PV Finalizers",
		report,
		func() error {
			return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				if err := r.seedClusterClient.Get(context.Background(), types.NamespacedName{Name: clusterName}, cluster); err != nil {
					return err
				}
				cluster.Finalizers = append(cluster.Finalizers,
					kubermaticapiv1.InClusterPVCleanupFinalizer,
					kubermaticapiv1.InClusterLBCleanupFinalizer,
				)
				return r.seedClusterClient.Update(context.Background(), cluster)
			})
		},
	); err != nil {
		return fmt.Errorf("failed to add PV and LB cleanup finalizers: %v", err)
	}

	providerName, err := provider.ClusterCloudProviderName(cluster.Spec.Cloud)
	if err != nil {
		return fmt.Errorf("failed to get cloud provider name from cluster: %v", err)
	}

	log = log.With("cloud-provider", providerName)

	_, exists := r.seed.Spec.Datacenters[cluster.Spec.Cloud.DatacenterName]
	if !exists {
		return fmt.Errorf("datacenter %q doesn't exist", cluster.Spec.Cloud.DatacenterName)
	}

	userClusterClient, err := r.clusterClientProvider.GetClient(cluster)
	if err != nil {
		return fmt.Errorf("failed to get the client for the cluster: %v", err)
	}

	if err := junitReporterWrapper(
		"[Kubermatic] Create NodeDeployments",
		report,
		func() error {
			return r.createNodeDeployments(log, scenario, clusterName)
		},
	); err != nil {
		return fmt.Errorf("failed to setup nodes: %v", err)
	}

	if r.printContainerLogs {
		defer logEventsForAllMachines(context.Background(), log, userClusterClient)
		defer logUserClusterPodEventsAndLogs(
			log,
			r.clusterClientProvider,
			cluster.DeepCopy(),
		)
	}

	overallTimeout := r.nodeReadyTimeout
	if cluster.IsOpenshift() {
		// Openshift installs a lot more during node provisioning, hence this may take longer
		overallTimeout += 5 * time.Minute
	}
	// The initialization of the external CCM is super slow
	if cluster.Spec.Features[kubermaticv1.ClusterFeatureExternalCloudProvider] {
		overallTimeout += 5 * time.Minute
	}
	// Packet is slower at provisioning the instances, presumably because those are actual
	// physical hosts.
	if cluster.Spec.Cloud.Packet != nil {
		overallTimeout += 5 * time.Minute
	}

	var timeoutRemaining time.Duration

	if err := junitReporterWrapper(
		"[Kubermatic] Wait for machines to get a node",
		report,
		timeMeasurementWrapper(
			nodeCreationDuration.With(prometheus.Labels{"scenario": scenario.Name()}),
			log,
			func() error {
				var err error
				timeoutRemaining, err = waitForMachinesToJoinCluster(log, userClusterClient, overallTimeout)
				return err
			},
		),
	); err != nil {
		return fmt.Errorf("failed to wait for machines to get a node: %v", err)
	}

	if err := junitReporterWrapper(
		"[Kubermatic] Wait for nodes to be ready",
		report,
		timeMeasurementWrapper(
			nodeRadinessDuration.With(prometheus.Labels{"scenario": scenario.Name()}),
			log,
			func() error {
				// Getting ready just implies starting the CNI deamonset, so that should
				// be quick.
				var err error
				timeoutRemaining, err = waitForNodesToBeReady(log, userClusterClient, timeoutRemaining)
				return err
			},
		),
	); err != nil {
		return fmt.Errorf("failed to wait for all nodes to be ready: %v", err)
	}

	if err := junitReporterWrapper(
		"[Kubermatic] Wait for Pods inside usercluster to be ready",
		report,
		timeMeasurementWrapper(
			seedControlplaneDurationMetric.With(prometheus.Labels{"scenario": scenario.Name()}),
			log,
			func() error {
				return r.waitUntilAllPodsAreReady(log, userClusterClient, timeoutRemaining)
			},
		),
	); err != nil {
		return fmt.Errorf("failed to wait for all pods to get ready: %v", err)
	}

	if r.onlyTestCreation {
		return nil
	}

	return nil
}

func (r *testRunner) deleteCluster(report *reporters.JUnitTestSuite, cluster *kubermaticv1.Cluster, log *zap.SugaredLogger) error {
	deleteTimeout := 15 * time.Minute
	if cluster.Spec.Cloud.Azure != nil {
		// 15 Minutes are not enough for Azure
		deleteTimeout = 30 * time.Minute
	}

	if err := junitReporterWrapper(
		"[Kubermatic] Delete cluster",
		report,
		func() error {
			var selector labels.Selector
			var err error
			if r.workerName != "" {
				selector, err = labels.Parse(fmt.Sprintf("worker-name=%s", r.workerName))
				if err != nil {
					return fmt.Errorf("failed to parse selector: %v", err)
				}
			}
			return wait.PollImmediate(5*time.Second, deleteTimeout, func() (bool, error) {
				clusterList := &kubermaticv1.ClusterList{}
				listOpts := &ctrlruntimeclient.ListOptions{LabelSelector: selector}
				if err := r.seedClusterClient.List(r.ctx, clusterList, listOpts); err != nil {
					log.Errorw("Listing clusters failed", zap.Error(err))
					return false, nil
				}

				// Success!
				if len(clusterList.Items) == 0 {
					return true, nil
				}

				// Should never happen
				if len(clusterList.Items) > 1 {
					return false, fmt.Errorf("expected to find zero or one cluster, got %d", len(clusterList.Items))
				}

				// Cluster is currently being deleted
				if clusterList.Items[0].DeletionTimestamp != nil {
					return false, nil
				}

				// Issue Delete call
				log.With("cluster", clusterList.Items[0].Name).Info("Deleting user cluster now...")

				deleteParms := &projectclient.DeleteClusterParams{
					ProjectID: r.kubermaticProjectID,
					ClusterID: clusterList.Items[0].Name,
					DC:        r.seed.Name,
				}
				utils.SetupParams(nil, deleteParms, 3*time.Second, deleteTimeout)

				if _, err := r.kubermaticClient.Project.DeleteCluster(deleteParms, r.kubermaticAuthenticator); err != nil {
					log.Warnw("Failed to delete cluster", zap.Error(err))
				}

				return false, nil
			})
		},
	); err != nil {
		log.Errorw("Failed to delete cluster", zap.Error(err))
		return err
	}

	return nil
}

func (r *testRunner) createNodeDeployments(log *zap.SugaredLogger, scenario testScenario, clusterName string) error {
	nodeDeploymentGetParams := &projectclient.ListNodeDeploymentsParams{
		ProjectID: r.kubermaticProjectID,
		ClusterID: clusterName,
		DC:        r.seed.Name,
	}
	utils.SetupParams(nil, nodeDeploymentGetParams, 5*time.Second, 1*time.Minute)

	log.Info("Getting existing NodeDeployments")
	resp, err := r.kubermaticClient.Project.ListNodeDeployments(nodeDeploymentGetParams, r.kubermaticAuthenticator)
	if err != nil {
		return fmt.Errorf("failed to get existing NodeDeployments: %v", err)
	}

	existingReplicas := 0
	for _, nodeDeployment := range resp.Payload {
		existingReplicas += int(*nodeDeployment.Spec.Replicas)
	}
	log.Infof("Found %d pre-existing node replicas", existingReplicas)

	nodeCount := r.nodeCount - existingReplicas
	if nodeCount < 0 {
		return fmt.Errorf("found %d existing replicas and want %d, scaledown not supported", existingReplicas, r.nodeCount)
	}
	if nodeCount == 0 {
		return nil
	}

	log.Info("Preparing NodeDeployments")
	var nodeDeployments []apimodels.NodeDeployment
	if err := wait.PollImmediate(10*time.Second, time.Minute, func() (bool, error) {
		var err error
		nodeDeployments, err = scenario.NodeDeployments(nodeCount, r.secrets)
		if err != nil {
			log.Warnw("Getting NodeDeployments from scenario failed", zap.Error(err))
			return false, nil
		}
		return true, nil
	}); err != nil {
		return fmt.Errorf("didn't get NodeDeployments from scenario within a minute: %v", err)
	}

	log.Info("Creating NodeDeployments via Kubermatic API")
	for _, nd := range nodeDeployments {
		params := &projectclient.CreateNodeDeploymentParams{
			ProjectID: r.kubermaticProjectID,
			ClusterID: clusterName,
			DC:        r.seed.Name,
			Body:      &nd,
		}
		utils.SetupParams(nil, params, 5*time.Second, 1*time.Minute, http.StatusConflict)

		if _, err := r.kubermaticClient.Project.CreateNodeDeployment(params, r.kubermaticAuthenticator); err != nil {
			return fmt.Errorf("failed to create NodeDeployment %s: %v", nd.Name, err)
		}
	}

	log.Infof("Successfully created %d NodeDeployments via Kubermatic API", nodeCount)
	return nil
}

func (r *testRunner) getKubeconfig(log *zap.SugaredLogger, cluster *kubermaticv1.Cluster) (string, error) {
	log.Debug("Getting kubeconfig...")
	var kubeconfig []byte
	// Needed for Openshift where we have to create a SA and bindings inside the cluster
	// which can only be done after the APIServer is up and ready
	if err := wait.PollImmediate(time.Second, time.Minute, func() (bool, error) {
		var err error
		kubeconfig, err = r.clusterClientProvider.GetAdminKubeconfig(cluster)
		if err != nil {
			log.Debugw("Failed to get Kubeconfig", zap.Error(err))
			return false, nil
		}
		return true, nil
	}); err != nil {
		return "", fmt.Errorf("failed to wait for kubeconfig: %v", err)
	}
	filename := path.Join(r.homeDir, fmt.Sprintf("%s-kubeconfig", cluster.Name))
	if err := ioutil.WriteFile(filename, kubeconfig, 0644); err != nil {
		return "", fmt.Errorf("failed to write kubeconfig to %s: %v", filename, err)
	}

	log.Infof("Successfully wrote kubeconfig to %s", filename)
	return filename, nil
}

func (r *testRunner) getCloudConfig(log *zap.SugaredLogger, cluster *kubermaticv1.Cluster) (string, error) {
	log.Debug("Getting cloud-config...")

	name := types.NamespacedName{Namespace: cluster.Status.NamespaceName, Name: resources.CloudConfigConfigMapName}
	cmData := ""

	if err := wait.PollImmediate(3*time.Second, 5*time.Minute, func() (bool, error) {
		cm := &corev1.ConfigMap{}
		if err := r.seedClusterClient.Get(context.Background(), name, cm); err != nil {
			log.Warnw("Failed to load cloud-config", zap.Error(err))
			return false, nil
		}

		cmData = cm.Data["config"]
		return true, nil
	}); err != nil {
		return "", fmt.Errorf("failed to get ConfigMap %s: %v", name.String(), err)
	}

	filename := path.Join(r.homeDir, fmt.Sprintf("%s-cloud-config", cluster.Name))
	if err := ioutil.WriteFile(filename, []byte(cmData), 0644); err != nil {
		return "", fmt.Errorf("failed to write cloud config: %v", err)
	}

	log.Infof("Successfully wrote cloud-config to %s", filename)
	return filename, nil
}

func (r *testRunner) createCluster(log *zap.SugaredLogger, scenario testScenario) (*kubermaticv1.Cluster, error) {
	log.Info("Creating cluster via Kubermatic API")

	cluster := scenario.Cluster(r.secrets)
	if r.openshift {
		cluster.Cluster.Type = "openshift"
		cluster.Cluster.Spec.Openshift = &apimodels.Openshift{
			ImagePullSecret: r.openshiftPullSecret,
		}
	}

	// The cluster name must be unique per project.
	// We build up a readable name with the various cli parameters & add a random string in the end to ensure
	// we really have a unique name
	if r.namePrefix != "" {
		cluster.Cluster.Name = r.namePrefix + "-"
	}
	if r.workerName != "" {
		cluster.Cluster.Name += r.workerName + "-"
	}
	cluster.Cluster.Name += scenario.Name() + "-"
	cluster.Cluster.Name += rand.String(8)

	cluster.Cluster.Spec.UsePodSecurityPolicyAdmissionPlugin = r.pspEnabled

	params := &projectclient.CreateClusterParams{
		ProjectID: r.kubermaticProjectID,
		DC:        r.seed.Name,
		Body:      cluster,
	}
	utils.SetupParams(nil, params, 3*time.Second, 1*time.Minute, http.StatusConflict)

	response, err := r.kubermaticClient.Project.CreateCluster(params, r.kubermaticAuthenticator)
	if err != nil {
		return nil, err
	}

	clusterID := response.Payload.ID
	crCluster := &kubermaticv1.Cluster{}

	if err := wait.PollImmediate(2*time.Second, 1*time.Minute, func() (bool, error) {
		key := types.NamespacedName{Name: clusterID}

		if err := r.seedClusterClient.Get(r.ctx, key, crCluster); err != nil {
			if kerrors.IsNotFound(err) {
				return false, nil
			}

			return false, err
		}

		return true, nil
	}); err != nil {
		return nil, fmt.Errorf("failed to wait for Cluster to appear: %v", err)
	}

	// fetch all existing SSH keys
	listKeysBody := &projectclient.ListSSHKeysParams{
		ProjectID: r.kubermaticProjectID,
	}
	utils.SetupParams(nil, listKeysBody, 3*time.Second, 1*time.Minute, http.StatusConflict, http.StatusNotFound)

	result, err := r.kubermaticClient.Project.ListSSHKeys(listKeysBody, r.kubermaticAuthenticator)
	if err != nil {
		return nil, fmt.Errorf("failed to list project's SSH keys: %v", err)
	}

	keyIDs := []string{}
	for _, key := range result.Payload {
		keyIDs = append(keyIDs, key.ID)
	}

	// assign all keys to the new cluster
	for _, keyID := range keyIDs {
		assignKeyBody := &projectclient.AssignSSHKeyToClusterParams{
			ProjectID: r.kubermaticProjectID,
			DC:        r.seed.Name,
			ClusterID: crCluster.Name,
			KeyID:     keyID,
		}
		utils.SetupParams(nil, assignKeyBody, 3*time.Second, 1*time.Minute, http.StatusConflict, http.StatusNotFound, http.StatusForbidden)

		if _, err := r.kubermaticClient.Project.AssignSSHKeyToCluster(assignKeyBody, r.kubermaticAuthenticator); err != nil {
			return nil, fmt.Errorf("failed to assign SSH key to cluster: %v", err)
		}
	}

	log.Infof("Successfully created cluster %s", crCluster.Name)
	return crCluster, nil
}

func (r *testRunner) waitForControlPlane(log *zap.SugaredLogger, clusterName string) (*kubermaticv1.Cluster, error) {
	log.Debug("Waiting for control plane to become ready...")
	started := time.Now()
	namespacedClusterName := types.NamespacedName{Name: clusterName}

	err := wait.Poll(3*time.Second, r.controlPlaneReadyWaitTimeout, func() (done bool, err error) {
		newCluster := &kubermaticv1.Cluster{}

		if err := r.seedClusterClient.Get(context.Background(), namespacedClusterName, newCluster); err != nil {
			if kerrors.IsNotFound(err) {
				return false, nil
			}
		}
		// Check for this first, because otherwise we instantly return as the cluster-controller did not
		// create any pods yet
		if !newCluster.Status.ExtendedHealth.AllHealthy() {
			return false, nil
		}

		controlPlanePods := &corev1.PodList{}
		if err := r.seedClusterClient.List(
			context.Background(),
			controlPlanePods,
			&ctrlruntimeclient.ListOptions{Namespace: newCluster.Status.NamespaceName},
		); err != nil {
			return false, fmt.Errorf("failed to list controlplane pods: %v", err)
		}
		for _, pod := range controlPlanePods.Items {
			if !podIsReady(&pod) {
				return false, nil
			}
		}

		return true, nil
	})
	// Timeout or other error
	if err != nil {
		return nil, err
	}

	// Get copy of latest version
	cluster := &kubermaticv1.Cluster{}
	if err := r.seedClusterClient.Get(context.Background(), namespacedClusterName, cluster); err != nil {
		return nil, err
	}

	log.Debugf("Control plane became ready after %.2f seconds", time.Since(started).Seconds())
	return cluster, nil
}

func (r *testRunner) waitUntilAllPodsAreReady(log *zap.SugaredLogger, userClusterClient ctrlruntimeclient.Client, timeout time.Duration) error {
	log.Debug("Waiting for all pods to be ready...")
	started := time.Now()

	err := wait.Poll(r.userClusterPollInterval, timeout, func() (done bool, err error) {
		podList := &corev1.PodList{}
		if err := userClusterClient.List(context.Background(), podList); err != nil {
			log.Warnw("Failed to load pod list while waiting until all pods are running", zap.Error(err))
			return false, nil
		}

		for _, pod := range podList.Items {
			if !podIsReady(&pod) {
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		return err
	}

	log.Debugf("All pods became ready after %.2f seconds", time.Since(started).Seconds())
	return nil
}

func (r *testRunner) printAllControlPlaneLogs(log *zap.SugaredLogger, clusterName string) {
	log.Info("Printing control plane logs")
	cluster := &kubermaticv1.Cluster{}
	ctx := context.Background()
	if err := r.seedClusterClient.Get(ctx, types.NamespacedName{Name: clusterName}, cluster); err != nil {
		log.Errorw("Failed to get cluster", zap.Error(err))
		return
	}

	log.Debugw("Cluster health status", "status", cluster.Status.ExtendedHealth)

	log.Info("Logging events for cluster")
	if err := logEventsObject(ctx, log, r.seedClusterClient, "default", cluster.UID); err != nil {
		log.Errorw("Failed to log cluster events", zap.Error(err))
	}

	if err := printEventsAndLogsForAllPods(
		ctx,
		log,
		r.seedClusterClient,
		r.seedGeneratedClient,
		cluster.Status.NamespaceName,
	); err != nil {
		log.Errorw("Failed to print events and logs of pods", zap.Error(err))
	}
}

// waitForMachinesToJoinCluster waits for machines to join the cluster. It does so by checking
// if the machines have a nodeRef. It does not check if the nodeRef is valid.
// All errors are swallowed, only the timeout error is returned.
func waitForMachinesToJoinCluster(log *zap.SugaredLogger, client ctrlruntimeclient.Client, timeout time.Duration) (time.Duration, error) {
	startTime := time.Now()
	err := wait.Poll(10*time.Second, timeout, func() (bool, error) {
		machineList := &clusterv1alpha1.MachineList{}
		if err := client.List(context.Background(), machineList); err != nil {
			log.Warnw("Failed to list machines", zap.Error(err))
			return false, nil
		}
		for _, machine := range machineList.Items {
			if !machineHasNodeRef(machine) {
				log.Infow("Machine has no nodeRef yet", "machine", machine.Name)
				return false, nil
			}
		}
		log.Infow("All machines got a Node", "duration-in-seconds", time.Since(startTime).Seconds())
		return true, nil
	})
	return timeout - time.Since(startTime), err
}

func machineHasNodeRef(machine clusterv1alpha1.Machine) bool {
	return machine.Status.NodeRef != nil && machine.Status.NodeRef.Name != ""
}

// WaitForNodesToBeReady waits for all nodes to be ready. It does so by checking the Nodes "Ready"
// condition. It swallows all errors except for the timeout.
func waitForNodesToBeReady(log *zap.SugaredLogger, client ctrlruntimeclient.Client, timeout time.Duration) (time.Duration, error) {
	startTime := time.Now()
	err := wait.Poll(10*time.Second, timeout, func() (bool, error) {
		nodeList := &corev1.NodeList{}
		if err := client.List(context.Background(), nodeList); err != nil {
			log.Warnw("Failed to list nodes", zap.Error(err))
			return false, nil
		}
		for _, node := range nodeList.Items {
			if !nodeIsReady(node) {
				log.Infow("Node is not ready", "node", node.Name)
				return false, nil
			}
		}
		log.Infow("All nodes got ready", "duration-in-seconds", time.Since(startTime).Seconds())
		return true, nil
	})
	return timeout - time.Since(startTime), err
}

func nodeIsReady(node corev1.Node) bool {
	for _, c := range node.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// printUnbuffered uses io.Copy to print data to stdout.
// It should be used for all bigger logs, to avoid buffering
// them in memory and getting oom killed because of that.
func printUnbuffered(src io.Reader) error {
	_, err := io.Copy(os.Stdout, src)
	return err
}

// junitReporterWrapper is a convenience func to get junit results for a step
// It will create a report, append it to the passed in testsuite and propagate
// the error of the executor back up
// TODO: Should we add optional retrying here to limit the amount of wrappers we need?
func junitReporterWrapper(
	testCaseName string,
	report *reporters.JUnitTestSuite,
	executor func() error,
	extraErrOutputFn ...func() string,
) error {
	junitTestCase := reporters.JUnitTestCase{
		Name:      testCaseName,
		ClassName: testCaseName,
	}

	startTime := time.Now()
	err := executor()
	junitTestCase.Time = time.Since(startTime).Seconds()
	if err != nil {
		junitTestCase.FailureMessage = &reporters.JUnitFailureMessage{Message: err.Error()}
		report.Failures++
		for _, extraOut := range extraErrOutputFn {
			extraOutString := extraOut()
			err = fmt.Errorf("%v\n%s", err, extraOutString)
			junitTestCase.FailureMessage.Message += "\n" + extraOutString
		}
	}

	report.TestCases = append(report.TestCases, junitTestCase)
	report.Tests++

	return err
}

// printEvents and logs for all pods. Include ready pods, because they may still contain useful information.
func printEventsAndLogsForAllPods(
	ctx context.Context,
	log *zap.SugaredLogger,
	client ctrlruntimeclient.Client,
	k8sclient kubernetes.Interface,
	namespace string,
) error {
	log.Infow("Printing logs for all pods", "namespace", namespace)

	pods := &corev1.PodList{}
	if err := client.List(ctx, pods, ctrlruntimeclient.InNamespace(namespace)); err != nil {
		return fmt.Errorf("failed to list pods: %v", err)
	}

	var errs []error
	for _, pod := range pods.Items {
		log := log.With("pod", pod.Name)
		if !podIsReady(&pod) {
			log.Error("Pod is not ready")
		}
		log.Info("Logging events for pod")
		if err := logEventsObject(ctx, log, client, pod.Namespace, pod.UID); err != nil {
			log.Errorw("Failed to log events for pod", zap.Error(err))
			errs = append(errs, err)
		}
		log.Info("Printing logs for pod")
		if err := printLogsForPod(log, k8sclient, &pod); err != nil {
			log.Errorw("Failed to print logs for pod", zap.Error(utilerror.NewAggregate(err)))
			errs = append(errs, err...)
		}
	}

	return utilerror.NewAggregate(errs)
}

func printLogsForPod(log *zap.SugaredLogger, k8sclient kubernetes.Interface, pod *corev1.Pod) []error {
	var errs []error
	for _, container := range pod.Spec.Containers {
		containerLog := log.With("container", container.Name)
		containerLog.Info("Printing logs for container")
		if err := printLogsForContainer(k8sclient, pod, container.Name); err != nil {
			containerLog.Errorw("Failed to print logs for container", zap.Error(err))
			errs = append(errs, err)
		}
	}
	for _, initContainer := range pod.Spec.InitContainers {
		containerLog := log.With("initContainer", initContainer.Name)
		containerLog.Infow("Printing logs for initContainer")
		if err := printLogsForContainer(k8sclient, pod, initContainer.Name); err != nil {
			containerLog.Errorw("Failed to print logs for initContainer", zap.Error(err))
			errs = append(errs, err)
		}
	}
	return errs
}

func printLogsForContainer(client kubernetes.Interface, pod *corev1.Pod, containerName string) error {
	readCloser, err := client.
		CoreV1().
		Pods(pod.Namespace).
		GetLogs(pod.Name, &corev1.PodLogOptions{Container: containerName}).
		Stream(context.Background())
	if err != nil {
		return err
	}
	defer readCloser.Close()
	return printUnbuffered(readCloser)
}

func logEventsForAllMachines(
	ctx context.Context,
	log *zap.SugaredLogger,
	client ctrlruntimeclient.Client,
) {
	machines := &clusterv1alpha1.MachineList{}
	if err := client.List(ctx, machines); err != nil {
		log.Errorw("Failed to list machines", zap.Error(err))
		return
	}

	for _, machine := range machines.Items {
		machineLog := log.With("name", machine.Name)
		machineLog.Infow("Logging events for machine")
		if err := logEventsObject(ctx, log, client, machine.Namespace, machine.UID); err != nil {
			machineLog.Errorw("Failed to log events for machine", "namespace", machine.Namespace, zap.Error(err))
		}
	}
}

func logEventsObject(
	ctx context.Context,
	log *zap.SugaredLogger,
	client ctrlruntimeclient.Client,
	namespace string,
	uid types.UID,
) error {
	events := &corev1.EventList{}
	listOpts := &ctrlruntimeclient.ListOptions{
		Namespace:     namespace,
		FieldSelector: fields.OneTermEqualSelector("involvedObject.uid", string(uid)),
	}
	if err := client.List(ctx, events, listOpts); err != nil {
		return fmt.Errorf("failed to get events: %v", err)
	}

	for _, event := range events.Items {
		var msg string
		if event.Type == corev1.EventTypeWarning {
			// Make sure this gets highlighted
			msg = "ERROR"
		}
		log.Infow(
			msg,
			"EventType", event.Type,
			"Number", event.Count,
			"Reason", event.Reason,
			"Message", event.Message,
			"Source", event.Source.Component,
		)
	}
	return nil
}

func logUserClusterPodEventsAndLogs(
	log *zap.SugaredLogger,
	connProvider *clusterclient.Provider,
	cluster *kubermaticv1.Cluster,
) {
	log.Info("Attempting to log usercluster pod events and logs")
	cfg, err := connProvider.GetClientConfig(cluster)
	if err != nil {
		log.Errorw("Failed to get usercluster admin kubeconfig", zap.Error(err))
		return
	}
	k8sClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		log.Errorw("Failed to construct k8sClient for usercluster", zap.Error(err))
		return
	}
	client, err := connProvider.GetClient(cluster)
	if err != nil {
		log.Errorw("Failed to construct client for usercluster", zap.Error(err))
		return
	}
	if err := printEventsAndLogsForAllPods(
		context.Background(),
		log,
		client,
		k8sClient,
		"",
	); err != nil {
		log.Errorw("Failed to print events and logs for usercluster pods", zap.Error(err))
	}
}
