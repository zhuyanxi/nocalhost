/*
 * Tencent is pleased to support the open source community by making Nocalhost available.,
 * Copyright (C) 2019 THL A29 Limited, a Tencent company. All rights reserved.
 * Licensed under the MIT License (the "License"); you may not use this file except
 * in compliance with the License. You may obtain a copy of the License at
 * http://opensource.org/licenses/MIT
 * Unless required by applicable law or agreed to in writing, software distributed under,
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions and
 * limitations under the License.
 */

package daemon_handler

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"nocalhost/internal/nhctl/app"
	"nocalhost/internal/nhctl/appmeta_manager"
	"nocalhost/internal/nhctl/daemon_server/command"
	"nocalhost/internal/nhctl/nocalhost"
	"nocalhost/internal/nhctl/profile"
	"nocalhost/internal/nhctl/resouce_cache"
	"nocalhost/pkg/nhctl/log"
	"sort"
)

func getServiceProfile(ns, appName string) map[string]*profile.SvcProfileV2 {
	serviceMap := make(map[string]*profile.SvcProfileV2)
	profileV2, err := nocalhost.GetProfileV2(ns, appName)
	if err != nil {
		log.Error(err)
	}
	if profileV2 != nil {
		nocalhostApp, err2 := app.NewApplication(appName, ns, profileV2.Kubeconfig, true)
		if err2 != nil {
			log.Error(err2)
		}
		if nocalhostApp != nil {
			description := nocalhostApp.GetDescription()
			if description != nil {
				for _, svcProfileV2 := range description.SvcProfile {
					if svcProfileV2 != nil {
						serviceMap[svcProfileV2.Name] = svcProfileV2
					}
				}
			}
		}
	}
	return serviceMap
}

func HandleGetResourceInfoRequest(request *command.GetResourceInfoCommand) interface{} {
	var s *resouce_cache.Search
	var err error
	var ns string
	if request.Namespace == "" {
		config, err := clientcmd.NewClientConfigFromBytes([]byte(request.KubeConfig))
		if err != nil && config != nil {
			ns, _, _ = config.Namespace()
		}
		s, err = resouce_cache.GetSearch(request.KubeConfig, ns)
	} else {
		s, err = resouce_cache.GetSearch(request.KubeConfig, request.Namespace)
	}
	if err != nil {
		return nil
	}

	switch request.Resource {
	case "all":
		// means it's cluster kubeconfig
		if request.Namespace == "" {
			nsObjectList, err := s.GetAllByResourceType("namespaces")
			if err == nil && nsObjectList != nil && len(nsObjectList) > 0 {
				result := make([]Result, 0, len(nsObjectList))
				for _, nsObject := range nsObjectList {
					result = append(result, getApplicationByNs(nsObject.(metav1.Object).GetName(), request.KubeConfig, s))
				}
				return result
			} else {
				// default namespace
				request.Namespace = ns
			}
		}
		return getApplicationByNs(request.Namespace, request.KubeConfig, s)
	case "app", "application":
		if request.ResourceName == "" {
			metas := appmeta_manager.GetApplicationMetas(request.Namespace, request.KubeConfig)
			if metas != nil {
				sort.SliceStable(metas, func(i, j int) bool {
					var n1, n2 string
					if metas[i] != nil {
						n1 = metas[i].Application
					}
					if metas[j] != nil {
						n2 = metas[j].Application
					}
					if n1 > n2 {
						return false
					}
					return true
				})
			}
			return metas
		} else {
			return appmeta_manager.GetApplicationMeta(request.Namespace, request.ResourceName, request.KubeConfig)
		}
	default:
		serviceMap := getServiceProfile(request.Namespace, request.ResourceName)
		// get all resource in namespace
		var items []interface{}
		var err error
		if request.ResourceName == "" {
			if request.AppName == "" {
				items, err = s.GetByResourceAndNamespace(request.Resource, "", request.Namespace)
			} else {
				items, err = s.GetByResourceAndNameAndAppAndNamespace(request.Resource, "", request.AppName, request.Namespace)
			}
			if err != nil || len(items) == 0 {
				return nil
			}
			resouce_cache.SortByCreateTimestampAsc(items)
			result := make([]Item, 0, len(items))
			for _, i := range items {
				result = append(result, Item{Metadata: i, Description: serviceMap[i.(metav1.Object).GetName()]})
			}
			return result
		} else {
			// get specify resource name in namespace
			if request.AppName == "" {
				items, err = s.GetByResourceAndNamespace(request.Resource, request.ResourceName, request.Namespace)
			} else {
				items, err = s.GetByResourceAndNameAndAppAndNamespace(request.Resource, request.ResourceName, request.AppName, request.Namespace)
			}
			if err != nil || len(items) == 0 {
				return nil
			}
			return Item{Metadata: items[0], Description: serviceMap[items[0].(metav1.Object).GetName()]}
		}
	}
}

func getApplicationByNs(ns, kubeconfig string, search *resouce_cache.Search) Result {
	result := Result{Namespace: ns}
	applicationMetaList := appmeta_manager.GetApplicationMetas(ns, kubeconfig)
	for _, applicationMeta := range applicationMetaList {
		if applicationMeta != nil {
			result.Application = append(result.Application, getApp(ns, applicationMeta.Application, search))
		}
	}
	return result
}

func getApp(namespace, appName string, search *resouce_cache.Search) App {
	groupToTypeMap := map[string][]string{
		"Workloads":      {"deployments", "statefulsets", "daemonsets", "jobs", "cronjobs", "pods"},
		"Networks":       {"services", "endpoints", "ingresses", "networkpolicies"},
		"Configurations": {"configmaps", "secrets", "horizontalpodautoscalers", "resourcequotas", "poddisruptionbudgets"},
		"Storages":       {"persistentvolumes", "persistentvolumeclaims", "storageclasses"},
	}
	result := App{Name: appName}
	profileMap := getServiceProfile(namespace, appName)
	for groupName, types := range groupToTypeMap {
		resources := make([]Resource, 0, len(types))
		for _, resource := range types {
			resourceList, err := search.GetByResourceAndNamespace(resource, "", namespace)
			if err == nil {
				items := make([]Item, 0, len(resourceList))
				for _, v := range resourceList {
					items = append(items, Item{Metadata: v, Description: profileMap[v.(metav1.Object).GetName()]})
				}
				resources = append(resources, Resource{Name: resource, List: items})
			}
		}
		result.Groups = append(result.Groups, Group{GroupName: groupName, List: resources})
	}
	return result
}

type Result struct {
	Namespace   string `json:"namespace"`
	Application []App  `json:"application"`
}

type App struct {
	Name   string  `json:"name"`
	Groups []Group `json:"group"`
}

type Group struct {
	GroupName string     `json:"type"`
	List      []Resource `json:"resource"`
}

type Resource struct {
	Name string `json:"name"`
	List []Item `json:"list"`
}

type Item struct {
	Metadata    interface{} `json:"data,omitempty"`
	Description interface{} `json:"description,omitempty"`
}