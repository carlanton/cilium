// Copyright 2017 Authors of Cilium
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package k8sTest

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/asaskevich/govalidator"
	. "github.com/cilium/cilium/test/ginkgo-ext"
	"github.com/cilium/cilium/test/helpers"

	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"
)

var _ = Describe("K8sValidatedServicesTest", func() {

	var kubectl *helpers.Kubectl
	var logger *logrus.Entry
	var once sync.Once
	var serviceName string = "app1-service"
	var microscopeErr error
	var microscopeCancel func() error

	applyPolicy := func(path string) {
		_, err := kubectl.CiliumPolicyAction(helpers.KubeSystemNamespace, path, helpers.KubectlApply, helpers.HelperTimeout)
		Expect(err).Should(BeNil(), fmt.Sprintf("Error creating resource %s: %s", path, err))
	}

	initialize := func() {
		logger = log.WithFields(logrus.Fields{"testName": "K8sServiceTest"})
		logger.Info("Starting")

		kubectl = helpers.CreateKubectl(helpers.K8s1VMName(), logger)
		path := kubectl.ManifestGet("cilium_ds.yaml")
		kubectl.Apply(path)
		_, err := kubectl.WaitforPods(helpers.KubeSystemNamespace, "-l k8s-app=cilium", 600)
		Expect(err).Should(BeNil())

		err = kubectl.WaitKubeDNS()
		Expect(err).Should(BeNil())
	}

	BeforeEach(func() {
		once.Do(initialize)
	})

	AfterFailed(func() {
		kubectl.CiliumReport(helpers.KubeSystemNamespace, []string{
			"cilium service list",
			"cilium policy get",
			"cilium endpoint list"})
	})

	JustBeforeEach(func() {
		microscopeErr, microscopeCancel = kubectl.MicroscopeStart()
		Expect(microscopeErr).To(BeNil(), "Microscope cannot be started")
	})

	JustAfterEach(func() {
		kubectl.ValidateNoErrorsOnLogs(CurrentGinkgoTestDescription().Duration)
		Expect(microscopeCancel()).To(BeNil(), "cannot stop microscope")
	})

	AfterEach(func() {
		err := kubectl.WaitCleanAllTerminatingPods()
		Expect(err).To(BeNil(), "Terminating containers are not deleted after timeout")
	})

	testHTTPRequest := func(url string) {
		output, err := kubectl.GetPods(helpers.DefaultNamespace, "-l zgroup=testDSClient").Filter("{.items[*].metadata.name}")
		ExpectWithOffset(1, err).Should(BeNil())
		pods := strings.Split(output.String(), " ")
		// A DS with client is running in each node. So we try from each node
		// that can connect to the service.  To make sure that the cross-node
		// service connectivity is correct we tried 10 times, so balance in the
		// two nodes
		for _, pod := range pods {
			for i := 1; i <= 10; i++ {
				res := kubectl.ExecPodCmd(
					helpers.DefaultNamespace, pod,
					helpers.CurlFail(url))
				ExpectWithOffset(1, res.WasSuccessful()).Should(BeTrue(),
					"Pod %q can not connect to service %q", pod, url)
			}
		}
	}

	waitPodsDs := func() {
		groups := []string{"zgroup=testDS", "zgroup=testDSClient"}
		for _, pod := range groups {
			pods, err := kubectl.WaitforPods(helpers.DefaultNamespace, fmt.Sprintf("-l %s", pod), 300)
			ExpectWithOffset(1, pods).Should(BeTrue())
			ExpectWithOffset(1, err).Should(BeNil())
		}
	}

	It("Check Service", func() {
		demoDSPath := kubectl.ManifestGet("demo.yaml")
		kubectl.Apply(demoDSPath)
		defer kubectl.Delete(demoDSPath)

		pods, err := kubectl.WaitforPods(helpers.DefaultNamespace, "-l zgroup=testapp", 300)
		Expect(pods).Should(BeTrue())
		Expect(err).Should(BeNil())

		svcIP, err := kubectl.Get(
			helpers.DefaultNamespace, fmt.Sprintf("service %s", serviceName)).Filter("{.spec.clusterIP}")
		Expect(err).Should(BeNil())
		Expect(govalidator.IsIP(svcIP.String())).Should(BeTrue())

		status := kubectl.Exec(fmt.Sprintf("curl http://%s/", svcIP))
		status.ExpectSuccess()

		ciliumPod, err := kubectl.GetCiliumPodOnNode(helpers.KubeSystemNamespace, helpers.K8s1)
		Expect(err).Should(BeNil())

		service := kubectl.CiliumExec(ciliumPod, "cilium service list")
		Expect(service.Output()).Should(ContainSubstring(svcIP.String()))
		service.ExpectSuccess()

	}, 300)

	It("Check Service with cross-node", func() {
		demoDSPath := kubectl.ManifestGet("demo_ds.yaml")
		kubectl.Apply(demoDSPath)
		defer kubectl.Delete(demoDSPath)

		waitPodsDs()

		svcIP, err := kubectl.Get(
			helpers.DefaultNamespace, "service testds-service").Filter("{.spec.clusterIP}")
		Expect(err).Should(BeNil())
		log.Debugf("svcIP: %s", svcIP.String())
		Expect(govalidator.IsIP(svcIP.String())).Should(BeTrue())

		url := fmt.Sprintf("http://%s/", svcIP)
		testHTTPRequest(url)
	})

	//TODO: Check service with IPV6

	It("Check NodePort", func() {
		demoDSPath := kubectl.ManifestGet("demo_ds.yaml")
		kubectl.Apply(demoDSPath)
		defer kubectl.Delete(demoDSPath)

		waitPodsDs()

		var data v1.Service
		err := kubectl.Get("default", "service test-nodeport").Unmarshal(&data)
		Expect(err).Should(BeNil(), "Can not retrieve service")
		url := fmt.Sprintf("http://%s",
			net.JoinHostPort(data.Spec.ClusterIP, fmt.Sprintf("%d", data.Spec.Ports[0].Port)))
		testHTTPRequest(url)

		url = fmt.Sprintf("http://%s",
			net.JoinHostPort(helpers.K8s1Ip, fmt.Sprintf("%d", data.Spec.Ports[0].NodePort)))
		testHTTPRequest(url)

		url = fmt.Sprintf("http://%s",
			net.JoinHostPort(helpers.K8s2Ip, fmt.Sprintf("%d", data.Spec.Ports[0].NodePort)))
		testHTTPRequest(url)
	})

	Context("External services", func() {

		var endpointPath string
		var expectedCIDR string = "198.49.23.144/32"
		var podName string = "toservices"
		var podPath string
		var policyPath string
		var policyLabeledPath string
		var servicePath string

		BeforeEach(func() {
			servicePath = kubectl.ManifestGet("external_service.yaml")
			res := kubectl.Apply(servicePath)
			res.ExpectSuccess(res.GetDebugMessage())

			endpointPath = kubectl.ManifestGet("external_endpoint.yaml")
			podPath = kubectl.ManifestGet("external_pod.yaml")
			policyPath = kubectl.ManifestGet("external_policy.yaml")
			policyLabeledPath = kubectl.ManifestGet("external_policy_labeled.yaml")

			res = kubectl.Apply(podPath)
			res.ExpectSuccess()
		})

		AfterEach(func() {
			res := kubectl.Delete(endpointPath)
			res.ExpectSuccess()

			res = kubectl.Delete(servicePath)
			res.ExpectSuccess()

			res = kubectl.Delete(podPath)
			res.ExpectSuccess()

			waitFinish := func() bool {
				data, err := kubectl.GetPodNames(helpers.DefaultNamespace, "zgroup=external")
				if err != nil {
					return false
				}
				if len(data) == 0 {
					return true
				}
				return false
			}
			err := helpers.WithTimeout(waitFinish, "cannot finish deleting containers",
				&helpers.TimeoutConfig{Timeout: helpers.HelperTimeout})
			Expect(err).To(BeNil())

		})

		validateEgress := func() {
			kubectl.WaitforPods(helpers.DefaultNamespace, "", 300)

			pods, err := kubectl.GetPodsNodes(helpers.DefaultNamespace, "")
			Expect(err).To(BeNil())

			node := pods[podName]
			ciliumPod, err := kubectl.GetCiliumPodOnNode(helpers.KubeSystemNamespace, node)
			Expect(err).Should(BeNil())

			status := kubectl.CiliumEndpointWait(ciliumPod)
			Expect(status).To(BeTrue())

			endpointIDs := kubectl.CiliumEndpointsIDs(ciliumPod)
			endpointID := endpointIDs[fmt.Sprintf("%s:%s", helpers.DefaultNamespace, podName)]
			Expect(endpointID).NotTo(BeNil())

			Eventually(func() string {
				res := kubectl.CiliumEndpointGet(ciliumPod, endpointID)

				data, err := res.Filter(`{[0].status.policy.realized.cidr-policy.egress}`)
				Expect(err).To(BeNil())
				return data.String()

			}, 5*time.Minute, 10*time.Second).Should(ContainSubstring(expectedCIDR))
		}

		deletePath := func(path string) {
			res := kubectl.Delete(path)
			res.ExpectSuccess()
		}

		It("To Services first endpoint creation", func() {
			res := kubectl.Apply(endpointPath)
			res.ExpectSuccess()

			applyPolicy(policyPath)
			defer deletePath(policyPath)

			validateEgress()
		})

		It("To Services first policy", func() {
			applyPolicy(policyPath)
			defer deletePath(policyPath)

			res := kubectl.Apply(endpointPath)
			res.ExpectSuccess()

			validateEgress()
		})

		It("To Services first endpoint creation match service by labels", func() {
			res := kubectl.Apply(endpointPath)
			res.ExpectSuccess()

			applyPolicy(policyLabeledPath)
			defer deletePath(policyLabeledPath)

			validateEgress()
		})

		It("To Services first policy, match service by labels", func() {
			applyPolicy(policyLabeledPath)
			defer deletePath(policyLabeledPath)

			res := kubectl.Apply(endpointPath)
			res.ExpectSuccess()

			validateEgress()
		})
	})

	Context("Bookinfo Demo", func() {

		var (
			bookinfoV1YAML, bookinfoV2YAML string
			resourceYAMLs                  []string
			policyPath                     string
		)

		BeforeEach(func() {

			bookinfoV1YAML = kubectl.ManifestGet("bookinfo-v1.yaml")
			bookinfoV2YAML = kubectl.ManifestGet("bookinfo-v2.yaml")
			policyPath = kubectl.ManifestGet("cnp-specs.yaml")

			resourceYAMLs = []string{bookinfoV1YAML, bookinfoV2YAML}

			for _, resourcePath := range resourceYAMLs {
				By(fmt.Sprintf("Creating objects in file %s", resourcePath))
				res := kubectl.Create(resourcePath)
				res.ExpectSuccess("unable to create resource %s: %s", resourcePath, res.CombineOutput())
			}
		})

		AfterEach(func() {

			// Explicitly do not check result to avoid having assertions in AfterEach.
			_, _ = kubectl.CiliumPolicyAction(helpers.KubeSystemNamespace, policyPath, helpers.KubectlDelete, helpers.HelperTimeout)

			for _, resourcePath := range resourceYAMLs {
				By(fmt.Sprintf("Deleting resource %s", resourcePath))
				// Explicitly do not check result to avoid having assertions in AfterEach.
				_ = kubectl.Delete(resourcePath)
			}
		})

		It("Tests bookinfo demo", func() {

			// Various constants used in this test
			wgetCommand := "%s exec -t %s wget -- --tries=2 --connect-timeout 10 %s"

			version := "version"
			v1 := "v1"
			v2 := "v2"

			productPage := "productpage"
			reviews := "reviews"
			ratings := "ratings"
			details := "details"
			dnsChecks := []string{productPage, reviews, ratings, details}
			app := "app"
			health := "health"

			apiPort := "9080"

			podNameFilter := "{.items[*].metadata.name}"

			// shouldConnect asserts that srcPod can connect to dst.
			shouldConnect := func(srcPod, dst string) {
				By(fmt.Sprintf("Checking that %s can connect to %s", srcPod, dst))
				res := kubectl.Exec(fmt.Sprintf(wgetCommand, helpers.KubectlCmd, srcPod, dst))
				res.ExpectSuccess(fmt.Sprintf("Unable to connect from %s to %s: %s", srcPod, dst, res.CombineOutput()))
			}

			// shouldNotConnect asserts that srcPod cannot connect to dst.
			shouldNotConnect := func(srcPod, dst string) {
				By(fmt.Sprintf("Checking that %s cannot connect to %s", srcPod, dst))
				res := kubectl.Exec(fmt.Sprintf(wgetCommand, helpers.KubectlCmd, srcPod, dst))
				res.ExpectFail(fmt.Sprintf("Was able to connect from %s to %s, but expected no connection: %s", srcPod, dst, res.CombineOutput()))
			}

			// formatLabelArgument formats the provided key-value pairs as labels for use in
			// querying Kubernetes.
			formatLabelArgument := func(firstKey, firstValue string, nextLabels ...string) string {
				baseString := fmt.Sprintf("-l %s=%s", firstKey, firstValue)
				if nextLabels == nil {
					return baseString
				} else if len(nextLabels)%2 != 0 {
					panic("must provide even number of arguments for label key-value pairings")
				} else {
					for i := 0; i < len(nextLabels); i += 2 {
						baseString = fmt.Sprintf("%s,%s=%s", baseString, nextLabels[i], nextLabels[i+1])
					}
				}
				return baseString
			}

			// formatAPI is a helper function which formats a URI to access.
			formatAPI := func(service, port, resource string) string {
				target := fmt.Sprintf(
					"%s.%s.svc.cluster.local:%s",
					service, helpers.DefaultNamespace, port)
				if resource != "" {
					return fmt.Sprintf("%s/%s", target, resource)
				}
				return target
			}

			By(fmt.Sprintf("Getting Cilium Pod on node %s", helpers.K8s1))
			ciliumPodK8s1, err := kubectl.GetCiliumPodOnNode(helpers.KubeSystemNamespace, helpers.K8s1)
			Expect(err).Should(BeNil())

			By(fmt.Sprintf("Getting Cilium Pod on node %s", helpers.K8s2))
			_, err = kubectl.GetCiliumPodOnNode(helpers.KubeSystemNamespace, helpers.K8s2)

			Expect(err).Should(BeNil())

			By("Waiting for v1 pods to be ready")
			pods, err := kubectl.WaitforPods(helpers.DefaultNamespace, formatLabelArgument(version, v1), helpers.HelperTimeout)
			Expect(pods).Should(BeTrue())
			Expect(err).Should(BeNil())

			By("Waiting for v2 pods to be ready")
			pods, err = kubectl.WaitforPods(helpers.DefaultNamespace, formatLabelArgument(version, v2), helpers.HelperTimeout)
			Expect(pods).Should(BeTrue())
			Expect(err).Should(BeNil())

			By("Getting reviews v1 pod")
			reviewsPodV1, err := kubectl.GetPods(helpers.DefaultNamespace, formatLabelArgument(app, reviews, version, v1)).Filter(podNameFilter)
			Expect(err).Should(BeNil())

			By("Getting productpage v1 pod")
			productpagePodV1, err := kubectl.GetPods(helpers.DefaultNamespace, formatLabelArgument(app, productPage, version, v1)).Filter(podNameFilter)
			Expect(err).Should(BeNil())

			By("Waiting for endpoints to be ready in Cilium")
			areEndpointsReady := kubectl.CiliumEndpointWait(ciliumPodK8s1)
			Expect(areEndpointsReady).Should(BeTrue())

			By("Waiting for details service endpoints to be ready")
			pods, err = kubectl.WaitForServiceEndpoints(helpers.DefaultNamespace, "", details, apiPort, helpers.HelperTimeout)
			Expect(pods).Should(BeTrue())
			Expect(err).Should(BeNil())

			By("Waiting for ratings service endpoints to be ready")
			pods, err = kubectl.WaitForServiceEndpoints(helpers.DefaultNamespace, "", ratings, apiPort, helpers.HelperTimeout)
			Expect(pods).Should(BeTrue())
			Expect(err).Should(BeNil())

			By("Waiting for reviews service endpoints to be ready")
			pods, err = kubectl.WaitForServiceEndpoints(helpers.DefaultNamespace, "", reviews, apiPort, helpers.HelperTimeout)
			Expect(pods).Should(BeTrue())
			Expect(err).Should(BeNil())

			By("Waiting for productpage service endpoints to be ready")
			pods, err = kubectl.WaitForServiceEndpoints(helpers.DefaultNamespace, "", productPage, apiPort, helpers.HelperTimeout)
			Expect(pods).Should(BeTrue())
			Expect(err).Should(BeNil())

			By("Validating DNS")
			for _, name := range dnsChecks {
				err = kubectl.WaitForKubeDNSEntry(fmt.Sprintf("%s.%s", name, helpers.DefaultNamespace))
				Expect(err).To(BeNil(), "DNS entry is not ready after timeout")
			}

			By("Before policy import; all pods should be able to connect")
			shouldConnect(reviewsPodV1.String(), formatAPI(ratings, apiPort, health))
			shouldConnect(reviewsPodV1.String(), formatAPI(ratings, apiPort, ""))

			shouldConnect(productpagePodV1.String(), formatAPI(details, apiPort, health))
			shouldConnect(productpagePodV1.String(), formatAPI(details, apiPort, ""))
			shouldConnect(productpagePodV1.String(), formatAPI(ratings, apiPort, health))
			shouldConnect(productpagePodV1.String(), formatAPI(ratings, apiPort, ""))

			policyCmd := "cilium policy get io.cilium.k8s.policy.name=multi-rules"

			By("Importing policy")

			_, err = kubectl.CiliumPolicyAction(helpers.KubeSystemNamespace, policyPath, helpers.KubectlCreate, helpers.HelperTimeout)
			Expect(err).Should(BeNil(), fmt.Sprintf("Error creating resource %s: %s", policyPath, err))

			By("Checking that policies were correctly imported into Cilium")
			res := kubectl.ExecPodCmd(helpers.KubeSystemNamespace, ciliumPodK8s1, policyCmd)
			res.ExpectSuccess("Policy %s is not imported", policyCmd)

			By("Validating DNS")
			for _, name := range dnsChecks {
				err = kubectl.WaitForKubeDNSEntry(fmt.Sprintf("%s.%s", name, helpers.DefaultNamespace))
				Expect(err).To(BeNil(), "DNS entry is not ready after timeout")
			}

			By("After policy import")
			shouldConnect(reviewsPodV1.String(), formatAPI(ratings, apiPort, health))
			shouldNotConnect(reviewsPodV1.String(), formatAPI(ratings, apiPort, ""))

			shouldConnect(productpagePodV1.String(), formatAPI(details, apiPort, health))
			shouldConnect(productpagePodV1.String(), formatAPI(details, apiPort, ""))

			shouldNotConnect(productpagePodV1.String(), formatAPI(ratings, apiPort, health))
			shouldNotConnect(productpagePodV1.String(), formatAPI(ratings, apiPort, ""))
		})
	})
})
